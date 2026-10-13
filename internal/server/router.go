package server

import (
	"net/http"

	"Real_TimePricingEngine/internal/auth"
	"Real_TimePricingEngine/internal/middleware"
	pricingws "Real_TimePricingEngine/internal/websocket"

	"github.com/gin-gonic/gin"
)

func NewRouter(
	hub *pricingws.Hub,
	authService *auth.Service,
) *gin.Engine {
	router := gin.New()

	// Global middleware
	rateLimiter := middleware.NewIPRateLimiter(20, 40)
	router.Use(rateLimiter.Middleware())

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	// WebSocket endpoint
	router.GET("/ws", func(c *gin.Context) {
		if hub == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "websocket service unavailable",
			})
			return
		}

		hub.ServeHTTP(c.Writer, c.Request)
	})

	// Public authentication routes
	authRoutes := router.Group("/api/v1/auth")
	{
		authRoutes.POST("/register", registerHandler(authService))
		authRoutes.POST("/login", loginHandler(authService))
	}

	// Admin-only routes
	if authService != nil {
		adminRoutes := router.Group("/api/v1/admin")
		adminRoutes.Use(
			authService.Authenticate(),
			auth.RequireAdmin(),
		)
		{
			adminRoutes.POST(
				"/merchants/:id/kyc",
				updateMerchantKYCHandler(authService),
			)
		}

		// Active merchants with verified KYC only
		tradingRoutes := router.Group("/api/v1/trading")
		tradingRoutes.Use(
			authService.Authenticate(),
			auth.RequireVerifiedMerchant(),
		)
		{
			tradingRoutes.GET("/access-check", func(c *gin.Context) {
				merchantID, ok := auth.MerchantIDFromContext(c)
				if !ok {
					c.JSON(http.StatusUnauthorized, gin.H{
						"error": "merchant identity missing",
					})
					return
				}

				c.JSON(http.StatusOK, gin.H{
					"message":     "trading access granted",
					"merchant_id": merchantID,
				})
			})
		}
	}

	return router
}
