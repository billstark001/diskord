package archive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"diskord/internal/securefs"
	"diskord/internal/store"
)

// Split boundaries are between whole messages. The target size is soft for
// a single oversized message, headers and the terminal summary.
type Document struct {
	Name     string `json:"name"`
	Messages int64  `json:"messages"`
	Bytes    int64  `json:"bytes"`
}

type documents struct {
	dir    string
	format string
	split  int64
	header func(io.Writer) error
	budget *quotaWriter
	file   *os.File
	writer *bufio.Writer
	files  []Document
	start  int64
}

func (d *documents) open() error {
	if len(d.files) >= 10000 {
		return errors.New("export exceeds the 10000 document limit")
	}
	name := fmt.Sprintf("part-%05d.%s", len(d.files)+1, d.format)
	f, err := securefs.NewFile(filepath.Join(d.dir, name))
	if err != nil {
		return err
	}
	d.file = f
	d.start = d.budget.bytes
	d.budget.w = f
	d.writer = bufio.NewWriter(d.budget)
	d.files = append(d.files, Document{Name: name})
	return d.header(d.writer)
}

func (d *documents) finish() error {
	if d.file == nil {
		return nil
	}
	if d.split > 0 {
		if _, err := fmt.Fprintf(d.writer, "\n---\n\nPart complete: %d messages.\n", d.files[len(d.files)-1].Messages); err != nil {
			return err
		}
	}
	if err := d.writer.Flush(); err != nil {
		return err
	}
	if err := d.file.Sync(); err != nil {
		return err
	}
	if err := d.file.Close(); err != nil {
		return err
	}
	d.file = nil
	d.files[len(d.files)-1].Bytes = d.budget.bytes - d.start
	return nil
}

func (d *documents) message(ctx context.Context, m store.ArchiveMessage, o Options) error {
	if d.split > 0 && d.files[len(d.files)-1].Messages > 0 {
		// Count using the same bounded renderer, without keeping a second body in
		// memory or guessing how escaping and line normalization affect its size.
		count := &quotaWriter{ctx: ctx, w: io.Discard, limit: MaxBytes}
		if err := markdown(ctx, count, m, o); err != nil {
			return err
		}
		size := d.budget.bytes - d.start + int64(d.writer.Buffered())
		if size+count.bytes > d.split {
			if err := d.finish(); err != nil {
				return err
			}
			if err := d.open(); err != nil {
				return err
			}
		}
	}
	if err := markdown(ctx, d.writer, m, o); err != nil {
		return err
	}
	d.files[len(d.files)-1].Messages++
	return nil
}

func filenamePart(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len()+len(string(r)) > 80 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-_")
}

func filenameStem(scope string, s store.Selection, first, last *time.Time) string {
	name := filenamePart(scope)
	if name == "" {
		switch {
		case s.ThreadID != "":
			name = "channel"
		case s.GuildID != "":
			name = "guild"
		case s.Scope == "dm":
			name = "private-conversations"
		default:
			name = "all-messages"
		}
	}
	if s.ThreadID != "" {
		name += "-" + s.ThreadID
	} else if s.GuildID != "" {
		name += "-" + s.GuildID
	}
	date := func(t *time.Time) string { return t.UTC().Format("2006-01-02T150405.999999999Z") }
	start, end := first, last
	endLabel := "through"
	if s.Since != nil {
		start = s.Since
	}
	if s.Until != nil {
		end = s.Until
		endLabel = "until"
	}
	stem := "diskord-" + name
	if start != nil {
		stem += "_from-" + date(start)
	}
	if end != nil {
		stem += "_" + endLabel + "-" + date(end)
	}
	if start == nil && end == nil {
		stem += "_undated"
	}
	return stem
}

func (d *documents) rename(stem string) error {
	for i := range d.files {
		name := stem + "." + d.format
		if d.split > 0 {
			name = fmt.Sprintf("%s_part-%05d.%s", stem, i+1, d.format)
		}
		if err := os.Rename(filepath.Join(d.dir, d.files[i].Name), filepath.Join(d.dir, name)); err != nil {
			return err
		}
		d.files[i].Name = name
	}
	return nil
}
