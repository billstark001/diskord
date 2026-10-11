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
	"unicode/utf8"

	"diskord/internal/model"
	"diskord/internal/securefs"
	"diskord/internal/store"
	"diskord/internal/version"
)

const MaxBytes int64 = 8 << 30

type Options struct {
	Format      string
	Attachments bool
	SplitBytes  int64
	SourceLinks bool
}

func (o Options) Validate() error {
	if o.Format != "md" && o.Format != "jsonl" {
		return errors.New("format must be md or jsonl")
	}
	if o.SplitBytes != 0 && (o.Format != "md" || o.SplitBytes < 1<<20 || o.SplitBytes > MaxBytes) {
		return errors.New("split_bytes requires Markdown and a size from 1 MiB to 8 GiB")
	}
	return nil
}

func (o Options) ZIP() bool { return o.Attachments || o.SplitBytes > 0 }

type Manifest struct {
	Type               string          `json:"type"`
	SchemaVersion      int             `json:"schema_version"`
	ApplicationVersion string          `json:"application_version"`
	DatabaseSchema     int             `json:"database_schema"`
	ExportedAt         time.Time       `json:"exported_at"`
	Selection          store.Selection `json:"selection"`
	Format             string          `json:"format"`
	Attachments        bool            `json:"attachments"`
	SplitBytes         int64           `json:"split_bytes,omitempty"`
	Documents          []Document      `json:"documents,omitempty"`
	SourceLinks        bool            `json:"source_links"`
	Limitations        []string        `json:"limitations"`
}

type Summary struct {
	Type               string `json:"type"`
	DocumentFiles      int    `json:"document_files"`
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

type resource struct {
	path, hash string
	size       int64
}

type quotaWriter struct {
	ctx   context.Context
	w     io.Writer
	bytes int64
	limit int64
}

func (w *quotaWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.limit-w.bytes {
		return 0, errors.New("export exceeds the 8 GiB limit")
	}
	n, err := w.w.Write(p)
	w.bytes += int64(n)
	if n != len(p) && err == nil {
		err = io.ErrShortWrite
	}
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
	a := &Artifact{dir: dir, ContentType: "text/markdown; charset=utf-8"}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()
	if options.Format == "jsonl" {
		a.ContentType = "application/x-ndjson; charset=utf-8"
	}
	a.Manifest = Manifest{Type: "manifest", SchemaVersion: 1, ApplicationVersion: version.Current, DatabaseSchema: db.DatabaseSchema(),
		SplitBytes: options.SplitBytes, ExportedAt: time.Now().UTC(), Selection: selection, Format: options.Format, Attachments: options.Attachments, SourceLinks: options.Format == "md" && options.SourceLinks,
		Limitations: []string{"Only locally observed latest message states; no completeness guarantee or edit history.",
			"Replies/embeds and unobserved fields are not reconstructed. Reaction identities may be incomplete.",
			"Time range is [since, until), using creation timestamp or snowflake fallback; unknown times are excluded when bounded.",
			"Only already cached attachment images can be included. No network fetches are performed."}}
	a.Summary = Summary{Type: "summary", Complete: true}
	reader, err := db.BeginArchive(ctx)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	scopeName, err := reader.SelectionName(ctx, selection)
	if err != nil {
		return nil, err
	}
	budget := &quotaWriter{ctx: ctx, limit: MaxBytes}
	docs := &documents{dir: dir, format: options.Format, split: options.SplitBytes, budget: budget}
	docs.header = func(w io.Writer) error {
		if options.Format == "jsonl" {
			return json.NewEncoder(w).Encode(a.Manifest)
		}
		_, err := fmt.Fprintf(w, "# Observed Discord archive\n\nExported: %s\n\nThread/channel: %s\n\nInclude child channels/threads: %t\n\nGuild: %s · Scope: %s\n\nRange: [%s, %s)\n\n",
			a.Manifest.ExportedAt.Format(time.RFC3339Nano), defaultText(selection.ThreadID, "all"), selection.IncludeThreads, defaultText(selection.GuildID, "all"), defaultText(selection.Scope, "all"), timeText(selection.Since), timeText(selection.Until))
		for _, note := range a.Manifest.Limitations {
			if err == nil {
				_, err = fmt.Fprintf(w, "> %s\n\n", note)
			}
		}
		if err == nil {
			_, err = io.WriteString(w, "> Markdown line endings are normalized to LF (CRLF, CR, NEL, LS and PS); JSONL preserves stored text.\n\n")
		}
		return err
	}
	defer func() {
		if docs.file != nil {
			docs.file.Close()
		}
	}()
	if err := docs.open(); err != nil {
		return nil, err
	}
	var first, last *time.Time
	resources := map[string]resource{}
	resourcePaths := map[string]string{}
	var resourceBytes int64
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
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if options.Attachments {
				for i := range message.Attachments {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					attachment := &message.Attachments[i]
					if path, ok := resourcePaths[attachment.CachedHash]; ok {
						attachment.ExportPath = path
						continue
					}
					path, _, assetErr := reader.Asset(ctx, attachment.CachedHash)
					var info os.FileInfo
					if assetErr == nil {
						info, assetErr = os.Stat(path)
					}
					if assetErr != nil || !info.Mode().IsRegular() {
						if err := ctx.Err(); err != nil {
							return nil, err
						}
						a.Summary.MissingAttachments++
						continue
					}
					attachment.ExportPath = "attachments/" + filepath.Base(path)
					// Reserve attachment input before copying it. Oversized or grown
					// cache files must not fill staging up to the limit first.
					if info.Size() > MaxBytes-resourceBytes {
						return nil, errors.New("export exceeds the 8 GiB limit")
					}
					resourceBytes += info.Size()
					budget.limit = MaxBytes - resourceBytes
					if budget.bytes+int64(docs.writer.Buffered()) > budget.limit {
						return nil, errors.New("export exceeds the 8 GiB limit")
					}
					resources[attachment.ExportPath] = resource{path, attachment.CachedHash, info.Size()}
					resourcePaths[attachment.CachedHash] = attachment.ExportPath
					if len(resources) > 100000 {
						return nil, errors.New("export exceeds the 100000 attachment file limit")
					}
				}
			}
			if options.Format == "jsonl" {
				err = json.NewEncoder(docs.writer).Encode(struct {
					Type string `json:"type"`
					store.ArchiveMessage
				}{"message", message})
			} else {
				err = docs.message(ctx, message, options)
			}
			if err != nil {
				return nil, err
			}
			if message.CreationTime != nil {
				if first == nil || message.CreationTime.Before(*first) {
					first = message.CreationTime
				}
				if last == nil || message.CreationTime.After(*last) {
					last = message.CreationTime
				}
			}
			if options.Format == "jsonl" {
				docs.files[len(docs.files)-1].Messages++
			}
			a.Summary.Messages++
		}
		if page.NextAfter == "" {
			break
		}
		after = page.NextAfter
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Release the database before potentially expensive ZIP work or HTTP delivery.
	if err := reader.Close(); err != nil {
		return nil, err
	}
	a.Summary.AttachmentFiles = len(resources)
	a.Summary.DocumentFiles = len(docs.files)
	if options.Format == "jsonl" {
		err = json.NewEncoder(docs.writer).Encode(a.Summary)
	} else {
		_, err = fmt.Fprintf(docs.writer, "\n---\n\nExport complete: %d messages; %d document files; %d attachment files; %d missing attachments.\n", a.Summary.Messages, a.Summary.DocumentFiles, a.Summary.AttachmentFiles, a.Summary.MissingAttachments)
	}
	if err != nil {
		return nil, err
	}
	if err = docs.finish(); err != nil {
		return nil, err
	}
	stem := filenameStem(scopeName, selection, first, last)
	if err = docs.rename(stem); err != nil {
		return nil, err
	}
	a.Manifest.Documents = docs.files
	a.Filename = docs.files[0].Name
	a.Path = filepath.Join(dir, a.Filename)
	if options.ZIP() {
		if err = a.bundle(ctx, resources, budget.bytes, stem); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Artifact) bundle(ctx context.Context, resources map[string]resource, used int64, stem string) error {
	path := filepath.Join(a.dir, "archive.zip")
	f, err := securefs.NewFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	z := zip.NewWriter(&quotaWriter{ctx: ctx, w: f, limit: MaxBytes})
	defer z.Close()
	for _, doc := range a.Manifest.Documents {
		entry, err := z.Create(doc.Name)
		if err != nil {
			return err
		}
		if err := copyResource(ctx, entry, filepath.Join(a.dir, doc.Name), "", MaxBytes); err != nil {
			return err
		}
	}
	entry, err := z.Create("manifest.json")
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
		counter := &quotaWriter{ctx: ctx, w: entry, bytes: used, limit: MaxBytes}
		if err := copyResource(ctx, counter, r.path, r.hash, r.size); err != nil {
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
	a.Path, a.Filename, a.ContentType = path, stem+".zip", "application/zip"
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
			if written, e := w.Write(buffer[:n]); e != nil {
				return e
			} else if written != n {
				return io.ErrShortWrite
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
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

var lineEndings = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u0085", "\n", "\u2028", "\n", "\u2029", "\n")
var markdownEscapes = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`", "\n", " ")

func markdownText(s string) string {
	return markdownEscapes.Replace(lineEndings.Replace(s))
}

func discordLink(label, target string) string {
	if target == "" {
		return markdownText(label)
	}
	return "[" + markdownText(label) + "](" + target + ")"
}

func markdown(ctx context.Context, w io.Writer, m store.ArchiveMessage, options Options) error {
	text := bufio.NewWriter(w)
	fmt.Fprintf(text, "## %s — %s\n\n",
		markdownText(defaultText(m.AuthorName, "Unknown author")), timeText(m.CreationTime))
	if options.SourceLinks {
		channelURL, messageURL, authorURL := "", "", ""
		owner := m.GuildID
		if owner == "" && (m.ChannelKind == 1 || m.ChannelKind == 3) {
			owner = "@me"
		}
		if (model.ID(owner) || owner == "@me") && model.ID(m.ChannelID) {
			channelURL = "https://discord.com/channels/" + owner + "/" + m.ChannelID
			if model.ID(m.ID) {
				messageURL = channelURL + "/" + m.ID
			}
		}
		if model.ID(m.AuthorID) {
			authorURL = "https://discord.com/users/" + m.AuthorID
		}
		channelLabel := defaultText(m.ChannelName, "Unknown channel")
		if m.ChannelID != "" {
			channelLabel += " (" + m.ChannelID + ")"
		}
		fmt.Fprintf(text, "Message: %s · Channel: %s · Author: %s\n\n",
			discordLink(m.ID, messageURL), discordLink(channelLabel, channelURL), discordLink(defaultText(m.AuthorID, "Unknown author"), authorURL))
	}
	if m.Deleted {
		text.WriteString("**Deleted; last observed content retained.**\n\n")
	}
	if m.EditedAt != nil {
		fmt.Fprintf(text, "Edited: %s\n\n", markdownText(*m.EditedAt))
	}
	if m.Content == nil {
		text.WriteString("Content was not observed.\n\n")
	} else {
		// Fenced text preserves message content apart from normalized line endings without making an export
		// renderer execute raw HTML or fetch embedded remote Markdown images.
		longest, run := 0, 0
		for i := 0; i < len(*m.Content); i++ {
			if i%32768 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			if (*m.Content)[i] == '`' {
				run++
				longest = max(longest, run)
			} else {
				run = 0
			}
		}
		fence := strings.Repeat("`", max(3, longest+1))
		fmt.Fprintf(text, "%stext\n", fence)
		// Avoid a second full-size body in a formatting buffer; check cancellation
		// between bounded writes even for an unusually long observed message.
		for offset := 0; offset < len(*m.Content); {
			if err := ctx.Err(); err != nil {
				return err
			}
			end := min(offset+32768, len(*m.Content))
			if end < len(*m.Content) {
				for back := 0; back < utf8.UTFMax-1 && end > offset && !utf8.RuneStart((*m.Content)[end]); back++ {
					end--
				}
				if end > offset && (*m.Content)[end-1] == '\r' && (*m.Content)[end] == '\n' {
					end--
				}
			}
			if _, err := text.WriteString(lineEndings.Replace((*m.Content)[offset:end])); err != nil {
				return err
			}
			offset = end
		}
		fmt.Fprintf(text, "\n%s\n\n", fence)
	}
	for _, a := range m.Attachments {
		if a.ExportPath != "" {
			fmt.Fprintf(text, "![%s](%s)\n\nAttachment: %s (%d bytes)\n\n", markdownText(a.Name), a.ExportPath, markdownText(a.Name), a.Size)
		} else {
			status := "metadata only"
			if options.Attachments {
				status = "unavailable in local cache"
			}
			fmt.Fprintf(text, "Attachment: %s (%d bytes; %s)\n\n", markdownText(a.Name), a.Size, status)
		}
	}
	for _, reaction := range m.Reactions {
		fmt.Fprintf(text, "Reaction: %s × %d; observed users: %s; unknown: %d\n\n", markdownText(reaction.EmojiName), reaction.Count, markdownText(strings.Join(reaction.KnownUsers, ", ")), reaction.UnknownCount)
	}
	keys := make([]string, 0, len(m.MentionNames))
	for key := range m.MentionNames {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(text, "Mention: %s = %s\n\n", markdownText(key), markdownText(m.MentionNames[key]))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return text.Flush()
}
