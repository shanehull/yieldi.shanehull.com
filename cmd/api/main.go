package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shanehull/yieldi/internal/handlers"
	"github.com/shanehull/yieldi/internal/quant"
	"github.com/shanehull/yieldi/internal/satellite"
	"github.com/shanehull/yieldi/internal/weather"
)

func main() {
	// Setup logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Initialize STAT service
	satelliteService := satellite.NewService(logger, "", "")

	// Initialize dependencies
	model := quant.NewYieldModel()
	weatherService := weather.NewService(logger)

	// Create HTTP server
	server := handlers.NewServer(model, satelliteService, weatherService, logger)
	mux := http.NewServeMux()

	// Routes
	mux.HandleFunc("/health", server.HandleHealth)
	mux.HandleFunc("/api/assess", server.HandleAssess)

	// Serve static files (Tailwind, JS, etc.) from /static
	staticHandler := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", staticHandler))

	// Root handler (serve dashboard)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "static/index.html")
	})

	httpServer := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // Satellite/weather requests can be slow
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		logger.Info("starting HTTP server", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown on signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}
