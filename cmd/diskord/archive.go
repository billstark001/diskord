package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"diskord/internal/archive"
	"diskord/internal/config"
	"diskord/internal/model"
	"diskord/internal/securefs"
	"diskord/internal/store"
	"diskord/internal/ui"

	"github.com/spf13/cobra"
)

func readRuntime(path string) (config.Config, string, error) {
	c, _, err := config.Load(path)
	if err != nil {
		return c, "", err
	}
	root, err := filepath.Abs(c.RuntimeDir)
	if err != nil {
		return c, "", err
	}
	root, err = filepath.EvalSymlinks(root)
	return c, root, err
}

func addArchiveCommands(root *cobra.Command, configPath *string) {
	root.AddCommand(&cobra.Command{Use: "api-token", Short: "Print the existing read-only archive API token", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, runtimeRoot, err := readRuntime(*configPath)
			if err != nil {
				return err
			}
			token, err := ui.ReadAPIToken(runtimeRoot)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), token)
			return err
		}})
	for _, operation := range []string{"status", "guilds", "dms", "channels", "threads", "messages", "export"} {
		var offline, attachments, includeThreads bool
		var thread, threadName, since, until, after, guild, scope, format, output string
		var limit int
		cmd := &cobra.Command{Use: operation, Short: "Read local archive " + operation, Args: cobra.NoArgs}
		cmd.Flags().BoolVar(&offline, "offline", false, "read a stopped archive without modifying its runtime")
		if operation == "guilds" {
			cmd.Aliases = []string{"servers"}
		}
		if operation == "channels" || operation == "threads" || operation == "messages" || operation == "export" {
			cmd.Flags().StringVar(&guild, "guild", "", "filter by server ID")
			cmd.Flags().StringVar(&scope, "scope", "", "dm selects private conversations; mutually exclusive with --guild")
		}
		if operation == "messages" || operation == "export" {
			cmd.Flags().StringVar(&thread, "thread", "", "thread or channel ID (default: all observed messages)")
			cmd.Flags().StringVar(&threadName, "thread-name", "", "exact observed channel/thread name; ambiguous names require --thread ID")
			cmd.MarkFlagsMutuallyExclusive("thread", "thread-name")
			cmd.Flags().BoolVar(&includeThreads, "include-threads", false, "include child channels/threads of the selected channel")
			cmd.Flags().StringVar(&since, "since", "", "inclusive creation date/time; omitted timezone uses local time")
			cmd.Flags().StringVar(&until, "until", "", "exclusive creation date/time; date-only includes the whole day; local timezone by default")
		}
		if operation == "messages" {
			cmd.Flags().StringVar(&after, "after", "", "message ID cursor; does not track edits to older messages")
			cmd.Flags().IntVar(&limit, "limit", 100, "page size, 1..1000")
		}
		if operation == "export" {
			cmd.Flags().StringVar(&format, "format", "md", "md or jsonl")
			cmd.Flags().BoolVar(&attachments, "attachments", false, "include cached attachment images in a ZIP")
			cmd.Flags().StringVar(&output, "output", "-", "new output file outside the source runtime; '-' writes text to stdout")
		}
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			for _, selector := range []struct{ name, value string }{{"thread", thread}, {"thread-name", threadName}, {"guild", guild}, {"scope", scope}} {
				if cmd.Flags().Changed(selector.name) && selector.value == "" {
					return fmt.Errorf("--%s must not be empty; omit it to select all", selector.name)
				}
			}
			normalizedSince, err := cliTimeBound(since, false, time.Local)
			if err != nil {
				return err
			}
			normalizedUntil, err := cliTimeBound(until, true, time.Local)
			if err != nil {
				return err
			}
			selection, err := store.ParseArchiveSelection(thread, guild, scope, normalizedSince, normalizedUntil)
			if err != nil {
				return err
			}
			selection.IncludeThreads = includeThreads
			if includeThreads && thread == "" && threadName == "" {
				return errors.New("--include-threads requires --thread or --thread-name")
			}
			if guild != "" && !model.ID(guild) || operation == "messages" && (limit < 1 || limit > 1000 || after != "" && !model.ID(after)) {
				return errors.New("invalid guild ID, cursor, or page limit")
			}
			options := archive.Options{Format: format, Attachments: attachments}
			if operation == "export" {
				if err := options.Validate(); err != nil {
					return err
				}
				if output == "-" && (offline || attachments) {
					return errors.New("offline or attachment export requires --output FILE outside the source runtime")
				}
			}
			c, runtimeRoot, err := readRuntime(*configPath)
			if err != nil {
				return err
			}
			if operation == "export" && output != "-" {
				output, err = exportOutput(runtimeRoot, output)
				if err != nil {
					return err
				}
			}
			interrupted, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(interrupted, 6*time.Minute)
			defer cancel()
			if offline {
				lock, err := securefs.LockExisting(runtimeRoot)
				if err != nil {
					return errors.New("offline archive unavailable or running; use the online API")
				}
				defer lock.Close()
				db, err := store.OpenReadOnly(runtimeRoot)
				if err != nil {
					return err
				}
				defer db.Close()
				return offlineArchive(ctx, cmd.OutOrStdout(), db, operation, selection, options, guild, scope, threadName, after, limit, output)
			}
			if operation == "guilds" || operation == "dms" || operation == "channels" || operation == "threads" || threadName != "" {
				nav, err := readOnlineNavigation(ctx, c, runtimeRoot)
				if err != nil {
					return err
				}
				if threadName != "" {
					selection.ThreadID, err = resolveThreadName(nav, threadName, selection)
					if err != nil {
						return err
					}
				} else {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(navigationResult(nav, operation, guild, scope))
				}
			}

			q := url.Values{}
			switch operation {
			case "channels":
				q.Set("guild", guild)
			case "messages", "export":
				q.Set("thread", selection.ThreadID)
				q.Set("include_threads", strconv.FormatBool(includeThreads))
				q.Set("guild", selection.GuildID)
				q.Set("scope", selection.Scope)
				q.Set("since", normalizedSince)
				q.Set("until", normalizedUntil)
				if operation == "messages" {
					q.Set("after", after)
					q.Set("limit", strconv.Itoa(limit))
				} else {
					q.Set("format", format)
					q.Set("attachments", strconv.FormatBool(attachments))
				}
			}
			response, err := archiveRequest(ctx, c, runtimeRoot, operation, q)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			if operation == "export" && output != "-" {
				return saveExport(ctx, output, response.Body)
			}
			return copyArchive(ctx, cmd.OutOrStdout(), response.Body)
		}
		root.AddCommand(cmd)
	}
}

func archiveRequest(ctx context.Context, c config.Config, root, operation string, q url.Values) (*http.Response, error) {
	token, err := ui.ReadAPIToken(root)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "http", Host: c.Web.Listen, Path: "/api/v1/" + operation, RawQuery: q.Encode()}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(r)
	if err != nil {
		return nil, fmt.Errorf("local read-only API unavailable (use --offline for a stopped archive): %w", err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 16*1024)).Decode(&body)
		return nil, fmt.Errorf("archive API HTTP %d: %s", response.StatusCode, body.Error)
	}
	return response, nil
}

func readOnlineNavigation(ctx context.Context, c config.Config, root string) (ui.ArchiveNavigation, error) {
	response, err := archiveRequest(ctx, c, root, "navigation", nil)
	if err != nil {
		return ui.ArchiveNavigation{}, err
	}
	defer response.Body.Close()
	var nav ui.ArchiveNavigation
	err = json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(&nav)
	return nav, err
}

func offlineArchive(ctx context.Context, out io.Writer, db *store.Store, operation string, selection store.Selection, options archive.Options, guild, scope, threadName, after string, limit int, output string) error {
	enc := json.NewEncoder(out)
	if operation == "guilds" || operation == "dms" || operation == "channels" || operation == "threads" || threadName != "" {
		nav, err := ui.ReadNavigation(ctx, db)
		if err != nil {
			return err
		}
		if threadName != "" {
			selection.ThreadID, err = resolveThreadName(nav, threadName, selection)
			if err != nil {
				return err
			}
		} else {
			return enc.Encode(navigationResult(nav, operation, guild, scope))
		}
	}

	switch operation {
	case "status":
		status, err := ui.ReadStatus(ctx, db)
		if err != nil {
			return err
		}
		return enc.Encode(status)
	case "messages":
		reader, err := db.BeginArchive(ctx)
		if err != nil {
			return err
		}
		defer reader.Close()
		page, err := reader.Page(ctx, selection, after, limit)
		if err != nil {
			return err
		}
		reader.Close()
		return enc.Encode(page)
	case "export":
		staging, err := os.MkdirTemp(filepath.Dir(output), ".diskord-export-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(staging)
		if err := securefs.Private(staging); err != nil {
			return err
		}
		a, err := archive.Build(ctx, db, staging, selection, options)
		if err != nil {
			return err
		}
		defer a.Close()
		f, err := os.Open(a.Path)
		if err != nil {
			return err
		}
		defer f.Close()
		return saveExport(ctx, output, f)
	}
	return errors.New("unknown archive operation")
}

func exportOutput(runtimeRoot, output string) (string, error) {
	path, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	path = filepath.Join(parent, filepath.Base(path))
	if strings.EqualFold(filepath.VolumeName(runtimeRoot), filepath.VolumeName(path)) {
		rel, err := filepath.Rel(runtimeRoot, path)
		if err != nil {
			return "", err
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", errors.New("export output must be outside the source runtime")
		}
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return "", errors.New("export output already exists or is inaccessible; choose a new path")
	}
	return path, nil
}

func saveExport(ctx context.Context, path string, in io.Reader) (err error) {
	f, err := securefs.NewFile(path)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(path)
		}
	}()
	if err = copyArchive(ctx, f, in); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

func copyArchive(ctx context.Context, out io.Writer, in io.Reader) error {
	buffer := make([]byte, 32*1024)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := in.Read(buffer)
		size += int64(n)
		if size > archive.MaxBytes {
			return errors.New("archive response exceeds 8 GiB")
		}
		if n > 0 {
			if written, e := out.Write(buffer[:n]); e != nil {
				return e
			} else if written != n {
				return io.ErrShortWrite
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
