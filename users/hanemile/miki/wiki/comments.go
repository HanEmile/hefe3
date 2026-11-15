package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const CommentDir = "comments"

type Comment struct {
	ID        string // Unique ID (Filename for now)
	Hash      string // Commit Hash
	ParentID  string // Page Title or Parent Comment ID
	Author    string
	Date      time.Time
	Content   string
	Children  []*Comment
	IsPageRef bool   // True if ParentID refers to a Page
	PageTitle string // Stores the root page title for easy linking
	IsDeleted bool
}

// EnsureCommentDir creates the comments directory if it doesn't exist
func EnsureCommentDir() error {
	path := filepath.Join(PageDir, CommentDir)
	return os.MkdirAll(path, 0755)
}

// LoadComments loads all comments for a specific page recursively.
func LoadComments(pageTitle string) ([]*Comment, error) {
	if err := EnsureCommentDir(); err != nil {
		return nil, err
	}

	root := filepath.Join(PageDir, CommentDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err // Return nil if no comments dir or error
	}

	// 1. Read all comments into a map
	allComments := make(map[string]*Comment)

	// We need to scan all files because we don't know which ones belong to this page
	// without reading the "Parent" header.
	// This might be slow for valid large wikis, but valid for this task.

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		id := strings.TrimSuffix(e.Name(), ".md")
		contentBytes, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}

		content := string(contentBytes)
		parentID, status, body := parseCommentFile(content)

		isDeleted := status == "deleted"

		// Fetch metadata from git
		// We use the ID (filename) to look up git history
		// Use Creation Info to get the original author and date
		hash, author, date, err := GetFileCreationInfo(filepath.Join(CommentDir, e.Name()))
		if err != nil {
			// If not in git yet, try GetFileMetadata (Head) or use defaults
			hash, author, date, err = GetFileMetadata(filepath.Join(CommentDir, e.Name()))
			if err != nil {
				hash = "unknown"
				author = "unknown"
				date = time.Now()
			}
		}

		allComments[id] = &Comment{
			ID:        id,
			Hash:      hash,
			ParentID:  parentID,
			Author:    author,
			Date:      date,
			Content:   body,
			Children:  []*Comment{},
			PageTitle: pageTitle,
			IsDeleted: isDeleted,
		}
	}

	// 2. Build Tree
	var rootComments []*Comment

	for _, c := range allComments {
		if c.ParentID == pageTitle {
			rootComments = append(rootComments, c)
		} else {
			if parent, ok := allComments[c.ParentID]; ok {
				parent.Children = append(parent.Children, c)
			}
			// If parent not found, it's an orphan or belongs to another page/tree
			// We ignore it for THIS page's render
		}
	}

	// 3. Sort by Date
	sortComments(rootComments)

	return rootComments, nil
}

func sortComments(list []*Comment) {
	sort.Slice(list, func(i, j int) bool {
		return list[i].Date.Before(list[j].Date)
	})
	for _, c := range list {
		sortComments(c.Children)
	}
}

// parseCommentFile extracts Parent, Status and Body
// Format:
// Parent: <ID>
// Status: <Status> (Optional)
// <Blank Line>
// <Body>
func parseCommentFile(content string) (string, string, string) {
	lines := strings.Split(content, "\n")
	parent := ""
	status := ""
	bodyStart := 0

	for i, line := range lines {
		if strings.HasPrefix(line, "Parent: ") {
			parent = strings.TrimSpace(strings.TrimPrefix(line, "Parent: "))
		} else if strings.HasPrefix(line, "Status: ") {
			status = strings.TrimSpace(strings.TrimPrefix(line, "Status: "))
		} else if strings.TrimSpace(line) == "" {
			bodyStart = i + 1
			break
		}
	}

	if bodyStart >= len(lines) {
		return parent, status, ""
	}

	return parent, status, strings.Join(lines[bodyStart:], "\n")
}

// RenderCommentsMarkdown generates the markdown list for the comments
func RenderCommentsMarkdown(pageTitle string, comments []*Comment) string {
	var sb strings.Builder
	sb.WriteString("\n# Comments\n\nThis is a comment section\n\n")

	// Link to add a top-level comment
	sb.WriteString(fmt.Sprintf("[Add Comment](/comment?page=%s&parent=%s)\n\n", pageTitle, pageTitle))

	if len(comments) == 0 {
		return sb.String()
	}

	for _, c := range comments {
		renderCommentRecursive(&sb, c, 0, pageTitle)
	}

	return sb.String()
}

func renderCommentRecursive(sb *strings.Builder, c *Comment, level int, pageTitle string) {
	indent := strings.Repeat("  ", level)
	dateStr := c.Date.Format("2006-01-02 15:04")
	authorLink := fmt.Sprintf("[%s](/%s)", c.Author, c.Author)
	cleanContent := strings.TrimSpace(c.Content)

	// Add anchor with Hash and Reply link
	// We use raw HTML for the reply link to make it unobtrusive, or markdown link.
	// Markdown link: [Reply](/comment?page=...&parent=...)
	replyLink := fmt.Sprintf("([Reply](/comment?page=%s&parent=%s))", pageTitle, c.ID)

	sb.WriteString(fmt.Sprintf("%s* <a id=\"%s\"></a>%s (%s): %s %s\n", indent, c.Hash, authorLink, dateStr, cleanContent, replyLink))

	for _, child := range c.Children {
		renderCommentRecursive(sb, child, level+1, pageTitle)
	}
}

// SaveComment creates a new comment file
func SaveComment(pageTitle, parentID, content, author string) error {
	if err := EnsureCommentDir(); err != nil {
		return err
	}

	// Generate ID: timestamp-random
	id := fmt.Sprintf("comment-%d", time.Now().UnixNano())
	filename := filepath.Join(PageDir, CommentDir, id+".md")

	// Format content
	// Parent: <ID>
	//
	// <Content>
	fileContent := fmt.Sprintf("Parent: %s\n\n%s", parentID, content)

	if err := os.WriteFile(filename, []byte(fileContent), 0644); err != nil {
		return err
	}

	return GitCommit(PageDir, "Add key comment "+id, author)
}

// DeleteComment marks a comment as deleted (Soft Delete)
func DeleteComment(id, author string) error {
	filename := filepath.Join(PageDir, CommentDir, id+".md")

	// Read existing to keep parent
	contentBytes, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	content := string(contentBytes)
	parentID, _, _ := parseCommentFile(content)

	newContent := fmt.Sprintf("Parent: %s\nStatus: deleted\n\n[deleted]", parentID)

	if err := os.WriteFile(filename, []byte(newContent), 0644); err != nil {
		return err
	}
	return GitCommit(PageDir, "Delete comment "+id, author)
}

// UpdateComment updates an existing comment
func UpdateComment(id, content, author string) error {
	filename := filepath.Join(PageDir, CommentDir, id+".md")

	// Read existing to keep parent & status
	contentBytes, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	fileContent := string(contentBytes)
	parentID, status, _ := parseCommentFile(fileContent)

	// Reconstruct content
	newFileContent := fmt.Sprintf("Parent: %s", parentID)
	if status != "" {
		newFileContent += fmt.Sprintf("\nStatus: %s", status)
	}
	newFileContent += fmt.Sprintf("\n\n%s", content)

	if err := os.WriteFile(filename, []byte(newFileContent), 0644); err != nil {
		return err
	}

	return GitCommit(PageDir, "Edit comment "+id, author)
}
