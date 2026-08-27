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

	"github.com/citradigital/cinemas/internal/admin"
	"github.com/citradigital/cinemas/internal/auth"
	"github.com/citradigital/cinemas/internal/booking"
	"github.com/citradigital/cinemas/internal/catalog"
	"github.com/citradigital/cinemas/internal/httpapi"
	"github.com/citradigital/cinemas/internal/payments"
	"github.com/citradigital/cinemas/internal/postgres"
	"github.com/citradigital/cinemas/internal/scheduling"
	"github.com/citradigital/cinemas/internal/seatinventory"
	"github.com/citradigital/cinemas/internal/tickets"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	databasePingTimeout        = 5 * time.Second
	defaultHoldDuration        = 10 * time.Minute
	readHeaderTimeout          = 5 * time.Second
	shutdownTimeout            = 10 * time.Second
	defaultAccessTokenTTL      = time.Hour
	minimumJWTSecretBytes      = 32
	minimumWebhookSecretBytes  = 32
	holdExpiryInterval         = 30 * time.Second
	holdExpiryBatchSize        = 100
	defaultWebhookReplayWindow = 5 * time.Minute
	ticketDeliveryInterval     = 30 * time.Second
	ticketDeliveryBatchSize    = 100
	ticketDeliveryRetryDelay   = time.Minute
)

type authenticationConfig struct {
	jwtSecret           []byte
	accessTokenTTL      time.Duration
	adminBootstrapToken string
}

type paymentConfig struct {
	provider      string
	webhookSecret string
	replayWindow  time.Duration
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
	paymentConfig, err := loadPaymentConfig()
	if err != nil {
		logger.Error("load payment configuration", "error", err)
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
	expiryContext, cancelExpiry := context.WithCancel(context.Background())
	defer cancelExpiry()
	go runHoldExpiryWorker(expiryContext, logger, bookingService)
	seatMapService := seatinventory.NewService(postgres.NewSeatMapRepository(pool))
	movieCatalogService := catalog.NewService(postgres.NewMoviesRepository(pool))
	showtimeService := scheduling.NewService(postgres.NewShowtimesRepository(pool))
	paymentService := payments.NewService(
		postgres.NewPaymentsRepository(pool),
		payments.NewFakeProvider(paymentConfig.webhookSecret, paymentConfig.replayWindow),
		time.Now,
	)
	ticketService := tickets.NewService(
		postgres.NewTicketsRepository(pool),
		tickets.NewLoggingNotifier(logger),
		time.Now,
		ticketDeliveryRetryDelay,
	)
	ticketDeliveryContext, cancelTicketDelivery := context.WithCancel(context.Background())
	defer cancelTicketDelivery()
	go runTicketDeliveryWorker(ticketDeliveryContext, logger, ticketService)
	authenticationService := auth.NewService(
		postgres.NewAuthRepository(pool),
		authenticationConfig.jwtSecret,
		authenticationConfig.accessTokenTTL,
		time.Now,
	)
	api := httpapi.NewServerWithAllFeatures(
		bookingService,
		seatMapService,
		movieCatalogService,
		showtimeService,
		paymentService,
		authenticationService,
		authenticationConfig.adminBootstrapToken,
	)
	api.EnableAdminCinemaRoutes(authenticationService, admin.NewService(postgres.NewAdminRepository(pool)))
	api.EnableTicketRoutes(authenticationService, ticketService)
	server := &http.Server{
		Addr:              environmentOr("ADDR", ":8080"),
		Handler:           api,
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

func runHoldExpiryWorker(ctx context.Context, logger *slog.Logger, service *booking.Service) {
	ticker := time.NewTicker(holdExpiryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expired, err := service.ExpirePendingHolds(ctx, holdExpiryBatchSize)
			if err != nil {
				logger.Error("expire pending holds", "error", err)
				continue
			}
			if expired > 0 {
				logger.Info("expired pending holds", "count", expired)
			}
		}
	}
}

func runTicketDeliveryWorker(ctx context.Context, logger *slog.Logger, service *tickets.Service) {
	ticker := time.NewTicker(ticketDeliveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			delivered, err := service.DeliverPending(ctx, ticketDeliveryBatchSize)
			if err != nil {
				logger.Error("deliver tickets", "error", err)
				continue
			}
			if delivered > 0 {
				logger.Info("delivered tickets", "count", delivered)
			}
		}
	}
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

func loadPaymentConfig() (paymentConfig, error) {
	provider := environmentOr("PAYMENT_PROVIDER", payments.FakeProviderName)
	if provider != payments.FakeProviderName {
		return paymentConfig{}, errors.New("PAYMENT_PROVIDER is not configured")
	}
	if environmentOr("APP_ENV", "development") == "production" {
		return paymentConfig{}, errors.New("PAYMENT_PROVIDER=FAKE is not allowed in production")
	}
	webhookSecret := os.Getenv("PAYMENT_WEBHOOK_SECRET")
	if len(webhookSecret) < minimumWebhookSecretBytes {
		return paymentConfig{}, errors.New("PAYMENT_WEBHOOK_SECRET must be at least 32 bytes")
	}
	replayWindow := defaultWebhookReplayWindow
	if value := os.Getenv("PAYMENT_WEBHOOK_REPLAY_WINDOW"); value != "" {
		parsedDuration, err := time.ParseDuration(value)
		if err != nil || parsedDuration <= 0 {
			return paymentConfig{}, errors.New("PAYMENT_WEBHOOK_REPLAY_WINDOW must be a positive duration")
		}
		replayWindow = parsedDuration
	}
	return paymentConfig{provider: provider, webhookSecret: webhookSecret, replayWindow: replayWindow}, nil
}

func environmentOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
