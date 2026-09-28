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
	put(model.Message{ID: "5", Deleted: model.Ptr(true), Revision: 0})
	put(model.Message{ID: "5", Content: model.Ptr("previously unseen body"), Revision: 10})
	rows, e = s.Messages(ctx, model.Filter{})
	if e != nil {
		t.Fatal(e)
	}
	var found bool
	for _, row := range rows {
		if row.ID == "5" && row.Deleted && row.Content == "previously unseen body" {
			found = true
		}
	}
	if !found {
		t.Fatal("tombstone blocked later observed body")
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
func TestReactionNamesAndMessageIdentity(t *testing.T) {
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
	apply := func(b model.Batch) {
		t.Helper()
		if err := s.Apply(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	apply(model.Batch{Source: "http", Messages: []model.Message{{ID: "10", Content: model.Ptr("first"), Revision: 10, HasReactions: true, Reactions: []model.Reaction{{EmojiName: "👍", Count: 2}}}}})
	apply(model.Batch{Source: "ws", Users: []model.User{{ID: "3", Username: model.Ptr("Alice")}}, ReactionChanges: []model.ReactionChange{{MessageID: "10", EmojiName: "👍", UserID: "3", Operation: "observe"}}})
	apply(model.Batch{Source: "ws", Messages: []model.Message{{ID: "10", Content: model.Ptr("edited"), Revision: 20}}})
	apply(model.Batch{Source: "ws", Messages: []model.Message{{ID: "10", Deleted: model.Ptr(true), Revision: 21}}})
	apply(model.Batch{Source: "ws", Messages: []model.Message{{ID: "10", Deleted: model.Ptr(true), Revision: 22}}})
	rows, err := s.Messages(ctx, model.Filter{})
	if err != nil || len(rows) != 1 || rows[0].Content != "edited" || !rows[0].Deleted {
		t.Fatalf("message identity/edit/delete: %+v, %v", rows, err)
	}
	if len(rows[0].Reactions) != 1 || rows[0].Reactions[0].Count != 2 || len(rows[0].Reactions[0].Users) != 1 || rows[0].Reactions[0].Users[0] != "Alice" || rows[0].Reactions[0].UnknownCount != 1 {
		t.Fatalf("reaction names: %+v", rows[0].Reactions)
	}
	apply(model.Batch{Source: "ws", ReactionChanges: []model.ReactionChange{{MessageID: "10", EmojiName: "👍", UserID: "3", Operation: "remove"}}})
	rows, err = s.Messages(ctx, model.Filter{})
	if err != nil || rows[0].Reactions[0].Count != 1 || len(rows[0].Reactions[0].Users) != 0 {
		t.Fatalf("reaction removal: %+v, %v", rows, err)
	}
	add := model.Batch{Source: "ws", ReactionChanges: []model.ReactionChange{{MessageID: "10", EmojiName: "👍", UserID: "3", Operation: "add"}}}
	apply(add)
	apply(add)
	rows, err = s.Messages(ctx, model.Filter{})
	if err != nil || rows[0].Reactions[0].Count != 2 || len(rows[0].Reactions[0].Users) != 1 {
		t.Fatalf("duplicate reaction add: %+v, %v", rows, err)
	}
	apply(model.Batch{Source: "ws", ReactionChanges: []model.ReactionChange{{MessageID: "10", Operation: "clear"}}})
	rows, err = s.Messages(ctx, model.Filter{})
	if err != nil || len(rows[0].Reactions) != 0 {
		t.Fatalf("reaction clear: %+v, %v", rows, err)
	}
}
func TestMissingResourcesAndCachedAvatar(t *testing.T) {
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
	avatarPath := "/avatars/3/hash.png"
	attachmentPath := "/attachments/2/4/a.png"
	batch := model.Batch{Source: "http", Users: []model.User{{ID: "3", Username: model.Ptr("Alice"), Avatar: model.Ptr("hash")}}, Messages: []model.Message{{ID: "10", AuthorID: model.Ptr("3"), HasAttachments: true, Attachments: []model.Attachment{{ID: "4", MessageID: "10", Name: "a.png", Host: "cdn.discordapp.com", Path: attachmentPath}}}}}
	if err := s.Apply(ctx, batch); err != nil {
		t.Fatal(err)
	}
	targets, err := s.MissingResources(ctx)
	if err != nil || len(targets) != 2 {
		t.Fatalf("missing targets: %+v, %v", targets, err)
	}
	if err := s.SaveAsset(ctx, "cdn.discordapp.com", avatarPath, "image/png", []byte("image"), 1024); err != nil {
		t.Fatal(err)
	}
	targets, err = s.MissingResources(ctx)
	if err != nil || len(targets) != 1 || targets[0].Path != attachmentPath {
		t.Fatalf("cached avatar still missing: %+v, %v", targets, err)
	}
	rows, err := s.Messages(ctx, model.Filter{})
	if err != nil || len(rows) != 1 || rows[0].AvatarHash == "" {
		t.Fatalf("cached avatar not exposed: %+v, %v", rows, err)
	}
}
func TestSchemaV1UpgradesToReactions(t *testing.T) {
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
	for _, statement := range []string{"DROP TABLE reaction_users", "DROP TABLE reaction_totals", "UPDATE schema_version SET version=1"} {
		if _, err := s.writer.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(r)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err := s.reader.QueryRow("SELECT version FROM schema_version").Scan(&version); err != nil || version != 2 {
		t.Fatalf("migration version %d: %v", version, err)
	}
}
func TestCustomEmojiUsesCachedImage(t *testing.T) {
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
	if err := s.Apply(ctx, model.Batch{Source: "http", Messages: []model.Message{{ID: "10", HasReactions: true, Reactions: []model.Reaction{{EmojiID: "5", EmojiName: "wave", Count: 1}}}}}); err != nil {
		t.Fatal(err)
	}
	targets, err := s.MissingResources(ctx)
	if err != nil || len(targets) != 1 || targets[0].Path != "/emojis/5.png" {
		t.Fatalf("emoji backfill: %+v, %v", targets, err)
	}
	if err := s.SaveAsset(ctx, "cdn.discordapp.com", "/emojis/5.png", "image/png", []byte("image"), 1024); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Messages(ctx, model.Filter{})
	if err != nil || len(rows) != 1 || len(rows[0].Reactions) != 1 || rows[0].Reactions[0].EmojiHash == "" {
		t.Fatalf("emoji image: %+v, %v", rows, err)
	}
}
