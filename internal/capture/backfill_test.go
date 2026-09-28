package capture

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"diskord/internal/config"
	"diskord/internal/model"
	"diskord/internal/securefs"
	"diskord/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBackfillFetchesOnlyObservedCDNPath(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
		t.Setenv(key, os.Getenv(key))
	}
	if err := securefs.Prepare(root); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	path := "/avatars/3/hash.png"
	if err := s.Apply(context.Background(), model.Batch{Source: "http", Users: []model.User{{ID: "3", Avatar: model.Ptr("hash")}}}); err != nil {
		t.Fatal(err)
	}
	targets, err := s.MissingResources(context.Background())
	if err != nil || len(targets) != 1 || targets[0].Path != path {
		t.Fatalf("targets: %+v, %v", targets, err)
	}
	cfg := config.Default()
	cfg.Resources.Enabled = true
	engine := New(s, func() config.Config { return cfg })
	defer engine.Close()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" || req.URL.Host != "cdn.discordapp.com" || req.URL.EscapedPath() != path || req.URL.RawQuery != "" || req.Header.Get("Authorization") != "" {
			t.Fatalf("unsafe backfill request: %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(strings.NewReader("image"))}, nil
	})}
	if err := engine.fetchMissing(client, targets[0]); err != nil {
		t.Fatal(err)
	}
	targets, err = s.MissingResources(context.Background())
	if err != nil || len(targets) != 0 {
		t.Fatalf("saved image still missing: %+v, %v", targets, err)
	}
}
