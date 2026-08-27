package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/citradigital/cinemas/internal/seed"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	config := seed.ConfigFromEnvironment(os.Getenv)
	runner := seed.NewRunner(&http.Client{Timeout: 15 * time.Second}, []seed.Cinema{
		{Name: "Cineverse Jakarta", Address: "Jl. M.H. Thamrin No. 1", City: "Jakarta", Studios: []string{"Studio 1", "Studio 2"}},
		{Name: "Cineverse Bandung", Address: "Jl. Asia Afrika No. 8", City: "Bandung", Studios: []string{"Studio 1"}},
	}, []string{"Inception", "Dune: Part Two", "Oppenheimer"})
	if err := runner.Run(ctx, config); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
	slog.Info("seed completed")
}
