package service

import (
	"errors"
	"fmt"

	"github.com/fireman/fireman/internal/repository"
)

func wrapRepo(msg string, err error) error {
	if err == nil {
		return nil
	}
	var ae *AppError
	if errors.As(err, &ae) {
		return err
	}
	// A system profile identity/content conflict is a stable, client-facing error:
	// the on-disk system row no longer matches a recognized
	// published identity and must be resolved by an explicit release repair, never
	// silently overwritten.
	if errors.Is(err, repository.ErrSystemProfileIdentityConflict) {
		return newErr("system_profile_identity_conflict", err.Error(), nil)
	}
	return fmt.Errorf("%s: %w", msg, err)
}
