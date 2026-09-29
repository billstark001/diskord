package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"diskord/internal/config"
	"diskord/internal/securefs"
)

func TestCLIReportsOneConciseError(t *testing.T) {
	root := newRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"discord", "launch", "--unknown"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown flag") || !strings.Contains(err.Error(), "--help") {
		t.Fatalf("unexpected flag error: %v", err)
	}
	if strings.Contains(output.String(), "Usage:") {
		t.Fatalf("usage printed for one flag error: %q", output.String())
	}
	root = newRootCommand()
	root.SetArgs([]string{"ca"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "choose ca") {
		t.Fatalf("missing subcommand: %v", err)
	}
}

func TestBackgroundRejectsConfigOutsideWorkingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diskord.yaml")
	if err := os.WriteFile(path, []byte(config.Example), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := localBackground(path); err == nil || !strings.Contains(err.Error(), "current working directory") {
		t.Fatalf("expected working-directory rejection, got %v", err)
	}
}

func TestDiscordBundleUsesLaunchServicesArguments(t *testing.T) {
	bundle := filepath.Join("Applications", "Discord.app")
	binary := filepath.Join(bundle, "Contents", "MacOS", "Discord")
	if got := discordAppBundle(binary); got != bundle {
		t.Fatalf("bundle: %q", got)
	}
	if got := discordAppBundle(filepath.Join("usr", "bin", "discord")); got != "" {
		t.Fatalf("non-bundle executable: %q", got)
	}
	logPath := filepath.Join("tmp", "discord.log")
	args := discordOpenArgs(bundle, logPath, []string{"--proxy-server=http://127.0.0.1:3901", "--disable-quic"})
	if strings.Join(args, "|") != "-a|"+bundle+"|--stdin|"+os.DevNull+"|--stdout|"+logPath+"|--stderr|"+logPath+"|--args|--proxy-server=http://127.0.0.1:3901|--disable-quic" {
		t.Fatalf("open arguments: %q", args)
	}
}

func TestLaunchHelperProcess(t *testing.T) {
	if os.Getenv("DISKORD_TEST_LAUNCH_HELPER") != "1" {
		return
	}
	manager, err := config.New(os.Getenv("DISKORD_TEST_CONFIG"))
	if err != nil {
		os.Exit(2)
	}
	if err := launchDiscord(manager, os.Getenv("DISKORD_TEST_EXECUTABLE")); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestDiscordOutputSurvivesLauncherExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	for _, fileLog := range []bool{false, true} {
		t.Run(map[bool]string{false: "devnull", true: "file"}[fileLog], func(t *testing.T) {
			root := t.TempDir()
			for _, key := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
				t.Setenv(key, os.Getenv(key))
			}
			if err := securefs.Prepare(root); err != nil {
				t.Fatal(err)
			}
			cfg := strings.Replace(config.Example, "runtime_dir: .", "runtime_dir: "+root, 1)
			if fileLog {
				cfg = strings.Replace(cfg, "discord:\n    enabled: false", "discord:\n    enabled: true", 1)
			}
			configPath := filepath.Join(root, "diskord.yaml")
			if err := os.WriteFile(configPath, []byte(cfg), 0600); err != nil {
				t.Fatal(err)
			}
			executable := filepath.Join(root, "fake-discord")
			script := "#!/bin/sh\nset -e\nsleep 0.2\nprintf 'late stdout\\n'\nprintf 'late stderr\\n' >&2\nprintf ok > \"$DISKORD_TEST_SENTINEL\"\n"
			if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(root, "completed")
			child := exec.Command(os.Args[0], "-test.run=^TestLaunchHelperProcess$")
			child.Env = append(os.Environ(), "DISKORD_TEST_LAUNCH_HELPER=1", "DISKORD_TEST_CONFIG="+configPath, "DISKORD_TEST_EXECUTABLE="+executable, "DISKORD_TEST_SENTINEL="+sentinel)
			if output, err := child.CombinedOutput(); err != nil {
				t.Fatalf("launcher: %v: %s", err, output)
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(sentinel); err == nil {
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			if _, err := os.Stat(sentinel); err != nil {
				t.Fatalf("child lost stdout/stderr after launcher exit: %v", err)
			}
			if fileLog {
				paths, err := filepath.Glob(filepath.Join(root, "logs", "discord-*.log"))
				if err != nil || len(paths) != 1 {
					t.Fatalf("Discord session log paths: %v, %v", paths, err)
				}
				data, err := os.ReadFile(paths[0])
				if err != nil || !bytes.Contains(data, []byte("Discord launch started at")) || !bytes.Contains(data, []byte("late stdout")) || !bytes.Contains(data, []byte("late stderr")) {
					t.Fatalf("Discord log: %q, %v", data, err)
				}
			}
		})
	}
}
