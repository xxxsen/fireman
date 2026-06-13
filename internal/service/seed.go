package service

import (
	"errors"
	"fmt"
	"strconv"
)

const maxSeedInt64 = int64(9223372036854775807)

var (
	errSeedNotProvided      = errors.New("seed not provided")
	errInvalidSeed          = errors.New("seed must be a non-negative decimal integer")
	errSeedNegative         = errors.New("seed must be non-negative")
	errSeedExceedsMax       = errors.New("seed exceeds int64 maximum")
	errWithdrawalTaxInvalid = errors.New("withdrawal tax parameters invalid: denominator must be > 0")
)

// ParseSeedString validates a decimal seed string and returns int64.
func ParseSeedString(raw *string) (*int64, error) {
	if raw == nil || *raw == "" {
		return nil, errSeedNotProvided
	}
	v, err := ValidateSeedInput(*raw)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ValidateSeedInput parses seed in [0, 9223372036854775807].
func ValidateSeedInput(raw string) (int64, error) {
	if raw == "" {
		return 0, errInvalidSeed
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, errInvalidSeed
	}
	if v < 0 {
		return 0, errSeedNegative
	}
	if float64(v) > float64(maxSeedInt64) {
		return 0, errSeedExceedsMax
	}
	if strconv.FormatInt(v, 10) != raw {
		return 0, errInvalidSeed
	}
	return v, nil
}

// FormatSeedString formats int64 seed for API JSON.
func FormatSeedString(v *int64) *string {
	if v == nil {
		return nil
	}
	s := strconv.FormatInt(*v, 10)
	return &s
}

// FormatSeedInt64 formats a seed value for API JSON.
func FormatSeedInt64(v int64) string {
	if v < 0 {
		panic(fmt.Sprintf("internal error: negative path seed %d", v))
	}
	if v > maxSeedInt64 {
		panic(fmt.Sprintf("internal error: path seed %d exceeds int64 maximum", v))
	}
	return strconv.FormatInt(v, 10)
}
