package db

import (
	"encoding/json"

	bolt "go.etcd.io/bbolt"
)

type DB struct {
	db *bolt.DB
}

func Open(path string) (*DB, error) {
	db, err := bolt.Open(path, 0666, nil)
	if err != nil {
		return nil, err
	}

	return &DB{db: db}, nil
}

func (db *DB) putJSON(bucket, id string, data interface{}) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return db.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		return bucket.Put([]byte(id), jsonBytes)
	})
}

func (db *DB) getJSON(bucketName, id string, ptr interface{}) (bool, error) {
	if ptr == nil {
		panic("ptr is nil!")
	}

	found := true

	err := db.db.View(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return err
		}
		value := bucket.Get([]byte(id))
		if value == nil {
			found = false
			return nil
		} else {
			return json.Unmarshal(value, ptr)
		}
	})
	return found, err
}

//#region High-level

func (db *DB) GetSequence(id string) (*SequenceDBRecord, error) {
	record := new(SequenceDBRecord)
	found, err := db.getJSON("seq", id, record)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return record, err
}

func (db *DB) PutSequence(id string, record SequenceDBRecord) error {
	return db.putJSON("seq", id, record)
}

func (db *DB) UpdateSequence(id string, update func(*SequenceDBRecord)) (bool, error) {
	record, err := db.GetSequence(id)
	if err != nil {
		return false, err
	}
	if record == nil {
		return false, nil
	}
	update(record)
	return true, db.PutSequence(id, *record)
}

func (db *DB) IterateSequences(iteration func(id string, sequence *SequenceDBRecord)) {
	mapRecordsUtil(db, "seq", iteration)
}

func (db *DB) FindLocalOnlySequences() []*SequenceDBRecord {
	sequences := []*SequenceDBRecord{}
	db.IterateSequences(func(id string, sequence *SequenceDBRecord) {
		if sequence.OnlyLocal {
			seq := new(SequenceDBRecord)
			*seq = *sequence
			sequences = append(sequences, seq)
		}
	})
	return sequences
}

func mapRecordsUtil[T any](db *DB, bucketName string, iteration func(key string, v *T)) {
	db.db.View(func(tx *bolt.Tx) error {
		var (
			record T
			err    error
		)
		bucket := tx.Bucket([]byte(bucketName))
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(k, v []byte) error {
			err = json.Unmarshal(v, &record)
			if err == nil {
				iteration(string(k), &record)
			}
			return err
		})
	})
}

//#endregion
