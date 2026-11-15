package wiki

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Commit struct {
	Hash    string
	Message string
	Author  string
	Date    time.Time
}

// GetPageHistory returns the list of commits for a specific page.
func GetPageHistory(title string) ([]Commit, error) {
	filename := cleanTitle(title) + ".md"
	cmd := exec.Command("git", "log", "--pretty=format:%H|%s|%an|%aI", "--", filename)
	cmd.Dir = PageDir
	// fmt.Printf("Running git log for page %s (file: %s)\n", title, filename)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	var commits []Commit
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if c, ok := parseCommitLine(line); ok {
			commits = append(commits, c)
		} else {
			fmt.Printf("Failed to parse commit line: %q\n", line)
		}
	}
	// fmt.Printf("Found %d commits for page %s\n", len(commits), title)
	return commits, nil
}

// GetDiff returns the diff of a specific file at a specific commit.
func GetDiff(title, hash string) (string, error) {
	filename := cleanTitle(title) + ".md"

	// Check for ambiguity
	cmd := exec.Command("git", "rev-parse", "--verify", hash)
	cmd.Dir = PageDir
	errOutput, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(errOutput), "ambiguous") {
			return "", fmt.Errorf("ambiguous hash")
		}
		return "", fmt.Errorf("invalid hash: %v", err)
	}
	fullHash := strings.TrimSpace(string(errOutput))

	cmd = exec.Command("git", "show", "--unified=3", fullHash, "--", filename)
	cmd.Dir = PageDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git show failed: %w", err)
	}

	return string(output), nil
}

// GetFileDiff returns the diff of a specific file at a specific commit.
func GetFileDiff(relativePath, hash string) (string, error) {
	// Check for ambiguity
	cmd := exec.Command("git", "rev-parse", "--verify", hash)
	cmd.Dir = PageDir
	errOutput, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(errOutput), "ambiguous") {
			return "", fmt.Errorf("ambiguous hash")
		}
		return "", fmt.Errorf("invalid hash: %v", err)
	}
	fullHash := strings.TrimSpace(string(errOutput))

	cmd = exec.Command("git", "show", "--unified=3", fullHash, "--", relativePath)
	cmd.Dir = PageDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git show failed: %w", err)
	}

	return string(output), nil
}

type FullCommit struct {
	Commit
	Files []string
}

// GetAllHistory returns the history of the entire repository.
func GetAllHistory() ([]FullCommit, error) {
	cmd := exec.Command("git", "log", "--pretty=format:%H|%s|%an|%aI%n", "--name-only")
	cmd.Dir = PageDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	var commits []FullCommit
	var current *FullCommit

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.Contains(line, "|") {
			if c, ok := parseCommitLine(line); ok {
				commits = append(commits, FullCommit{Commit: c, Files: []string{}})
				current = &commits[len(commits)-1]
			}
		} else if current != nil && strings.HasSuffix(line, ".md") {
			filename := filepath.Base(line)
			title := strings.TrimSuffix(filename, ".md")

			// Filter out system pages and non-content files
			if title == "Admin" || title == "Special" || title == "Changes" || title == "DeadLinks" {
				continue
			}
			current.Files = append(current.Files, title)
		}
	}

	// Filter out commits that have no files interest (e.g. only system pages)
	var filtered []FullCommit
	for _, c := range commits {
		if len(c.Files) > 0 {
			filtered = append(filtered, c)
		}
	}

	return filtered, nil
}

func parseCommitLine(line string) (Commit, bool) {
	parts := strings.Split(line, "|")
	if len(parts) < 4 {
		return Commit{}, false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[3]))
	if err != nil {
		fmt.Printf("Error parsing time %q: %v\n", parts[3], err)
		return Commit{}, false
	}
	return Commit{
		Hash:    parts[0],
		Message: parts[1],
		Author:  parts[2],
		Date:    t,
	}, true
}

// GetFileMetadata returns the latest commit hash, author, and date for a file.
func GetFileMetadata(relativePath string) (string, string, time.Time, error) {
	// git log -n 1 --pretty=format:%H|%an|%aI -- <file>
	cmd := exec.Command("git", "log", "-n", "1", "--pretty=format:%H|%an|%aI", "--", relativePath)
	cmd.Dir = PageDir
	output, err := cmd.Output()
	if err != nil {
		return "", "", time.Time{}, err
	}

	outStr := strings.TrimSpace(string(output))
	if outStr == "" {
		// This happens if file is not in git history (new or ignored)
		// fmt.Printf("[Metadata] No history for %s\n", relativePath)
		return "", "", time.Time{}, fmt.Errorf("no history for file")
	}

	parts := strings.Split(outStr, "|")
	if len(parts) < 3 {
		fmt.Printf("[Metadata] Invalid format for %s: %q\n", relativePath, outStr)
		return "", "", time.Time{}, fmt.Errorf("invalid log output: %s", outStr)
	}

	hash := parts[0]
	// parts[1] is subject, we skip it or use it if needed
	author := parts[2]

	if len(parts) < 4 {
		// Log error but maybe try to recover? No, strict check.
		fmt.Printf("[Metadata] Missing parts for %s: count=%d line=%q\n", relativePath, len(parts), outStr)
		return "", "", time.Time{}, fmt.Errorf("invalid log format (need 4 parts): %s", outStr)
	}

	t, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[3]))
	if err != nil {
		fmt.Printf("[Metadata] Time parse error for %s: %v\n", relativePath, err)
		return "", "", time.Time{}, err
	}

	return hash, author, t, nil
}

// GetFileCreationInfo returns the commit hash, author, and date of the FIRST commit involving the file.
func GetFileCreationInfo(relativePath string) (string, string, time.Time, error) {
	// --diff-filter=A : Select only records that differ from the previous by addition
	// But simply `git log --reverse ... | head -n 1` is easier to parse with existing logic if we strictly want the first commit affecting the file.
	// Actually `git log --diff-filter=A` is more precise for "Creation".

	cmd := exec.Command("git", "log", "--diff-filter=A", "--pretty=format:%H|%an|%aI", "--", relativePath)
	cmd.Dir = PageDir
	output, err := cmd.Output()
	if err != nil {
		return "", "", time.Time{}, err
	}

	outStr := strings.TrimSpace(string(output))
	if outStr == "" {
		// Fallback: use the oldest commit if filter=A fails (e.g. renamed?)
		cmd = exec.Command("git", "log", "--reverse", "--pretty=format:%H|%an|%aI", "--", relativePath)
		cmd.Dir = PageDir
		output, err = cmd.Output()
		if err != nil {
			return "", "", time.Time{}, err
		}
		outStr = strings.TrimSpace(string(output))
	}

	if outStr == "" {
		return "", "", time.Time{}, fmt.Errorf("no history for file")
	}

	// There might be multiple lines if added, deleted, added again?
	// Take the last one? Or first?
	// `git log` output with `filter=A` might show multiple if multiple adds (which shouldn't happen for uniqueness).
	// But `outStr` contains all matches. We want the oldest (creation).
	// If standard log is reverse chrono, we want the LAST line?
	// Wait, `git log` is newest first.
	// So `git log --reverse` is oldest first.
	// If I used `git log --diff-filter=A`, it returns newest add first?
	// If I created, deleted, created same name...
	// Usually IDs are unique timestamps, so only one creation.
	// So just take the first line of output?

	// Actually, let's just use `git log --reverse` and take the first line. That is the creation/first appearance.

	cmd = exec.Command("git", "log", "--reverse", "-n", "1", "--pretty=format:%H|%an|%aI", "--", relativePath)
	cmd.Dir = PageDir
	output, err = cmd.Output()
	if err != nil {
		return "", "", time.Time{}, err
	}
	outStr = strings.TrimSpace(string(output))
	if outStr == "" {
		return "", "", time.Time{}, fmt.Errorf("no history")
	}

	parts := strings.Split(outStr, "|")
	if len(parts) < 3 {
		return "", "", time.Time{}, fmt.Errorf("invalid log format")
	}

	hash := parts[0]
	author := parts[1]

	if len(parts) < 3 { // Double check
		return "", "", time.Time{}, fmt.Errorf("invalid log format")
	}

	t, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[2]))
	if err != nil {
		return "", "", time.Time{}, err
	}

	return hash, author, t, nil
}

// GetFileHistory returns the list of commits for a specific file (relative to PageDir).
func GetFileHistory(relativePath string) ([]Commit, error) {
	fmt.Printf("[History] Fetching history for file: %s\n", relativePath)
	cmd := exec.Command("git", "log", "--pretty=format:%H|%s|%an|%aI", "--", relativePath)
	cmd.Dir = PageDir
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("[History] git log failed: %v\n", err)
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	outStr := string(output)
	if len(outStr) == 0 {
		fmt.Printf("[History] git log output is empty for %s\n", relativePath)
	} else {
		// fmt.Printf("[History] git log output length: %d\n", len(outStr))
	}

	var commits []Commit
	lines := strings.Split(outStr, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if c, ok := parseCommitLine(line); ok {
			commits = append(commits, c)
		} else {
			fmt.Printf("[History] Failed to parse line: %q\n", line)
		}
	}
	fmt.Printf("[History] Found %d commits for %s\n", len(commits), relativePath)
	return commits, nil
}
