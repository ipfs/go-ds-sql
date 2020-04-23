package postgres

import (
	"database/sql"
	"fmt"

	sqlds "github.com/ipfs/go-ds-sql"

	_ "github.com/lib/pq" //postgres driver
)

// Options are the postgres datastore options, reexported here for convenience.
type Options struct {
	Host      string
	Port      string
	User      string
	Password  string
	Database  string
	TableName string
}

// Queries are the postgres queries for a given table.
type Queries struct {
	TableName string
}

// Delete returns the postgres query for deleting a row.
func (q Queries) Delete() string {
	return fmt.Sprintf("DELETE FROM %s WHERE key = $1", q.TableName)
}

// Exists returns the postgres query for determining if a row exists.
func (q Queries) Exists() string {
	return fmt.Sprintf("SELECT exists(SELECT 1 FROM %s WHERE key=$1)", q.TableName)
}

// Get returns the postgres query for getting a row.
func (q Queries) Get() string {
	return fmt.Sprintf("SELECT data FROM %s WHERE key = $1", q.TableName)
}

// Put returns the postgres query for putting a row.
func (q Queries) Put() string {
	return fmt.Sprintf("INSERT INTO %s (key, data) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET data = $2", q.TableName)
}

// Query returns the postgres query for getting multiple rows.
func (q Queries) Query() string {
	return fmt.Sprintf("SELECT key, data FROM %s", q.TableName)
}

// Prefix returns the postgres query fragment for getting a rows with a key prefix.
func (Queries) Prefix() string {
	return ` WHERE key LIKE '%s%%' ORDER BY key`
}

// Limit returns the postgres query fragment for limiting results.
func (Queries) Limit() string {
	return ` LIMIT %d`
}

// Offset returns the postgres query fragment for returning rows from a given offset.
func (Queries) Offset() string {
	return ` OFFSET %d`
}

// GetSize returns the postgres query for determining the size of a value.
func (q Queries) GetSize() string {
	return fmt.Sprintf("SELECT octet_length(data) FROM %s WHERE key = $1", q.TableName)
}

// Create returns a datastore connected to postgres
func (opts *Options) Create() (*sqlds.Datastore, error) {
	opts.setDefaults()
	fmtstr := "postgresql:///%s?host=%s&port=%s&user=%s&password=%s&sslmode=disable"
	constr := fmt.Sprintf(fmtstr, opts.Database, opts.Host, opts.Port, opts.User, opts.Password)
	db, err := sql.Open("postgres", constr)
	if err != nil {
		return nil, err
	}

	return sqlds.NewDatastore(db, Queries{TableName: opts.TableName}), nil
}

func (opts *Options) setDefaults() {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}

	if opts.Port == "" {
		opts.Port = "5432"
	}

	if opts.User == "" {
		opts.User = "postgres"
	}

	if opts.Database == "" {
		opts.Database = "datastore"
	}

	if opts.TableName == "" {
		opts.TableName = "blocks"
	}
}
