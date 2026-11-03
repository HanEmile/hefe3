package main

// backupsizes.go - per-repo on-disk sizes for the Backups tab.
//
// du over the storagebox CIFS mount takes seconds-to-minutes per repo when
// the metadata cache is cold, so sizes can never be measured during a page
// render (the old inline 1.5s-timeout du silently always came back empty).
// Instead a background refresher walks every configured repo sequentially,
// every six hours, and keeps the result in memory + on disk:
//
//	STATUS_BOARD_REPO_SIZES  (default /var/lib/status-board/reposizes.json)

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	repoSizesPath = "/var/lib/status-board/reposizes.json"

	repoSizesMu sync.RWMutex
	// repo path (".../backup/<vm>") -> bytes used by its data/ subdir
	repoSizes = map[string]int64{}
)

func init() {
	if env := os.Getenv("STATUS_BOARD_REPO_SIZES"); env != "" {
		repoSizesPath = env
	}
}

func repoSizeStr(repo string) string {
	repoSizesMu.RLock()
	b, ok := repoSizes[repo]
	repoSizesMu.RUnlock()
	if !ok || b <= 0 {
		return ""
	}
	return humanBytes(b)
}

func humanBytes(b int64) string {
	const gib = 1 << 30
	const mib = 1 << 20
	switch {
	case b >= gib:
		return fmt.Sprintf("%.1fG", float64(b)/gib)
	case b >= mib:
		return fmt.Sprintf("%.0fM", float64(b)/mib)
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// startRepoSizer seeds from disk, then rescans every six hours. The scan is
// sequential on purpose: this is a CIFS mount over WAN - parallel walks
// wedge it for everyone.
func startRepoSizer() {
	if b, err := os.ReadFile(repoSizesPath); err == nil {
		var m map[string]int64
		if json.Unmarshal(b, &m) == nil && len(m) > 0 {
			repoSizesMu.Lock()
			repoSizes = m
			repoSizesMu.Unlock()
			log.Printf("repo sizes: seeded %d entries from cache", len(m))
		}
	}
	go func() {
		for {
			refreshRepoSizes()
			time.Sleep(6 * time.Hour)
		}
	}()
}

func refreshRepoSizes() {
	start := time.Now()
	fresh := map[string]int64{}
	walk := func(repo string) {
		if _, done := fresh[repo]; done {
			return
		}
		sz, err := dirSize(filepath.Join(repo, "data"))
		if err != nil {
			log.Printf("repo sizes: %s: %v", repo, err)
			return
		}
		fresh[repo] = sz
	}
	for vm, targets := range backupTargets {
		if !backupsEnabled[vm] {
			continue
		}
		for _, t := range targets {
			walk(t.Repo)
		}
	}
	// explicit per-path targets (lampadas job repos + grave mirrors)
	for _, paths := range explicitPathTargets {
		for _, bp := range paths {
			for _, t := range bp.Targets {
				walk(t.Repo)
			}
		}
	}
	if len(fresh) == 0 {
		return // storagebox unreachable? keep whatever we had
	}
	repoSizesMu.Lock()
	repoSizes = fresh
	repoSizesMu.Unlock()
	if b, err := json.Marshal(fresh); err == nil {
		_ = os.WriteFile(repoSizesPath, b, 0o644)
	}
	log.Printf("repo sizes: refreshed %d repos in %s", len(fresh), time.Since(start).Round(time.Second))
}

// dirSize sums regular-file sizes under root. Symlinks are not followed
// (restic data dirs contain only files anyway).
func dirSize(root string) (int64, error) {
	var total int64
	var firstErr error
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	if total == 0 && firstErr != nil {
		return 0, firstErr
	}
	return total, nil
}
