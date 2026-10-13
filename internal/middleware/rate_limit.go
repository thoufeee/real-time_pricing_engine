package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type visitor struct {
	requests  int
	resetTime time.Time
}

type IPRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    int
	window   time.Duration
}

func NewIPRateLimiter(limit int, burst int) *IPRateLimiter {
	if limit <= 0 {
		limit = 20
	}

	if burst <= 0 {
		burst = 40
	}

	return &IPRateLimiter{
		visitors: make(map[string]*visitor),
		limit:    burst,
		window:   time.Minute,
	}
}

// Middleware returns a Gin-compatible rate-limiting handler.
func (rl *IPRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		rl.mu.Lock()

		v, exists := rl.visitors[ip]
		if !exists || !now.Before(v.resetTime) {
			v = &visitor{
				requests:  0,
				resetTime: now.Add(rl.window),
			}
			rl.visitors[ip] = v
		}

		if v.requests >= rl.limit {
			remaining := time.Until(v.resetTime)
			retryAfter := int(remaining.Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}

			rl.mu.Unlock()

			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}

		v.requests++
		rl.mu.Unlock()

		c.Next()
	}
}
