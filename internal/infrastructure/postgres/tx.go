package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// InTx executes fn inside a DB transaction.
// It commits on nil error and rolls back otherwise.
func (r *Repo) InTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if fn == nil {
		return fmt.Errorf("transaction function is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
