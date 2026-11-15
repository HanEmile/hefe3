package wiki

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// DeadLink represents a link from one page to a non-existent page
type DeadLink struct {
	SourcePage string
	TargetPage string
}

// FindDeadLinks scans all pages and finds links to non-existent pages
func FindDeadLinks() ([]DeadLink, error) {
	pages, err := ListPages()
	if err != nil {
		return nil, err
	}

	// Create a map of existing pages for quick lookup
	existingPages := make(map[string]bool)
	for _, page := range pages {
		existingPages[page] = true
	}

	var deadLinks []DeadLink

	// Regex to match markdown links: [text](target) or [text](/target)
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(/?([^)]+)\)`)

	for _, sourcePage := range pages {
		// Skip special pages to avoid circular references
		if sourcePage == "Special" || sourcePage == "DeadLinks" || sourcePage == "Admin" || sourcePage == "Changes" {
			continue
		}
		content, err := os.ReadFile(getFilePath(sourcePage))
		if err != nil {
			continue
		}

		// Find all links in the page
		matches := linkRegex.FindAllSubmatch(content, -1)
		for _, match := range matches {
			if len(match) >= 3 {
				targetPage := string(match[2])

				// Skip external links (http://, https://, etc.)
				if bytes.HasPrefix(match[2], []byte("http://")) ||
					bytes.HasPrefix(match[2], []byte("https://")) ||
					bytes.HasPrefix(match[2], []byte("mailto:")) {
					continue
				}

				// Check if target page exists
				if !existingPages[targetPage] {
					deadLinks = append(deadLinks, DeadLink{
						SourcePage: sourcePage,
						TargetPage: targetPage,
					})
				}
			}
		}
	}

	return deadLinks, nil
}

// UpdateSpecialPages creates/updates special pages with current data.
func UpdateSpecialPages() error {
	if err := SavePage("Special", generateSpecialIndex(), "system", "Update Special page (auto-generated)"); err != nil {
		return err
	}

	if err := updateDeadLinksPage(); err != nil {
		return err
	}

	return updateChangesPage()
}

func generateSpecialIndex() string {
	return `This page contains links to special wiki pages that are automatically generated.

# Available Special Pages

- [DeadLinks](/DeadLinks) - Links to pages that don't exist yet
- [Changes](/Changes) - All changes in the wiki
`
}

func updateDeadLinksPage() error {
	deadLinks, err := FindDeadLinks()
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	buf.WriteString("This page lists all links to pages that don't exist yet.\n\n")

	if len(deadLinks) == 0 {
		buf.WriteString("✅ No dead links found! All links point to existing pages.\n")
	} else {
		buf.WriteString(fmt.Sprintf("Found %d dead link(s):\n\n| Source Page | Missing Target | Action |\n|-------------|----------------|--------|\n", len(deadLinks)))
		for _, dl := range deadLinks {
			buf.WriteString(fmt.Sprintf("| [%s](/%s) | %s | [Create](/%s?edit) |\n", dl.SourcePage, dl.SourcePage, dl.TargetPage, dl.TargetPage))
		}
	}
	buf.WriteString("\n---\n\n[Back to Special Pages](/Special)\n")

	msg := fmt.Sprintf("Update DeadLinks page (%d dead links)", len(deadLinks))
	return SavePage("DeadLinks", buf.String(), "system", msg)
}

func updateChangesPage() error {
	history, err := GetAllHistory()
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	buf.WriteString("This page lists all changes across the wiki.\n\n| Hash | Page | Author | Message |\n|------|------|--------|---------|\n")

	for _, c := range history {
		hash := c.Hash
		if len(hash) > 7 {
			hash = hash[:7]
		}

		pages := "-"
		if len(c.Files) > 0 {
			var links []string
			for _, f := range c.Files {
				links = append(links, fmt.Sprintf("[%s](/%s)", f, f))
			}
			pages = strings.Join(links, ", ")
		}

		buf.WriteString(fmt.Sprintf("| [%s](/%s?diff=%s) | %s | %s | %s |\n",
			hash, c.Files[0], c.Hash, pages, c.Author, c.Message))
	}
	buf.WriteString("\n---\n\n[Back to Special Pages](/Special)\n")

	msg := fmt.Sprintf("Update Changes page (%d total changes)", len(history))
	return SavePage("Changes", buf.String(), "system", msg)
}

// UpdateAdminPages creates/updates admin pages with current data.
func UpdateAdminPages() error {
	cacheStats := GetCacheStats()
	totalBacklinks := 0
	for _, backlinks := range cacheStats {
		totalBacklinks += len(backlinks)
	}

	var buf bytes.Buffer
	buf.WriteString(`Administrative tools and information.

# Backlink Cache Statistics
`)
	buf.WriteString(fmt.Sprintf("- **Total pages with backlinks**: %d\n", len(cacheStats)))
	buf.WriteString(fmt.Sprintf("- **Total backlink entries**: %d\n\n", totalBacklinks))

	if len(cacheStats) > 0 {
		buf.WriteString("# Cache Contents\n\n| Page | Backlinks |\n|------|----------|\n")
		for page, backlinks := range cacheStats {
			if len(backlinks) > 0 {
				var links []string
				for _, bl := range backlinks {
					links = append(links, fmt.Sprintf("[%s](/%s)", bl, bl))
				}
				buf.WriteString(fmt.Sprintf("| [%s](/%s) | %s |\n", page, page, strings.Join(links, ", ")))
			}
		}
		buf.WriteString("\n")
	}

	buf.WriteString(`
# Actions

- [Run All Jobs](/Admin?run_all_jobs)
- [Rebuild Cache](/Admin?rebuild_cache)
- [Purge Cache](/Admin?purge_cache)
- [View Cache JSON](/Admin?view_cache_json)

[Back to Home](/)
`)

	msg := fmt.Sprintf("Update Admin page (%d pages, %d backlinks)", len(cacheStats), totalBacklinks)
	return SavePage("Admin", buf.String(), "system", msg)
}
