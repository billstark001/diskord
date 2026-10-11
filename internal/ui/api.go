package ui

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"diskord/internal/archive"
	"diskord/internal/model"
	"diskord/internal/securefs"
	"diskord/internal/store"
	"diskord/internal/version"
)

// ReadAPIToken never creates a token or changes runtime permissions.
func ReadAPIToken(root string) (string, error) {
	path, err := securefs.Within(root, "state/api-token")
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("read-only API token unavailable; start diskord first")
	}
	token := strings.TrimSpace(string(b))
	if len(token) != 64 {
		return "", errors.New("invalid api-token file")
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", errors.New("invalid api-token file")
	}
	return token, nil
}

func ensureAPIToken(root string) (string, error) {
	path, err := securefs.Within(root, "state/api-token")
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		if err := securefs.Private(path); err != nil {
			return "", err
		}
		return ReadAPIToken(root)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	token, err := securefs.Random(32)
	if err != nil {
		return "", err
	}
	f, err := securefs.NewFile(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(token + "\n"); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	return token, f.Close()
}

func (u *UI) archiveAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || u.apiToken == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(u.apiToken)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			problem(w, http.StatusUnauthorized, "read-only API token required")
			return
		}
		next(w, r)
	})
}

func (u *UI) archiveRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/status", u.archiveAuth(u.archiveStatus))
	mux.Handle("GET /api/v1/navigation", u.archiveAuth(u.archiveNavigation))
	mux.Handle("GET /api/v1/channels", u.archiveAuth(u.archiveChannels))
	mux.Handle("GET /api/v1/messages", u.archiveAuth(u.archiveMessages))
	mux.Handle("GET /api/v1/export", u.archiveAuth(u.archiveExport))
}

type ArchiveStatus struct {
	SchemaVersion  int              `json:"schema_version"`
	Version        string           `json:"version"`
	DatabaseSchema int              `json:"database_schema"`
	Counts         map[string]int64 `json:"counts"`
}

func ReadStatus(ctx context.Context, db *store.Store) (ArchiveStatus, error) {
	c, err := db.Counts(ctx)
	return ArchiveStatus{1, version.Current, db.DatabaseSchema(),
		map[string]int64{"users": c.Users, "guilds": c.Guilds, "channels": c.Channels, "messages": c.Messages}}, err
}

func (u *UI) archiveStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	status, err := ReadStatus(ctx, u.Store)
	if err != nil {
		problem(w, 503, "archive unavailable")
		return
	}
	writeJSON(w, 200, status)
}

type ArchiveChannel struct {
	ID       string `json:"id"`
	GuildID  string `json:"guild_id"`
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
	Kind     int    `json:"kind"`
	Position *int   `json:"position"`
	Deleted  bool   `json:"deleted"`
}

type ArchiveGuild struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Unavailable bool   `json:"unavailable"`
	Deleted     bool   `json:"deleted"`
}

type ArchiveNavigation struct {
	SchemaVersion int              `json:"schema_version"`
	Guilds        []ArchiveGuild   `json:"guilds"`
	Channels      []ArchiveChannel `json:"channels"`
}

func ReadNavigation(ctx context.Context, db *store.Store) (ArchiveNavigation, error) {
	nav, err := db.Navigation(ctx)
	result := ArchiveNavigation{SchemaVersion: 1, Guilds: []ArchiveGuild{}, Channels: []ArchiveChannel{}}
	for _, g := range nav.Guilds {
		result.Guilds = append(result.Guilds, ArchiveGuild{g.ID, g.Name, g.Unavailable, g.Deleted})
	}
	for _, c := range nav.Channels {
		result.Channels = append(result.Channels, ArchiveChannel{c.ID, c.GuildID, c.ParentID, c.Name, c.Kind, c.Position, c.Deleted})
	}
	return result, err
}

func ReadChannels(ctx context.Context, db *store.Store, guild string) ([]ArchiveChannel, error) {
	nav, err := ReadNavigation(ctx, db)
	channels := []ArchiveChannel{}
	for _, c := range nav.Channels {
		if guild == "" || c.GuildID == guild {
			channels = append(channels, c)
		}
	}
	return channels, err
}

func (u *UI) archiveNavigation(w http.ResponseWriter, r *http.Request) {
	if _, err := archiveQuery(r); err != nil {
		problem(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	nav, err := ReadNavigation(ctx, u.Store)
	if err != nil {
		problem(w, 503, "navigation unavailable")
		return
	}
	writeJSON(w, 200, nav)
}

func archiveQuery(r *http.Request, allowed ...string) (url.Values, error) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, errors.New("invalid query encoding")
	}
	for key, values := range q {
		found := false
		for _, name := range allowed {
			found = found || key == name
		}
		if !found || len(values) != 1 {
			return nil, errors.New("unknown or repeated query parameter")
		}
	}
	return q, nil
}

func (u *UI) archiveChannels(w http.ResponseWriter, r *http.Request) {
	q, err := archiveQuery(r, "guild")
	if err != nil || q.Get("guild") != "" && !model.ID(q.Get("guild")) {
		problem(w, 400, "invalid guild filter")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	channels, err := ReadChannels(ctx, u.Store, q.Get("guild"))
	if err != nil {
		problem(w, 503, "archive unavailable")
		return
	}
	writeJSON(w, 200, map[string]any{"schema_version": 1, "channels": channels})
}

func archiveSelection(q url.Values) (store.Selection, error) {
	selection, err := store.ParseArchiveSelection(q.Get("thread"), q.Get("guild"), q.Get("scope"), q.Get("since"), q.Get("until"))
	if err != nil {
		return selection, err
	}
	if value := q.Get("include_threads"); value != "" {
		selection.IncludeThreads, err = strconv.ParseBool(value)
	}
	if err != nil || selection.IncludeThreads && selection.ThreadID == "" {
		return selection, errors.New("include_threads requires a thread/channel and a boolean value")
	}
	return selection, nil
}

func (u *UI) archiveMessages(w http.ResponseWriter, r *http.Request) {
	q, err := archiveQuery(r, "thread", "guild", "scope", "include_threads", "since", "until", "after", "limit")
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	selection, err := archiveSelection(q)
	limit := 100
	if q.Get("limit") != "" {
		if n, e := strconv.Atoi(q.Get("limit")); e == nil {
			limit = n
		} else {
			err = errors.New("limit must be 1..1000")
		}
	}
	if err != nil || limit < 1 || limit > 1000 || q.Get("after") != "" && !model.ID(q.Get("after")) {
		problem(w, 400, "invalid selection, cursor, or limit")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	reader, err := u.Store.BeginArchive(ctx)
	if err != nil {
		problem(w, 503, "archive unavailable")
		return
	}
	defer reader.Close()
	page, err := reader.Page(ctx, selection, q.Get("after"), limit)
	if err != nil {
		problem(w, 503, "archive query failed")
		return
	}
	reader.Close()
	writeJSON(w, 200, page)
}

func (u *UI) archiveExport(w http.ResponseWriter, r *http.Request) {
	q, err := archiveQuery(r, "thread", "guild", "scope", "include_threads", "since", "until", "format", "attachments", "source_links", "split_bytes")
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	selection, err := archiveSelection(q)
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	options := archive.Options{Format: q.Get("format"), SourceLinks: true}
	if options.Format == "" {
		options.Format = "md"
	}
	if q.Get("attachments") != "" {
		options.Attachments, err = strconv.ParseBool(q.Get("attachments"))
	}
	if _, present := q["source_links"]; present {
		var linkErr error
		options.SourceLinks, linkErr = strconv.ParseBool(q.Get("source_links"))
		err = errors.Join(err, linkErr)
	}
	if _, present := q["split_bytes"]; present {
		var splitErr error
		options.SplitBytes, splitErr = strconv.ParseInt(q.Get("split_bytes"), 10, 64)
		err = errors.Join(err, splitErr)
	}
	if err != nil || options.Validate() != nil {
		problem(w, 400, "invalid format, attachments, source_links, or split_bytes option")
		return
	}
	select {
	case u.exportSlot <- struct{}{}:
		defer func() { <-u.exportSlot }()
	default:
		w.Header().Set("Retry-After", "5")
		problem(w, 429, "another export is in progress")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	a, err := archive.Build(ctx, u.Store, u.Manager.Root, selection, options)
	if err != nil {
		problem(w, 503, "export failed: "+err.Error())
		return
	}
	defer a.Close()
	f, err := os.Open(a.Path)
	if err != nil {
		problem(w, 503, "export file unavailable")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		problem(w, 503, "export file unavailable")
		return
	}
	// The SQLite snapshot is already released. Give local file delivery its own
	// deadline rather than letting slow clients retain a WAL reader.
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Minute))
	// Shutdown cancels request contexts; interrupt a blocked socket write as
	// well so staged files are removed before the service exits.
	cancellationDone := make(chan struct{})
	stopCancellation := context.AfterFunc(r.Context(), func() {
		defer close(cancellationDone)
		_ = controller.SetWriteDeadline(time.Now())
	})
	defer func() {
		// Do not let a cancellation callback outlive this response and race with
		// net/http reusing the connection for a subsequent request.
		if !stopCancellation() {
			<-cancellationDone
		}
	}()
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename}))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	// Every request builds a new snapshot. Conditional/range delivery could
	// otherwise return an empty file or splice different snapshots on resume.
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, f)
	}
}
