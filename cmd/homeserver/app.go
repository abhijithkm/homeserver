package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/abhijithkm/homeserver/internal/paths"
	"github.com/abhijithkm/homeserver/internal/server"
	"github.com/abhijithkm/homeserver/internal/store"
	"github.com/abhijithkm/homeserver/web"
)

// newLogger writes structured JSON logs to a size-bounded rotating file
// (5 MB x 4 backups, 30 days) and, for interactive runs, to stderr as well.
func newLogger(d paths.Dirs, interactive bool) *slog.Logger {
	_ = os.MkdirAll(d.LogDir, 0o755)
	var w io.Writer = &lumberjack.Logger{Filename: d.LogFile, MaxSize: 5, MaxBackups: 4, MaxAge: 30}
	if interactive {
		w = io.MultiWriter(w, os.Stderr)
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// runServer opens the database and serves until ctx is cancelled. A restart
// requested from the web UI re-reads settings and re-listens in-process.
func runServer(ctx context.Context, d paths.Dirs, interactive bool) error {
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(256 << 20)
	log := newLogger(d, interactive)
	slog.SetDefault(log)
	log.Info("starting", "version", version, "data", d.Data)

	st, recovered, err := store.Open(d.DB)
	if err != nil {
		log.Error("cannot open database", "err", err)
		return fmt.Errorf("cannot open database %s: %w", d.DB, err)
	}
	defer st.Close()
	if recovered != "" {
		log.Error("database was corrupt and has been replaced; previous file kept", "kept", recovered)
	}
	for {
		srv, err := server.New(server.Config{Store: st, Dirs: d, Version: version, Log: log, Web: web.FS()})
		if err != nil {
			return err
		}
		err = srv.Run(ctx)
		if errors.Is(err, server.ErrRestart) {
			log.Info("restarting in-process")
			continue
		}
		if err != nil {
			log.Error("server stopped with error", "err", err)
		} else {
			log.Info("stopped cleanly")
		}
		return err
	}
}
