package store

import (
	"context"
	"database/sql"
	"regexp"
	"sort"
	"strings"

	"diskord/internal/model"
	"diskord/internal/scope"
)

type ResourceTarget struct{ Host, Path string }

var contentEmoji = regexp.MustCompile(`<(?P<animated>a?):[A-Za-z0-9_]{1,128}:(?P<id>[0-9]{1,20})>`)

// MissingResources returns only allowlisted CDN hosts and paths. Signed URL query
// strings are deliberately not retained, so some attachments may no longer be
// fetchable until the Discord client supplies a fresh request.
func (s *Store) MissingResources(ctx context.Context) ([]ResourceTarget, error) {
	targets := []ResourceTarget{}
	seen := map[ResourceTarget]bool{}
	add := func(host, path string) {
		target := ResourceTarget{Host: host, Path: path}
		if !scope.CDN(host) || path == "" || seen[target] {
			return
		}
		seen[target] = true
		targets = append(targets, target)
	}
	attachments, err := s.reader.QueryContext(ctx, `SELECT DISTINCT a.host,a.path FROM attachments a
        LEFT JOIN resource_urls r ON r.host=a.host AND r.path=a.path
        WHERE r.hash IS NULL ORDER BY a.host,a.path`)
	if err != nil {
		return nil, err
	}
	for attachments.Next() {
		var host, path string
		if err := attachments.Scan(&host, &path); err != nil {
			attachments.Close()
			return nil, err
		}
		add(host, path)
	}
	err = attachments.Err()
	attachments.Close()
	if err != nil {
		return nil, err
	}
	users, err := s.reader.QueryContext(ctx, `SELECT id,avatar FROM users WHERE avatar IS NOT NULL AND avatar!=''`)
	if err != nil {
		return nil, err
	}
	for users.Next() {
		var id, hash string
		if err := users.Scan(&id, &hash); err != nil {
			users.Close()
			return nil, err
		}
		path := scope.AvatarPath(id, hash)
		if path != "" {
			add("cdn.discordapp.com", path)
		}
	}
	err = users.Err()
	users.Close()
	if err != nil {
		return nil, err
	}
	guilds, err := s.reader.QueryContext(ctx, `SELECT id,icon FROM guilds WHERE icon IS NOT NULL AND icon!=''`)
	if err != nil {
		return nil, err
	}
	for guilds.Next() {
		var id, icon string
		if err := guilds.Scan(&id, &icon); err != nil {
			guilds.Close()
			return nil, err
		}
		if path := scope.GuildIconPath(id, icon); path != "" {
			add("cdn.discordapp.com", path)
		}
	}
	err = guilds.Err()
	guilds.Close()
	if err != nil {
		return nil, err
	}
	emojis, err := s.reader.QueryContext(ctx, `SELECT DISTINCT emoji_id,animated FROM reaction_totals WHERE emoji_id!=''`)
	if err != nil {
		return nil, err
	}
	for emojis.Next() {
		var id string
		var animated bool
		if err := emojis.Scan(&id, &animated); err != nil {
			emojis.Close()
			return nil, err
		}
		if !model.ID(id) {
			continue
		}
		ext := ".png"
		if animated {
			ext = ".gif"
		}
		add("cdn.discordapp.com", "/emojis/"+id+ext)
	}
	err = emojis.Err()
	emojis.Close()
	if err != nil {
		return nil, err
	}
	contents, err := s.reader.QueryContext(ctx, `SELECT content FROM messages WHERE content LIKE '%<:%' OR content LIKE '%<a:%'`)
	if err != nil {
		return nil, err
	}
	for contents.Next() {
		var content string
		if err := contents.Scan(&content); err != nil {
			contents.Close()
			return nil, err
		}
		for _, match := range contentEmoji.FindAllStringSubmatch(content, -1) {
			if !model.ID(match[2]) {
				continue
			}
			ext := ".png"
			if match[1] == "a" {
				ext = ".gif"
			}
			add("cdn.discordapp.com", "/emojis/"+match[2]+ext)
		}
	}
	err = contents.Err()
	contents.Close()
	if err != nil {
		return nil, err
	}
	result := make([]ResourceTarget, 0, len(targets))
	for _, target := range targets {
		if strings.HasPrefix(target.Path, "/attachments/") {
			var cached int
			err := s.reader.QueryRowContext(ctx, `SELECT 1 FROM resource_urls WHERE host IN ('cdn.discordapp.com','media.discordapp.net') AND path=? LIMIT 1`, target.Path).Scan(&cached)
			if err == nil {
				continue
			}
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
		}
		foundVariant := false
		for _, prefix := range []string{"/avatars/", "/icons/", "/emojis/"} {
			if !strings.HasPrefix(target.Path, prefix) {
				continue
			}
			period := strings.LastIndexByte(target.Path, '.')
			if period < 0 {
				continue
			}
			stem := target.Path[:period+1]
			var cached int
			err := s.reader.QueryRowContext(ctx, `SELECT 1 FROM resource_urls WHERE host IN ('cdn.discordapp.com','media.discordapp.net') AND substr(path,1,length(?))=? LIMIT 1`, stem, stem).Scan(&cached)
			if err == nil {
				foundVariant = true
				break
			}
			if err != sql.ErrNoRows {
				return nil, err
			}
		}
		if foundVariant {
			continue
		}
		var exists int
		err := s.reader.QueryRowContext(ctx, "SELECT 1 FROM resource_urls WHERE host=? AND path=?", target.Host, target.Path).Scan(&exists)
		if err == nil {
			continue
		}
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		result = append(result, target)
	}
	priority := func(path string) int {
		switch {
		case strings.HasPrefix(path, "/icons/"):
			return 0
		case strings.HasPrefix(path, "/emojis/"):
			return 1
		case strings.HasPrefix(path, "/avatars/"):
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return priority(result[i].Path) < priority(result[j].Path) })
	return result, nil
}
