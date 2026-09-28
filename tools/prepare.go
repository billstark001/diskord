// prepare obtains a pinned build-time asset. No network asset loading occurs in
// the running application. The git blob digest was read from the upstream API.
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const assetURL = "https://raw.githubusercontent.com/bigskysoftware/htmx/v4.0.0/dist/htmx.min.js"
const expectedBlob = "6e099f1d3bfc5362b6f419919bceb522ca80d5fa"

func digest(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "asset preparation:", err)
		os.Exit(1)
	}
}
func run() error {
	dest := filepath.Join("internal", "ui", "static", "htmx.min.js")
	if b, e := os.ReadFile(dest); e == nil && digest(b) == expectedBlob {
		fmt.Println("Pinned htmx asset already verified.")
		return nil
	}
	client := &http.Client{Timeout: 45 * time.Second}
	res, e := client.Get(assetURL)
	if e != nil {
		return fmt.Errorf("build needs internet access for the pinned htmx asset: %w", e)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("asset server returned HTTP %d", res.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, 1024*1024))
	if e != nil {
		return e
	}
	if digest(data) != expectedBlob {
		return errors.New("htmx git-blob digest mismatch; refusing to embed")
	}
	temp := dest + ".download"
	if e = os.WriteFile(temp, data, 0600); e != nil {
		return e
	}
	defer os.Remove(temp)
	if e = os.Rename(temp, dest); e != nil {
		return e
	}
	fmt.Println("Pinned htmx 4.0.0 downloaded and verified.")
	return nil
}
