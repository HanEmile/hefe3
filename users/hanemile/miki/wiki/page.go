package wiki

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"fmt" // Keep fmt because fmt.Sprintf is still used

	"sync"
)

const PageDir = "data"

type Page struct {
	Title     string
	Body      []byte // Rendered HTML
	Raw       []byte // Raw Markdown
	Backlinks []string
	TOC       []TOCEntry
	Comments  []*Comment
}

var (
	backlinkCache = make(map[string][]string)
	cacheMutex    sync.RWMutex
)

// cleanTitle sanitizes the title for filename usage
func cleanTitle(title string) string {
	return strings.ReplaceAll(title, "/", "_")
}

func getFilePath(title string) string {
	return filepath.Join(PageDir, cleanTitle(title)+".md")
}

// InitCache rebuilds the backlink cache entirely.
func InitCache() {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	backlinkCache = make(map[string][]string)
	files, err := ListPages()
	if err != nil {
		return
	}

	for _, target := range files {
		sources := scanForLinksTo(target, files)
		backlinkCache[target] = sources
	}
	fmt.Println("[Log] Backlink cache initialized.")
}

// scanForLinksTo checks which files link to targetTitle. Returns list of sources.
// If allFiles is nil, it fetches them using ListPages.
func scanForLinksTo(targetTitle string, allFiles []string) []string {
	if allFiles == nil {
		allFiles, _ = ListPages()
	}

	var sources []string
	searchLink1 := fmt.Sprintf("](%s)", targetTitle)
	searchLink2 := fmt.Sprintf("](/%s)", targetTitle)

	for _, title := range allFiles {
		if title == targetTitle {
			continue
		}
		content, err := os.ReadFile(getFilePath(title))
		if err != nil {
			continue
		}
		if bytes.Contains(content, []byte(searchLink1)) || bytes.Contains(content, []byte(searchLink2)) {
			sources = append(sources, title)
		}
	}
	return sources
}

// GetBacklinks returns the list of pages that link to the given title from the cache.
func GetBacklinks(title string) []string {
	cacheMutex.RLock()
	defer cacheMutex.RUnlock()
	return backlinkCache[title]
}

// GetCacheStats returns a deep copy of the backlink cache for reporting.
func GetCacheStats() map[string][]string {
	cacheMutex.RLock()
	defer cacheMutex.RUnlock()

	result := make(map[string][]string)
	for k, v := range backlinkCache {
		result[k] = append([]string{}, v...)
	}
	return result
}

// PurgeCache clears the backlink cache.
func PurgeCache() {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	backlinkCache = make(map[string][]string)
	fmt.Println("[Log] Backlink cache purged.")
}

// RefreshPageCache triggers a full cache rebuild.
func RefreshPageCache(title string) {
	fmt.Printf("[Cache] Refreshing cache (triggered by page: %s)\n", title)
	InitCache()
}

// UpdateBacklinksForSource updates the cache given a source page and its new content.
func UpdateBacklinksForSource(sourceTitle string, newContent []byte) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	// 1. Remove sourceTitle from all lists in cache.
	removeFromCache(sourceTitle)

	// 2. Scan content for links to existing pages.
	files, _ := ListPages()
	for _, target := range files {
		if target == sourceTitle {
			continue
		}
		if isLinked(newContent, target) {
			backlinkCache[target] = append(backlinkCache[target], sourceTitle)
		}
	}
}

// removeFromCache removes a title as a source from all backlink lists.
func removeFromCache(title string) {
	for target, sources := range backlinkCache {
		var newSources []string
		for _, s := range sources {
			if s != title {
				newSources = append(newSources, s)
			}
		}
		backlinkCache[target] = newSources
	}
}

// isLinked checks if content contains a markdown link to target.
func isLinked(content []byte, target string) bool {
	l1 := fmt.Sprintf("](%s)", target)
	l2 := fmt.Sprintf("](/%s)", target)
	return bytes.Contains(content, []byte(l1)) || bytes.Contains(content, []byte(l2))
}

func LoadPage(title string) (*Page, error) {
	body, err := os.ReadFile(getFilePath(title))
	if err != nil {
		return nil, err
	}

	// Load and append comments
	comments, _ := LoadComments(title)

	// We no longer append text/markdown comments to body, as we render them in template
	fullBody := body

	htmlBytes, toc, err := RenderPage(fullBody)
	if err != nil {
		return nil, err
	}

	return &Page{
		Title:     title,
		Raw:       body, // Keep original raw without comments for editing?
		Body:      htmlBytes,
		TOC:       toc,
		Backlinks: GetBacklinks(title),
		Comments:  comments,
	}, nil
}

func SavePage(title, content, author, message string) error {
	filename := getFilePath(title)
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		return err
	}

	UpdateBacklinksForSource(title, []byte(content))

	if message == "" {
		message = "Update page " + title
	}
	return GitCommit(PageDir, message, author)
}

func DeletePage(title, author string) error {
	if err := os.Remove(getFilePath(title)); err != nil {
		return err
	}

	cacheMutex.Lock()
	delete(backlinkCache, title)
	removeFromCache(title)
	cacheMutex.Unlock()

	return GitCommit(PageDir, "Delete page "+title, author)
}

// ListPages returns a list of all page titles.
func ListPages() ([]string, error) {
	entries, err := os.ReadDir(PageDir)
	if err != nil {
		return nil, err
	}
	var pages []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			pages = append(pages, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return pages, nil
}
