package capture

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"diskord/internal/store"
)

// StartBackfill retries previously observed CDN paths after resource capture is
// enabled. It never uses Discord credentials, retained URL queries, or a proxy.
func (e *Engine) StartBackfill() {
	if !e.Config().Resources.Enabled {
		return
	}
	e.backfillMu.Lock()
	if e.backfillRunning {
		e.backfillMu.Unlock()
		return
	}
	e.backfillRunning = true
	e.backfillWG.Add(1)
	e.backfillMu.Unlock()
	go func() {
		defer e.backfillWG.Done()
		defer func() { e.backfillMu.Lock(); e.backfillRunning = false; e.backfillMu.Unlock() }()
		e.backfill()
	}()
}

func (e *Engine) backfill() {
	targets, err := e.Store.MissingResources(e.writeCtx)
	if err != nil {
		log.Printf("resource backfill could not enumerate targets: %v", err)
		return
	}
	client := &http.Client{
		Timeout:       6 * time.Second,
		Transport:     &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 2, IdleConnTimeout: 10 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	success, skipped := 0, 0
targetsLoop:
	for _, target := range targets {
		if e.writeCtx.Err() != nil || !e.Config().Resources.Enabled {
			break
		}
		if err := e.fetchMissing(client, target); err != nil {
			if errors.Is(err, store.ErrQuota) {
				e.Metrics.AssetSkipped.Add(1)
				break
			}
			skipped++
		} else {
			success++
			e.Metrics.Assets.Add(1)
		}
		select {
		case <-e.writeCtx.Done():
			break targetsLoop
		case <-time.After(75 * time.Millisecond):
		}
	}
	if len(targets) > 0 {
		log.Printf("resource backfill finished: targets=%d saved=%d unavailable=%d", len(targets), success, skipped)
	}
}

func (e *Engine) fetchMissing(client *http.Client, target store.ResourceTarget) error {
	if !strings.HasPrefix(target.Path, "/") || strings.ContainsAny(target.Path, "?#") {
		return errors.New("invalid CDN path")
	}
	u, err := url.Parse("https://" + target.Host + target.Path)
	if err != nil || u.Host != target.Host || u.RawQuery != "" {
		return errors.New("invalid CDN URL")
	}
	req, err := http.NewRequestWithContext(e.writeCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("resource unavailable")
	}
	cfg := e.Config()
	if resp.ContentLength > int64(cfg.Resources.MaxBytes) {
		return errors.New("resource too large")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(cfg.Resources.MaxBytes)+1))
	if err != nil || len(data) > cfg.Resources.MaxBytes {
		return errors.New("resource too large or unreadable")
	}
	if !e.Config().Resources.Enabled {
		return errors.New("resource capture disabled")
	}
	ctx, cancel := context.WithTimeout(e.writeCtx, 10*time.Second)
	defer cancel()
	return e.Store.SaveAsset(ctx, target.Host, target.Path, resp.Header.Get("Content-Type"), data, cfg.Resources.MaxTotalBytes)
}
