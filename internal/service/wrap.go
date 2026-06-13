package service

import (
	"errors"
	"fmt"
)

func wrapRepo(msg string, err error) error {
	if err == nil {
		return nil
	}
	var ae *AppError
	if errors.As(err, &ae) {
		return err
	}
	return fmt.Errorf("%s: %w", msg, err)
}
