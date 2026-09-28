package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"

	"diskord/internal/config"
	"diskord/internal/logfile"
)

func discordExecutable(explicit string) (string, error) {
	if explicit != "" {
		info, err := os.Stat(explicit)
		if err != nil || info.IsDir() {
			return "", errors.New("discord executable path is not a file")
		}
		return explicit, nil
	}
	var candidates []string
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/Applications/Discord.app/Contents/MacOS/Discord", filepath.Join(home, "Applications/Discord.app/Contents/MacOS/Discord")}
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			break
		}
		for _, root := range []string{filepath.Join(base, "Discord"), filepath.Join(base, "DiscordPTB"), filepath.Join(base, "DiscordCanary")} {
			versions, _ := filepath.Glob(filepath.Join(root, "app-*", "Discord.exe"))
			sort.Sort(sort.Reverse(sort.StringSlice(versions)))
			candidates = append(candidates, versions...)
		}
	default:
		candidates = []string{"/usr/bin/discord", "/opt/discord/Discord", filepath.Join(home, ".local/bin/discord")}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("discord not found in typical installation locations; pass --path")
}

func launchDiscord(m *config.Manager, args []string) error {
	fs := flag.NewFlagSet("discord launch", flag.ContinueOnError)
	path := fs.String("path", "", "Discord executable path; typical locations are searched by default")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected discord launch arguments")
	}
	binary, err := discordExecutable(*path)
	if err != nil {
		return err
	}
	cmd := exec.Command(binary, "--proxy-server=http://"+m.Current().Proxy.Listen, "--disable-quic")
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if m.Current().Logging.Discord.Enabled {
		writer, err := logfile.Open(m.Root, "discord")
		if err != nil {
			return fmt.Errorf("open Discord file logger: %w", err)
		}
		defer writer.Close()
		cmd.Stdout, cmd.Stderr = writer.File(), writer.File()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		return err
	}
	fmt.Printf("Discord started with the configured proxy (pid %d).\n", pid)
	return nil
}
