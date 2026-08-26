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

	"github.com/citradigital/cinemas/internal/auth"
	"github.com/citradigital/cinemas/internal/booking"
	"github.com/citradigital/cinemas/internal/catalog"
	"github.com/citradigital/cinemas/internal/httpapi"
	"github.com/citradigital/cinemas/internal/payments"
	"github.com/citradigital/cinemas/internal/postgres"
	"github.com/citradigital/cinemas/internal/scheduling"
	"github.com/citradigital/cinemas/internal/seatinventory"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	databasePingTimeout   = 5 * time.Second
	defaultHoldDuration   = 10 * time.Minute
	readHeaderTimeout     = 5 * time.Second
	shutdownTimeout       = 10 * time.Second
	defaultAccessTokenTTL = time.Hour
	minimumJWTSecretBytes = 32
)

type authenticationConfig struct {
	jwtSecret           []byte
	accessTokenTTL      time.Duration
	adminBootstrapToken string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	authenticationConfig, err := loadAuthenticationConfig()
	if err != nil {
		logger.Error("load authentication configuration", "error", err)
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
	movieCatalogService := catalog.NewService(postgres.NewMoviesRepository(pool))
	showtimeService := scheduling.NewService(postgres.NewShowtimesRepository(pool))
	paymentService := payments.NewService(postgres.NewPaymentsRepository(pool), time.Now)
	authenticationService := auth.NewService(
		postgres.NewAuthRepository(pool),
		authenticationConfig.jwtSecret,
		authenticationConfig.accessTokenTTL,
		time.Now,
	)
	server := &http.Server{
		Addr: environmentOr("ADDR", ":8080"),
		Handler: httpapi.NewServerWithAllFeatures(
			bookingService,
			seatMapService,
			movieCatalogService,
			showtimeService,
			paymentService,
			authenticationService,
			authenticationConfig.adminBootstrapToken,
		),
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

func loadAuthenticationConfig() (authenticationConfig, error) {
	jwtSecret := os.Getenv("AUTH_JWT_SECRET")
	if len(jwtSecret) < minimumJWTSecretBytes {
		return authenticationConfig{}, errors.New("AUTH_JWT_SECRET must be at least 32 bytes")
	}

	accessTokenTTL := defaultAccessTokenTTL
	if value := os.Getenv("AUTH_ACCESS_TOKEN_TTL"); value != "" {
		parsedDuration, err := time.ParseDuration(value)
		if err != nil || parsedDuration <= 0 {
			return authenticationConfig{}, errors.New("AUTH_ACCESS_TOKEN_TTL must be a positive duration")
		}
		accessTokenTTL = parsedDuration
	}

	return authenticationConfig{
		jwtSecret:           []byte(jwtSecret),
		accessTokenTTL:      accessTokenTTL,
		adminBootstrapToken: os.Getenv("AUTH_ADMIN_BOOTSTRAP_TOKEN"),
	}, nil
}

func environmentOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
