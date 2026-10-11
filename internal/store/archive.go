package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"diskord/internal/model"
)

// Selection uses creation time, not last observation or last edit time.
type Selection struct {
	IncludeThreads bool       `json:"include_threads,omitempty"`
	GuildID        string     `json:"guild_id,omitempty"`
	Scope          string     `json:"scope,omitempty"`
	ThreadID       string     `json:"thread_id,omitempty"`
	Since          *time.Time `json:"since,omitempty"`
	Until          *time.Time `json:"until,omitempty"`
}

func ParseSelection(thread, since, until string) (Selection, error) {
	return ParseArchiveSelection(thread, "", "", since, until)
}

func ParseArchiveSelection(thread, guild, scope, since, until string) (Selection, error) {
	s := Selection{ThreadID: thread, GuildID: guild, Scope: scope}
	if guild != "" && !model.ID(guild) || scope != "" && scope != "dm" || guild != "" && scope != "" {
		return s, errors.New("invalid guild or scope filter")
	}
	if thread != "" && !model.ID(thread) {
		return s, errors.New("thread must be a decimal channel ID")
	}
	for _, value := range []struct {
		name, raw string
		target    **time.Time
	}{{"since", since, &s.Since}, {"until", until, &s.Until}} {
		if value.raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, value.raw)
		if err != nil {
			return s, fmt.Errorf("%s must be RFC3339 with a timezone", value.name)
		}
		t = t.UTC()
		*value.target = &t
	}
	if s.Since != nil && s.Until != nil && !s.Since.Before(*s.Until) {
		return s, errors.New("since must be earlier than until")
	}
	return s, nil
}

type ArchiveAttachment struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Host       string `json:"host"`
	Path       string `json:"path"`
	CachedHash string `json:"cached_hash,omitempty"`
	ExportPath string `json:"export_path,omitempty"`
}

type ArchiveReaction struct {
	EmojiID      string   `json:"emoji_id,omitempty"`
	EmojiName    string   `json:"emoji_name"`
	Animated     bool     `json:"animated"`
	Count        int      `json:"count"`
	KnownUsers   []string `json:"known_user_names"`
	UnknownCount int      `json:"unknown_count"`
}

type ArchiveMessage struct {
	ID           string              `json:"id"`
	ChannelID    string              `json:"channel_id"`
	ChannelName  string              `json:"channel_name"`
	GuildID      string              `json:"guild_id"`
	AuthorID     string              `json:"author_id"`
	AuthorName   string              `json:"author_name"`
	Content      *string             `json:"content"`
	CreatedAt    *string             `json:"created_at"`
	CreationTime *time.Time          `json:"creation_time"`
	TimeSource   string              `json:"time_source"`
	EditedAt     *string             `json:"edited_at"`
	Kind         *int                `json:"kind"`
	Deleted      bool                `json:"deleted"`
	Revision     int64               `json:"revision"`
	ObservedAtMS int64               `json:"observed_at_ms"`
	Source       string              `json:"source"`
	Attachments  []ArchiveAttachment `json:"attachments"`
	Reactions    []ArchiveReaction   `json:"reactions"`
	MentionNames map[string]string   `json:"mention_names"`
}

type ArchivePage struct {
	SchemaVersion int              `json:"schema_version"`
	Selection     Selection        `json:"selection"`
	Messages      []ArchiveMessage `json:"messages"`
	NextAfter     string           `json:"next_after,omitempty"`
}

type ArchiveReader struct {
	store *Store
	tx    *sql.Tx
}

// BeginArchive gives all queries, enrichment and export pages one SQLite
// snapshot. Callers must close it before sending a staged export over HTTP.
func (s *Store) BeginArchive(ctx context.Context) (*ArchiveReader, error) {
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return &ArchiveReader{tx: tx, store: &Store{Root: s.Root, snapshot: tx, schemaVersion: s.DatabaseSchema()}}, nil
}

func (r *ArchiveReader) Close() error { return r.tx.Rollback() }

func creationTime(m *ArchiveMessage) {
	if m.CreatedAt != nil {
		if t, err := time.Parse(time.RFC3339Nano, *m.CreatedAt); err == nil {
			t = t.UTC()
			m.CreationTime, m.TimeSource = &t, "timestamp"
			return
		}
	}
	if id, err := strconv.ParseUint(m.ID, 10, 64); err == nil {
		t := time.UnixMilli(int64(id>>22) + 1420070400000).UTC()
		m.CreationTime, m.TimeSource = &t, "snowflake"
		return
	}
	m.TimeSource = "unknown"
}

func (s Selection) matches(m ArchiveMessage) bool {
	if s.Since == nil && s.Until == nil {
		return true
	}
	if m.CreationTime == nil {
		return false
	}
	return (s.Since == nil || !m.CreationTime.Before(*s.Since)) && (s.Until == nil || m.CreationTime.Before(*s.Until))
}

func (r *ArchiveReader) Page(ctx context.Context, selection Selection, after string, limit int) (ArchivePage, error) {
	page := ArchivePage{SchemaVersion: 1, Selection: selection, Messages: []ArchiveMessage{}}
	if limit < 1 || limit > 1000 || after != "" && !model.ID(after) {
		return page, errors.New("limit must be 1..1000 and after must be a decimal message ID")
	}
	// Timestamp filtering happens in Go: SQLite date functions round fractions,
	// and the stored timestamps can have different offsets and precisions.
	for {
		where := []string{"m.sort_key>?"}
		args := []any{model.SortKey(after)}
		if selection.ThreadID != "" {
			if selection.IncludeThreads {
				where = append(where, `m.channel_id IN (WITH RECURSIVE subtree(id) AS (SELECT ? UNION SELECT c.id FROM channels c JOIN subtree ON c.parent_id=subtree.id) SELECT id FROM subtree)`)
			} else {
				where = append(where, "m.channel_id=?")
			}
			args = append(args, selection.ThreadID)
		}
		if selection.GuildID != "" {
			where = append(where, "c.guild_id=?")
			args = append(args, selection.GuildID)
		}
		if selection.Scope == "dm" {
			where = append(where, "c.guild_id IS NULL AND c.kind IN (1,3)")
		}
		rows, err := r.tx.QueryContext(ctx, `SELECT m.id,COALESCE(m.channel_id,''),`+channelDisplayNameSQL+`,
			COALESCE(c.guild_id,''),COALESCE(m.author_id,''),`+userDisplayNameSQL+`,
			m.content,m.created_at,m.edited_at,m.kind,m.deleted,m.revision,m.observed_at,m.source
			FROM messages m LEFT JOIN channels c ON c.id=m.channel_id LEFT JOIN users u ON u.id=m.author_id
			WHERE `+strings.Join(where, " AND ")+` ORDER BY m.sort_key LIMIT 256`, args...)
		if err != nil {
			return page, err
		}
		batch := []ArchiveMessage{}
		for rows.Next() {
			var m ArchiveMessage
			if err := rows.Scan(&m.ID, &m.ChannelID, &m.ChannelName, &m.GuildID, &m.AuthorID, &m.AuthorName,
				&m.Content, &m.CreatedAt, &m.EditedAt, &m.Kind, &m.Deleted, &m.Revision, &m.ObservedAtMS, &m.Source); err != nil {
				rows.Close()
				return page, err
			}
			batch = append(batch, m)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return page, err
		}
		for _, m := range batch {
			after = m.ID
			creationTime(&m)
			if !selection.matches(m) {
				continue
			}
			if len(page.Messages) == limit {
				page.NextAfter = page.Messages[len(page.Messages)-1].ID
				return page, nil
			}
			if err := r.enrich(ctx, &m); err != nil {
				return page, err
			}
			page.Messages = append(page.Messages, m)
		}
		if len(batch) < 256 {
			return page, nil
		}
	}
}

func (r *ArchiveReader) enrich(ctx context.Context, m *ArchiveMessage) error {
	m.Attachments = []ArchiveAttachment{}
	rows, err := r.tx.QueryContext(ctx, `SELECT a.id,a.name,a.size,a.host,a.path,`+attachmentHashSQL+`
		FROM attachments a WHERE a.message_id=? ORDER BY a.id`, m.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a ArchiveAttachment
		if err := rows.Scan(&a.ID, &a.Name, &a.Size, &a.Host, &a.Path, &a.CachedHash); err != nil {
			rows.Close()
			return err
		}
		m.Attachments = append(m.Attachments, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	reactions, err := r.store.messageReactions(ctx, m.ID)
	if err != nil {
		return err
	}
	m.Reactions = []ArchiveReaction{}
	for _, v := range reactions {
		users := v.Users
		if users == nil {
			users = []string{}
		}
		m.Reactions = append(m.Reactions, ArchiveReaction{v.EmojiID, v.EmojiName, v.Animated, v.Count, users, v.UnknownCount})
	}
	inline := model.MessageRow{}
	if m.Content != nil {
		inline.Content = *m.Content
	}
	if err := r.store.enrichInline(ctx, &inline); err != nil {
		return err
	}
	m.MentionNames = inline.MentionNames
	return nil
}

func (r *ArchiveReader) Asset(ctx context.Context, hash string) (string, string, error) {
	return r.store.Asset(ctx, hash)
}
