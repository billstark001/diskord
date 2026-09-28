package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"diskord/internal/ca"
	"diskord/internal/config"
	"diskord/internal/securefs"
	"diskord/internal/ui"
)

func initialize(path, runtime, tool string) error {
	root, e := securefs.Root(runtime)
	if e != nil {
		return e
	}
	if e = securefs.Prepare(root); e != nil {
		return e
	}
	lock, e := securefs.Acquire(root)
	if e != nil {
		return e
	}
	defer lock.Close()
	target, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	f, e := securefs.NewFile(target)
	if e != nil {
		return fmt.Errorf("refusing to overwrite config or create it in a missing parent: %w", e)
	}
	f.Close()
	success := false
	defer func() {
		if !success {
			os.Remove(target)
		}
	}()
	storedRoot := root
	if runtime == "." {
		storedRoot = "."
	}
	text := strings.Replace(config.Example, "runtime_dir: .", "runtime_dir: "+strconv.Quote(storedRoot), 1)
	text = strings.Replace(text, "openssl: openssl", "openssl: "+strconv.Quote(tool), 1)
	if e = securefs.AtomicWrite(root, target, []byte(text)); e != nil {
		return e
	}
	if _, e = ui.Token(root); e != nil {
		return e
	}
	success = true
	fmt.Printf("Initialized.\nConfig: %s\nRuntime: %s\nNext: diskord ca issue --cert ca/root.pem --key ca/root.key\n", target, root)
	return nil
}
func doctor(m *config.Manager) error {
	c := m.Current()
	fmt.Printf("Config: valid\nRuntime: %s\nProxy: %s\nWeb: %s\n", m.Root, c.Proxy.Listen, c.Web.Listen)
	if p, e := exec.LookPath(c.CA.OpenSSL); e == nil {
		fmt.Println("CA toolchain:", p)
	} else {
		fmt.Println("CA toolchain: MISSING (issuance unavailable; existing CA can still run)")
	}
	if c.CA.Cert == "" {
		return errors.New("no CA selected")
	}
	a, e := ca.Load(m.Root, c.CA.Cert, c.CA.Key)
	if e != nil {
		return e
	}
	fmt.Printf("CA: valid\nSHA-256: %s\nExpires: %s\n", a.Fingerprint(), a.Root.NotAfter.Format(time.RFC3339))
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	response, e := client.Get("http://" + c.Web.Listen + "/healthz")
	if e != nil {
		fmt.Println("Control plane: not reachable (normal before run)")
	} else {
		response.Body.Close()
		fmt.Println("Control plane HTTP status:", response.StatusCode)
	}
	fmt.Println("Doctor does not prove that Chrome/Discord routes through the proxy or trusts the CA.")
	return nil
}
