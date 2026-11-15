// internal/server/git.go — Git integration: list tags/branches, checkout
// a tag into the working tree, then enqueue re-indexing.
// Tag names are validated to prevent shell injection before any exec.
//
// Part of github.com/hanemile/kscope
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
)

// handleGitTags returns all git tags sorted newest-first.
func (s *Server) handleGitTags(w http.ResponseWriter, r *http.Request) {
	tags, branches, err := gitListRefs(s.cfg.KernelRoot)
	if err != nil {
		writeJSON(w, map[string]interface{}{"tags": []string{}, "branches": []string{}, "error": err.Error()})
		return
	}
	// Also return HEAD info
	head, _ := gitCmd(s.cfg.KernelRoot, "rev-parse", "--abbrev-ref", "HEAD")
	writeJSON(w, map[string]interface{}{
		"tags":     tags,
		"branches": branches,
		"head":     strings.TrimSpace(head),
	})
}

// handleGitCheckout checks out a tag/branch and enqueues re-indexing.
func (s *Server) handleGitCheckout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tag   string `json:"tag"`
		Label string `json:"label,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Tag == "" {
		http.Error(w, "tag required", 400)
		return
	}
	if err := validateRef(req.Tag); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// Stash any dirty state so checkout succeeds on partial trees
	gitCmd(s.cfg.KernelRoot, "stash") //nolint:errcheck

	out, err := gitCmd(s.cfg.KernelRoot, "checkout", req.Tag)
	if err != nil {
		http.Error(w, fmt.Sprintf("git checkout failed: %s\n%s", err, out), 500)
		return
	}

	label := req.Label
	if label == "" { label = req.Tag }

	jobID := s.enqueueIndex(s.cfg.KernelRoot, label, req.Tag)
	writeJSON(w, map[string]interface{}{
		"label":  label,
		"tag":    req.Tag,
		"job_id": jobID,
		"status": "indexing",
	})
}

// ── git helpers ───────────────────────────────────────────────────────────────

func gitCmd(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("git %v: %w", args, err)
	}
	return buf.String(), nil
}

func gitListRefs(root string) (tags, branches []string, err error) {
	// Tags sorted by version
	tOut, tErr := gitCmd(root, "tag", "--sort=-version:refname")
	if tErr == nil {
		for _, t := range strings.Split(strings.TrimSpace(tOut), "\n") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	} else {
		err = tErr
	}

	// Branches
	bOut, bErr := gitCmd(root, "branch", "--format=%(refname:short)")
	if bErr == nil {
		for _, b := range strings.Split(strings.TrimSpace(bOut), "\n") {
			if b = strings.TrimSpace(strings.TrimPrefix(b, "* ")); b != "" {
				branches = append(branches, b)
			}
		}
	}
	return
}

// validateRef rejects ref names that could escape the git argument.
func validateRef(ref string) error {
	for _, c := range ref {
		if !isRefChar(c) {
			return fmt.Errorf("invalid ref character: %q", c)
		}
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("ref must not start with '-'")
	}
	return nil
}

func isRefChar(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' ||
		c == '/' || c == '^' || c == '~'
}
