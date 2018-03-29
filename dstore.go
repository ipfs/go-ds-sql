package sqlds

import (
	"database/sql"

	"fmt"
	ds "gx/ipfs/QmXRKBQA4wXP7xWbFiZsR1GP4HV6wMDQ1aWFxZZ4uBcPX9/go-datastore"
	dsq "gx/ipfs/QmXRKBQA4wXP7xWbFiZsR1GP4HV6wMDQ1aWFxZZ4uBcPX9/go-datastore/query"
	"log"
)

type Queries interface {
	Delete() string
	Exists() string
	Get() string
	Put() string
	Query() string
	Prefix() string
	Limit() string
	Offset() string
}

type Datastore struct {
	db      *sql.DB
	queries Queries
}

// NewDatastore returns a new datastore
func NewDatastore(db *sql.DB, queries Queries) *Datastore {
	return &Datastore{db: db, queries: queries}
}

type batch struct {
	db      *sql.DB
	queries Queries
	txn     *sql.Tx
}

func (b batch) Put(key ds.Key, val interface{}) error {
	data, ok := val.([]byte)
	if !ok {
		return ds.ErrInvalidType
	}
	_, err := b.txn.Exec(b.queries.Put(), key.String(), data)
	if err != nil {
		b.txn.Rollback()
	}
	return err
}

func (b batch) Delete(key ds.Key) error {
	_, err := b.txn.Exec(b.queries.Delete(), key.String())
	if err != nil {
		b.txn.Rollback()
	}
	return err
}

func (b batch) Commit() error {
	return b.txn.Commit()
}

func (d *Datastore) Batch() (ds.Batch, error) {
	txn, err := d.db.Begin()
	if err != nil {
		return nil, err
	}
	return &batch{
		db:      d.db,
		queries: d.queries,
		txn:     txn,
	}, nil
}

func (d *Datastore) Close() error {
	return d.db.Close()
}

func (d *Datastore) Delete(key ds.Key) error {
	result, err := d.db.Exec(d.queries.Delete(), key.String())
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ds.ErrNotFound
	}
	return nil
}

func (d *Datastore) Get(key ds.Key) (value interface{}, err error) {
	row := d.db.QueryRow(d.queries.Get(), key.String())

	var out []byte
	switch err := row.Scan(&out); err {
	case sql.ErrNoRows:
		return nil, ds.ErrNotFound
	case nil:
		return out, nil
	default:
		return nil, err
	}
}

func (d *Datastore) Has(key ds.Key) (exists bool, err error) {
	row := d.db.QueryRow(d.queries.Exists(), key.String())

	switch err := row.Scan(&exists); err {
	case sql.ErrNoRows:
		return exists, nil
	case nil:
		return exists, nil
	default:
		return exists, err
	}
}

func (d *Datastore) Put(key ds.Key, value interface{}) error {
	data, ok := value.([]byte)
	if !ok {
		return ds.ErrInvalidType
	}

	_, err := d.db.Exec(d.queries.Put(), key.String(), data)
	if err != nil {
		return err
	}

	return nil
}

func (d *Datastore) Query(q dsq.Query) (dsq.Results, error) {
	raw, err := d.RawQuery(q)
	if err != nil {
		return nil, err
	}
	for _, f := range q.Filters {
		raw = dsq.NaiveFilter(raw, f)
	}
	for _, o := range q.Orders {
		raw = dsq.NaiveOrder(raw, o)
	}
	return raw, nil
}

func (d *Datastore) RawQuery(q dsq.Query) (dsq.Results, error) {
	var rows *sql.Rows
	var err error
	if q.Prefix != "" {
		rows, err = QueryWithParams(d, q)
	} else {
		rows, err = d.db.Query(d.queries.Query())
	}

	if err != nil {
		return nil, err
	}

	var entries []dsq.Entry
	defer rows.Close()

	for rows.Next() {
		var key string
		var out []byte
		err := rows.Scan(&key, &out)
		if err != nil {
			log.Fatal("Error reading rows from query")
		}
		entry := dsq.Entry{
			Key:   key,
			Value: out,
		}
		entries = append(entries, entry)
	}
	results := dsq.ResultsWithEntries(q, entries)
	return results, nil
}

// QueryWithParams applies prefix, limit, and offset params in pg query
func QueryWithParams(d *Datastore, q dsq.Query) (*sql.Rows, error) {
	var qNew = d.queries.Query()
	if q.Prefix != "" {
		qNew += fmt.Sprintf(d.queries.Prefix(), q.Prefix)
	}
	if q.Limit != 0 {
		qNew += fmt.Sprintf(d.queries.Limit(), q.Limit)
	}
	if q.Offset != 0 {
		qNew += fmt.Sprintf(d.queries.Offset(), q.Offset)
	}
	return d.db.Query(qNew)

}

var _ ds.Datastore = (*Datastore)(nil)
