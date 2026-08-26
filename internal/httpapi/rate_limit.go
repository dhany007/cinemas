package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	rateLimitMaxRequests = 10
	rateLimitWindow      = time.Minute
)

type clientRateLimiter struct {
	mu      sync.Mutex
	clients map[string]rateLimitBucket
	now     func() time.Time
}

type rateLimitBucket struct {
	windowStartedAt time.Time
	requests        int
}

func newClientRateLimiter(now func() time.Time) *clientRateLimiter {
	return &clientRateLimiter{clients: make(map[string]rateLimitBucket), now: now}
}

func (l *clientRateLimiter) allow(clientID, route string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := clientID + "\x00" + route
	now := l.now()
	bucket := l.clients[key]
	if bucket.windowStartedAt.IsZero() || !now.Before(bucket.windowStartedAt.Add(rateLimitWindow)) {
		l.clients[key] = rateLimitBucket{windowStartedAt: now, requests: 1}
		return true
	}
	if bucket.requests >= rateLimitMaxRequests {
		return false
	}
	bucket.requests++
	l.clients[key] = bucket
	return true
}

func (s *Server) rateLimit(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if !s.rateLimiter.allow(clientAddress(c.Request()), c.Path()) {
			return writeError(c, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests")
		}
		return next(c)
	}
}

func clientAddress(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}
