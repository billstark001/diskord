// Package archive stages complete exports before exposing them to a caller.
package archive

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"diskord/internal/securefs"
	"diskord/internal/store"
	"diskord/internal/version"
)

const MaxBytes int64 = 8 << 30

type Options struct {
	Format      string
	Attachments bool
}

func (o Options) Validate() error {
	if o.Format != "md" && o.Format != "jsonl" {
		return errors.New("format must be md or jsonl")
	}
	return nil
}

type Manifest struct {
	Type               string          `json:"type"`
	SchemaVersion      int             `json:"schema_version"`
	ApplicationVersion string          `json:"application_version"`
	DatabaseSchema     int             `json:"database_schema"`
	ExportedAt         time.Time       `json:"exported_at"`
	Selection          store.Selection `json:"selection"`
	Format             string          `json:"format"`
	Attachments        bool            `json:"attachments"`
	Limitations        []string        `json:"limitations"`
}

type Summary struct {
	Type               string `json:"type"`
	Messages           int64  `json:"messages"`
	AttachmentFiles    int    `json:"attachment_files"`
	MissingAttachments int64  `json:"missing_attachments"`
	Complete           bool   `json:"complete"`
}

type Artifact struct {
	Path, Filename, ContentType string
	Manifest                    Manifest
	Summary                     Summary
	dir                         string
}

func (a *Artifact) Close() error { return os.RemoveAll(a.dir) }

type resource struct{ path, hash string }

type quotaWriter struct {
	w     io.Writer
	bytes int64
}

func (w *quotaWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > MaxBytes-w.bytes {
		return 0, errors.New("export exceeds the 8 GiB limit")
	}
	n, err := w.w.Write(p)
	w.bytes += int64(n)
	return n, err
}

// Build writes only beneath stagingRoot/tmp. Online callers use the runtime;
// offline callers supply a private staging root beside their explicit output.
func Build(ctx context.Context, db *store.Store, stagingRoot string, selection store.Selection, options Options) (_ *Artifact, err error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	parent, err := securefs.Dir(stagingRoot, "tmp")
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(parent, "export-")
	if err != nil {
		return nil, err
	}
	if err := securefs.Private(dir); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	a := &Artifact{dir: dir, Filename: "messages." + options.Format, ContentType: "text/markdown; charset=utf-8"}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()
	if options.Format == "jsonl" {
		a.ContentType = "application/x-ndjson; charset=utf-8"
	}
	a.Manifest = Manifest{Type: "manifest", SchemaVersion: 1, ApplicationVersion: version.Current, DatabaseSchema: db.DatabaseSchema(),
		ExportedAt: time.Now().UTC(), Selection: selection, Format: options.Format, Attachments: options.Attachments,
		Limitations: []string{"Only locally observed latest message states; no completeness guarantee or edit history.",
			"Replies/embeds and unobserved fields are not reconstructed. Reaction identities may be incomplete.",
			"Time range is [since, until), using creation timestamp or snowflake fallback; unknown times are excluded when bounded.",
			"Only already cached attachment images can be included. No network fetches are performed."}}
	a.Summary = Summary{Type: "summary", Complete: true}
	a.Path = filepath.Join(dir, a.Filename)
	file, err := securefs.NewFile(a.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	budget := &quotaWriter{w: file}
	w := bufio.NewWriter(budget)
	enc := json.NewEncoder(w)
	if options.Format == "jsonl" {
		err = enc.Encode(a.Manifest)
	} else {
		_, err = fmt.Fprintf(w, "# Observed Discord archive\n\nExported: %s\n\nThread/channel: %s\n\nInclude child channels/threads: %t\n\nGuild: %s · Scope: %s\n\nRange: [%s, %s)\n\n",
			a.Manifest.ExportedAt.Format(time.RFC3339Nano), defaultText(selection.ThreadID, "all"), selection.IncludeThreads, defaultText(selection.GuildID, "all"), defaultText(selection.Scope, "all"), timeText(selection.Since), timeText(selection.Until))
		for _, note := range a.Manifest.Limitations {
			if err == nil {
				_, err = fmt.Fprintf(w, "> %s\n\n", note)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	reader, err := db.BeginArchive(ctx)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	resources := map[string]resource{}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := reader.Page(ctx, selection, after, 100)
		if err != nil {
			return nil, err
		}
		for _, message := range page.Messages {
			if options.Attachments {
				for i := range message.Attachments {
					attachment := &message.Attachments[i]
					path, _, assetErr := reader.Asset(ctx, attachment.CachedHash)
					var info os.FileInfo
					if assetErr == nil {
						info, assetErr = os.Stat(path)
					}
					if assetErr != nil || !info.Mode().IsRegular() {
						a.Summary.MissingAttachments++
						continue
					}
					attachment.ExportPath = "attachments/" + filepath.Base(path)
					resources[attachment.ExportPath] = resource{path, attachment.CachedHash}
					if len(resources) > 100000 {
						return nil, errors.New("export exceeds the 100000 attachment file limit")
					}
				}
			}
			if options.Format == "jsonl" {
				err = enc.Encode(struct {
					Type string `json:"type"`
					store.ArchiveMessage
				}{"message", message})
			} else {
				err = markdown(w, message, options.Attachments)
			}
			if err != nil {
				return nil, err
			}
			a.Summary.Messages++
		}
		if page.NextAfter == "" {
			break
		}
		after = page.NextAfter
	}
	// Release the database before potentially expensive ZIP work or HTTP delivery.
	if err := reader.Close(); err != nil {
		return nil, err
	}
	a.Summary.AttachmentFiles = len(resources)
	if options.Format == "jsonl" {
		err = enc.Encode(a.Summary)
	} else {
		_, err = fmt.Fprintf(w, "\n---\n\nExport complete: %d messages; %d attachment files; %d missing attachments.\n", a.Summary.Messages, a.Summary.AttachmentFiles, a.Summary.MissingAttachments)
	}
	if err != nil {
		return nil, err
	}
	if err = w.Flush(); err != nil {
		return nil, err
	}
	if err = file.Sync(); err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	if options.Attachments {
		if err = a.bundle(ctx, resources, budget.bytes); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *Artifact) bundle(ctx context.Context, resources map[string]resource, used int64) error {
	path := filepath.Join(a.dir, "archive.zip")
	f, err := securefs.NewFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	z := zip.NewWriter(&quotaWriter{w: f})
	defer z.Close()
	entry, err := z.Create(a.Filename)
	if err != nil {
		return err
	}
	if err := copyResource(ctx, entry, a.Path, "", MaxBytes); err != nil {
		return err
	}
	entry, err = z.Create("manifest.json")
	if err != nil {
		return err
	}
	if err := json.NewEncoder(entry).Encode(struct {
		Manifest
		Summary Summary `json:"summary"`
	}{a.Manifest, a.Summary}); err != nil {
		return err
	}
	names := make([]string, 0, len(resources))
	for name := range resources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		r := resources[name]
		// Cached image formats are already compressed. Store their original bytes
		// so packaging does not spend its preparation deadline recompressing them.
		entry, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			return err
		}
		counter := &quotaWriter{w: entry, bytes: used}
		if err := copyResource(ctx, counter, r.path, r.hash, MaxBytes-used); err != nil {
			return err
		}
		used = counter.bytes
	}
	if err := z.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	a.Path, a.Filename, a.ContentType = path, "archive.zip", "application/zip"
	return nil
}

func copyResource(ctx context.Context, w io.Writer, path, hash string, limit int64) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.New("export resource became unavailable")
	}
	defer f.Close()
	digest := sha256.New()
	if hash != "" {
		w = io.MultiWriter(w, digest)
	}
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := f.Read(buffer)
		total += int64(n)
		if total > limit {
			return errors.New("export exceeds the 8 GiB limit")
		}
		if n > 0 {
			if _, e := w.Write(buffer[:n]); e != nil {
				return e
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if hash != "" && hex.EncodeToString(digest.Sum(nil)) != hash {
		return errors.New("cached attachment failed content hash verification")
	}
	return nil
}

func defaultText(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func timeText(t *time.Time) string {
	if t == nil {
		return "unbounded"
	}
	return t.Format(time.RFC3339Nano)
}

func markdownText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`", "\n", " ", "\r", " ").Replace(s)
}

func markdown(w io.Writer, m store.ArchiveMessage, attachments bool) error {
	var text strings.Builder
	fmt.Fprintf(&text, "## %s — %s\n\nMessage: %s · Channel: %s (%s) · Author: %s\n\n",
		markdownText(defaultText(m.AuthorName, "Unknown author")), timeText(m.CreationTime), m.ID,
		markdownText(m.ChannelName), m.ChannelID, m.AuthorID)
	if m.Deleted {
		text.WriteString("**Deleted; last observed content retained.**\n\n")
	}
	if m.EditedAt != nil {
		fmt.Fprintf(&text, "Edited: %s\n\n", markdownText(*m.EditedAt))
	}
	if m.Content == nil {
		text.WriteString("Content was not observed.\n\n")
	} else {
		// Verbatim fenced text preserves message content without making an export
		// renderer execute raw HTML or fetch embedded remote Markdown images.
		longest, run := 0, 0
		for _, c := range *m.Content {
			if c == '`' {
				run++
				longest = max(longest, run)
			} else {
				run = 0
			}
		}
		fence := strings.Repeat("`", max(3, longest+1))
		fmt.Fprintf(&text, "%stext\n%s\n%s\n\n", fence, *m.Content, fence)
	}
	for _, a := range m.Attachments {
		if a.ExportPath != "" {
			fmt.Fprintf(&text, "Attachment: [%s](%s) (%d bytes)\n\n", markdownText(a.Name), a.ExportPath, a.Size)
		} else {
			status := "metadata only"
			if attachments {
				status = "unavailable in local cache"
			}
			fmt.Fprintf(&text, "Attachment: %s (%d bytes; %s)\n\n", markdownText(a.Name), a.Size, status)
		}
	}
	for _, reaction := range m.Reactions {
		fmt.Fprintf(&text, "Reaction: %s × %d; observed users: %s; unknown: %d\n\n", markdownText(reaction.EmojiName), reaction.Count, markdownText(strings.Join(reaction.KnownUsers, ", ")), reaction.UnknownCount)
	}
	keys := make([]string, 0, len(m.MentionNames))
	for key := range m.MentionNames {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&text, "Mention: %s = %s\n\n", markdownText(key), markdownText(m.MentionNames[key]))
	}
	_, err := io.WriteString(w, text.String())
	return err
}
