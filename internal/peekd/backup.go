package peekd

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func runBackup(args []string) int {
	dataDir, backupPath, err := backupArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backup: %v\n", err)
		return 2
	}
	if err := backupDatabase(dataDir, backupPath); err != nil {
		fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
		return 1
	}
	fmt.Printf("backup written to %s\n", backupPath)
	return 0
}

func backupArgs(args []string) (dataDir, backupPath string, err error) {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	data := fs.String("data", getenv("PEEK_DATA", "./data"), "data directory")
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	if fs.NArg() > 1 {
		return "", "", fmt.Errorf("usage: peekd backup [--data <dir>] [path/to/backup.db]")
	}
	abs, err := filepath.Abs(*data)
	if err != nil {
		return "", "", fmt.Errorf("data dir: %w", err)
	}
	dest := filepath.Join(abs, "peek-backup.db")
	if fs.NArg() == 1 {
		dest = fs.Arg(0)
	}
	return abs, dest, nil
}

// backupDatabase creates a consistent snapshot of the SQLite database using
// VACUUM INTO, which works even while the server is running. The backup file
// is a standalone SQLite database that can be restored by simply replacing
// the original peek.db.
func backupDatabase(dataDir, destPath string) error {
	dbPath := filepath.Join(dataDir, "peek.db")
	dsn := "file:" + dbPath + "?_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open source db: %w", err)
	}
	defer db.Close()

	destURI := "file:" + destPath
	_, err = db.Exec("VACUUM INTO ?", destURI)
	if err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return nil
}
