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
)

func Run(ctx context.Context, path string) error {
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
		writer, err := logfile.Open(m.Root, "diskord")
		if err != nil {
			return fmt.Errorf("open diskord file logger: %w", err)
		}
		defer writer.Close()
		log.SetOutput(io.MultiWriter(os.Stderr, writer))
		defer log.SetOutput(os.Stderr)
	}
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
	control, e := ui.New(m, live, engine, db)
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
	log.Printf("diskord v0.1; runtime=%s proxy=http://%s ui=http://%s", m.Root, c.Proxy.Listen, c.Web.Listen)
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
