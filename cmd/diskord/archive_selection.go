package main

import (
	"fmt"
	"strings"
	"time"

	"diskord/internal/store"
	"diskord/internal/ui"
)

// Date-only upper bounds cover the entire final day, including fractional
// seconds at 23:59:59. Wire bounds remain exclusive at the next midnight.
func cliTimeBound(raw string, upper bool, location *time.Location) (string, error) {
	if raw == "" {
		return "", nil
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC().Format(time.RFC3339Nano), nil
	}
	if date, err := time.Parse("2006-01-02", raw); err == nil {
		t, err := localMidnight(date, location)
		if err != nil {
			return "", err
		}
		if upper {
			t, err = localMidnight(date.AddDate(0, 0, 1), location)
			if err != nil {
				return "", err
			}
		}
		return t.UTC().Format(time.RFC3339Nano), nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, raw, location); err == nil {
			// ParseInLocation normalizes nonexistent local times during DST gaps.
			// Reject that normalization instead of silently selecting another time.
			if t.Format(strings.Split(layout, ".999")[0]) != strings.Split(raw, ".")[0] {
				return "", fmt.Errorf("nonexistent local time: %s", raw)
			}
			return t.UTC().Format(time.RFC3339Nano), nil
		}
	}
	return "", fmt.Errorf("invalid date/time %q; use YYYY-MM-DD or YYYY-MM-DDTHH:MM[:SS] with an optional timezone", raw)
}

func localMidnight(date time.Time, location *time.Location) (time.Time, error) {
	t := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)
	if t.Format("2006-01-02T15:04:05") != date.Format("2006-01-02")+"T00:00:00" {
		return time.Time{}, fmt.Errorf("nonexistent local midnight on %s; use an explicit RFC3339 bound", date.Format("2006-01-02"))
	}
	return t, nil
}

func resolveThreadName(nav ui.ArchiveNavigation, name string, selection store.Selection) (string, error) {
	matches := []ui.ArchiveChannel{}
	guildNames := map[string]string{}
	for _, g := range nav.Guilds {
		guildNames[g.ID] = g.Name
	}
	for _, c := range nav.Channels {
		if c.Kind == 4 || c.Name != name {
			continue
		}
		if selection.GuildID != "" && c.GuildID != selection.GuildID {
			continue
		}
		if selection.Scope == "dm" && (c.GuildID != "" || c.Kind != 1 && c.Kind != 3) {
			continue
		}
		matches = append(matches, c)
	}
	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no observed thread/channel named %q in the selected scope", name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "multiple threads/channels named %q; specify --thread ID:\n", name)
	for _, c := range matches {
		owner := "unclassified conversation"
		if c.GuildID != "" {
			owner = fmt.Sprintf("server %s (%q)", c.GuildID, guildNames[c.GuildID])
		} else if c.Kind == 1 || c.Kind == 3 {
			owner = "private conversation"
		}
		fmt.Fprintf(&b, "  %s (%q) — %s\n", c.ID, c.Name, owner)
	}
	return "", fmt.Errorf("%s", strings.TrimSuffix(b.String(), "\n"))
}

func navigationResult(nav ui.ArchiveNavigation, operation, guild, scope string) any {
	if operation == "guilds" {
		return map[string]any{"schema_version": 1, "guilds": nav.Guilds}
	}
	channels := []ui.ArchiveChannel{}
	for _, c := range nav.Channels {
		if guild != "" && c.GuildID != guild {
			continue
		}
		if (operation == "dms" || scope == "dm") && (c.GuildID != "" || c.Kind != 1 && c.Kind != 3) {
			continue
		}
		if operation == "threads" && c.Kind != 10 && c.Kind != 11 && c.Kind != 12 {
			continue
		}
		channels = append(channels, c)
	}
	return map[string]any{"schema_version": 1, "channels": channels}
}
