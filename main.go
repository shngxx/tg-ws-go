package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	port := flag.Int("port", 1080, "Listen port")
	host := flag.String("host", "127.0.0.1", "Listen host")
	user := flag.String("user", "", "SOCKS5 username (enables auth if set)")
	pass := flag.String("pass", "", "SOCKS5 password")
	verbose := flag.Bool("v", false, "Verbose (debug) logging")
	logFile := flag.String("log-file", "", "Log file path (optional)")
	logMaxMB := flag.Float64("log-max-mb", 5, "Max log file size in MB before rotation")
	logBackups := flag.Int("log-backups", 0, "Number of rotated log files to keep")
	bufKB := flag.Int("buf-kb", 256, "Socket send/recv buffer size in KB")
	poolSize := flag.Int("pool-size", 4, "WS connection pool size per DC (min 0)")

	var dcIPs []string
	flag.Func("dc-ip", "Target IP for a DC (repeatable), e.g. 2:149.154.167.220", func(s string) error {
		dcIPs = append(dcIPs, s)
		return nil
	})

	flag.Parse()

	if len(dcIPs) == 0 {
		dcIPs = []string{"2:149.154.167.220", "4:149.154.167.220"}
	}

	dcOpt, err := ParseDcIPList(dcIPs)
	if err != nil {
		slog.Error("invalid --dc-ip", "err", err)
		os.Exit(1)
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.String("time", time.Now().Format("15:04:05"))
			}
			return a
		},
	}

	var writers []io.Writer
	writers = append(writers, os.Stderr)
	if *logFile != "" {
		if dir := filepath.Dir(filepath.Clean(*logFile)); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				slog.Error("log dir", "err", err)
				os.Exit(1)
			}
		}
		f, err := openRotatingLog(*logFile, *logMaxMB, *logBackups)
		if err != nil {
			slog.Error("log file", "err", err)
			os.Exit(1)
		}
		defer f.Close()
		writers = append(writers, f)
	}

	log := slog.New(slog.NewTextHandler(io.MultiWriter(writers...), opts))

	stats := &Stats{}
	pool := NewWsPool(max(0, *poolSize), max(4, *bufKB)*1024, log, stats)
	srv := NewServer(dcOpt, *user, *pass, pool, stats, log, *bufKB)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Info("shutdown signal received")
	}()

	if err := srv.Run(ctx, *host, *port); err != nil && ctx.Err() == nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
	log.Info("server stopped", "final_stats", stats.Summary())
}
