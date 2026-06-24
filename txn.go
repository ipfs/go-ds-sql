package sqlds

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	datastore "github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"
)

// ErrNotImplemented is returned when the SQL datastore does not yet implement the function call.
var ErrNotImplemented = fmt.Errorf("not implemented")

type txn struct {
	db      *sql.DB
	queries Queries
	txn     *sql.Tx
}

// NewTransaction creates a new database transaction, note the readOnly parameter is ignored by this implementation.
func (ds *Datastore) NewTransaction(ctx context.Context, _ bool) (datastore.Txn, error) {
	sqlTxn, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		if sqlTxn != nil {
			// nothing we can do about this error.
			_ = sqlTxn.Rollback()
		}

		return nil, err
	}

	return &txn{
		db:      ds.db,
		queries: ds.queries,
		txn:     sqlTxn,
	}, nil
}

func (t *txn) Get(ctx context.Context, key datastore.Key) ([]byte, error) {
	row := t.txn.QueryRowContext(ctx, t.queries.Get(), key.String())
	var out []byte

	err := row.Scan(&out)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, datastore.ErrNotFound
		}
		return nil, err
	}
	return out, nil
}

func (t *txn) Has(ctx context.Context, key datastore.Key) (bool, error) {
	row := t.txn.QueryRowContext(ctx, t.queries.Exists(), key.String())
	var exists bool

	err := row.Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return exists, nil
		}
		return exists, err
	}
	return exists, nil
}

func (t *txn) GetSize(ctx context.Context, key datastore.Key) (int, error) {
	row := t.txn.QueryRowContext(ctx, t.queries.GetSize(), key.String())
	var size int

	err := row.Scan(&size)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, datastore.ErrNotFound
		}
		return 0, err
	}
	return size, nil
}

func (t *txn) Query(ctx context.Context, q dsq.Query) (dsq.Results, error) {
	return nil, ErrNotImplemented
}

// Put adds a value to the datastore identified by the given key.
func (t *txn) Put(ctx context.Context, key datastore.Key, val []byte) error {
	_, err := t.txn.ExecContext(ctx, t.queries.Put(), key.String(), val)
	if err != nil {
		_ = t.txn.Rollback()
		return err
	}
	return nil
}

// Delete removes a value from the datastore that matches the given key.
func (t *txn) Delete(ctx context.Context, key datastore.Key) error {
	_, err := t.txn.ExecContext(ctx, t.queries.Delete(), key.String())
	if err != nil {
		_ = t.txn.Rollback()
		return err
	}
	return nil
}

// Commit finalizes a transaction.
func (t *txn) Commit(ctx context.Context) error {
	err := t.txn.Commit()
	if err != nil {
		_ = t.txn.Rollback()
		return err
	}
	return nil
}

// Discard throws away changes recorded in a transaction without committing
// them to the underlying Datastore.
func (t *txn) Discard(ctx context.Context) {
	_ = t.txn.Rollback()
}

var _ datastore.TxnDatastore = (*Datastore)(nil)
