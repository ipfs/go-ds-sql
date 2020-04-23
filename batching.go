package sqlds

import (
	ds "github.com/ipfs/go-datastore"
)

type op struct {
	delete bool
	value  []byte
}

type batch struct {
	ds  *Datastore
	ops map[ds.Key]op
}

// Batch creates a set of deferred updates to the database.
// Since SQL does not support a true batch of updates,
// operations are buffered and then executed sequentially
// when Commit is called.
func (d *Datastore) Batch() (ds.Batch, error) {
	return &batch{
		ds:  d,
		ops: make(map[ds.Key]op),
	}, nil
}

func (bt *batch) Put(key ds.Key, val []byte) error {
	bt.ops[key] = op{value: val}
	return nil
}

func (bt *batch) Delete(key ds.Key) error {
	bt.ops[key] = op{delete: true}
	return nil
}

func (bt *batch) Commit() error {
	var err error
	for k, op := range bt.ops {
		if op.delete {
			err = bt.ds.Delete(k)
		} else {
			err = bt.ds.Put(k, op.value)
		}
		if err != nil {
			break
		}
	}

	return err
}

var _ ds.Batching = (*Datastore)(nil)
