package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mohammed-shakir/hit-visualizer/internal/kafkaconsumer"
	ws "github.com/mohammed-shakir/hit-visualizer/internal/websocket"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "hit-visualizer exited: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	httpAddr := getenv("HTTP_ADDR", ":8081")
	kafkaBrokers := splitAndTrim(getenv("KAFKA_BROKERS", "localhost:9092"))
	kafkaTopic := getenv("KAFKA_TOPIC", "spatial-hit-events")
	groupID := getenv("KAFKA_GROUP_ID", "hit-visualizer")
	logLevel := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL")))

	logger := newLogger(logLevel)
	logger.Info("starting hit-visualizer",
		"http_addr", httpAddr,
		"kafka_brokers", kafkaBrokers,
		"kafka_topic", kafkaTopic,
		"kafka_group_id", groupID,
	)

	// WebSocket hub
	hub := ws.NewHub(logger)
	go hub.Run()

	// Kafka consumer.
	cons, err := kafkaconsumer.NewConsumer(kafkaBrokers, groupID, kafkaTopic, logger)
	if err != nil {
		return fmt.Errorf("init kafka consumer: %w", err)
	}
	defer func() {
		if err := cons.Close(); err != nil {
			logger.Error("kafka consumer close", "err", err)
		}
	}()

	// Start consuming Kafka in the background.
	go func() {
		if err := cons.Start(ctx); err != nil {
			logger.Error("kafka consumer exited", "err", err)
		}
		logger.Info("kafka consumer stopped")
	}()

	// Forwarder: Kafka events to WebSocket hub.
	go func() {
		for evt := range cons.Events() {
			b, err := json.Marshal(evt)
			if err != nil {
				logger.Error("marshal hit event", "err", err)
				continue
			}
			hub.Broadcast(b)
		}
		logger.Info("forwarder stopped (events channel closed)")
	}()

	http.Handle("/", http.FileServer(http.Dir("frontend")))
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWS(hub, w, r)
	})

	srv := &http.Server{
		Addr:              httpAddr,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)

	// Start HTTP server.
	go func() {
		logger.Info("http server listening", "addr", httpAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown requested, stopping server")
	case err := <-errCh:
		logger.Error("http server error", "err", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", "err", err)
		return err
	}

	logger.Info("hit-visualizer stopped cleanly")
	return nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: &lvl,
	})
	return slog.New(h)
}
