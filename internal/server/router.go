package server

import (
	"Real_TimePricingEngine/internal/middleware"
	pricingws "Real_TimePricingEngine/internal/websocket"
	"net/http"

	"github.com/gin-gonic/gin"
)

func NewRouter(hub *pricingws.Hub) *gin.Engine {
	router := gin.New()

	router.Use(gin.Logger(), gin.Recovery())

	limiter := middleware.NewIPRateLimiter(20, 40)
	router.Use(limiter.Middleware())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "realtime-pricing-engine",
		})
	})

	router.GET("/ws", func(c *gin.Context) {
		hub.ServeHTTP(c.Writer, c.Request)
	})

	return router
}
