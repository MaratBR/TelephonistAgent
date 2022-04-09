package db

import "github.com/dgraph-io/badger/v3"

type DB struct {
	db *badger.DB
}

func NewDatabase(path string) (*DB, error) {
	db, err := badger.Open(badger.DefaultOptions(path))
	if err != nil {
		return nil, err
	}

	return &DB{db: db}, nil
}

func (self *DB) SetSequenceState(id string, state SequenceState) error {

	return self.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("seq/"+id), []byte("42"))
	})
}
