package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"strings"

	"diskord/internal/model"
)

var messageToken = regexp.MustCompile(`<@!?([0-9]{1,20})>|<@&([0-9]{1,20})>|<#([0-9]{1,20})>|<a?:[A-Za-z0-9_]{1,128}:([0-9]{1,20})>`)

func messageLimit(limit int) int {
	if limit < 1 || limit > 100 {
		return 50
	}
	return limit
}

// Messages preserves the old newest-first store API for callers that do not
// use cursors. The console uses the chronological page methods below.
func (s *Store) Messages(ctx context.Context, f model.Filter) ([]model.MessageRow, error) {
	page, err := s.ListBefore(ctx, f)
	if err != nil {
		return nil, err
	}
	slices.Reverse(page.Rows)
	return page.Rows, nil
}

func (s *Store) ListBefore(ctx context.Context, f model.Filter) (model.MessagePage, error) {
	limit := messageLimit(f.Limit)
	rows, err := s.queryMessages(ctx, f, "<", f.Before, true, limit+1)
	if err != nil {
		return model.MessagePage{}, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	slices.Reverse(rows)
	page := model.MessagePage{Rows: rows}
	if more && len(rows) > 0 {
		page.Before = rows[0].ID
	}
	if f.Before != "" && len(rows) > 0 {
		page.After = rows[len(rows)-1].ID
	}
	return page, nil
}

func (s *Store) ListAfter(ctx context.Context, f model.Filter) (model.MessagePage, error) {
	if !model.ID(f.After) {
		return model.MessagePage{}, errors.New("invalid after cursor")
	}
	limit := messageLimit(f.Limit)
	rows, err := s.queryMessages(ctx, f, ">", f.After, false, limit+1)
	if err != nil {
		return model.MessagePage{}, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	page := model.MessagePage{Rows: rows}
	if more && len(rows) > 0 {
		page.After = rows[len(rows)-1].ID
	}
	if len(rows) > 0 {
		page.Before = rows[0].ID
	}
	return page, nil
}

func (s *Store) ListAround(ctx context.Context, f model.Filter) (model.MessagePage, error) {
	if !model.ID(f.Around) {
		return model.MessagePage{}, errors.New("invalid around cursor")
	}
	limit := messageLimit(f.Limit)
	olderLimit := max(1, limit/2)
	newerLimit := limit - olderLimit
	older, err := s.queryMessages(ctx, f, "<=", f.Around, true, olderLimit+1)
	if err != nil {
		return model.MessagePage{}, err
	}
	newer, err := s.queryMessages(ctx, f, ">", f.Around, false, newerLimit+1)
	if err != nil {
		return model.MessagePage{}, err
	}
	moreBefore, moreAfter := len(older) > olderLimit, len(newer) > newerLimit
	if moreBefore {
		older = older[:olderLimit]
	}
	if moreAfter {
		newer = newer[:newerLimit]
	}
	slices.Reverse(older)
	page := model.MessagePage{Rows: append(older, newer...)}
	if moreBefore && len(page.Rows) > 0 {
		page.Before = page.Rows[0].ID
	}
	if moreAfter && len(page.Rows) > 0 {
		page.After = page.Rows[len(page.Rows)-1].ID
	}
	return page, nil
}

func (s *Store) queryMessages(ctx context.Context, f model.Filter, comparison, cursor string, descending bool, limit int) ([]model.MessageRow, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.Query != "" {
		where = append(where, "m.content LIKE ? ESCAPE '\\'")
		value := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(f.Query)
		args = append(args, "%"+value+"%")
	}
	if model.ID(f.ChannelID) {
		where = append(where, "m.channel_id=?")
		args = append(args, f.ChannelID)
	}
	if model.ID(f.GuildID) {
		where = append(where, "c.guild_id=?")
		args = append(args, f.GuildID)
	} else if f.Scope == "dm" {
		where = append(where, "c.guild_id IS NULL AND c.kind IN (1,3)")
	}
	if cursor != "" {
		if !model.ID(cursor) {
			return nil, errors.New("invalid message cursor")
		}
		where = append(where, "m.sort_key"+comparison+"?")
		args = append(args, model.SortKey(cursor))
	}
	args = append(args, limit)
	order := "ASC"
	if descending {
		order = "DESC"
	}
	query := `SELECT m.id,COALESCE(m.channel_id,''),` + channelDisplayNameSQL + `,COALESCE(c.guild_id,''),COALESCE(c.kind,-1),COALESCE(g.name,c.guild_id,''),
		` + userDisplayNameSQL + `,COALESCE(m.content,''),COALESCE(m.created_at,''),COALESCE(m.edited_at,''),m.source,m.deleted,
		COALESCE((SELECT ru.hash FROM resource_urls ru WHERE ru.host IN ('cdn.discordapp.com','media.discordapp.net')
		AND substr(ru.path,1,length('/avatars/'||u.id||'/'||u.avatar||'.'))='/avatars/'||u.id||'/'||u.avatar||'.' LIMIT 1),'')
		FROM messages m LEFT JOIN channels c ON c.id=m.channel_id LEFT JOIN guilds g ON g.id=c.guild_id
		LEFT JOIN users u ON u.id=m.author_id WHERE ` + strings.Join(where, " AND ") + ` ORDER BY m.sort_key ` + order + ` LIMIT ?`
	rows, err := s.queries().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	result := []model.MessageRow{}
	for rows.Next() {
		var item model.MessageRow
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.ChannelName, &item.GuildID, &item.ChannelKind, &item.GuildName, &item.AuthorName, &item.Content, &item.Timestamp, &item.EditedTimestamp, &item.Source, &item.Deleted, &item.AvatarHash); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range result {
		if err := s.enrichMessage(ctx, &result[i]); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Store) enrichMessage(ctx context.Context, message *model.MessageRow) error {
	attachments, err := s.queries().QueryContext(ctx, `SELECT a.name,`+attachmentHashSQL+`
		FROM attachments a WHERE a.message_id=? ORDER BY a.id`, message.ID)
	if err != nil {
		return err
	}
	for attachments.Next() {
		var asset model.AssetRow
		if err := attachments.Scan(&asset.Name, &asset.Hash); err != nil {
			attachments.Close()
			return err
		}
		message.Attachments = append(message.Attachments, asset)
	}
	err = attachments.Err()
	attachments.Close()
	if err != nil {
		return err
	}
	message.Reactions, err = s.messageReactions(ctx, message.ID)
	if err != nil {
		return err
	}
	return s.enrichInline(ctx, message)
}

func (s *Store) enrichInline(ctx context.Context, message *model.MessageRow) error {
	message.MentionNames = map[string]string{}
	message.EmojiHashes = map[string]string{}
	remaining := message.Content
	seenTokens := map[string]bool{}
	for remaining != "" {
		if err := ctx.Err(); err != nil {
			return err
		}
		indices := messageToken.FindStringSubmatchIndex(remaining)
		if indices == nil {
			break
		}
		var kind int
		var id string
		for i := 1; i < len(indices)/2; i++ {
			if indices[2*i] >= 0 {
				kind, id = i, remaining[indices[2*i]:indices[2*i+1]]
				break
			}
		}
		remaining = remaining[indices[1]:]
		// Do not allocate every repeated mention at once or query an unknown
		// identity again for each occurrence. User/emoji spelling variants share
		// the same lookup, including identities that have not been observed.
		key := [...]string{"", "user:", "role:", "channel:", "emoji:"}[kind] + id
		if seenTokens[key] {
			continue
		}
		seenTokens[key] = true
		switch kind {
		case 1:
			var name string
			err := s.queries().QueryRowContext(ctx, "SELECT COALESCE(NULLIF(display_name,''),NULLIF(username,''),id) FROM users WHERE id=?", id).Scan(&name)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			if err == nil {
				message.MentionNames[key] = name
			}
		case 2:
			message.MentionNames[key] = "role " + id
		case 3:
			var name string
			err := s.queries().QueryRowContext(ctx, "SELECT COALESCE(NULLIF(name,''),id) FROM channels WHERE id=?", id).Scan(&name)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			if err == nil {
				message.MentionNames[key] = name
			}
		case 4:
			var hash string
			prefix := "/emojis/" + id + "."
			err := s.queries().QueryRowContext(ctx, `SELECT hash FROM resource_urls WHERE host IN ('cdn.discordapp.com','media.discordapp.net')
			AND substr(path,1,length(?))=? LIMIT 1`, prefix, prefix).Scan(&hash)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			message.EmojiHashes[id] = hash
		}
	}
	return nil
}
