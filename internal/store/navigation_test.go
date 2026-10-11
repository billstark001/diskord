package store

import (
	"context"
	"reflect"
	"testing"

	"diskord/internal/model"
	"diskord/internal/securefs"
)

func TestSharedNavigationOrderingAndScopes(t *testing.T) {
	db := archiveFixture(t)
	ctx := context.Background()
	batch := model.Batch{Source: "ws", HasGuildOrder: true, GuildOrder: []string{"9", "1"}, Guilds: []model.Guild{{ID: "1", Name: model.Ptr("Alpha")}, {ID: "9", Name: model.Ptr("Zulu")}}, Channels: []model.Channel{
		{ID: "10", GuildID: model.Ptr("9"), Type: model.Ptr(0), Name: model.Ptr("z-root"), Position: model.Ptr(9)},
		{ID: "20", GuildID: model.Ptr("9"), Type: model.Ptr(4), Name: model.Ptr("a-category"), Position: model.Ptr(1)},
		{ID: "21", ParentID: model.Ptr("20"), Type: model.Ptr(0), Position: model.Ptr(2)},
		{ID: "22", ParentID: model.Ptr("20"), Type: model.Ptr(15), Position: model.Ptr(1)},
		{ID: "23", ParentID: model.Ptr("22"), Type: model.Ptr(11), LastMessageID: model.Ptr("500")},
		{ID: "24", ParentID: model.Ptr("22"), Type: model.Ptr(11), LastMessageID: model.Ptr("600")},
		{ID: "30", Type: model.Ptr(1), Name: model.Ptr("A DM"), LastMessageID: model.Ptr("100")},
		{ID: "31", Type: model.Ptr(3), Name: model.Ptr("Z DM"), LastMessageID: model.Ptr("300")},
	}, Messages: []model.Message{{ID: "900", ChannelID: model.Ptr("30"), Content: model.Ptr("DM")}, {ID: "500", ChannelID: model.Ptr("23"), Content: model.Ptr("post")}}}
	if err := db.Apply(ctx, batch); err != nil {
		t.Fatal(err)
	}
	nav, err := db.Navigation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nav.Guilds[0].ID != "9" {
		t.Fatal(nav.Guilds)
	}
	ids := []string{}
	for _, c := range nav.Channels {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, []string{"30", "31", "10", "20", "22", "24", "23", "21"}) {
		t.Fatal(ids)
	}
	r, err := db.BeginArchive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, tt := range []struct {
		s     Selection
		count int
	}{{Selection{GuildID: "9"}, 1}, {Selection{Scope: "dm"}, 1}, {Selection{ThreadID: "22", IncludeThreads: true}, 1}, {Selection{ThreadID: "22"}, 0}} {
		page, err := r.Page(ctx, tt.s, "", 100)
		if err != nil || len(page.Messages) != tt.count {
			t.Fatal(tt, page, err)
		}
	}
}

func TestReadOnlySchemaFourNavigation(t *testing.T) {
	db := archiveFixture(t)
	ctx := context.Background()
	if err := db.Apply(ctx, model.Batch{Source: "ws", Guilds: []model.Guild{{ID: "9", Name: model.Ptr("Alpha")}, {ID: "1", Name: model.Ptr("Zulu")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.writer.Exec("DROP TABLE guild_positions"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.writer.Exec("UPDATE schema_version SET version=4"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	lock, err := securefs.Acquire(db.Root)
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	before := runtimeState(t, db.Root)
	lock, err = securefs.LockExisting(db.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	ro, err := OpenReadOnly(db.Root)
	if err != nil {
		t.Fatal(err)
	}
	nav, err := ro.Navigation(ctx)
	if err != nil || nav.Guilds[0].ID != "1" || ro.DatabaseSchema() != 4 {
		t.Fatal(nav, err)
	}
	ro.Close()
	if !reflect.DeepEqual(before, runtimeState(t, db.Root)) {
		t.Fatal("legacy read changed files")
	}
}

func TestForumNavigationMatchesPostsWithEmptyActivity(t *testing.T) {
	db := archiveFixture(t)
	ctx := context.Background()
	if err := db.Apply(ctx, model.Batch{Source: "ws", Channels: []model.Channel{
		{ID: "10", GuildID: model.Ptr("1"), Type: model.Ptr(15)},
		{ID: "20", ParentID: model.Ptr("10"), Type: model.Ptr(11), LastMessageID: model.Ptr("")},
		{ID: "19", ParentID: model.Ptr("10"), Type: model.Ptr(11)},
		{ID: "18", ParentID: model.Ptr("10"), Type: model.Ptr(11), LastMessageID: model.Ptr("50")},
		{ID: "17", ParentID: model.Ptr("10"), Type: model.Ptr(11), Flags: model.Ptr(2)},
	}, Messages: []model.Message{{ID: "100", ChannelID: model.Ptr("19")}}}); err != nil {
		t.Fatal(err)
	}
	for _, sortOrder := range []int{0, 1} {
		if err := db.Apply(ctx, model.Batch{Source: "ws", Channels: []model.Channel{{ID: "10", DefaultSortOrder: &sortOrder}}}); err != nil {
			t.Fatal(err)
		}
		nav, err := db.Navigation(ctx)
		if err != nil {
			t.Fatal(err)
		}
		forum, err := db.Forum(ctx, "10")
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, c := range nav.Channels {
			if c.ParentID == "10" {
				ids = append(ids, c.ID)
			}
		}
		posts := []string{}
		for _, p := range forum.Posts {
			posts = append(posts, p.ID)
		}
		want := []string{"17", "19", "18", "20"}
		if sortOrder == 1 {
			want = []string{"17", "20", "19", "18"}
		}
		if !reflect.DeepEqual(ids, posts) || !reflect.DeepEqual(ids, want) {
			t.Fatalf("sort %d: navigation %v, posts %v, want %v", sortOrder, ids, posts, want)
		}
	}
}
