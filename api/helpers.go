package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/xraph/forge"

	"github.com/xraph/keysmith"
)

// mapStoreError converts keysmith sentinel errors to forge HTTP errors.
func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, keysmith.ErrKeyNotFound),
		errors.Is(err, keysmith.ErrPolicyNotFound),
		errors.Is(err, keysmith.ErrScopeNotFound),
		errors.Is(err, keysmith.ErrRotationNotFound):
		return forge.NotFound(err.Error())
	case errors.Is(err, keysmith.ErrInvalidKey):
		return forge.Unauthorized(err.Error())
	case errors.Is(err, keysmith.ErrKeyExpired),
		errors.Is(err, keysmith.ErrKeyRevoked),
		errors.Is(err, keysmith.ErrKeySuspended),
		errors.Is(err, keysmith.ErrKeyInactive):
		return forge.Forbidden(err.Error())
	case errors.Is(err, keysmith.ErrRateLimited),
		errors.Is(err, keysmith.ErrQuotaExceeded):
		return forge.NewHTTPError(http.StatusTooManyRequests, err.Error())
	case errors.Is(err, keysmith.ErrPolicyInUse),
		errors.Is(err, keysmith.ErrInvalidStateTransition):
		return forge.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, keysmith.ErrIPNotAllowed),
		errors.Is(err, keysmith.ErrOriginNotAllowed),
		errors.Is(err, keysmith.ErrScopeNotAllowed):
		return forge.Forbidden(err.Error())
	default:
		return err
	}
}

func defaultLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func parseDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err == nil {
		return d
	}
	// Handle "30d", "90d", "2w" style durations.
	if len(s) > 1 {
		val := s[:len(s)-1]
		switch s[len(s)-1] {
		case 'd':
			var days int
			if err := parseIntFromString(val, &days); err == nil {
				return time.Duration(days) * 24 * time.Hour
			}
		case 'w':
			var weeks int
			if err := parseIntFromString(val, &weeks); err == nil {
				return time.Duration(weeks) * 7 * 24 * time.Hour
			}
		}
	}
	return 0
}

func parseIntFromString(s string, out *int) error {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return errors.New("not a number")
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return nil
}
