package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"diskord/internal/config"
	"diskord/internal/logfile"
	"diskord/internal/securefs"
)

type backgroundState struct {
	Root   string `json:"root"`
	Config string `json:"config"`
	Web    string `json:"web"`
	PID    int    `json:"pid"`
	Token  string `json:"token"`
}

var backgroundClient = &http.Client{
	Transport: &http.Transport{Proxy: nil},
	Timeout:   1200 * time.Millisecond,
}

func localBackgroundPath(configPath string) (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", "", err
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return "", "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	} else {
		if !os.IsNotExist(resolveErr) {
			return "", "", resolveErr
		}
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(abs))
		if parentErr != nil {
			return "", "", parentErr
		}
		abs = filepath.Join(parent, filepath.Base(abs))
	}
	if filepath.Dir(abs) != cwd {
		return "", "", errors.New("run-bg and stop-bg require a configuration file in the current working directory")
	}
	return cwd, abs, nil
}

func localBackground(configPath string) (*config.Manager, string, error) {
	cwd, abs, err := localBackgroundPath(configPath)
	if err != nil {
		return nil, "", err
	}
	m, err := config.New(abs)
	if err != nil {
		return nil, "", err
	}
	if m.Root != cwd {
		return nil, "", errors.New("run-bg and stop-bg require runtime_dir to be the current working directory")
	}
	return m, cwd, nil
}

func backgroundPath(root string) (string, error) {
	return securefs.Within(root, "state/diskord-bg.json")
}

func readBackground(path string) (backgroundState, error) {
	if err := securefs.Private(path); err != nil {
		return backgroundState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return backgroundState{}, err
	}
	var state backgroundState
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if state.PID <= 0 || len(state.Token) != 64 || state.Root == "" || state.Config == "" || state.Web == "" {
		return state, errors.New("invalid background state file")
	}
	if _, err := hex.DecodeString(state.Token); err != nil {
		return state, errors.New("invalid background control token")
	}
	host, _, err := net.SplitHostPort(state.Web)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return state, errors.New("background control address must use a literal loopback IP")
	}
	return state, nil
}

func writeBackground(path, root string, state backgroundState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return securefs.AtomicWrite(root, path, append(data, '\n'))
}

func backgroundRequest(state backgroundState, method, route string) (int, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, "http://"+state.Web+route, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("X-Diskord-Bg-Token", state.Token)
	resp, err := backgroundClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	return resp.StatusCode, strings.TrimSpace(string(data)), err
}

func backgroundAlive(state backgroundState) bool {
	status, body, err := backgroundRequest(state, http.MethodGet, "/internal/bg-status")
	return err == nil && status == http.StatusOK && body == strconv.Itoa(state.PID)
}

func runBackground(configPath string) error {
	started := time.Now()
	m, cwd, err := localBackground(configPath)
	if err != nil {
		return err
	}
	statePath, err := backgroundPath(cwd)
	if err != nil {
		return err
	}
	if old, err := readBackground(statePath); err == nil {
		if old.Root != cwd {
			return errors.New("background state belongs to another directory")
		}
		if backgroundAlive(old) {
			return fmt.Errorf("diskord is already running in the background (PID %d)", old.PID)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read background state: %w", err)
	}
	logFile, logPath, err := logfile.OpenOutputAt(cwd, "diskord-bg", started)
	if err != nil {
		return err
	}
	defer logFile.Close()
	token, err := securefs.Random(32)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	child := exec.Command(executable, "--config", m.Path, "run")
	child.Dir = cwd
	child.Env = append(os.Environ(), "DISKORD_BG_ROOT="+cwd, "DISKORD_BG_TOKEN="+token)
	child.Stdin = nil
	child.Stdout, child.Stderr = logFile, logFile
	detachBackground(child)
	if err := child.Start(); err != nil {
		return err
	}
	state := backgroundState{Root: cwd, Config: m.Path, Web: m.Current().Web.Listen, PID: child.Process.Pid, Token: token}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	if err := writeBackground(statePath, cwd, state); err != nil {
		_ = child.Process.Kill()
		<-done
		return err
	}
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if backgroundAlive(state) {
			fmt.Printf("diskord started in %s (PID %d); output: %s\n", cwd, state.PID, logPath)
			return nil
		}
		select {
		case err := <-done:
			_ = os.Remove(statePath)
			return fmt.Errorf("background diskord exited during startup: %v; see %s", err, logPath)
		case <-deadline.C:
			_ = child.Process.Kill()
			<-done
			_ = os.Remove(statePath)
			return fmt.Errorf("background diskord did not become ready within 15 seconds; see %s", logPath)
		case <-ticker.C:
		}
	}
}

func stopBackground(configPath string) error {
	cwd, abs, err := localBackgroundPath(configPath)
	if err != nil {
		return err
	}
	statePath, err := backgroundPath(cwd)
	if err != nil {
		return err
	}
	state, err := readBackground(statePath)
	if os.IsNotExist(err) {
		return errors.New("no background diskord is registered in the current working directory")
	}
	if err != nil {
		return err
	}
	if state.Root != cwd || state.Config != abs {
		return errors.New("background state does not match this working directory and configuration")
	}
	if !backgroundAlive(state) {
		return errors.New("registered background instance is not responding; state file retained for inspection")
	}
	status, _, err := backgroundRequest(state, http.MethodPost, "/internal/bg-stop")
	if err != nil {
		return err
	}
	if status != http.StatusAccepted {
		return fmt.Errorf("background stop request rejected (HTTP %d)", status)
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if !backgroundAlive(state) {
			// The UI listener closes before the proxy and database finish draining.
			// Wait for the runtime lock so a new run-bg can start immediately.
			if lock, lockErr := securefs.Acquire(cwd); lockErr == nil {
				_ = lock.Close()
				if err := os.Remove(statePath); err != nil {
					return err
				}
				fmt.Printf("diskord stopped in %s (PID %d)\n", cwd, state.PID)
				return nil
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("background shutdown did not complete within 45 seconds; try stop-bg again")
}
