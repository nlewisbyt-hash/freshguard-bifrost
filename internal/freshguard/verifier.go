package freshguard

import (
	"context"
	"errors"
)

var ErrVerificationUnavailable = errors.New("verification unavailable")

type Verifier interface {
	Verify(ctx context.Context, claims []Claim) ([]ClaimResult, error)
}
