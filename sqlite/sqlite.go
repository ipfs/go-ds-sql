package sqlite

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	sqlds "github.com/ipfs/go-ds-sql"
	// we don't import a specific driver to let the user choose
)

// Options are the sqlite datastore options, reexported here for convenience.
type Options struct {
	Driver string
	DSN    string
	Table  string
	// Don't try to create table
	NoCreate bool

	// sqlcipher extension specific
	Key            []byte
	CipherPageSize uint
}

// Queries are the sqlite queries for a given table.
type Queries struct {
	deleteQuery  string
	existsQuery  string
	getQuery     string
	putQuery     string
	queryQuery   string
	prefixQuery  string
	limitQuery   string
	offsetQuery  string
	getSizeQuery string
}

// NewQueries creates a new sqlite set of queries for the passed table
func NewQueries(tbl string) Queries {
	return Queries{
		deleteQuery:  fmt.Sprintf("DELETE FROM %s WHERE key = $1", tbl),
		existsQuery:  fmt.Sprintf("SELECT exists(SELECT 1 FROM %s WHERE key=$1)", tbl),
		getQuery:     fmt.Sprintf("SELECT data FROM %s WHERE key = $1", tbl),
		putQuery:     fmt.Sprintf("INSERT OR REPLACE INTO %s(key, data) VALUES($1, $2)", tbl),
		queryQuery:   fmt.Sprintf("SELECT key, data FROM %s", tbl),
		prefixQuery:  ` WHERE key GLOB '%s*' ORDER BY key`,
		limitQuery:   ` LIMIT %d`,
		offsetQuery:  ` OFFSET %d`,
		getSizeQuery: fmt.Sprintf("SELECT length(data) FROM %s WHERE key = $1", tbl),
	}
}

// Delete returns the sqlite query for deleting a row.
func (q Queries) Delete() string {
	return q.deleteQuery
}

// Exists returns the sqlite query for determining if a row exists.
func (q Queries) Exists() string {
	return q.existsQuery
}

// Get returns the sqlite query for getting a row.
func (q Queries) Get() string {
	return q.getQuery
}

// Put returns the sqlite query for putting a row.
func (q Queries) Put() string {
	return q.putQuery
}

// Query returns the sqlite query for getting multiple rows.
func (q Queries) Query() string {
	return q.queryQuery
}

// Prefix returns the sqlite query fragment for getting a rows with a key prefix.
func (q Queries) Prefix() string {
	return q.prefixQuery
}

// Limit returns the sqlite query fragment for limiting results.
func (q Queries) Limit() string {
	return q.limitQuery
}

// Offset returns the sqlite query fragment for returning rows from a given offset.
func (q Queries) Offset() string {
	return q.offsetQuery
}

// GetSize returns the sqlite query for determining the size of a value.
func (q Queries) GetSize() string {
	return q.getSizeQuery
}

// Create returns a datastore connected to sqlite
func (opts *Options) Create() (*sqlds.Datastore, error) {
	opts.setDefaults()

	args := []string{}
	if len(opts.Key) != 0 {
		// sqlcipher expects a 32 bytes key
		if len(opts.Key) != 32 {
			return nil, fmt.Errorf("bad key length, expected 32 bytes, got %d", len(opts.Key))
		}
		args = append(args, fmt.Sprintf("_pragma_key=x'%s'", hex.EncodeToString(opts.Key)))
		args = append(args, fmt.Sprintf("_pragma_cipher_page_size=%d", opts.CipherPageSize))
	}
	dsn := opts.DSN
	if len(args) != 0 {
		if strings.ContainsRune(dsn, '?') {
			dsn += "&"
		} else {
			dsn += "?"
		}
		dsn += strings.Join(args, "&")
	}

	db, err := sql.Open(opts.Driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if !opts.NoCreate {
		if _, err := db.Exec(fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				key TEXT PRIMARY KEY,
				data BLOB
			) WITHOUT ROWID;
		`, opts.Table)); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to ensure table exists: %w", err)
		}
	}

	return sqlds.NewDatastore(db, NewQueries(opts.Table)), nil
}

func (opts *Options) setDefaults() {
	if opts.Driver == "" {
		opts.Driver = "sqlite3"
	}

	if opts.DSN == "" {
		opts.DSN = ":memory:"
	}

	if len(opts.Key) != 0 && opts.CipherPageSize == 0 {
		opts.CipherPageSize = 4096
	}

	if opts.Table == "" {
		opts.Table = "blocks"
	}
}
