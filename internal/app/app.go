package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"diskord/internal/ca"
	"diskord/internal/capture"
	"diskord/internal/config"
	"diskord/internal/gateway"
	"diskord/internal/logfile"
	"diskord/internal/mitm"
	"diskord/internal/securefs"
	"diskord/internal/store"
	"diskord/internal/ui"
	"diskord/internal/version"
)

func Run(ctx context.Context, path string) error {
	started := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m, e := config.New(path)
	if e != nil {
		return e
	}
	lock, e := securefs.Acquire(m.Root)
	if e != nil {
		return e
	}
	defer lock.Close()
	c := m.Current()
	if c.Logging.File.Enabled {
		writer, err := logfile.OpenAt(m.Root, "diskord", started)
		if err != nil {
			return fmt.Errorf("open diskord file logger: %w", err)
		}
		defer writer.Close()
		log.SetOutput(io.MultiWriter(os.Stderr, writer))
		defer log.SetOutput(os.Stderr)
	}
	log.Printf("diskord process started at %s", started.Format(time.RFC3339Nano))
	if c.CA.Cert == "" {
		return errors.New("no CA selected; run diskord ca issue, then diskord ca select before starting the proxy")
	}
	root, e := ca.Load(m.Root, c.CA.Cert, c.CA.Key)
	if e != nil {
		return e
	}
	live := ca.NewLive(root)
	db, e := store.Open(m.Root)
	if e != nil {
		return e
	}
	defer db.Close()
	engine := capture.New(db, m.Current)
	engine.StartBackfill()
	defer engine.Close()
	backend, e := mitm.Start(live, engine)
	if e != nil {
		return e
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = backend.Close(ctx)
	}()
	var background []ui.BackgroundControl
	if token := os.Getenv("DISKORD_BG_TOKEN"); len(token) == 64 && os.Getenv("DISKORD_BG_ROOT") == m.Root {
		background = append(background, ui.BackgroundControl{Token: token, Stop: cancel})
	}
	control, e := ui.New(m, live, engine, db, background...)
	if e != nil {
		return e
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = control.Close(ctx)
	}()
	front, e := gateway.New(c.Proxy.Listen, backend.Addr, live, engine, m.Current)
	if e != nil {
		return e
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = front.Close(ctx)
	}()
	failures := make(chan error, 3)
	go func() { failures <- control.Serve() }()
	go func() { failures <- front.Serve() }()
	go func() { failures <- <-backend.Errors }()
	log.Printf("diskord %s; runtime=%s proxy=http://%s ui=http://%s", version.Current, m.Root, c.Proxy.Listen, c.Web.Listen)
	fmt.Println("Use 'diskord --config <yaml-path> ui-token' to retrieve the console token. Capture is passive; Ctrl+C stops the proxy.")
	select {
	case <-ctx.Done():
		return nil
	case e := <-failures:
		if e == nil || errors.Is(e, http.ErrServerClosed) {
			return errors.New("a required listener stopped unexpectedly")
		}
		return fmt.Errorf("a required listener failed: %w", e)
	}
}
