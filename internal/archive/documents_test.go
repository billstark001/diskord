package archive

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"diskord/internal/model"
	"diskord/internal/store"
)

func TestMarkdownNormalizesAllLineSeparatorsAndChunkBoundaries(t *testing.T) {
	// Put Unicode separators and CRLF pairs across the renderer's chunk edge.
	for _, separator := range []string{"\r\n", "\r", "\u0085", "\u2028", "\u2029"} {
		for _, padding := range []int{32766, 32767, 32768} {
			original := strings.Repeat("x", padding) + separator + "tail" + separator + "end"
			var out strings.Builder
			m := store.ArchiveMessage{Content: &original, AuthorName: "name\u2028next", ChannelName: "channel\u2029next", Attachments: []store.ArchiveAttachment{{Name: "file\u0085name", ExportPath: "attachments/test.png"}}}
			if err := markdown(context.Background(), &out, m, Options{SourceLinks: true}); err != nil {
				t.Fatal(err)
			}
			got := out.String()
			if strings.ContainsAny(got, "\r\u0085\u2028\u2029") || !utf8.ValidString(got) || !strings.Contains(got, strings.Repeat("x", padding)+"\ntail\nend") {
				t.Fatalf("separator %q at %d not normalized", separator, padding)
			}
			if *m.Content != original {
				t.Fatal("stored text mutated")
			}
		}
	}
	db, _ := exportFixture(t)
	original := "A\r\nB\rC\u0085D\u2028E\u2029F"
	if err := db.Apply(context.Background(), model.Batch{Source: "ws", Messages: []model.Message{{ID: "1", Content: &original, Revision: 20}}}); err != nil {
		t.Fatal(err)
	}
	a, err := Build(context.Background(), db, db.Root, store.Selection{}, Options{Format: "jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	f, err := os.Open(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	var record store.ArchiveMessage
	if err := decoder.Decode(&record); err != nil || record.Content == nil || *record.Content != original {
		t.Fatal("JSONL changed stored line separators", record, err)
	}
}

func TestSplitMarkdownAndAttachmentsShareOneZIP(t *testing.T) {
	db, _ := exportFixture(t)
	ctx := context.Background()
	batch := model.Batch{Source: "ws", Channels: []model.Channel{{ID: "2", GuildID: model.Ptr("8"), Type: model.Ptr(0), Name: model.Ptr("讨论/频道\u2028../unsafe")}}}
	for i, size := range []int{700 << 10, 700 << 10, 1300 << 10, 16} {
		content := fmt.Sprintf("BEGIN-%d\n", i) + strings.Repeat("x", size) + fmt.Sprintf("\nEND-%d", i)
		m := model.Message{ID: fmt.Sprint(100 + i), ChannelID: model.Ptr("2"), AuthorID: model.Ptr("9"), Content: &content, Timestamp: model.Ptr(fmt.Sprintf("2026-10-0%dT00:00:00Z", i+1)), HasAttachments: true, Attachments: []model.Attachment{{ID: fmt.Sprint(200 + i), Name: "again.png", Host: "cdn.discordapp.com", Path: "/attachments/2/10/image.png", Size: 4}}}
		batch.Messages = append(batch.Messages, m)
	}
	if err := db.Apply(ctx, batch); err != nil {
		t.Fatal(err)
	}
	for _, attachments := range []bool{false, true} {
		selection, err := store.ParseArchiveSelection("2", "", "", "2014-01-01T00:00:00Z", "2027-01-01T00:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		a, err := Build(ctx, db, db.Root, selection, Options{Format: "md", Attachments: attachments, SourceLinks: true, SplitBytes: 1 << 20})
		if err != nil {
			t.Fatal(err)
		}
		if a.ContentType != "application/zip" || !strings.HasSuffix(a.Filename, ".zip") || !strings.Contains(a.Filename, "讨论-频道") || !strings.Contains(a.Filename, "_from-2014-01-01T000000Z_until-2027-01-01T000000Z") || strings.ContainsAny(a.Filename, "/\\\r\u2028\u2029") {
			t.Fatal(a.Filename, a.ContentType)
		}
		z, err := zip.OpenReader(a.Path)
		if err != nil {
			t.Fatal(err)
		}
		var combined strings.Builder
		var docs []string
		var files []Document
		images := map[string]bool{}
		var manifest Manifest
		for _, entry := range z.File {
			if strings.HasSuffix(entry.Name, ".zip") {
				t.Fatal("nested ZIP", entry.Name)
			}
			r, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case strings.HasSuffix(entry.Name, ".md"):
				docs = append(docs, entry.Name)
				combined.Write(b)
				if !strings.Contains(string(b), "# Observed Discord archive") || !strings.Contains(string(b), "Part complete:") {
					t.Fatal("part lacks context or footer", entry.Name)
				}
				files = append(files, Document{Name: entry.Name, Bytes: int64(len(b))})
			case strings.HasPrefix(entry.Name, "attachments/"):
				images[entry.Name] = true
			case entry.Name == "manifest.json":
				if err := json.Unmarshal(b, &manifest); err != nil {
					t.Fatal(err)
				}
			}
		}
		z.Close()
		a.Close()
		if len(docs) != 4 || a.Summary.DocumentFiles != 4 || len(manifest.Documents) != 4 || a.Summary.Messages != 6 || manifest.SplitBytes != 1<<20 {
			t.Fatal(docs, a.Summary, manifest)
		}
		var counted int64
		for i, doc := range manifest.Documents {
			counted += doc.Messages
			if doc.Name != docs[i] || doc.Bytes != files[i].Bytes || !strings.HasSuffix(doc.Name, fmt.Sprintf("_part-%05d.md", i+1)) {
				t.Fatal(doc, files[i])
			}
			if i != 2 && doc.Bytes > 1<<20 {
				t.Fatal("ordinary part exceeds target", doc)
			}
			if i == 2 && doc.Messages != 1 {
				t.Fatal("oversized message not isolated", doc)
			}
		}
		if counted != a.Summary.Messages {
			t.Fatal(counted, a.Summary)
		}
		for i := 0; i < 4; i++ {
			if strings.Count(combined.String(), fmt.Sprintf("BEGIN-%d", i)) != 1 || strings.Count(combined.String(), fmt.Sprintf("END-%d", i)) != 1 {
				t.Fatal("message lost, duplicated, or truncated", i)
			}
		}
		if attachments {
			if len(images) != 1 {
				t.Fatal("resources not deduplicated", images)
			}
			for image := range images {
				if strings.Count(combined.String(), "]("+image+")") != 6 {
					t.Fatal("part image links do not resolve", image)
				}
			}
		} else if len(images) != 0 || strings.Contains(combined.String(), "](attachments/") {
			t.Fatal("disabled resources embedded")
		}
	}
}

func TestExportFilenamesAndSplitValidation(t *testing.T) {
	first := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	last := first.Add(48 * time.Hour)
	stem := filenameStem(strings.Repeat("频道", 100)+"../", store.Selection{ThreadID: "123"}, &first, &last)
	if !utf8.ValidString(stem) || len(stem) > 200 || strings.ContainsAny(stem, "/\\:<>\r\u2028\u2029") || !strings.Contains(stem, "-123_from-") || !strings.Contains(stem, "_through-2026-10-03") {
		t.Fatal(stem)
	}
	if got := filenameStem("", store.Selection{}, nil, nil); got != "diskord-all-messages_undated" {
		t.Fatal(got)
	}
	for _, tt := range []struct {
		selection store.Selection
		want      string
	}{
		{store.Selection{ThreadID: "123"}, "diskord-channel-123_undated"},
		{store.Selection{GuildID: "456"}, "diskord-guild-456_undated"},
		{store.Selection{Scope: "dm"}, "diskord-private-conversations_undated"},
	} {
		if got := filenameStem("🎉/../", tt.selection, nil, nil); got != tt.want {
			t.Fatalf("unsafe-only label misrepresents selected scope: %s, want %s", got, tt.want)
		}
	}
	for _, o := range []Options{{Format: "jsonl", SplitBytes: 1 << 20}, {Format: "md", SplitBytes: -1}, {Format: "md", SplitBytes: 1}, {Format: "md", SplitBytes: MaxBytes + 1}} {
		if o.Validate() == nil {
			t.Fatal("invalid split accepted", o)
		}
	}
	for _, size := range []int64{0, 1 << 20, MaxBytes} {
		if err := (Options{Format: "md", SplitBytes: size}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	db, _ := exportFixture(t)
	a, err := Build(context.Background(), db, db.Root, store.Selection{ThreadID: "999"}, Options{Format: "md", SplitBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Summary.Messages != 0 || a.Summary.DocumentFiles != 1 || !strings.Contains(a.Filename, "channel-999_undated.zip") {
		t.Fatal(a.Filename, a.Summary)
	}
	if _, err := os.Stat(filepath.Join(a.dir, a.Manifest.Documents[0].Name)); err != nil {
		t.Fatal(err)
	}
}
