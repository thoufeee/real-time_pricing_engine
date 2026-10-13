package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"Real_TimePricingEngine/internal/models"
	"Real_TimePricingEngine/internal/storage"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidInput       = errors.New("invalid registration input")
	ErrInactiveAccount    = errors.New("account is not active")
)

type Service struct {
	store     *storage.Store
	jwtSecret []byte
}

type RegisterInput struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginInput struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type Claims struct {
	MerchantID int64 `json:"merchant_id"`
	jwt.RegisteredClaims
}

func NewService(store *storage.Store) (*Service, error) {
	if store == nil {
		return nil, errors.New("storage store is required")
	}

	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return nil, errors.New(
			"JWT_SECRET must contain at least 32 characters",
		)
	}

	return &Service{
		store:     store,
		jwtSecret: []byte(secret),
	}, nil
}

func (s *Service) Register(
	ctx context.Context,
	input RegisterInput,
) (models.Merchant, error) {
	if s == nil || s.store == nil {
		return models.Merchant{}, errors.New(
			"auth service is not initialized",
		)
	}

	name := strings.TrimSpace(input.Name)
	email := strings.ToLower(strings.TrimSpace(input.Email))
	password := input.Password

	if name == "" ||
		len(name) > 150 ||
		email == "" ||
		len(email) > 255 ||
		!strings.Contains(email, "@") ||
		len(password) < 8 ||
		len(password) > 72 {
		return models.Merchant{}, ErrInvalidInput
	}

	hash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return models.Merchant{}, fmt.Errorf(
			"hash password: %w",
			err,
		)
	}

	return s.store.CreateMerchant(ctx, models.Merchant{
		Name:         name,
		Email:        email,
		PasswordHash: string(hash),
	})
}

func (s *Service) Login(
	ctx context.Context,
	input LoginInput,
) (string, error) {
	if s == nil || s.store == nil {
		return "", errors.New("auth service is not initialized")
	}

	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" || input.Password == "" {
		return "", ErrInvalidCredentials
	}

	merchant, err := s.store.GetMerchantByEmail(ctx, email)
	if err != nil {
		return "", ErrInvalidCredentials
	}

	if bcrypt.CompareHashAndPassword(
		[]byte(merchant.PasswordHash),
		[]byte(input.Password),
	) != nil {
		return "", ErrInvalidCredentials
	}

	if merchant.AccountStatus != "active" {
		return "", ErrInactiveAccount
	}

	now := time.Now()

	claims := Claims{
		MerchantID: merchant.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", merchant.ID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(30 * time.Minute)),
		},
	}

	token := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		claims,
	)

	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return signed, nil
}

func (s *Service) ParseToken(
	tokenString string,
) (*Claims, error) {
	if s == nil || len(s.jwtSecret) < 32 {
		return nil, errors.New("auth service is not initialized")
	}

	claims := &Claims{}

	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("unexpected signing method")
			}

			return s.jwtSecret, nil
		},
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err != nil || token == nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	if claims.MerchantID <= 0 ||
		claims.Subject != fmt.Sprintf("%d", claims.MerchantID) {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}
