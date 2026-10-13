package auth

import (
	"context"
	"errors"
)

func (s *Service) UpdateMerchantKYC(
	ctx context.Context,
	merchantID int64,
	status string,
) error {
	if s == nil || s.store == nil {
		return errors.New("auth service is not initialized")
	}

	if merchantID <= 0 {
		return errors.New("invalid merchant ID")
	}

	if status != string(KYCVerified) &&
		status != string(KYCRejected) {
		return errors.New("invalid KYC status")
	}

	return s.store.UpdateMerchantKYC(ctx, merchantID, status)
}
