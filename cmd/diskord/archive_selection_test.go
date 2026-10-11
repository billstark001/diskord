package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"diskord/internal/model"
	"diskord/internal/store"
	"diskord/internal/ui"
)

func TestCLILocalDatesAndTimes(t *testing.T) {
	local := time.FixedZone("JST", 9*3600)
	for _, tt := range []struct {
		raw   string
		upper bool
		want  string
	}{
		{"2026-10-01", false, "2026-09-30T15:00:00Z"},
		{"2026-10-01", true, "2026-10-01T15:00:00Z"},
		{"2026-10-01T23:59:59", true, "2026-10-01T14:59:59Z"},
		{"2026-10-01 12:30", false, "2026-10-01T03:30:00Z"},
		{"2026-10-01T12:30:00.123456789", false, "2026-10-01T03:30:00.123456789Z"},
		{"2026-10-01T00:00:00-04:00", false, "2026-10-01T04:00:00Z"},
		{"", false, ""},
	} {
		got, err := cliTimeBound(tt.raw, tt.upper, local)
		if err != nil || got != tt.want {
			t.Fatal(tt, got, err)
		}
	}
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	since, _ := cliTimeBound("2026-03-08", false, newYork)
	until, _ := cliTimeBound("2026-03-08", true, newYork)
	a, _ := time.Parse(time.RFC3339, since)
	b, _ := time.Parse(time.RFC3339, until)
	if b.Sub(a) != 23*time.Hour {
		t.Fatal("DST date did not span the calendar day", since, until)
	}
	for _, raw := range []string{"2026-03-08T02:30", "2026-02-30", "2026-10-01T25:00", "2026-10-01garbage"} {
		if _, err := cliTimeBound(raw, false, newYork); err == nil {
			t.Fatal("invalid local time accepted", raw)
		}
	}
}

func TestCLIEmptySelectorsDoNotSelectEntireArchive(t *testing.T) {
	for _, flag := range []string{"--thread", "--thread-name", "--guild", "--scope"} {
		_, err := cliExecute(t, "unused.yaml", "export", flag, "")
		if err == nil || !strings.Contains(err.Error(), "must not be empty") {
			t.Fatalf("empty %s was not rejected before opening the archive: %v", flag, err)
		}
	}
}

func TestCLIDatesRejectNormalizedMidnights(t *testing.T) {
	for _, tt := range []struct {
		zone, date string
		upper      bool
	}{
		{"Pacific/Apia", "2011-12-30", false},
		{"Pacific/Apia", "2011-12-29", true},
		{"America/Havana", "2026-03-08", false},
		{"America/Havana", "2026-03-07", true},
	} {
		zone, err := time.LoadLocation(tt.zone)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cliTimeBound(tt.date, tt.upper, zone); err == nil {
			t.Fatalf("nonexistent midnight was silently normalized: %+v", tt)
		}
	}
}

func TestCLINameResolutionAndNavigationCommands(t *testing.T) {
	workspace, path, m, db := cliArchiveFixture(t)
	batch := model.Batch{Source: "ws", HasGuildOrder: true, GuildOrder: []string{"9", "1"},
		Guilds: []model.Guild{{ID: "1", Name: model.Ptr("Alpha")}, {ID: "9", Name: model.Ptr("Zulu")}},
		Channels: []model.Channel{
			{ID: "2", GuildID: model.Ptr("9"), Name: model.Ptr("same"), Type: model.Ptr(11)},
			{ID: "3", GuildID: model.Ptr("1"), Name: model.Ptr("same"), Type: model.Ptr(11)},
			{ID: "4", Name: model.Ptr("same"), Type: model.Ptr(1), LastMessageID: model.Ptr("999")},
			{ID: "5", Name: model.Ptr("private"), Type: model.Ptr(3), LastMessageID: model.Ptr("1000")},
		}}
	if err := db.Apply(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	u, err := ui.New(m, nil, nil, db)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- u.Serve() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		u.Close(ctx)
		<-served
	})
	for _, offline := range []bool{false, true} {
		if offline {
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
		}
		flags := []string{}
		if offline {
			flags = append(flags, "--offline")
		}
		result, err := cliExecute(t, path, append([]string{"guilds"}, flags...)...)
		if err != nil || strings.Index(result, `"id":"9"`) > strings.Index(result, `"id":"1"`) {
			t.Fatal(result, err)
		}
		result, err = cliExecute(t, path, append([]string{"dms"}, flags...)...)
		if err != nil || strings.Index(result, `"id":"5"`) > strings.Index(result, `"id":"4"`) {
			t.Fatal(result, err)
		}
		result, err = cliExecute(t, path, append([]string{"threads", "--guild", "9"}, flags...)...)
		if err != nil || !strings.Contains(result, `"id":"2"`) || strings.Contains(result, `"id":"3"`) {
			t.Fatal(result, err)
		}
		_, err = cliExecute(t, path, append([]string{"messages", "--thread-name", "same"}, flags...)...)
		if err == nil || !strings.Contains(err.Error(), `server 9 ("Zulu")`) || !strings.Contains(err.Error(), `server 1 ("Alpha")`) || !strings.Contains(err.Error(), "private conversation") {
			t.Fatal(err)
		}
		result, err = cliExecute(t, path, append([]string{"messages", "--thread-name", "same", "--guild", "9", "--since", "2026-10-01", "--until", "2026-10-01"}, flags...)...)
		var page store.ArchivePage
		if err != nil || json.Unmarshal([]byte(result), &page) != nil || page.Selection.ThreadID != "2" {
			t.Fatal(result, err)
		}
		_, err = cliExecute(t, path, append([]string{"export", "--thread-name", "SAME", "--output", filepath.Join(workspace, "nomatch.md")}, flags...)...)
		if err == nil || !strings.Contains(err.Error(), "no observed") {
			t.Fatal(err)
		}
	}
}
