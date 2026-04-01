package db

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backup creates a backup of the database to the specified path.
// Uses SQLite's VACUUM INTO for a consistent backup.
func (d *DB) Backup(destPath string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating backup directory: %w", err)
	}

	// Use VACUUM INTO for atomic, consistent backup
	_, err := d.Exec(fmt.Sprintf("VACUUM INTO '%s'", destPath))
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	return nil
}

// BackupWithTimestamp creates a backup with a timestamped filename in the given directory.
// Returns the full path to the created backup.
func (d *DB) BackupWithTimestamp(dir string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("makhor-backup-%s.db", timestamp)
	destPath := filepath.Join(dir, filename)

	if err := d.Backup(destPath); err != nil {
		return "", err
	}

	return destPath, nil
}

// Restore copies a backup file to the target database path.
// The application should be stopped before calling this, and restarted after.
func Restore(backupPath, dbPath string) error {
	// Verify backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", backupPath)
	}

	// Open source
	src, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("opening backup: %w", err)
	}
	defer src.Close()

	// Create destination (overwrites existing)
	dst, err := os.Create(dbPath)
	if err != nil {
		return fmt.Errorf("creating destination: %w", err)
	}
	defer dst.Close()

	// Copy
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copying backup: %w", err)
	}

	return nil
}

// CleanOldBackups removes old backups keeping only the most recent 'retain' files.
func CleanOldBackups(dir string, retain int) error {
	if retain <= 0 {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading backup directory: %w", err)
	}

	// Filter and collect backup files
	var backups []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "makhor-backup-") && strings.HasSuffix(name, ".db") {
			backups = append(backups, filepath.Join(dir, name))
		}
	}

	// Sort by name (timestamp ensures chronological order)
	sort.Strings(backups)

	// Remove oldest backups if we have more than retain
	if len(backups) > retain {
		toDelete := backups[:len(backups)-retain]
		for _, path := range toDelete {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("removing old backup %s: %w", path, err)
			}
		}
	}

	return nil
}
