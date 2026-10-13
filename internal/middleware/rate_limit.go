package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type clientLimiter struct {
	limiter  *rate.Limiter
	lastseen time.Time
}

type IPRateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientLimiter

	rate  rate.Limit
	burst int
}

func NewIPRateLimiter(requestPerSecond float64, burst int) *IPRateLimiter {
	return &IPRateLimiter{
		clients: make(map[string]*clientLimiter),
		rate:    rate.Limit(requestPerSecond),
		burst:   burst,
	}
}

func (l *IPRateLimiter) getlimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	client, exists := l.clients[ip]

	if !exists {
		client = &clientLimiter{
			limiter: rate.NewLimiter(l.rate, l.burst),
		}

		l.clients[ip] = client
	}

	client.lastseen = now

	//   removing inactive entries
	for key, entry := range l.clients {
		if now.Sub(entry.lastseen) > 10*time.Minute {
			delete(l.clients, key)
		}
	}

	return client.limiter
}

func (l *IPRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := l.getlimiter(ip)

		if !limiter.Allow() {
			c.Header("Retry-After", "1")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
