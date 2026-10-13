package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Authenticate validates the JWT and loads the merchant from storage.
func (s *Service) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s == nil || s.store == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "authentication service is unavailable",
			})
			return
		}

		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 ||
			!strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing or invalid authorization header",
			})
			return
		}

		claims, err := s.ParseToken(parts[1])
		if err != nil || claims == nil || claims.MerchantID <= 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
			})
			return
		}

		merchant, err := s.store.GetMerchantByID(
			c.Request.Context(),
			claims.MerchantID,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "merchant not found",
				})
				return
			}

			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "failed to authenticate merchant",
			})
			return
		}

		if merchant.AccountStatus != "active" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "account is not active",
			})
			return
		}

		c.Set(ContextMerchantID, merchant.ID)
		c.Set(ContextRole, string(merchant.Role))
		c.Set(ContextKYCStatus, string(merchant.KYCStatus))
		c.Set(ContextAccount, string(merchant.AccountStatus))

		c.Next()
	}
}

// MerchantIDFromContext retrieves the authenticated merchant ID.
func MerchantIDFromContext(c *gin.Context) (int64, bool) {
	value, exists := c.Get(ContextMerchantID)
	if !exists {
		return 0, false
	}

	id, ok := value.(int64)
	if !ok || id <= 0 {
		return 0, false
	}

	return id, true
}

// ParseMerchantID validates a merchant ID from a URL parameter.
func ParseMerchantID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid merchant ID")
	}

	return id, nil
}
