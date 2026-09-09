package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation reports whether err is PostgreSQL's 23505.
//
// It lives here because more than one package needs it and each had grown its own copy
// (internal/channels/telegram, internal/cron). A caller distinguishing "already there"
// from "failed" is asking a question about the database, not about its own domain.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
