package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"Real_TimePricingEngine/internal/models"
)

func (s *Store) CreateMerchant(
	ctx context.Context,
	merchant models.Merchant,
) (models.Merchant, error) {
	merchant.Name = strings.TrimSpace(merchant.Name)
	merchant.Email = strings.ToLower(strings.TrimSpace(merchant.Email))

	if merchant.Name == "" || merchant.Email == "" {
		return models.Merchant{}, fmt.Errorf("name and email are required")
	}
	if merchant.PasswordHash == "" {
		return models.Merchant{}, fmt.Errorf("password hash is required")
	}

	// Role, KYC status and account status are assigned by the server,
	// never accepted from public registration input.
	query := `
		INSERT INTO merchants (name, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, name, email, password_hash,
		          role, kyc_status, account_status, created_at
	`

	err := s.db.QueryRowContext(
		ctx,
		query,
		merchant.Name,
		merchant.Email,
		merchant.PasswordHash,
	).Scan(
		&merchant.ID,
		&merchant.Name,
		&merchant.Email,
		&merchant.PasswordHash,
		&merchant.Role,
		&merchant.KYCStatus,
		&merchant.AccountStatus,
		&merchant.CreatedAt,
	)
	if err != nil {
		return models.Merchant{}, fmt.Errorf("create merchant: %w", err)
	}

	return merchant, nil
}

func (s *Store) GetMerchantByEmail(
	ctx context.Context,
	email string,
) (models.Merchant, error) {
	return s.getMerchant(
		ctx,
		`WHERE email = $1`,
		strings.ToLower(strings.TrimSpace(email)),
	)
}

func (s *Store) GetMerchantByID(
	ctx context.Context,
	id int64,
) (models.Merchant, error) {
	return s.getMerchant(ctx, `WHERE id = $1`, id)
}

func (s *Store) getMerchant(
	ctx context.Context,
	condition string,
	value any,
) (models.Merchant, error) {
	query := `
		SELECT id, name, email, password_hash,
		       role, kyc_status, account_status, created_at
		FROM merchants
		` + condition

	var merchant models.Merchant

	err := s.db.QueryRowContext(ctx, query, value).Scan(
		&merchant.ID,
		&merchant.Name,
		&merchant.Email,
		&merchant.PasswordHash,
		&merchant.Role,
		&merchant.KYCStatus,
		&merchant.AccountStatus,
		&merchant.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.Merchant{}, sql.ErrNoRows
		}
		return models.Merchant{}, fmt.Errorf("get merchant: %w", err)
	}

	return merchant, nil
}

// UpdateMerchantKYC should only be called by an authenticated,
// authorized admin handler.
func (s *Store) UpdateMerchantKYC(
	ctx context.Context,
	id int64,
	status string,
) error {
	if status != "verified" && status != "rejected" {
		return fmt.Errorf("invalid KYC status")
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE merchants
		 SET kyc_status = $1, updated_at = NOW()
		 WHERE id = $2`,
		status,
		id,
	)
	if err != nil {
		return fmt.Errorf("update merchant KYC: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check KYC update: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}
