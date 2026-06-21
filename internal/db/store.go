package db

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

type Store struct {
	*sql.DB
}

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = db.Close()
		}
	}()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	store := &Store{db}
	if err := store.runMigrations(); err != nil {
		return nil, err
	}
	if err := store.migrateAccounts(); err != nil {
		return nil, err
	}
	if err := store.migrateUploadsOwner(); err != nil {
		return nil, err
	}
	if err := store.migrateInviteTokenCipher(); err != nil {
		return nil, err
	}
	if _, err := store.Exec(`CREATE INDEX IF NOT EXISTS idx_uploads_owner_account ON uploads(owner_account_id)`); err != nil {
		return nil, err
	}
	closeOnError = false
	return store, nil
}
