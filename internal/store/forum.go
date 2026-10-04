package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"diskord/internal/model"
)

// Forum returns only posts observed by the local archive. A forum is a
// container; its post messages live in child public threads.
func (s *Store) Forum(ctx context.Context, id string) (model.ForumRow, error) {
	if !model.ID(id) {
		return model.ForumRow{}, errors.New("invalid forum ID")
	}
	var forum model.ForumRow
	var kind int
	err := s.reader.QueryRowContext(ctx, `SELECT id,COALESCE(name,''),COALESCE(topic,''),kind,default_sort_order,default_forum_layout
		FROM channels WHERE id=?`, id).Scan(&forum.ID, &forum.Name, &forum.Topic, &kind, &forum.DefaultSortOrder, &forum.DefaultForumLayout)
	if err != nil {
		return forum, err
	}
	if kind != 15 && kind != 16 {
		return model.ForumRow{}, errors.New("channel is not a forum")
	}
	forum.Posts = []model.ForumPostRow{}
	rows, err := s.reader.QueryContext(ctx, `SELECT c.id,COALESCE(c.name,''),
		COALESCE(NULLIF(u.display_name,''),NULLIF(u.username,''),u.id,c.owner_id,''),COALESCE(sm.content,''),
		COALESCE(c.created_at,sm.created_at,''),COALESCE(c.last_message_id,''),
		COALESCE(c.message_count,0),COALESCE(c.archived,0),COALESCE(c.flags,0),c.deleted,
		COALESCE((SELECT m.id FROM messages m WHERE m.channel_id=c.id ORDER BY m.sort_key DESC LIMIT 1),''),
		COALESCE((SELECT m.created_at FROM messages m WHERE m.channel_id=c.id ORDER BY m.sort_key DESC LIMIT 1),''),
		(SELECT count(*) FROM messages m WHERE m.channel_id=c.id AND m.id!=c.id AND m.deleted=0)
		FROM channels c LEFT JOIN messages sm ON sm.id=c.id AND sm.channel_id=c.id
		LEFT JOIN users u ON u.id=COALESCE(sm.author_id,c.owner_id)
		WHERE c.parent_id=? AND c.kind=11`, id)
	if err != nil {
		return forum, err
	}
	type candidate struct {
		post    model.ForumPostRow
		sortKey string
	}
	items := []candidate{}
	for rows.Next() {
		var item candidate
		var lastID, observedID, observedAt string
		var count, flags, observedCount int
		if err = rows.Scan(&item.post.ID, &item.post.Name, &item.post.AuthorName, &item.post.Content,
			&item.post.CreatedAt, &lastID, &count, &item.post.Archived, &flags, &item.post.Deleted,
			&observedID, &observedAt, &observedCount); err != nil {
			break
		}
		item.post.Pinned = flags&2 != 0
		item.post.ReplyCount = max(count, observedCount)
		if model.SortKey(observedID) > model.SortKey(lastID) {
			lastID = observedID
		}
		if lastID == "" {
			lastID = item.post.ID
		}
		item.sortKey = model.SortKey(lastID)
		item.post.LastActivityID = lastID
		if lastID == observedID {
			item.post.LastActivity = observedAt
		}
		items = append(items, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return forum, err
	}
	for i := range items {
		post := &items[i].post
		tagRows, e := s.reader.QueryContext(ctx, `SELECT COALESCE(ft.name,tt.tag_id) FROM thread_tags tt
			LEFT JOIN forum_tags ft ON ft.channel_id=? AND ft.tag_id=tt.tag_id WHERE tt.thread_id=? ORDER BY ft.name,tt.tag_id`, id, post.ID)
		if e != nil {
			return forum, e
		}
		post.Tags = []string{}
		for tagRows.Next() {
			var tag string
			if e = tagRows.Scan(&tag); e != nil {
				break
			}
			post.Tags = append(post.Tags, tag)
		}
		if e == nil {
			e = tagRows.Err()
		}
		tagRows.Close()
		if e != nil {
			return forum, e
		}
		var name, hash string
		e = s.reader.QueryRowContext(ctx, `SELECT a.name,COALESCE((SELECT r.hash FROM resource_urls r WHERE r.host=a.host AND r.path=a.path), '')
			FROM attachments a WHERE a.message_id=? ORDER BY a.id LIMIT 1`, post.ID).Scan(&name, &hash)
		if e != nil && e != sql.ErrNoRows {
			return forum, e
		}
		post.Attachment = model.AssetRow{Name: name, Hash: hash}
		post.Reactions, e = s.messageReactions(ctx, post.ID)
		if e != nil {
			return forum, e
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.post.Pinned != b.post.Pinned {
			return a.post.Pinned
		}
		if forum.DefaultSortOrder != nil && *forum.DefaultSortOrder == 1 {
			return model.SortKey(a.post.ID) > model.SortKey(b.post.ID)
		}
		if a.sortKey != b.sortKey {
			return a.sortKey > b.sortKey
		}
		return model.SortKey(a.post.ID) > model.SortKey(b.post.ID)
	})
	for _, item := range items {
		forum.Posts = append(forum.Posts, item.post)
	}
	return forum, nil
}
