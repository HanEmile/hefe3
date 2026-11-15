package wiki

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
)

// GitCommit adds all changes and commits them with the given message
func GitCommit(repoPath, message, author string) error {
	// Add all changes
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %s: %w", string(output), err)
	}

	// Prepare author email (use `username@emile.space` format)
	authorEmail := fmt.Sprintf("%s@emile.space", author)

	// Commit with environment variables to set author/committer identity
	cmd = exec.Command("git", "commit", "-m", message)
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GIT_AUTHOR_NAME=%s", author),
		fmt.Sprintf("GIT_AUTHOR_EMAIL=%s", authorEmail),
		fmt.Sprintf("GIT_COMMITTER_NAME=%s", author),
		fmt.Sprintf("GIT_COMMITTER_EMAIL=%s", authorEmail),
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		// If nothing to commit, git commit returns 1. Check if that's the case.
		outp := string(output)
		nothing := strings.Contains(outp, "nothing to commit")
		no_changes := strings.Contains(outp, "no changes added to commit")
		if nothing || no_changes {
			// fmt.Println("[Git] Nothing to commit")
			return nil
		}
		fmt.Printf("[Git] Commit failed: %s (Err: %v)\n", outp, err)
		return fmt.Errorf("git commit failed: %s: %w", string(output), err)
	}

	// fmt.Printf("[Git] Commit success: %s\n", message)
	return nil
}

// EnsureRepo initializes the repo if it doesn't exist
func EnsureRepo(repoPath string) error {
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repo dir: %w", err)
	}

	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		// Not a repo, let's look if it's empty
		f, err := os.Open(repoPath)
		if err != nil {
			return fmt.Errorf("failed to open repo dir: %w", err)
		}

		// this is janky, but without the anonymous function, the error couldn't be handled
		defer func() {
			err = f.Close()
			if err != nil {
				log.Println(err)
			}
		}()

		_, err = f.Readdirnames(1)
		if err == io.EOF {
			// Empty directory, safe to init
			cmd := exec.Command("git", "init")
			cmd.Dir = repoPath
			if output, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git init failed: %s: %w", string(output), err)
			}
			return nil
		}

		// Not empty and not a git repo: error out for safety
		return fmt.Errorf("directory %s exists, is not empty, and is not a git repository", repoPath)
	}
	return nil
}
