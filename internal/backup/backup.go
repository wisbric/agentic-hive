// Package backup provides SQLite hot-copy via VACUUM INTO.
package backup

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// Run opens the source database at srcPath and writes a consistent copy to
// dstPath using VACUUM INTO. The destination file must not already exist;
// Run removes it first if it does.
func Run(srcPath, dstPath string) error {
	// Make dstPath absolute so SQLite's VACUUM INTO accepts it.
	abs, err := filepath.Abs(dstPath)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	dstPath = abs

	// Remove stale destination so VACUUM INTO doesn't fail.
	if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing backup file: %w", err)
	}

	db, err := sql.Open("sqlite3", srcPath+"?mode=ro&_foreign_keys=on")
	if err != nil {
		return fmt.Errorf("open source database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping source database: %w", err)
	}

	// VACUUM INTO performs an atomic, consistent snapshot of the live database.
	if _, err := db.Exec(fmt.Sprintf("VACUUM INTO %q", dstPath)); err != nil {
		return fmt.Errorf("vacuum into %s: %w", dstPath, err)
	}

	return nil
}
