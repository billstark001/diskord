package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"diskord/internal/model"
	"diskord/internal/securefs"
)

func archiveFixture(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
		t.Setenv(key, os.Getenv(key))
	}
	if err := securefs.Prepare(root); err != nil {
		t.Fatal(err)
	}
	db, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestArchivePageByteBudgetPreservesLargeMessages(t *testing.T) {
	db := archiveFixture(t)
	ctx := context.Background()
	content := strings.Repeat("x", 512<<10)
	batch := model.Batch{Source: "ws"}
	for i := 1; i <= 20; i++ {
		batch.Messages = append(batch.Messages, model.Message{ID: strconv.Itoa(i), ChannelID: model.Ptr("2"), Content: &content})
	}
	batch.Messages = append(batch.Messages, model.Message{ID: "21", ChannelID: model.Ptr("2"), Content: model.Ptr(strings.Repeat("y", 9<<20))})
	if err := db.Apply(ctx, batch); err != nil {
		t.Fatal(err)
	}
	r, err := db.BeginArchive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	after := ""
	count, pages := 0, 0
	for {
		page, err := r.Page(ctx, Selection{}, after, 1000)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		var bytes int64
		for _, message := range page.Messages {
			count++
			if message.ID != strconv.Itoa(count) {
				t.Fatal("byte-limited pagination skipped or duplicated a message", message.ID, count)
			}
			bytes += int64(len(*message.Content))
		}
		if bytes > MaxArchivePageBytes && len(page.Messages) != 1 {
			t.Fatal("oversized message was not isolated", bytes, len(page.Messages))
		}
		if page.NextAfter == "" {
			break
		}
		after = page.NextAfter
	}
	if count != 21 || pages < 3 {
		t.Fatal("payload budget did not split the complete archive", count, pages)
	}
}

func TestLongRepeatedMentionsAreEnrichedAndCancelable(t *testing.T) {
	db := archiveFixture(t)
	ctx := context.Background()
	if err := db.Apply(ctx, model.Batch{Source: "ws", Users: []model.User{{ID: "9", Username: model.Ptr("Alice")}}}); err != nil {
		t.Fatal(err)
	}
	m := model.MessageRow{Content: strings.Repeat("<@9> <@!9> <@99> <@!99> <#99> <:missing:88> <a:renamed:88> ", 20000)}
	if err := db.enrichInline(ctx, &m); err != nil || m.MentionNames["user:9"] != "Alice" || len(m.MentionNames) != 1 || len(m.EmojiHashes) != 1 {
		t.Fatal("long message enrichment lost observed identities", m.MentionNames, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := db.enrichInline(canceled, &m); err != context.Canceled {
		t.Fatal("long message enrichment ignored cancellation", err)
	}
}

func TestArchivePageBudgetIncludesJSONEscaping(t *testing.T) {
	db := archiveFixture(t)
	ctx := context.Background()
	body := strings.Repeat("\x00<\"\\\u2028", 128<<10)
	batch := model.Batch{Source: "ws"}
	for i := 1; i <= 6; i++ {
		batch.Messages = append(batch.Messages, model.Message{ID: strconv.Itoa(i), Content: &body})
	}
	if err := db.Apply(ctx, batch); err != nil {
		t.Fatal(err)
	}
	r, err := db.BeginArchive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	after, count, pages := "", 0, 0
	for {
		page, err := r.Page(ctx, Selection{}, after, 1000)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(page)
		if err != nil || int64(len(encoded)) > MaxArchivePageBytes {
			t.Fatalf("escaped page exceeds payload budget: %d bytes, %v", len(encoded), err)
		}
		for _, message := range page.Messages {
			count++
			if message.ID != strconv.Itoa(count) || message.Content == nil || *message.Content != body {
				t.Fatal("escaping-aware pagination lost or changed a message", message.ID)
			}
		}
		pages++
		if page.NextAfter == "" {
			break
		}
		after = page.NextAfter
	}
	if count != 6 || pages < 3 {
		t.Fatal("escaped messages were not paginated", count, pages)
	}
}

func TestArchiveRangePaginationAndSnapshot(t *testing.T) {
	db := archiveFixture(t)
	ctx := context.Background()
	batch := model.Batch{Source: "http", Users: []model.User{{ID: "9", Username: model.Ptr("Alice")}},
		Channels: []model.Channel{{ID: "2", Type: model.Ptr(11), Name: model.Ptr("Thread")}, {ID: "3", Type: model.Ptr(11)}}}
	for i := 1; i <= 310; i++ {
		created := "2026-10-01T00:00:00.000000001+09:00"
		if i == 1 {
			created = "2026-09-30T15:00:00Z"
		}
		if i == 309 {
			created = "2026-09-30T15:00:00.000000002Z"
		}
		batch.Messages = append(batch.Messages, model.Message{ID: strconv.Itoa(i), ChannelID: model.Ptr("2"), AuthorID: model.Ptr("9"),
			Timestamp: &created, Content: model.Ptr(fmt.Sprintf("message %d <@9>", i)), Revision: 10})
	}
	batch.Messages = append(batch.Messages, model.Message{ID: "999", ChannelID: model.Ptr("3"), Timestamp: model.Ptr("2026-10-01T00:00:00.000000001+09:00")})
	if err := db.Apply(ctx, batch); err != nil {
		t.Fatal(err)
	}
	selection, err := ParseSelection("2", "2026-10-01T00:00:00.000000001+09:00", "2026-09-30T15:00:00.000000002Z")
	if err != nil {
		t.Fatal(err)
	}
	r, err := db.BeginArchive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	page, err := r.Page(ctx, selection, "", 100)
	if err != nil || len(page.Messages) != 100 || page.Messages[0].ID != "2" || page.NextAfter != "101" {
		t.Fatalf("first page: %+v, %v", page, err)
	}
	if page.Messages[0].AuthorID != "9" || page.Messages[0].MentionNames["user:9"] != "Alice" {
		t.Fatal(page.Messages[0])
	}
	if err := db.Apply(ctx, model.Batch{Source: "ws", Messages: []model.Message{{ID: "150", Content: model.Ptr("new edit"), Revision: 20}}}); err != nil {
		t.Fatal(err)
	}
	count := len(page.Messages)
	for page.NextAfter != "" {
		page, err = r.Page(ctx, selection, page.NextAfter, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range page.Messages {
			if m.ID == "150" && *m.Content == "new edit" {
				t.Fatal("export snapshot mixed concurrent edits")
			}
		}
		count += len(page.Messages)
	}
	if count != 308 {
		t.Fatalf("range count: %d", count)
	}
	if _, err := db.reader.Exec("UPDATE messages SET content='unsafe'"); err == nil {
		t.Fatal("reader connection permits writes")
	}
}

func TestSelectionValidationAndFallback(t *testing.T) {
	for _, args := range [][3]string{{"../2", "", ""}, {"2", "2026-10-01", ""}, {"2", "2026-10-01T00:00:00Z", "2026-10-01T00:00:00Z"}} {
		if _, err := ParseSelection(args[0], args[1], args[2]); err == nil {
			t.Fatal(args)
		}
	}
	timestamp := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	id := strconv.FormatUint(uint64(timestamp.UnixMilli()-1420070400000)<<22, 10)
	m := ArchiveMessage{ID: id}
	creationTime(&m)
	if m.TimeSource != "snowflake" || !m.CreationTime.Equal(timestamp) {
		t.Fatal(m)
	}
	m = ArchiveMessage{ID: "99999999999999999999"}
	creationTime(&m)
	selection, _ := ParseSelection("", "2026-10-01T00:00:00Z", "")
	if selection.matches(m) || m.TimeSource != "unknown" {
		t.Fatal(m)
	}
}

func TestArchiveAndConsoleAuthorNameFallback(t *testing.T) {
	db := archiveFixture(t)
	ctx := context.Background()
	if err := db.Apply(ctx, model.Batch{Source: "ws", Users: []model.User{
		{ID: "9", DisplayName: model.Ptr(""), Username: model.Ptr("Alice")},
		{ID: "8", DisplayName: model.Ptr(""), Username: model.Ptr("")},
	}, Messages: []model.Message{
		{ID: "1", ChannelID: model.Ptr("2"), AuthorID: model.Ptr("9")},
		{ID: "2", ChannelID: model.Ptr("2"), AuthorID: model.Ptr("8")},
	}}); err != nil {
		t.Fatal(err)
	}
	r, err := db.BeginArchive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	page, err := r.Page(ctx, Selection{}, "", 100)
	if err != nil || len(page.Messages) != 2 || page.Messages[0].AuthorName != "Alice" || page.Messages[1].AuthorName != "8" {
		t.Fatal(page, err)
	}
	console, err := db.Messages(ctx, model.Filter{})
	if err != nil || len(console) != 2 || console[0].AuthorName != "8" || console[1].AuthorName != "Alice" {
		t.Fatal(console, err)
	}
}

type fileState struct {
	Hash     [32]byte
	Size     int64
	Mode     os.FileMode
	Modified time.Time
}

func runtimeState(t *testing.T, root string) map[string]fileState {
	t.Helper()
	state := map[string]fileState{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			state[path] = fileState{sha256.Sum256(b), info.Size(), info.Mode(), info.ModTime()}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestImmutableArchiveLeavesRuntimeUnchanged(t *testing.T) {
	db := archiveFixture(t)
	ctx := context.Background()
	lock, err := securefs.Acquire(db.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Apply(ctx, model.Batch{Source: "ws", Messages: []model.Message{{ID: "1", ChannelID: model.Ptr("2"), Content: model.Ptr("observed")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := securefs.LockExisting(db.Root); err == nil {
		t.Fatal("offline read accepted a running archive")
	}
	if _, err := OpenReadOnly(db.Root); err == nil {
		t.Fatal("nonempty WAL was silently ignored")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	before := runtimeState(t, db.Root)
	readLock, err := securefs.LockExisting(db.Root)
	if err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(db.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := ro.Apply(ctx, model.Batch{}); err != ErrReadOnly {
		t.Fatal("read-only store accepts entity writes", err)
	}
	if err := ro.SaveAsset(ctx, "cdn.discordapp.com", "/image.png", "image/png", []byte("new"), 1000); err != ErrReadOnly {
		t.Fatal("read-only store accepts resource writes", err)
	}
	r, err := ro.BeginArchive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err := r.Page(ctx, Selection{}, "", 100)
	if err != nil || len(page.Messages) != 1 {
		t.Fatal(page, err)
	}
	r.Close()
	ro.Close()
	readLock.Close()
	if after := runtimeState(t, db.Root); !reflect.DeepEqual(before, after) {
		t.Fatal("read-only archive changed runtime files")
	}
}
