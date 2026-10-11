package ui

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"diskord/internal/archive"
	"diskord/internal/capture"
	"diskord/internal/config"
	"diskord/internal/model"
)

// The normal suite uses 32 MiB; DISKORD_EXPORT_STRESS=1 exercises 320 MiB
// of resources plus more than 6 MiB of Markdown over a real loopback socket.
func TestLiveExportCompletionAndInterruption(t *testing.T) {
	base, _ := apiFixture(t)
	resources, messages := 4, 256
	if os.Getenv("DISKORD_EXPORT_STRESS") == "1" {
		resources, messages = 40, 2048
	}
	ctx := context.Background()
	data := make([]byte, 8<<20)
	for i := 0; i < resources; i++ {
		data[0] = byte(i)
		path := fmt.Sprintf("/attachments/2/%d/image.png", i+100)
		if err := base.Store.SaveAsset(ctx, "cdn.discordapp.com", path, "image/png", data, 512<<20); err != nil {
			t.Fatal(err)
		}
	}
	data = nil
	body := strings.Repeat("synthetic observed message body ", 100)
	batch := model.Batch{Source: "ws"}
	for i := 0; i < messages; i++ {
		m := model.Message{ID: fmt.Sprint(i + 100), ChannelID: model.Ptr("2"), AuthorID: model.Ptr("9"), Content: &body}
		if i < resources {
			m.HasAttachments = true
			m.Attachments = []model.Attachment{{ID: fmt.Sprint(i + 100), Name: "image.png", Host: "cdn.discordapp.com", Path: fmt.Sprintf("/attachments/2/%d/image.png", i+100), Size: 8 << 20}}
		}
		batch.Messages = append(batch.Messages, m)
	}
	if err := base.Store.Apply(ctx, batch); err != nil {
		t.Fatal(err)
	}
	batch = model.Batch{}
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	reservation.Close()
	path := filepath.Join(base.Manager.Root, "diskord.yaml")
	yaml, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(yaml), "127.0.0.1:3900", address, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := config.New(path)
	if err != nil {
		t.Fatal(err)
	}
	engine := capture.New(base.Store, manager.Current)
	u, err := New(manager, nil, engine, base.Store)
	if err != nil {
		engine.Close()
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- u.Serve() }()
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = u.Close(shutdown)
		<-served
		engine.Close()
	})
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 90 * time.Second}
	request := func(ctx context.Context, query string) (*http.Response, error) {
		r, err := http.NewRequestWithContext(ctx, "GET", "http://"+address+"/api/v1/"+query, nil)
		if err != nil {
			return nil, err
		}
		r.Header.Set("Authorization", "Bearer "+u.apiToken)
		return client.Do(r)
	}
	waitClean := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			dirs, err := filepath.Glob(filepath.Join(manager.Root, "tmp", "export-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(dirs) == 0 && len(u.exportSlot) == 0 {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("interrupted export retained staging or admission slot")
	}

	// Keep capture writes active during preparation, ZIP work, and delivery.
	writesDone := make(chan struct{})
	writeStopped := make(chan struct{})
	go func() {
		defer close(writeStopped)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for i := 100000; ; i++ {
			select {
			case <-writesDone:
				return
			case <-ticker.C:
				engine.Submit(model.Batch{Source: "ws", Messages: []model.Message{{ID: fmt.Sprint(i), ChannelID: model.Ptr("3"), Content: model.Ptr("concurrent capture")}}})
			}
		}
	}()
	defer func() { close(writesDone); <-writeStopped }()

	// Sample live heap, excluding fixture setup and reading the finished output.
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	var peak atomic.Uint64
	peak.Store(baseline.HeapAlloc)
	samplingDone := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-samplingDone:
				return
			case <-ticker.C:
				var stats runtime.MemStats
				runtime.ReadMemStats(&stats)
				if stats.HeapAlloc > peak.Load() {
					peak.Store(stats.HeapAlloc)
				}
			}
		}
	}()
	stopSample := func() { close(samplingDone); <-sampled }
	defer func() {
		if samplingDone != nil {
			stopSample()
		}
	}()
	start := time.Now()
	response, err := request(ctx, "export?format=md&attachments=true&guild=1&split_bytes=1048576")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		t.Fatal(response.Status)
	}
	// A stalled/slow download holds only the export slot, and status remains usable.
	status, err := request(ctx, "status")
	if err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	status.Body.Close()
	if status.StatusCode != 200 {
		response.Body.Close()
		t.Fatal(status.Status)
	}
	denied, err := request(ctx, "export?format=md")
	if err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	denied.Body.Close()
	if denied.StatusCode != 429 {
		response.Body.Close()
		t.Fatal("concurrent export was not rejected", denied.Status)
	}
	output := filepath.Join(t.TempDir(), "snapshot.zip")
	f, err := os.Create(output)
	if err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	copied, copyErr := io.Copy(f, response.Body)
	response.Body.Close()
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil || copied != response.ContentLength {
		t.Fatal(copied, response.ContentLength, copyErr, closeErr)
	}
	elapsed := time.Since(start)
	stopSample()
	samplingDone = nil
	delta := peak.Load() - baseline.HeapAlloc
	if delta > 128<<20 {
		t.Fatalf("export heap grew by %.1f MiB", float64(delta)/(1<<20))
	}
	waitClean()
	z, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	mdSize := uint64(0)
	fileCount := 0
	for _, entry := range z.File {
		if strings.HasPrefix(entry.Name, "attachments/") {
			fileCount++
		}
		if strings.HasSuffix(entry.Name, ".md") {
			mdSize += entry.UncompressedSize64
		}
		if entry.Name == "manifest.json" {
			r, err := entry.Open()
			if err != nil {
				z.Close()
				t.Fatal(err)
			}
			var manifest struct {
				Summary archive.Summary `json:"summary"`
			}
			err = json.NewDecoder(r).Decode(&manifest)
			r.Close()
			if err != nil || !manifest.Summary.Complete || manifest.Summary.Messages != int64(messages+1) || manifest.Summary.AttachmentFiles != resources {
				z.Close()
				t.Fatal(manifest, err)
			}
		}
		// Read every member to verify CRCs, without retaining resource contents.
		r, err := entry.Open()
		if err != nil {
			z.Close()
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, r)
		r.Close()
		if err != nil {
			z.Close()
			t.Fatal(err)
		}
	}
	z.Close()
	os.Remove(output)
	if fileCount != resources {
		t.Fatal(fileCount, resources)
	}
	if os.Getenv("DISKORD_EXPORT_STRESS") == "1" && mdSize < 6<<20 {
		t.Fatal("fixture Markdown too small", mdSize)
	}
	t.Logf("complete snapshot: %.2f MiB Markdown + %d MiB resources, %.2fs, peak additional Go heap %.2f MiB", float64(mdSize)/(1<<20), resources*8, elapsed.Seconds(), float64(delta)/(1<<20))

	// Cancel after ZIP copying has actually started, before response headers.
	cancelCtx, cancel := context.WithCancel(ctx)
	canceled := make(chan error, 1)
	go func() {
		r, err := request(cancelCtx, "export?format=md&attachments=true&guild=1&split_bytes=1048576")
		if r != nil {
			r.Body.Close()
		}
		canceled <- err
	}()
	deadline := time.Now().Add(30 * time.Second)
	packing := false
	for time.Now().Before(deadline) {
		paths, _ := filepath.Glob(filepath.Join(manager.Root, "tmp", "export-*", "archive.zip"))
		for _, p := range paths {
			if info, err := os.Stat(p); err == nil && info.Size() > 1<<20 {
				packing = true
				break
			}
		}
		if packing {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if !packing {
		t.Fatal("did not observe active ZIP preparation")
	}
	select {
	case err := <-canceled:
		if err == nil {
			t.Fatal("canceled preparation succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("preparation did not cancel")
	}
	waitClean()

	// Abort a live socket download and prove cleanup and subsequent requests work.
	deliveryCtx, abort := context.WithCancel(ctx)
	r, err := request(deliveryCtx, "export?format=md&attachments=true&guild=1&split_bytes=1048576")
	if err != nil {
		abort()
		t.Fatal(err)
	}
	_, err = io.CopyN(io.Discard, r.Body, 32<<10)
	abort()
	r.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	waitClean()
	r, err = request(ctx, "export?format=md&thread=999")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	if err != nil || r.StatusCode != 200 {
		t.Fatal(r.Status, err)
	}
	waitClean()
	metrics := engine.Metrics.Snapshot()
	if metrics.BatchesCommitted == 0 || metrics.StoreErrors != 0 || metrics.DroppedBatches != 0 {
		t.Fatal("capture disrupted", metrics)
	}
	t.Logf("ZIP preparation canceled; socket delivery aborted; staging and slot released; concurrent capture committed %d batches without errors", metrics.BatchesCommitted)
}
