package server

import (
	"database/sql"
	"errors"
	"net/http"

	"Real_TimePricingEngine/internal/auth"

	"github.com/gin-gonic/gin"
)

func registerHandler(service *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input auth.RegisterInput

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid request body",
			})
			return
		}

		merchant, err := service.Register(
			c.Request.Context(),
			input,
		)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidInput) {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": err.Error(),
				})
				return
			}

			c.JSON(http.StatusConflict, gin.H{
				"error": "could not register merchant",
			})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"id":             merchant.ID,
			"name":           merchant.Name,
			"email":          merchant.Email,
			"role":           merchant.Role,
			"kyc_status":     merchant.KYCStatus,
			"account_status": merchant.AccountStatus,
			"created_at":     merchant.CreatedAt,
		})
	}
}

func loginHandler(service *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input auth.LoginInput

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid request body",
			})
			return
		}

		token, err := service.Login(
			c.Request.Context(),
			input,
		)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid credentials or inactive account",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"access_token": token,
			"token_type":   "Bearer",
		})
	}
}

func updateMerchantKYCHandler(
	service *auth.Service,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "authentication service is unavailable",
			})
			return
		}

		merchantID, err := auth.ParseMerchantID(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid merchant ID",
			})
			return
		}

		var input struct {
			Status string `json:"status" binding:"required"`
		}

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "status is required",
			})
			return
		}

		if input.Status != string(auth.KYCVerified) &&
			input.Status != string(auth.KYCRejected) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "status must be verified or rejected",
			})
			return
		}

		err = service.UpdateMerchantKYC(
			c.Request.Context(),
			merchantID,
			input.Status,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				c.JSON(http.StatusNotFound, gin.H{
					"error": "merchant not found",
				})
				return
			}

			if err.Error() == "invalid merchant ID" ||
				err.Error() == "invalid KYC status" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": err.Error(),
				})
				return
			}

			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to update merchant KYC",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":     "merchant KYC updated successfully",
			"merchant_id": merchantID,
			"kyc_status":  input.Status,
		})
	}
}
