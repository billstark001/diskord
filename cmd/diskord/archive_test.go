package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"diskord/internal/config"
	"diskord/internal/model"
	"diskord/internal/securefs"
	"diskord/internal/store"
	"diskord/internal/ui"
)

func cliArchiveFixture(t *testing.T) (string, string, *config.Manager, *store.Store) {
	t.Helper()
	workspace := t.TempDir()
	root := filepath.Join(workspace, "runtime")
	for _, key := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
		t.Setenv(key, os.Getenv(key))
	}
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listen.Addr().String()
	listen.Close()
	path := filepath.Join(workspace, "diskord.yaml")
	body := strings.Replace(config.Example, "runtime_dir: .", "runtime_dir: "+filepath.ToSlash(root), 1)
	body = strings.Replace(body, "127.0.0.1:3900", addr, 1)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := config.New(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Apply(context.Background(), model.Batch{Source: "ws", Channels: []model.Channel{{ID: "2", GuildID: model.Ptr("1"), Type: model.Ptr(11)}},
		Messages: []model.Message{{ID: "10", ChannelID: model.Ptr("2"), Timestamp: model.Ptr("2026-10-01T00:00:00+09:00"), Content: model.Ptr("CLI fixture")}}}); err != nil {
		t.Fatal(err)
	}
	lock, err := securefs.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	return workspace, path, m, db
}

func cliExecute(t *testing.T, configPath string, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--config", configPath}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func cliExecuteSerialExport(t *testing.T, configPath string, args ...string) (string, error) {
	t.Helper()
	// Reading Content-Length bytes can finish before the server's deferred
	// staging cleanup releases its export slot. Serial success cases wait for
	// that cleanup; admission rejection is tested separately in internal/ui.
	deadline := time.Now().Add(2 * time.Second)
	for {
		text, err := cliExecute(t, configPath, args...)
		if err == nil || !strings.HasPrefix(err.Error(), "archive API HTTP 429:") || !time.Now().Before(deadline) {
			return text, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type cliFileState struct {
	Hash     [32]byte
	Mode     os.FileMode
	Modified time.Time
}

func cliRuntimeState(t *testing.T, root string) map[string]cliFileState {
	t.Helper()
	state := map[string]cliFileState{}
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			state[path] = cliFileState{sha256.Sum256(b), info.Mode(), info.ModTime()}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestOfflineExportSourceLinks(t *testing.T) {
	workspace, path, _, db := cliArchiveFixture(t)
	db.Close()
	for _, enabled := range []bool{true, false} {
		suffix := "with"
		if !enabled {
			suffix = "without"
		}
		output := filepath.Join(workspace, suffix+".md")
		args := []string{"export", "--offline", "--output", output}
		if !enabled {
			args = append(args, "--source-links=false")
		}
		if text, err := cliExecute(t, path, args...); err != nil {
			t.Fatal(text, err)
		}
		data, err := os.ReadFile(output)
		if err != nil || strings.Contains(string(data), "https://discord.com/channels/1/2/10") != enabled {
			t.Fatal(string(data), err)
		}
	}
}

func TestOfflineCommandsAndExportsPreserveSource(t *testing.T) {
	workspace, path, m, db := cliArchiveFixture(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before := cliRuntimeState(t, m.Root)
	for _, operation := range []string{"status", "channels", "messages"} {
		text, err := cliExecute(t, path, operation, "--offline")
		if err != nil || !json.Valid([]byte(text)) {
			t.Fatal(operation, text, err)
		}
	}
	for _, format := range []string{"md", "jsonl"} {
		for _, attachments := range []bool{false, true} {
			output := filepath.Join(workspace, format+map[bool]string{false: ".txt", true: ".zip"}[attachments])
			args := []string{"export", "--offline", "--thread", "2", "--since", "2026-09-30T15:00:00Z", "--until", "2026-09-30T15:00:01Z", "--format", format, "--output", output}
			if attachments {
				args = append(args, "--attachments")
			}
			if text, err := cliExecute(t, path, args...); err != nil || text != "" {
				t.Fatal(text, err)
			}
			if attachments {
				z, err := zip.OpenReader(output)
				if err != nil {
					t.Fatal(err)
				}
				z.Close()
			} else {
				b, err := os.ReadFile(output)
				if err != nil || !bytes.Contains(b, []byte("CLI fixture")) {
					t.Fatal(string(b), err)
				}
			}
			if _, err := cliExecute(t, path, args...); err == nil {
				t.Fatal("existing output overwritten")
			}
		}
	}
	if _, err := cliExecute(t, path, "export", "--offline", "--output", filepath.Join(m.Root, "forbidden.md")); err == nil {
		t.Fatal("export wrote into source runtime")
	}
	if _, err := cliExecute(t, path, "export", "--offline"); err == nil {
		t.Fatal("offline staging destination was implicit")
	}
	if _, err := cliExecute(t, path, "api-token"); err == nil {
		t.Fatal("read command initialized missing API token")
	}
	if after := cliRuntimeState(t, m.Root); !reflect.DeepEqual(before, after) {
		t.Fatal("offline commands modified source runtime")
	}
	files, err := filepath.Glob(filepath.Join(workspace, ".diskord-export-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("offline staging leaked", files, err)
	}
}

func TestCLIOnlineAPIAndExport(t *testing.T) {
	workspace, path, m, db := cliArchiveFixture(t)
	u, err := ui.New(m, nil, nil, db)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- u.Serve() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		u.Close(ctx)
		<-done
	})
	for _, operation := range []string{"status", "channels", "messages"} {
		text, err := cliExecute(t, path, operation)
		if err != nil || !json.Valid([]byte(text)) {
			t.Fatal(operation, text, err)
		}
	}
	text, err := cliExecuteSerialExport(t, path, "export", "--format", "jsonl", "--thread", "2")
	if err != nil || !strings.Contains(text, "CLI fixture") || !strings.Contains(text, `"type":"summary"`) {
		t.Fatal(text, err)
	}
	for _, enabled := range []bool{true, false} {
		args := []string{"export"}
		if !enabled {
			args = append(args, "--source-links=false")
		}
		text, err := cliExecuteSerialExport(t, path, args...)
		if err != nil || strings.Contains(text, "https://discord.com/channels/1/2/10") != enabled {
			t.Fatal(text, err)
		}
	}
	output := filepath.Join(workspace, "online.zip")
	if text, err := cliExecuteSerialExport(t, path, "export", "--attachments", "--output", output); err != nil || text != "" {
		t.Fatal(text, err)
	}
	z, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	z.Close()
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("interrupted") }

func TestInterruptedExportAndRedirects(t *testing.T) {
	workspace, _, m, _ := cliArchiveFixture(t)
	output := filepath.Join(workspace, "failed.jsonl")
	if err := saveExport(context.Background(), output, failingReader{}); err == nil {
		t.Fatal("interrupted transfer succeeded")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("partial output retained")
	}
	if err := os.WriteFile(filepath.Join(m.Root, "state", "api-token"), []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	seen := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen++; w.WriteHeader(200) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	c := config.Default()
	c.Web.Listen = strings.TrimPrefix(redirect.URL, "http://")
	if _, err := archiveRequest(context.Background(), c, m.Root, "status", nil); err == nil {
		t.Fatal("redirect accepted")
	}
	if seen != 0 {
		t.Fatal("archive token followed a redirect")
	}
}

func TestCLISplitExportsOnlineAndOffline(t *testing.T) {
	for _, offline := range []bool{false, true} {
		workspace, path, m, db := cliArchiveFixture(t)
		if offline {
			db.Close()
		} else {
			u, err := ui.New(m, nil, nil, db)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- u.Serve() }()
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = u.Close(ctx)
				<-done
			})
		}
		for _, attachments := range []bool{false, true} {
			output := filepath.Join(workspace, fmt.Sprintf("split-%t.zip", attachments))
			args := []string{"export", "--thread", "2", "--split-mib", "1", "--output", output}
			if offline {
				args = append(args, "--offline")
			}
			if attachments {
				args = append(args, "--attachments")
			}
			if text, err := cliExecuteSerialExport(t, path, args...); err != nil {
				t.Fatal(text, err)
			}
			z, err := zip.OpenReader(output)
			if err != nil {
				t.Fatal(err)
			}
			if len(z.File) != 2 || !strings.HasSuffix(z.File[0].Name, "_part-00001.md") {
				z.Close()
				t.Fatal(z.File)
			}
			for _, f := range z.File {
				if strings.HasSuffix(f.Name, ".zip") {
					z.Close()
					t.Fatal("nested zip")
				}
			}
			z.Close()
		}
	}
	for _, args := range [][]string{{"export", "--split-mib", "1"}, {"export", "--split-mib", "-1"}, {"export", "--split-mib", "8193"}, {"export", "--split-mib", "1", "--format", "jsonl", "--output", "ignored.zip"}} {
		if _, err := cliExecute(t, "missing.yaml", args...); err == nil || strings.Contains(err.Error(), "missing.yaml") {
			t.Fatal(args, err)
		}
	}
}
