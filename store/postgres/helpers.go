package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

type notFoundError struct{ entity string }

func (e *notFoundError) Error() string { return e.entity + " not found" }

func errNotFound(entity string) error { return &notFoundError{entity: entity} }

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
