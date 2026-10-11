package archive

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"diskord/internal/model"
	"diskord/internal/securefs"
	"diskord/internal/store"
)

func exportFixture(t *testing.T) (*store.Store, string) {
	t.Helper()
	root := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
		t.Setenv(key, os.Getenv(key))
	}
	if err := securefs.Prepare(root); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	content := "```\n<script>alert(1)</script>\n![remote](https://evil.test/image)\n````\n<@9>"
	attachments := []model.Attachment{{ID: "10", MessageID: "1", Name: "../../danger.png", Host: "cdn.discordapp.com", Path: "/attachments/2/10/image.png", Size: 4},
		{ID: "11", MessageID: "1", Name: "missing.pdf", Host: "cdn.discordapp.com", Path: "/attachments/2/11/missing.pdf"},
		{ID: "12", MessageID: "1", Name: "again.png", Host: "media.discordapp.net", Path: "/attachments/2/10/image.png", Size: 4}}
	batch := model.Batch{Source: "ws", Users: []model.User{{ID: "9", Username: model.Ptr("A <script>\n# name")}},
		Messages: []model.Message{{ID: "1", ChannelID: model.Ptr("2"), AuthorID: model.Ptr("9"), Content: &content, Revision: 10, HasAttachments: true, Attachments: attachments, HasReactions: true, Reactions: []model.Reaction{{EmojiName: "👍", Count: 2}}},
			{ID: "2", ChannelID: model.Ptr("2"), Deleted: model.Ptr(true), Revision: 10}}}
	if err := db.Apply(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAsset(context.Background(), "cdn.discordapp.com", attachments[0].Path, "image/png", []byte("test"), 1000); err != nil {
		t.Fatal(err)
	}
	return db, content
}

func TestMarkdownMediaAndOptionalDiscordLinks(t *testing.T) {
	db, original := exportFixture(t)
	ctx := context.Background()
	if err := db.Apply(ctx, model.Batch{Source: "ws", Channels: []model.Channel{{ID: "2", GuildID: model.Ptr("8"), Type: model.Ptr(0), Name: model.Ptr("general [chat]")}}}); err != nil {
		t.Fatal(err)
	}
	for _, links := range []bool{true, false} {
		a, err := Build(ctx, db, db.Root, store.Selection{}, Options{Format: "md", Attachments: true, SourceLinks: links})
		if err != nil {
			t.Fatal(err)
		}
		z, err := zip.OpenReader(a.Path)
		if err != nil {
			t.Fatal(err)
		}
		var markdown string
		imagePath := ""
		for _, entry := range z.File {
			if strings.HasPrefix(entry.Name, "attachments/") {
				imagePath = entry.Name
			}
			if strings.HasSuffix(entry.Name, ".md") {
				r, err := entry.Open()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(r)
				r.Close()
				if err != nil {
					t.Fatal(err)
				}
				markdown = string(data)
			}
		}
		z.Close()
		a.Close()
		if imagePath == "" || !strings.Contains(markdown, "![../../danger.png]("+imagePath+")") || !strings.Contains(markdown, "![again.png]("+imagePath+")") || !strings.Contains(markdown, original) {
			t.Fatal("embedded images do not resolve to the bundled file, or content was changed", markdown)
		}
		for _, expected := range []string{
			"Message: [1](https://discord.com/channels/8/2/1)",
			"Channel: [general \\[chat\\] (2)](https://discord.com/channels/8/2)",
			"Author: [9](https://discord.com/users/9)",
		} {
			if strings.Contains(markdown, expected) != links {
				t.Fatalf("source_links=%t: %s", links, markdown)
			}
		}
		if !links && strings.Contains(markdown, "Message:") {
			t.Fatal("optional source row was retained")
		}
		if a.Manifest.SourceLinks != links {
			t.Fatal(a.Manifest)
		}
	}
}

func TestMarkdownLinksRequireObservedOwnership(t *testing.T) {
	for _, kind := range []int{1, 3, -1, 11} {
		var out strings.Builder
		m := store.ArchiveMessage{ID: "10", ChannelID: "2", ChannelKind: kind, AuthorID: "9"}
		if err := markdown(context.Background(), &out, m, Options{SourceLinks: true}); err != nil {
			t.Fatal(err)
		}
		isDM := kind == 1 || kind == 3
		if strings.Contains(out.String(), "https://discord.com/channels/@me/2/10") != isDM || !strings.Contains(out.String(), "https://discord.com/users/9") {
			t.Fatal(kind, out.String())
		}
	}
}

func TestOversizedCachedResourceFailsBeforeCopying(t *testing.T) {
	db, _ := exportFixture(t)
	files, err := os.ReadDir(filepath.Join(db.Root, "resources"))
	if err != nil {
		t.Fatal(err)
	}
	// A sparse corrupt/grown cache file exercises preflight without allocating
	// or copying more than eight GiB into staging.
	if err := os.Truncate(filepath.Join(db.Root, "resources", files[0].Name()), MaxBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), db, db.Root, store.Selection{}, Options{Format: "md", Attachments: true}); err == nil || !strings.Contains(err.Error(), "8 GiB") {
		t.Fatal(err)
	}
	files, err = os.ReadDir(filepath.Join(db.Root, "tmp"))
	if err != nil || len(files) != 0 {
		t.Fatal("oversized export leaked staging", files, err)
	}
}

type cancelingWriter struct {
	cancel context.CancelFunc
	bytes  int
}

func (w *cancelingWriter) Write(p []byte) (int, error) {
	w.bytes += len(p)
	w.cancel()
	return len(p), nil
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestResourceCopyCancellationAndWriteFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resource")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 2<<20)), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &cancelingWriter{cancel: cancel}
	if err := copyResource(ctx, w, path, "", MaxBytes); !errors.Is(err, context.Canceled) || w.bytes > 32768 {
		t.Fatal("copy did not stop after cancellation", w.bytes, err)
	}
	if err := copyResource(context.Background(), shortWriter{}, path, "", MaxBytes); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short write was ignored", err)
	}
	budget := &quotaWriter{ctx: context.Background(), w: io.Discard, limit: 10}
	if _, err := budget.Write(make([]byte, 11)); err == nil {
		t.Fatal("staging budget was ignored")
	}
	budget = &quotaWriter{ctx: ctx, w: io.Discard, limit: MaxBytes}
	if _, err := budget.Write([]byte("footer")); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled packaging wrote more output", err)
	}
}

func TestJSONLBundleDedupMissingAndTombstones(t *testing.T) {
	db, original := exportFixture(t)
	a, err := Build(context.Background(), db, db.Root, store.Selection{}, Options{Format: "jsonl", Attachments: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := a.dir
	defer a.Close()
	if a.Summary.Messages != 2 || a.Summary.AttachmentFiles != 1 || a.Summary.MissingAttachments != 1 || !a.Summary.Complete {
		t.Fatal(a.Summary)
	}
	z, err := zip.OpenReader(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if len(z.File) != 3 {
		t.Fatal(z.File)
	}
	for _, f := range z.File {
		if strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") {
			t.Fatal("unsafe ZIP name", f.Name)
		}
		if !strings.HasSuffix(f.Name, ".jsonl") {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		dec := json.NewDecoder(r)
		var manifest Manifest
		if err := dec.Decode(&manifest); err != nil || manifest.Type != "manifest" || manifest.SchemaVersion != 1 {
			t.Fatal(manifest, err)
		}
		var record struct {
			Type string `json:"type"`
			store.ArchiveMessage
		}
		if err := dec.Decode(&record); err != nil || record.Type != "message" || *record.Content != original || record.AuthorID != "9" {
			t.Fatal(record, err)
		}
		if record.Attachments[0].ExportPath == "" || record.Attachments[0].ExportPath != record.Attachments[2].ExportPath {
			t.Fatal(record.Attachments)
		}
		if err := dec.Decode(&record); err != nil || !record.Deleted || record.Content != nil {
			t.Fatal(record, err)
		}
		var summary Summary
		if err := dec.Decode(&summary); err != nil || !summary.Complete || summary.Messages != 2 {
			t.Fatal(summary, err)
		}
		if err := dec.Decode(&summary); err != io.EOF {
			t.Fatal("extra JSONL records", err)
		}
	}
	z.Close()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("staged archive leaked")
	}
}

func TestMarkdownFencesAndCanceledExportCleanup(t *testing.T) {
	db, original := exportFixture(t)
	a, err := Build(context.Background(), db, db.Root, store.Selection{}, Options{Format: "md"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	a.Close()
	if !strings.Contains(string(b), "`````text\n"+original+"\n`````") || !strings.Contains(string(b), "&lt;script&gt;") || !strings.Contains(string(b), "2 messages") {
		t.Fatal(string(b))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, db, db.Root, store.Selection{}, Options{Format: "jsonl"}); err == nil {
		t.Fatal("canceled export succeeded")
	}
	entries, err := os.ReadDir(filepath.Join(db.Root, "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatal("temporary export data leaked", entries, err)
	}
}

func TestCorruptCachedAttachmentFailsExport(t *testing.T) {
	db, _ := exportFixture(t)
	files, err := os.ReadDir(filepath.Join(db.Root, "resources"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(db.Root, "resources", files[0].Name()), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), db, db.Root, store.Selection{}, Options{Format: "jsonl", Attachments: true}); err == nil || !strings.Contains(err.Error(), "hash verification") {
		t.Fatal(err)
	}
}
