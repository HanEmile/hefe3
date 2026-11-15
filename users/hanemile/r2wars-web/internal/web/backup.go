package web

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"git.emile.space/r2wars-web/internal/store"
)

const backupTimeFormat = "20060102-150405"

func backupOnce(st *store.Store, dir string, keep int) error {
	dest := filepath.Join(dir, fmt.Sprintf("main-%s.db", time.Now().UTC().Format(backupTimeFormat)))
	if err := st.Backup(dest); err != nil {
		return err
	}
	return pruneBackups(dir, keep)
}

// pruneBackups keeps only the keep most recent "main-*.db" snapshots in dir.
func pruneBackups(dir string, keep int) error {
	if keep <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("web: listing backups: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // timestamp-formatted filenames sort chronologically

	if len(names) <= keep {
		return nil
	}
	for _, name := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("web: pruning old backup %q: %w", name, err)
		}
	}
	return nil
}
