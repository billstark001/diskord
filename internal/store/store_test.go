package store

import (
	"context"
	"diskord/internal/model"
	"diskord/internal/securefs"
	"os"
	"testing"
)

func TestMergingTombstonesAndFKs(t *testing.T) {
	r := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
		t.Setenv(k, os.Getenv(k))
	}
	if e := securefs.Prepare(r); e != nil {
		t.Fatal(e)
	}
	s, e := Open(r)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	put := func(m model.Message) {
		t.Helper()
		if e := s.Apply(ctx, model.Batch{Source: "ws", Messages: []model.Message{m}}); e != nil {
			t.Fatal(e)
		}
	}
	put(model.Message{ID: "9007199254740993", ChannelID: model.Ptr("2"), AuthorID: model.Ptr("3"), Content: model.Ptr("original"), Revision: 10})
	put(model.Message{ID: "9007199254740993", Content: model.Ptr("edited"), Revision: 20})
	put(model.Message{ID: "9007199254740993", Content: model.Ptr("stale HTTP"), Revision: 10})
	put(model.Message{ID: "9007199254740993", Deleted: model.Ptr(true), Revision: 21})
	put(model.Message{ID: "9007199254740993", Content: model.Ptr("replayed"), Revision: 10})
	rows, e := s.Messages(ctx, model.Filter{})
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	if rows[0].Content != "edited" || !rows[0].Deleted || rows[0].ChannelID != "2" {
		t.Fatal(rows)
	}
	var n int
	if e = s.writer.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	put(model.Message{ID: "4", Deleted: model.Ptr(true), Revision: 20})
	put(model.Message{ID: "4", Content: model.Ptr("late"), Revision: 10})
	var deleted int
	s.writer.QueryRow("SELECT deleted FROM messages WHERE id='4'").Scan(&deleted)
	if deleted != 1 {
		t.Fatal("tombstone resurrected")
	}
}

func TestNavigationAndScopedMessages(t *testing.T) {
	r := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
		t.Setenv(k, os.Getenv(k))
	}
	if err := securefs.Prepare(r); err != nil {
		t.Fatal(err)
	}
	s, err := Open(r)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	category := 4
	batch := model.Batch{Source: "ws",
		Guilds: []model.Guild{{ID: "1", Name: model.Ptr("Server")}},
		Channels: []model.Channel{
			{ID: "2", GuildID: model.Ptr("1"), Name: model.Ptr("Category"), Type: &category},
			{ID: "3", GuildID: model.Ptr("1"), ParentID: model.Ptr("2"), Name: model.Ptr("General")},
			{ID: "4", Name: model.Ptr("DM")},
		},
		Messages: []model.Message{
			{ID: "10", ChannelID: model.Ptr("3"), Content: model.Ptr("server message")},
			{ID: "11", ChannelID: model.Ptr("4"), Content: model.Ptr("dm message")},
		},
	}
	if err := s.Apply(ctx, batch); err != nil {
		t.Fatal(err)
	}
	guilds, err := s.Guilds(ctx)
	if err != nil || len(guilds) != 1 || guilds[0].Name != "Server" {
		t.Fatalf("guild navigation: %+v, %v", guilds, err)
	}
	channels, err := s.Channels(ctx, "1")
	if err != nil || len(channels) != 2 {
		t.Fatalf("guild channels: %+v, %v", channels, err)
	}
	var found bool
	for _, channel := range channels {
		if channel.ID == "3" && channel.ParentID == "2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("category relation lost: %+v", channels)
	}
	dms, err := s.Channels(ctx, "")
	if err != nil || len(dms) != 1 || dms[0].ID != "4" {
		t.Fatalf("dm channels: %+v, %v", dms, err)
	}
	rows, err := s.Messages(ctx, model.Filter{Scope: "dm"})
	if err != nil || len(rows) != 1 || rows[0].ID != "11" {
		t.Fatalf("dm messages: %+v, %v", rows, err)
	}
	rows, err = s.Messages(ctx, model.Filter{GuildID: "1", ChannelID: "3"})
	if err != nil || len(rows) != 1 || rows[0].ID != "10" {
		t.Fatalf("channel messages: %+v, %v", rows, err)
	}
}
