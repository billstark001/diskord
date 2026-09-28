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
