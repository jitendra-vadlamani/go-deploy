package db

import (
	"encoding/json"
	"fmt"

	badger "github.com/dgraph-io/badger/v4"
)

var db *badger.DB

// Project represents a saved build configuration
type Project struct {
	ID          string            `json:"ID"` // This will be the SourceDir
	Name        string            `json:"Name"`
	Description string            `json:"Description"`
	Version     string            `json:"Version"`
	BrowserURL  string            `json:"BrowserURL"`
	OutputDir   string            `json:"OutputDir"`
	DefaultMode string            `json:"DefaultMode"`
	BuildEnv    map[string]string `json:"BuildEnv"`
	Formats     []string          `json:"Formats"`
	UpdatedAt   int64             `json:"UpdatedAt"`
}

func Init(path string) error {
	opts := badger.DefaultOptions(path).WithLogger(nil)
	var err error
	db, err = badger.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open badger db: %w", err)
	}
	return nil
}

func Close() {
	if db != nil {
		db.Close()
	}
}

func SaveProject(p Project) error {
	return db.Update(func(txn *badger.Txn) error {
		data, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return txn.Set([]byte("project:"+p.ID), data)
	})
}

func GetProject(id string) (*Project, error) {
	var p Project
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("project:" + id))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &p)
		})
	})
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func DeleteProject(id string) error {
	return db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte("project:" + id))
	})
}

func GetAllProjects() ([]Project, error) {
	var projects []Project
	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()
		prefix := []byte("project:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			err := it.Item().Value(func(val []byte) error {
				var p Project
				if err := json.Unmarshal(val, &p); err != nil {
					return err
				}
				projects = append(projects, p)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return projects, err
}
