package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/citradigital/cinemas/internal/booking"
	"github.com/citradigital/cinemas/internal/httpapi"
	"github.com/citradigital/cinemas/internal/postgres"
	"github.com/citradigital/cinemas/internal/seatinventory"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	databasePingTimeout = 5 * time.Second
	defaultHoldDuration = 10 * time.Minute
	readHeaderTimeout   = 5 * time.Second
	shutdownTimeout     = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		logger.Error("create PostgreSQL pool", "error", err)
		os.Exit(1)
	}

	pingContext, cancelPing := context.WithTimeout(context.Background(), databasePingTimeout)
	err = pool.Ping(pingContext)
	cancelPing()
	if err != nil {
		pool.Close()
		logger.Error("ping PostgreSQL", "error", err)
		os.Exit(1)
	}

	bookingService := booking.NewService(postgres.NewBookingRepository(pool), defaultHoldDuration, time.Now)
	seatMapService := seatinventory.NewService(postgres.NewSeatMapRepository(pool))
	server := &http.Server{
		Addr:              environmentOr("ADDR", ":8080"),
		Handler:           httpapi.NewServerWithSeatMap(bookingService, seatMapService),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("starting API", "addr", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case signal := <-shutdown:
		logger.Info("shutting down API", "signal", signal.String())
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
		shutdownErr := server.Shutdown(shutdownContext)
		cancelShutdown()
		if shutdownErr != nil {
			pool.Close()
			logger.Error("shutdown API", "error", shutdownErr)
			os.Exit(1)
		}
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			pool.Close()
			logger.Error("serve API", "error", err)
			os.Exit(1)
		}
	}
	pool.Close()
}

func environmentOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
