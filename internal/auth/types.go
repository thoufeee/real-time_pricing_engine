package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Role string
type KYCStatus string
type AccountStatus string

const (
	RoleMerchant Role = "merchant"
	RoleAdmin    Role = "admin"

	KYCPending  KYCStatus = "pending"
	KYCVerified KYCStatus = "verified"
	KYCRejected KYCStatus = "rejected"

	AccountActive    AccountStatus = "active"
	AccountSuspended AccountStatus = "suspended"
)

const (
	ContextMerchantID = "merchant_id"
	ContextRole       = "role"
	ContextKYCStatus  = "kyc_status"
	ContextAccount    = "account_status"
)

// RequireAdmin allows only authenticated admins.
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		value, exists := c.Get(ContextRole)
		role, ok := value.(string)

		if !exists || !ok || role != string(RoleAdmin) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "admin access required",
			})
			return
		}

		c.Next()
	}
}

// RequireVerifiedMerchant allows only active, KYC-verified merchants.
func RequireVerifiedMerchant() gin.HandlerFunc {
	return func(c *gin.Context) {
		roleValue, roleExists := c.Get(ContextRole)
		kycValue, kycExists := c.Get(ContextKYCStatus)
		accountValue, accountExists := c.Get(ContextAccount)

		role, roleOK := roleValue.(string)
		kyc, kycOK := kycValue.(string)
		account, accountOK := accountValue.(string)

		if !roleExists || !kycExists || !accountExists ||
			!roleOK || !kycOK || !accountOK {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			return
		}

		if role != string(RoleMerchant) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "merchant access required",
			})
			return
		}

		if account != string(AccountActive) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "account is not active",
			})
			return
		}

		if kyc != string(KYCVerified) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "KYC verification required",
			})
			return
		}

		c.Next()
	}
}
