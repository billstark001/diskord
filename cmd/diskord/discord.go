package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

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

func launchDiscord(m *config.Manager, path string) error {
	started := time.Now()
	binary, err := discordExecutable(path)
	if err != nil {
		return err
	}
	args := []string{"--proxy-server=http://" + m.Current().Proxy.Listen, "--disable-quic"}
	if runtime.GOOS == "darwin" {
		if bundle := discordAppBundle(binary); bundle != "" {
			return launchDiscordApp(m, bundle, args, started)
		}
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdin = nil
	// A child outlives this launcher. io.Discard makes os/exec create copy pipes,
	// which close when the launcher exits and cause EPIPE in Electron.
	output, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer output.Close()
	cmd.Stdout, cmd.Stderr = output, output
	if m.Current().Logging.Discord.Enabled {
		writer, err := logfile.OpenAt(m.Root, "discord", started)
		if err != nil {
			return fmt.Errorf("open Discord file logger: %w", err)
		}
		defer writer.Close()
		if _, err := fmt.Fprintf(writer, "Discord launch started at %s\n", started.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		cmd.Stdout, cmd.Stderr = writer.File(), writer.File()
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		return err
	}
	fmt.Printf("Discord started at %s with the configured proxy (pid %d).\n", started.Format(time.RFC3339Nano), pid)
	return nil
}

func discordAppBundle(binary string) string {
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}
	macOS := filepath.Dir(binary)
	contents := filepath.Dir(macOS)
	bundle := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.HasSuffix(filepath.Base(bundle), ".app") {
		return ""
	}
	return bundle
}

func launchDiscordApp(m *config.Manager, bundle string, args []string, started time.Time) error {
	outputPath := os.DevNull
	if m.Current().Logging.Discord.Enabled {
		writer, err := logfile.OpenAt(m.Root, "discord", started)
		if err != nil {
			return fmt.Errorf("open Discord file logger: %w", err)
		}
		outputPath = writer.Path()
		if _, err := fmt.Fprintf(writer, "Discord launch requested at %s via Launch Services\n", started.Format(time.RFC3339Nano)); err != nil {
			writer.Close()
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
	}
	// Launch Services starts the signed app bundle in its normal app context.
	// Directly exec'ing Contents/MacOS/Discord from Terminal can attribute macOS
	// privacy prompts for Discord to Terminal instead.
	output, err := exec.Command("/usr/bin/open", discordOpenArgs(bundle, outputPath, args)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launch Discord through Launch Services: %w: %s", err, strings.TrimSpace(string(output)))
	}
	fmt.Printf("Discord launch requested at %s with the configured proxy.\n", started.Format(time.RFC3339Nano))
	return nil
}

func discordOpenArgs(bundle, outputPath string, args []string) []string {
	openArgs := []string{"-a", bundle, "--stdin", os.DevNull, "--stdout", outputPath, "--stderr", outputPath, "--args"}
	return append(openArgs, args...)
}
