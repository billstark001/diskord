package store

import (
	"context"
	"database/sql"

	"diskord/internal/model"
	"diskord/internal/scope"
)

type ResourceTarget struct{ Host, Path string }

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
	result := make([]ResourceTarget, 0, len(targets))
	for _, target := range targets {
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
	return result, nil
}
