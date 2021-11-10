package sqlds

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	ds "github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"
	dstest "github.com/ipfs/go-datastore/test"
	_ "github.com/lib/pq"
)

var initOnce sync.Once

// Automatically re-create the test datastore.
func initPG() {
	initOnce.Do(func() {
		fmtstr := "postgres://%s:%s@%s/?sslmode=disable"
		constr := fmt.Sprintf(fmtstr, "postgres", "", "127.0.0.1")
		db, err := sql.Open("postgres", constr)
		if err != nil {
			panic(err)
		}

		// drop/create the database.
		_, err = db.Exec("DROP DATABASE IF EXISTS test_datastore")
		if err != nil {
			panic(err)
		}
		_, err = db.Exec("CREATE DATABASE test_datastore")
		if err != nil {
			panic(err)
		}
		err = db.Close()
		if err != nil {
			panic(err)
		}
	})
}

var testcases = map[string]string{
	"/a":     "a",
	"/a/b":   "ab",
	"/a/b/c": "abc",
	"/a/b/d": "a/b/d",
	"/a/c":   "ac",
	"/a/d":   "ad",
	"/e":     "e",
	"/f":     "f",
	"/g":     "",
}

type fakeQueries struct{}

func (fakeQueries) Delete() string {
	return `DELETE FROM blocks WHERE key = $1`
}

func (fakeQueries) Exists() string {
	return `SELECT exists(SELECT 1 FROM blocks WHERE key=$1)`
}

func (fakeQueries) Get() string {
	return `SELECT data FROM blocks WHERE key = $1`
}

func (fakeQueries) Put() string {
	return `INSERT INTO blocks (key, data) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET data = $2`
}

func (fakeQueries) Query() string {
	return `SELECT key, data FROM blocks`
}

func (fakeQueries) Prefix() string {
	return ` WHERE key LIKE '%s%%' ORDER BY key`
}

func (fakeQueries) Limit() string {
	return ` LIMIT %d`
}

func (fakeQueries) Offset() string {
	return ` OFFSET %d`
}

func (fakeQueries) GetSize() string {
	return `SELECT octet_length(data) FROM blocks WHERE key = $1`
}

// returns datastore, and a function to call on exit.
//
//  d, close := newDS(t)
//  defer close()
func newDS(t *testing.T) (*Datastore, func()) {
	initPG()
	// connect to that database.
	fmtstr := "postgres://%s:%s@%s/%s?sslmode=disable"
	constr := fmt.Sprintf(fmtstr, "postgres", "", "127.0.0.1", "test_datastore")
	db, err := sql.Open("postgres", constr)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS blocks (key TEXT NOT NULL UNIQUE, data BYTEA NOT NULL)")
	if err != nil {
		t.Fatal(err)
	}
	d := NewDatastore(db, fakeQueries{})
	return d, func() {
		_, _ = d.db.Exec("DROP TABLE IF EXISTS blocks")
		d.Close()
	}
}

func addTestCases(t *testing.T, ctx context.Context, d *Datastore, testcases map[string]string) {

	for k, v := range testcases {
		dsk := ds.NewKey(k)
		if err := d.Put(ctx, dsk, []byte(v)); err != nil {
			t.Fatal(err)
		}
	}

	for k, v := range testcases {
		dsk := ds.NewKey(k)
		v2, err := d.Get(ctx, dsk)
		if err != nil {
			t.Fatal(err)
		}
		v2b := v2
		if string(v2b) != v {
			t.Errorf("%s values differ: %s != %s", k, v, v2)
		}
	}
}

func TestQuery(t *testing.T) {
	d, done := newDS(t)
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addTestCases(t, ctx, d, testcases)

	// test prefix
	rs, err := d.Query(ctx, dsq.Query{Prefix: "/a/"})
	if err != nil {
		t.Fatal(err)
	}
	expectMatches(t, []string{
		"/a/b",
		"/a/b/c",
		"/a/b/d",
		"/a/c",
		"/a/d",
	}, rs)

	// test offset and limit
	rs, err = d.Query(ctx, dsq.Query{Prefix: "/a/", Offset: 2, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	expectMatches(t, []string{
		"/a/b/d",
		"/a/c",
	}, rs)

	// test orders
	orbk := dsq.OrderByKey{}
	orderByKey := []dsq.Order{orbk}
	rs, err = d.Query(ctx, dsq.Query{Prefix: "/a/", Orders: orderByKey})
	if err != nil {
		t.Fatal(err)
	}
	expectKeyOrderMatches(t, rs, []string{
		"/a/b",
		"/a/b/c",
		"/a/b/d",
		"/a/c",
		"/a/d",
	})

	orbkd := dsq.OrderByKeyDescending{}
	orderByDesc := []dsq.Order{orbkd}
	rs, err = d.Query(ctx, dsq.Query{Prefix: "/a/", Orders: orderByDesc})
	if err != nil {
		t.Fatal(err)
	}
	expectKeyOrderMatches(t, rs, []string{
		"/a/d",
		"/a/c",
		"/a/b/d",
		"/a/b/c",
		"/a/b",
	})

	// test filters
	equalFilter := dsq.FilterKeyCompare{Op: dsq.Equal, Key: "/a/b"}
	equalFilters := []dsq.Filter{equalFilter}
	rs, err = d.Query(ctx, dsq.Query{Prefix: "/a/", Filters: equalFilters})
	if err != nil {
		t.Fatal(err)
	}
	expectKeyFilterMatches(t, rs, []string{"/a/b"})

	greaterThanFilter := dsq.FilterKeyCompare{Op: dsq.GreaterThan, Key: "/a/b"}
	greaterThanFilters := []dsq.Filter{greaterThanFilter}
	rs, err = d.Query(ctx, dsq.Query{Prefix: "/a/", Filters: greaterThanFilters})
	if err != nil {
		t.Fatal(err)
	}
	expectKeyFilterMatches(t, rs, []string{
		"/a/b/c",
		"/a/b/d",
		"/a/c",
		"/a/d",
	})

	lessThanFilter := dsq.FilterKeyCompare{Op: dsq.LessThanOrEqual, Key: "/a/b/c"}
	lessThanFilters := []dsq.Filter{lessThanFilter}
	rs, err = d.Query(ctx, dsq.Query{Prefix: "/a/", Filters: lessThanFilters})
	if err != nil {
		t.Fatal(err)
	}
	expectKeyFilterMatches(t, rs, []string{
		"/a/b",
		"/a/b/c",
	})
}

func TestHas(t *testing.T) {
	d, done := newDS(t)
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addTestCases(t, ctx, d, testcases)

	has, err := d.Has(ctx, ds.NewKey("/a/b/c"))
	if err != nil {
		t.Error(err)
	}

	if !has {
		t.Error("Key should be found")
	}

	has, err = d.Has(ctx, ds.NewKey("/a/b/c/d"))
	if err != nil {
		t.Error(err)
	}

	if has {
		t.Error("Key should not be found")
	}
}

func TestNotExistGet(t *testing.T) {
	d, done := newDS(t)
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addTestCases(t, ctx, d, testcases)

	has, err := d.Has(ctx, ds.NewKey("/a/b/c/d"))
	if err != nil {
		t.Error(err)
	}

	if has {
		t.Error("Key should not be found")
	}

	val, err := d.Get(ctx, ds.NewKey("/a/b/c/d"))
	if val != nil {
		t.Error("Key should not be found")
	}

	if err != ds.ErrNotFound {
		t.Error("Error was not set to ds.ErrNotFound")
		if err != nil {
			t.Error(err)
		}
	}
}

func TestDelete(t *testing.T) {
	d, done := newDS(t)
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addTestCases(t, ctx, d, testcases)

	has, err := d.Has(ctx, ds.NewKey("/a/b/c"))
	if err != nil {
		t.Error(err)
	}
	if !has {
		t.Error("Key should be found")
	}

	err = d.Delete(ctx, ds.NewKey("/a/b/c"))
	if err != nil {
		t.Error(err)
	}

	has, err = d.Has(ctx, ds.NewKey("/a/b/c"))
	if err != nil {
		t.Error(err)
	}
	if has {
		t.Error("Key should not be found")
	}
}

func TestGetEmpty(t *testing.T) {
	d, done := newDS(t)
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := d.Put(ctx, ds.NewKey("/a"), []byte{})
	if err != nil {
		t.Error(err)
	}

	v, err := d.Get(ctx, ds.NewKey("/a"))
	if err != nil {
		t.Error(err)
	}

	if len(v) != 0 {
		t.Error("expected 0 len []byte form get")
	}
}

func TestBatching(t *testing.T) {
	d, done := newDS(t)
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b, err := d.Batch(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for k, v := range testcases {
		err := b.Put(ctx, ds.NewKey(k), []byte(v))
		if err != nil {
			t.Fatal(err)
		}
	}

	err = b.Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for k, v := range testcases {
		val, err := d.Get(ctx, ds.NewKey(k))
		if err != nil {
			t.Fatal(err)
		}

		if v != string(val) {
			t.Fatal("got wrong data!")
		}
	}

	//Test delete
	b, err = d.Batch(ctx)
	if err != nil {
		t.Fatal(err)
	}

	err = b.Delete(ctx, ds.NewKey("/a/b"))
	if err != nil {
		t.Fatal(err)
	}

	err = b.Delete(ctx, ds.NewKey("/a/b/c"))
	if err != nil {
		t.Fatal(err)
	}

	err = b.Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}

	rs, err := d.Query(ctx, dsq.Query{Prefix: "/"})
	if err != nil {
		t.Fatal(err)
	}

	expectMatches(t, []string{
		"/a",
		"/a/b/d",
		"/a/c",
		"/a/d",
		"/e",
		"/f",
		"/g",
	}, rs)
}

func SubtestBasicPutGet(t *testing.T) {
	d, done := newDS(t)
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := ds.NewKey("foo")
	val := []byte("Hello Datastore!")

	err := d.Put(ctx, k, val)
	if err != nil {
		t.Fatal("error putting to datastore: ", err)
	}

	have, err := d.Has(ctx, k)
	if err != nil {
		t.Fatal("error calling has on key we just put: ", err)
	}

	if !have {
		t.Fatal("should have key foo, has returned false")
	}

	size, err := d.GetSize(ctx, k)
	if err != nil {
		t.Fatal("error getting size after put: ", err)
	}
	if size != len(val) {
		t.Fatalf("incorrect size: expected %d, got %d", len(val), size)
	}

	out, err := d.Get(ctx, k)
	if err != nil {
		t.Fatal("error getting value after put: ", err)
	}

	if !bytes.Equal(out, val) {
		t.Fatal("value received on get wasnt what we expected:", out)
	}

	have, err = d.Has(ctx, k)
	if err != nil {
		t.Fatal("error calling has after get: ", err)
	}

	if !have {
		t.Fatal("should have key foo, has returned false")
	}

	size, err = d.GetSize(ctx, k)
	if err != nil {
		t.Fatal("error getting size after get: ", err)
	}
	if size != len(val) {
		t.Fatalf("incorrect size: expected %d, got %d", len(val), size)
	}

	err = d.Delete(ctx, k)
	if err != nil {
		t.Fatal("error calling delete: ", err)
	}

	have, err = d.Has(ctx, k)
	if err != nil {
		t.Fatal("error calling has after delete: ", err)
	}

	if have {
		t.Fatal("should not have key foo, has returned true")
	}

	size, err = d.GetSize(ctx, k)
	switch err {
	case ds.ErrNotFound:
	case nil:
		t.Fatal("expected error getting size after delete")
	default:
		t.Fatal("wrong error getting size after delete: ", err)
	}
	if size != -1 {
		t.Fatal("expected missing size to be -1")
	}
}

func TestNotFounds(t *testing.T) {
	d, done := newDS(t)
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	badk := ds.NewKey("notreal")

	val, err := d.Get(ctx, badk)
	if err != ds.ErrNotFound {
		t.Fatal("expected ErrNotFound for key that doesnt exist, got: ", err)
	}

	if val != nil {
		t.Fatal("get should always return nil for not found values")
	}

	have, err := d.Has(ctx, badk)
	if err != nil {
		t.Fatal("error calling has on not found key: ", err)
	}
	if have {
		t.Fatal("has returned true for key we don't have")
	}

	size, err := d.GetSize(ctx, badk)
	switch err {
	case ds.ErrNotFound:
	case nil:
		t.Fatal("expected error getting size after delete")
	default:
		t.Fatal("wrong error getting size after delete: ", err)
	}
	if size != -1 {
		t.Fatal("expected missing size to be -1")
	}
}

func SubtestManyKeysAndQuery(t *testing.T) {
	d, done := newDS(t)
	defer done()

	var keys []ds.Key
	var keystrs []string
	var values [][]byte
	count := 100
	for i := 0; i < count; i++ {
		s := fmt.Sprintf("%dkey%d", i, i)
		dsk := ds.NewKey(s)
		keystrs = append(keystrs, dsk.String())
		keys = append(keys, dsk)
		buf := make([]byte, 64)
		_, _ = rand.Read(buf)
		values = append(values, buf)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Logf("putting %d values", count)
	for i, k := range keys {
		err := d.Put(ctx, k, values[i])
		if err != nil {
			t.Fatalf("error on put[%d]: %s", i, err)
		}
	}

	t.Log("getting values back")
	for i, k := range keys {
		val, err := d.Get(ctx, k)
		if err != nil {
			t.Fatalf("error on get[%d]: %s", i, err)
		}

		if !bytes.Equal(val, values[i]) {
			t.Fatal("input value didnt match the one returned from Get")
		}
	}

	t.Log("querying values")
	q := dsq.Query{KeysOnly: true}
	resp, err := d.Query(ctx, q)
	if err != nil {
		t.Fatal("calling query: ", err)
	}

	t.Log("aggregating query results")
	var outkeys []string
	for {
		res, ok := resp.NextSync()
		if res.Error != nil {
			t.Fatal("query result error: ", res.Error)
		}
		if !ok {
			break
		}

		outkeys = append(outkeys, res.Key)
	}

	t.Log("verifying query output")
	sort.Strings(keystrs)
	sort.Strings(outkeys)

	if len(keystrs) != len(outkeys) {
		t.Fatal("got wrong number of keys back")
	}

	for i, s := range keystrs {
		if outkeys[i] != s {
			t.Fatalf("in key output, got %s but expected %s", outkeys[i], s)
		}
	}

	t.Log("deleting all keys")
	for _, k := range keys {
		if err := d.Delete(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
}

// Tests from basic_tests from go-datastore
func TestBasicPutGet(t *testing.T) {
	d, done := newDS(t)
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := ds.NewKey("foo")
	val := []byte("Hello Datastore!")

	err := d.Put(ctx, k, val)
	if err != nil {
		t.Fatal("error putting to datastore: ", err)
	}

	have, err := d.Has(ctx, k)
	if err != nil {
		t.Fatal("error calling has on key we just put: ", err)
	}

	if !have {
		t.Fatal("should have key foo, has returned false")
	}

	out, err := d.Get(ctx, k)
	if err != nil {
		t.Fatal("error getting value after put: ", err)
	}

	if !bytes.Equal(out, val) {
		t.Fatal("value received on get wasnt what we expected:", out)
	}

	have, err = d.Has(ctx, k)
	if err != nil {
		t.Fatal("error calling has after get: ", err)
	}

	if !have {
		t.Fatal("should have key foo, has returned false")
	}

	err = d.Delete(ctx, k)
	if err != nil {
		t.Fatal("error calling delete: ", err)
	}

	have, err = d.Has(ctx, k)
	if err != nil {
		t.Fatal("error calling has after delete: ", err)
	}

	if have {
		t.Fatal("should not have key foo, has returned true")
	}
	SubtestBasicPutGet(t)
}

func TestManyKeysAndQuery(t *testing.T) {
	d, done := newDS(t)
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var keys []ds.Key
	var keystrs []string
	var values [][]byte
	count := 100
	for i := 0; i < count; i++ {
		s := fmt.Sprintf("%dkey%d", i, i)
		dsk := ds.NewKey(s)
		keystrs = append(keystrs, dsk.String())
		keys = append(keys, dsk)
		buf := make([]byte, 64)
		_, _ = rand.Read(buf)
		values = append(values, buf)
	}

	t.Logf("putting %d values", count)
	for i, k := range keys {
		err := d.Put(ctx, k, values[i])
		if err != nil {
			t.Fatalf("error on put[%d]: %s", i, err)
		}
	}

	t.Log("getting values back")
	for i, k := range keys {
		val, err := d.Get(ctx, k)
		if err != nil {
			t.Fatalf("error on get[%d]: %s", i, err)
		}

		if !bytes.Equal(val, values[i]) {
			t.Fatal("input value didnt match the one returned from Get")
		}
	}

	t.Log("querying values")
	q := dsq.Query{KeysOnly: true}
	resp, err := d.Query(ctx, q)
	if err != nil {
		t.Fatal("calling query: ", err)
	}

	t.Log("aggregating query results")
	var outkeys []string
	for {
		res, ok := resp.NextSync()
		if res.Error != nil {
			t.Fatal("query result error: ", res.Error)
		}
		if !ok {
			break
		}

		outkeys = append(outkeys, res.Key)
	}

	t.Log("verifying query output")
	sort.Strings(keystrs)
	sort.Strings(outkeys)

	if len(keystrs) != len(outkeys) {
		t.Fatalf("got wrong number of keys back, %d != %d", len(keystrs), len(outkeys))
	}

	for i, s := range keystrs {
		if outkeys[i] != s {
			t.Fatalf("in key output, got %s but expected %s", outkeys[i], s)
		}
	}

	t.Log("deleting all keys")
	for _, k := range keys {
		if err := d.Delete(ctx, k); err != nil {
			t.Fatal(err)
		}
	}

	SubtestManyKeysAndQuery(t)
}

func TestSuite(t *testing.T) {
	d, done := newDS(t)
	defer done()

	dstest.SubtestAll(t, d)
}

func expectMatches(t *testing.T, expect []string, actualR dsq.Results) {
	t.Helper()
	actual, err := actualR.Rest()
	if err != nil {
		t.Error(err)
	}

	if len(actual) != len(expect) {
		t.Error("not enough", expect, actual)
	}
	for _, k := range expect {
		found := false
		for _, e := range actual {
			if e.Key == k {
				found = true
			}
		}
		if !found {
			t.Error(k, "not found")
		}
	}
}

func expectKeyOrderMatches(t *testing.T, actual dsq.Results, expect []string) {
	rs, err := actual.Rest()
	if err != nil {
		t.Error("error fetching dsq.Results", expect, actual)
		return
	}

	if len(rs) != len(expect) {
		t.Error("expect != actual.", expect, actual)
		return
	}

	for i, r := range rs {
		if r.Key != expect[i] {
			t.Error("expect != actual.", expect, actual)
			return
		}
	}
}

func expectKeyFilterMatches(t *testing.T, actual dsq.Results, expect []string) {
	actualE, err := actual.Rest()
	if err != nil {
		t.Error(err)
		return
	}
	actualS := make([]string, len(actualE))
	for i, e := range actualE {
		actualS[i] = e.Key
	}

	if len(actualS) != len(expect) {
		t.Error("length doesn't match.", expect, actualS)
		return
	}

	if strings.Join(actualS, "") != strings.Join(expect, "") {
		t.Error("expect != actual.", expect, actualS)
		return
	}
}
