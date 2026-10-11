package store

import (
	"context"
	"sort"

	"diskord/internal/model"
)

// Navigation is shared by the console, API, name lookup and offline CLI.
// Channel order follows the sidebar tree, with forum threads in the forum's
// default post order. Private conversations follow latest observed activity.
type Navigation struct {
	Guilds   []model.GuildRow
	Channels []model.ChannelRow
}

const channelIDSortKeySQL = `substr('00000000000000000000'||c.id,-20)`

// Empty last_message_id values are unobserved. Fall back to the channel ID
// only when neither the snapshot nor any locally observed message has activity.
const channelActivitySortKeySQL = `COALESCE(NULLIF(max(
	CASE WHEN NULLIF(c.last_message_id,'') IS NULL THEN '' ELSE substr('00000000000000000000'||c.last_message_id,-20) END,
	COALESCE((SELECT m.sort_key FROM messages m WHERE m.channel_id=c.id ORDER BY m.sort_key DESC LIMIT 1),'')),''),` + channelIDSortKeySQL + `)`

const forumPostOrderSQL = `(COALESCE(c.flags,0)&2)!=0 DESC,
	CASE WHEN parent.default_sort_order=1 THEN ` + channelIDSortKeySQL + ` ELSE ` + channelActivitySortKeySQL + ` END DESC,
	` + channelIDSortKeySQL + ` DESC`

func (s *Store) Navigation(ctx context.Context) (Navigation, error) {
	reader, err := s.BeginArchive(ctx)
	if err != nil {
		return Navigation{}, err
	}
	defer reader.Close()
	db := reader.store
	guilds, err := db.Guilds(ctx)
	if err != nil {
		return Navigation{}, err
	}
	rows, err := db.readChannels(ctx, "1=1", nil, "length(c.id),c.id")
	if err != nil {
		return Navigation{}, err
	}
	nav := Navigation{Guilds: guilds, Channels: []model.ChannelRow{}}
	dms, err := db.Channels(ctx, "")
	if err != nil {
		return Navigation{}, err
	}
	nav.Channels = append(nav.Channels, dms...)
	seen := map[string]bool{}
	for _, c := range dms {
		seen[c.ID] = true
	}
	appendTree := func(items []model.ChannelRow) error {
		ordered, err := db.channelTree(ctx, items)
		if err != nil {
			return err
		}
		for _, c := range ordered {
			nav.Channels = append(nav.Channels, c)
			seen[c.ID] = true
		}
		return nil
	}
	byGuild := map[string][]model.ChannelRow{}
	for _, c := range rows {
		byGuild[c.GuildID] = append(byGuild[c.GuildID], c)
	}
	for _, g := range guilds {
		if err := appendTree(byGuild[g.ID]); err != nil {
			return Navigation{}, err
		}
	}
	remaining := []model.ChannelRow{}
	for _, c := range rows {
		if !seen[c.ID] {
			remaining = append(remaining, c)
		}
	}
	if err := appendTree(remaining); err != nil {
		return Navigation{}, err
	}
	return nav, nil
}

func (s *Store) channelTree(ctx context.Context, items []model.ChannelRow) ([]model.ChannelRow, error) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if (a.Position == nil) != (b.Position == nil) {
			return a.Position != nil
		}
		if a.Position != nil && *a.Position != *b.Position {
			return *a.Position < *b.Position
		}
		return model.SortKey(a.ID) < model.SortKey(b.ID)
	})
	ids := map[string]bool{}
	children := map[string][]model.ChannelRow{}
	for _, c := range items {
		ids[c.ID] = true
	}
	for _, c := range items {
		if c.ParentID != "" && ids[c.ParentID] && c.Kind != 4 {
			children[c.ParentID] = append(children[c.ParentID], c)
		}
	}
	// Forum ordering uses the same store query as the frontend's initial view.
	for _, c := range items {
		if c.Kind != 15 && c.Kind != 16 {
			continue
		}
		rows, err := s.queries().QueryContext(ctx, `SELECT c.id FROM channels c JOIN channels parent ON parent.id=c.parent_id
			WHERE c.parent_id=? AND c.kind=11 ORDER BY `+forumPostOrderSQL, c.ID)
		if err != nil {
			return nil, err
		}
		ranks := map[string]int{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				break
			}
			ranks[id] = len(ranks)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}

		sort.SliceStable(children[c.ID], func(i, j int) bool {
			a, aok := ranks[children[c.ID][i].ID]
			b, bok := ranks[children[c.ID][j].ID]
			if aok != bok {
				return aok
			}
			return aok && a < b
		})
	}
	result := []model.ChannelRow{}
	seen := map[string]bool{}
	var visit func(model.ChannelRow)
	visit = func(c model.ChannelRow) {
		if seen[c.ID] {
			return
		}
		seen[c.ID] = true
		result = append(result, c)
		for _, child := range children[c.ID] {
			visit(child)
		}
	}
	for _, c := range items {
		if c.Kind != 4 && (!ids[c.ParentID]) {
			visit(c)
		}
	}
	for _, c := range items {
		if c.Kind == 4 {
			visit(c)
		}
	}
	// Retain orphaned/cyclic entries deterministically rather than omitting them.
	for _, c := range items {
		visit(c)
	}
	return result, nil
}
