package models

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/nyaruka/gocommon/dbutil"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Queryer lets us pass anything that supports QueryContext to a function (sql.DB, sql.Tx, sqlx.DB, sqlx.Tx)
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// DBorTxx contains functionality common to sqlx.Tx and sqlx.DB so we can write code that works with either
type DBorTxx interface {
	Queryer
	dbutil.BulkQueryer

	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	NamedExecContext(ctx context.Context, query string, arg any) (sql.Result, error)
	SelectContext(ctx context.Context, dest any, query string, args ...any) error
	GetContext(ctx context.Context, value any, query string, args ...any) error
}

// QueryerWithTx adds support for beginning transactions
type QueryerWithTx interface {
	DBorTxx

	BeginTxx(ctx context.Context, opts *sql.TxOptions) (*sqlx.Tx, error)
}

// BulkQuery runs the given query as a bulk operation
func BulkQuery[T any](ctx context.Context, label string, tx DBorTxx, sql string, structs []T) error {
	// no values, nothing to do
	if len(structs) == 0 {
		return nil
	}

	start := time.Now()

	err := dbutil.BulkQuery(ctx, tx, sql, structs)
	if err != nil {
		return errors.Wrap(err, "error making bulk query")
	}

	logrus.WithField("elapsed", time.Since(start)).WithField("rows", len(structs)).Infof("%s bulk sql complete", label)

	return nil
}

// BulkQueryBatches runs the given query as a bulk operation, in batches of the given size
func BulkQueryBatches(ctx context.Context, label string, tx DBorTxx, sql string, batchSize int, structs []interface{}) error {
	start := time.Now()

	batches := ChunkSlice(structs, batchSize)
	for i, batch := range batches {
		err := dbutil.BulkQuery(ctx, tx, sql, batch)
		if err != nil {
			return errors.Wrap(err, "error making bulk batch query")
		}

		logrus.WithField("elapsed", time.Since(start)).WithField("rows", len(batch)).WithField("batch", i+1).Infof("%s bulk sql batch complete", label)
	}

	return nil
}

func ChunkSlice[T any](slice []T, size int) [][]T {
	chunks := make([][]T, 0, len(slice)/size+1)

	for i := 0; i < len(slice); i += size {
		end := i + size
		if end > len(slice) {
			end = len(slice)
		}
		chunks = append(chunks, slice[i:end])
	}
	return chunks
}

// Map is a generic map which is written to the database as JSON. For nullable fields use null.Map.
type JSONMap map[string]any

// Scan implements the Scanner interface
func (m *JSONMap) Scan(value any) error {
	var raw []byte
	switch typed := value.(type) {
	case string:
		raw = []byte(typed)
	case []byte:
		raw = typed
	default:
		return fmt.Errorf("unable to scan %T as map", value)
	}

	if err := json.Unmarshal(raw, m); err != nil {
		return err
	}
	return nil
}

// Value implements the Valuer interface
func (m JSONMap) Value() (driver.Value, error) { return json.Marshal(m) }
