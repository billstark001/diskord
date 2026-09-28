package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"diskord/internal/model"
	"diskord/internal/scope"
	"diskord/internal/securefs"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Store struct {
	writer, reader *sql.DB
	Root           string
	assetBytes     int64
	assetMu        sync.Mutex
}

func Open(root string) (*Store, error) {
	path, e := securefs.Within(root, "db/main.sqlite")
	if e != nil {
		return nil, e
	}
	if _, e = os.Stat(path); os.IsNotExist(e) {
		f, e := securefs.NewFile(path)
		if e != nil {
			return nil, e
		}
		f.Close()
	} else if e != nil {
		return nil, e
	}
	if e = securefs.Private(path); e != nil {
		return nil, e
	}
	p := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	q := u.Query()
	for _, v := range []string{"busy_timeout(5000)", "foreign_keys(1)", "temp_store(MEMORY)", "synchronous(FULL)"} {
		q.Add("_pragma", v)
	}
	u.RawQuery = q.Encode()
	w, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	w.SetMaxOpenConns(1)
	fail := func(e error) (*Store, error) { w.Close(); return nil, e }
	if _, e = w.Exec("PRAGMA journal_mode=WAL"); e != nil {
		return fail(e)
	}
	if _, e = w.Exec(schema); e != nil {
		return fail(e)
	}
	var version int
	if e = w.QueryRow("SELECT version FROM schema_version").Scan(&version); e != nil {
		return fail(e)
	}
	if version != 2 {
		return fail(errors.New("unsupported database schema version"))
	}
	r, e := sql.Open("sqlite", u.String())
	if e != nil {
		return fail(e)
	}
	r.SetMaxOpenConns(4)
	s := &Store{writer: w, reader: r, Root: root}
	entries, e := os.ReadDir(filepath.Join(root, "resources"))
	if e != nil {
		r.Close()
		return fail(e)
	}
	for _, item := range entries {
		if item.Type()&os.ModeSymlink != 0 {
			r.Close()
			return fail(errors.New("symlink in resource directory"))
		}
		if !item.IsDir() {
			st, e := item.Info()
			if e != nil {
				r.Close()
				return fail(e)
			}
			s.assetBytes += st.Size()
		}
	}
	return s, nil
}
func (s *Store) Close() error {
	re := s.reader.Close()
	_, ce := s.writer.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	we := s.writer.Close()
	return errors.Join(re, ce, we)
}
func stub(tx *sql.Tx, table, id string) error {
	if !model.ID(id) {
		return nil
	}
	_, e := tx.Exec("INSERT OR IGNORE INTO "+table+"(id) VALUES(?)", id)
	return e
}
func set[T any](m map[string]any, key string, p *T) {
	if p != nil {
		m[key] = *p
	}
}
func merge(tx *sql.Tx, table, id string, values map[string]any) error {
	if !model.ID(id) {
		return nil
	}
	if e := stub(tx, table, id); e != nil {
		return e
	}
	values["observed_at"] = time.Now().UnixMilli()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]any, 0, len(keys)+1)
	assign := make([]string, 0, len(keys))
	for _, k := range keys {
		assign = append(assign, k+"=?")
		args = append(args, values[k])
	}
	args = append(args, id)
	_, e := tx.Exec("UPDATE "+table+" SET "+strings.Join(assign, ",")+" WHERE id=?", args...)
	return e
}
func (s *Store) Apply(ctx context.Context, b model.Batch) error {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, u := range b.Users {
		v := map[string]any{}
		set(v, "username", u.Username)
		set(v, "display_name", u.DisplayName)
		set(v, "avatar", u.Avatar)
		if e = merge(tx, "users", u.ID, v); e != nil {
			return e
		}
	}
	for _, g := range b.Guilds {
		v := map[string]any{}
		set(v, "name", g.Name)
		set(v, "unavailable", g.Unavailable)
		set(v, "deleted", g.Deleted)
		if e = merge(tx, "guilds", g.ID, v); e != nil {
			return e
		}
	}
	for _, c := range b.Channels {
		if !model.ID(c.ID) {
			continue
		}
		v := map[string]any{}
		if c.GuildID != nil {
			if e = stub(tx, "guilds", *c.GuildID); e != nil {
				return e
			}
			set(v, "guild_id", c.GuildID)
		}
		if c.ParentID != nil {
			if e = stub(tx, "channels", *c.ParentID); e != nil {
				return e
			}
			set(v, "parent_id", c.ParentID)
		}
		set(v, "name", c.Name)
		set(v, "kind", c.Type)
		set(v, "deleted", c.Deleted)
		if e = merge(tx, "channels", c.ID, v); e != nil {
			return e
		}
		if c.Recipients != nil {
			if _, e = tx.Exec("DELETE FROM channel_recipients WHERE channel_id=?", c.ID); e != nil {
				return e
			}
			for _, id := range c.Recipients {
				if !model.ID(id) {
					continue
				}
				if e = stub(tx, "users", id); e != nil {
					return e
				}
				if _, e = tx.Exec("INSERT OR IGNORE INTO channel_recipients(channel_id,user_id) VALUES(?,?)", c.ID, id); e != nil {
					return e
				}
			}
		}
	}
	for _, m := range b.Members {
		if !model.ID(m.GuildID) || !model.ID(m.UserID) {
			continue
		}
		if e = stub(tx, "guilds", m.GuildID); e != nil {
			return e
		}
		if e = stub(tx, "users", m.UserID); e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT OR IGNORE INTO guild_members(guild_id,user_id) VALUES(?,?)", m.GuildID, m.UserID); e != nil {
			return e
		}
	}
	for _, m := range b.Messages {
		if !model.ID(m.ID) {
			continue
		}
		if m.ChannelID != nil {
			if e = stub(tx, "channels", *m.ChannelID); e != nil {
				return e
			}
		}
		if m.AuthorID != nil {
			if e = stub(tx, "users", *m.AuthorID); e != nil {
				return e
			}
		}
		if _, e = tx.Exec("INSERT OR IGNORE INTO messages(id,sort_key,source) VALUES(?,?,?)", m.ID, model.SortKey(m.ID), b.Source); e != nil {
			return e
		}
		var revision int64
		if e = tx.QueryRow("SELECT revision FROM messages WHERE id=?", m.ID).Scan(&revision); e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE messages SET channel_id=COALESCE(channel_id,?),author_id=COALESCE(author_id,?),observed_at=? WHERE id=?", m.ChannelID, m.AuthorID, time.Now().UnixMilli(), m.ID); e != nil {
			return e
		}
		if m.Deleted != nil && *m.Deleted {
			if _, e = tx.Exec("UPDATE messages SET deleted=1 WHERE id=?", m.ID); e != nil {
				return e
			}
		}
		if m.Revision >= revision {
			v := map[string]any{"revision": m.Revision, "source": b.Source}
			set(v, "content", m.Content)
			set(v, "created_at", m.Timestamp)
			set(v, "edited_at", m.EditedTimestamp)
			set(v, "kind", m.Type)
			// The message and its required sort key already exist; apply only present fields.
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			assign := []string{}
			args := []any{}
			for _, k := range keys {
				assign = append(assign, k+"=?")
				args = append(args, v[k])
			}
			args = append(args, m.ID)
			if _, e = tx.Exec("UPDATE messages SET "+strings.Join(assign, ",")+" WHERE id=?", args...); e != nil {
				return e
			}
		}
		if m.HasAttachments && (m.Revision >= revision || m.Revision == 0) {
			if _, e = tx.Exec("DELETE FROM attachments WHERE message_id=?", m.ID); e != nil {
				return e
			}
			for _, a := range m.Attachments {
				if _, e = tx.Exec("INSERT INTO attachments(id,message_id,name,host,path,size) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,host=excluded.host,path=excluded.path,size=excluded.size", a.ID, m.ID, a.Name, a.Host, a.Path, a.Size); e != nil {
					return e
				}
			}
		}
	}
	if e = applyReactions(tx, b); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Counts(ctx context.Context) (model.Counts, error) {
	var c model.Counts
	e := s.reader.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM guilds),(SELECT count(*) FROM channels),(SELECT count(*) FROM messages)").Scan(&c.Users, &c.Guilds, &c.Channels, &c.Messages)
	return c, e
}
func (s *Store) Guilds(ctx context.Context) ([]model.GuildRow, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT id, COALESCE(name,''), unavailable, deleted FROM guilds ORDER BY lower(COALESCE(name,id)), id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.GuildRow{}
	for rows.Next() {
		var item model.GuildRow
		if err := rows.Scan(&item.ID, &item.Name, &item.Unavailable, &item.Deleted); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Store) Channels(ctx context.Context, guildID string) ([]model.ChannelRow, error) {
	if guildID != "" && !model.ID(guildID) {
		return nil, errors.New("invalid guild ID")
	}
	query := `SELECT id, COALESCE(guild_id,''), COALESCE(parent_id,''), COALESCE(name,''), COALESCE(kind,-1), deleted FROM channels WHERE guild_id IS NULL ORDER BY lower(COALESCE(name,id)), id`
	args := []any{}
	if guildID != "" {
		query = `SELECT id, COALESCE(guild_id,''), COALESCE(parent_id,''), COALESCE(name,''), COALESCE(kind,-1), deleted FROM channels WHERE guild_id=? ORDER BY lower(COALESCE(name,id)), id`
		args = append(args, guildID)
	}
	rows, err := s.reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.ChannelRow{}
	for rows.Next() {
		var item model.ChannelRow
		if err := rows.Scan(&item.ID, &item.GuildID, &item.ParentID, &item.Name, &item.Kind, &item.Deleted); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Store) Messages(ctx context.Context, f model.Filter) ([]model.MessageRow, error) {
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 50
	}
	where := []string{"1=1"}
	args := []any{}
	if f.Query != "" {
		where = append(where, "m.content LIKE ? ESCAPE '\\'")
		v := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(f.Query)
		args = append(args, "%"+v+"%")
	}
	if model.ID(f.ChannelID) {
		where = append(where, "m.channel_id=?")
		args = append(args, f.ChannelID)
	}
	if model.ID(f.GuildID) {
		where = append(where, "c.guild_id=?")
		args = append(args, f.GuildID)
	} else if f.Scope == "dm" {
		where = append(where, "c.guild_id IS NULL")
	}
	if model.ID(f.Before) {
		where = append(where, "m.sort_key<?")
		args = append(args, model.SortKey(f.Before))
	}
	args = append(args, f.Limit)
	query := `SELECT m.id,COALESCE(m.channel_id,''),COALESCE(c.name,c.id,''),COALESCE(g.name,c.guild_id,''),COALESCE(u.display_name,u.username,u.id,''),COALESCE(m.content,''),COALESCE(m.created_at,''),COALESCE(m.edited_at,''),m.source,m.deleted,COALESCE((SELECT ru.hash FROM resource_urls ru WHERE ru.host='cdn.discordapp.com' AND substr(ru.path,1,length('/avatars/'||u.id||'/'||u.avatar||'.'))='/avatars/'||u.id||'/'||u.avatar||'.' LIMIT 1),'') FROM messages m LEFT JOIN channels c ON c.id=m.channel_id LEFT JOIN guilds g ON g.id=c.guild_id LEFT JOIN users u ON u.id=m.author_id WHERE ` + strings.Join(where, " AND ") + ` ORDER BY m.sort_key DESC LIMIT ?`
	rows, e := s.reader.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	result := []model.MessageRow{}
	for rows.Next() {
		var m model.MessageRow
		if e = rows.Scan(&m.ID, &m.ChannelID, &m.ChannelName, &m.GuildName, &m.AuthorName, &m.Content, &m.Timestamp, &m.EditedTimestamp, &m.Source, &m.Deleted, &m.AvatarHash); e != nil {
			rows.Close()
			return nil, e
		}
		result = append(result, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for i := range result {
		a, e := s.reader.QueryContext(ctx, `SELECT a.name,COALESCE(r.hash,'') FROM attachments a LEFT JOIN resource_urls r ON r.host=a.host AND r.path=a.path WHERE a.message_id=? ORDER BY a.id`, result[i].ID)
		if e != nil {
			return nil, e
		}
		for a.Next() {
			var item model.AssetRow
			if e = a.Scan(&item.Name, &item.Hash); e != nil {
				a.Close()
				return nil, e
			}
			result[i].Attachments = append(result[i].Attachments, item)
		}
		e = a.Err()
		a.Close()
		if e != nil {
			return nil, e
		}
		result[i].Reactions, e = s.messageReactions(ctx, result[i].ID)
		if e != nil {
			return nil, e
		}
	}
	return result, nil
}

var ErrQuota = errors.New("resource cache quota reached")

func (s *Store) SaveAsset(ctx context.Context, host, path, mime string, data []byte, quota int64) error {
	s.assetMu.Lock()
	defer s.assetMu.Unlock()
	ext := scope.ImageExtension(mime)
	if !scope.CDN(host) || ext == "" {
		return errors.New("resource outside image allowlist")
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	dest, e := securefs.Within(s.Root, "resources/"+hash+ext)
	if e != nil {
		return e
	}
	if _, e = os.Stat(dest); os.IsNotExist(e) {
		if s.assetBytes+int64(len(data)) > quota {
			return ErrQuota
		}
		if e = securefs.AtomicWrite(s.Root, dest, data); e != nil {
			return e
		}
		s.assetBytes += int64(len(data))
	} else if e != nil {
		return e
	}
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT OR IGNORE INTO assets(hash,extension,mime,size,observed_at) VALUES(?,?,?,?,?)", hash, ext, strings.SplitN(mime, ";", 2)[0], len(data), time.Now().UnixMilli()); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO resource_urls(host,path,hash) VALUES(?,?,?) ON CONFLICT(host,path) DO UPDATE SET hash=excluded.hash", host, path, hash); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Asset(ctx context.Context, hash string) (path, mime string, err error) {
	if len(hash) != 64 {
		return "", "", os.ErrNotExist
	}
	if _, e := hex.DecodeString(hash); e != nil {
		return "", "", os.ErrNotExist
	}
	var ext string
	if e := s.reader.QueryRowContext(ctx, "SELECT extension,mime FROM assets WHERE hash=?", hash).Scan(&ext, &mime); e != nil {
		return "", "", e
	}
	if scope.ImageExtension(mime) != ext {
		return "", "", fmt.Errorf("invalid asset metadata")
	}
	path, err = securefs.Within(s.Root, "resources/"+hash+ext)
	return
}
