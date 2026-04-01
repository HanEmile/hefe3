// Collection database operations.
package db

import (
	"database/sql"
	"fmt"
	"makhor/pkg/models"
	"time"
)

// Collection represents a user's collection of tags.
type Collection struct {
	ID          int64      `json:"id"`
	UserID      int64      `json:"user_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	CreatedAt   time.Time  `json:"created_at"`
	Tags        []models.Tag `json:"tags,omitempty"`
	TagCount    int        `json:"tag_count,omitempty"`
}

// CreateCollection creates a new collection.
func (d *DB) CreateCollection(userID int64, name, description string) (*Collection, error) {
	result, err := d.Exec(
		`INSERT INTO collections (user_id, name, description) VALUES (?, ?, ?)`,
		userID, name, description,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting collection: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting last insert id: %w", err)
	}
	return d.GetCollectionByID(id, userID)
}

// GetCollectionByID retrieves a collection by ID.
func (d *DB) GetCollectionByID(id, userID int64) (*Collection, error) {
	col := &Collection{}
	err := d.QueryRow(`
		SELECT id, user_id, name, description, created_at
		FROM collections
		WHERE id = ? AND user_id = ?
	`, id, userID).Scan(&col.ID, &col.UserID, &col.Name, &col.Description, &col.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrCollectionNotFound
	}
	if err != nil {
		return nil, err
	}

	// Get tags
	col.Tags, _ = d.GetCollectionTags(id)
	col.TagCount = len(col.Tags)
	return col, nil
}

// GetUserCollections retrieves all collections for a user.
func (d *DB) GetUserCollections(userID int64) ([]*Collection, error) {
	rows, err := d.Query(`
		SELECT c.id, c.user_id, c.name, c.description, c.created_at,
		       (SELECT COUNT(*) FROM collection_tags WHERE collection_id = c.id) as tag_count
		FROM collections c
		WHERE c.user_id = ?
		ORDER BY c.name ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var collections []*Collection
	for rows.Next() {
		col := &Collection{}
		err := rows.Scan(&col.ID, &col.UserID, &col.Name, &col.Description, &col.CreatedAt, &col.TagCount)
		if err != nil {
			return nil, err
		}
		collections = append(collections, col)
	}

	return collections, nil
}

// UpdateCollection updates a collection's name and description.
func (d *DB) UpdateCollection(id, userID int64, name, description string) error {
	_, err := d.Exec(
		`UPDATE collections SET name = ?, description = ? WHERE id = ? AND user_id = ?`,
		name, description, id, userID,
	)
	return err
}

// DeleteCollection deletes a collection.
func (d *DB) DeleteCollection(id, userID int64) error {
	_, err := d.Exec(
		`DELETE FROM collections WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	return err
}

// AddTagToCollection adds a tag to a collection.
func (d *DB) AddTagToCollection(collectionID, tagID int64) error {
	_, err := d.Exec(
		`INSERT OR IGNORE INTO collection_tags (collection_id, tag_id) VALUES (?, ?)`,
		collectionID, tagID,
	)
	return err
}

// RemoveTagFromCollection removes a tag from a collection.
func (d *DB) RemoveTagFromCollection(collectionID, tagID int64) error {
	_, err := d.Exec(
		`DELETE FROM collection_tags WHERE collection_id = ? AND tag_id = ?`,
		collectionID, tagID,
	)
	return err
}

// GetCollectionTags retrieves all tags in a collection.
func (d *DB) GetCollectionTags(collectionID int64) ([]models.Tag, error) {
	rows, err := d.Query(`
		SELECT t.id, t.name, t.description
		FROM tags t
		JOIN collection_tags ct ON t.id = ct.tag_id
		WHERE ct.collection_id = ?
		ORDER BY t.name ASC
	`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		tag := models.Tag{}
		err := rows.Scan(&tag.ID, &tag.Name, &tag.Description)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}

	return tags, nil
}

// GetCollectionTagNames returns just the tag names for a collection.
func (d *DB) GetCollectionTagNames(collectionID int64) ([]string, error) {
	rows, err := d.Query(`
		SELECT t.name
		FROM tags t
		JOIN collection_tags ct ON t.id = ct.tag_id
		WHERE ct.collection_id = ?
		ORDER BY t.name ASC
	`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}

	return names, nil
}

// GetPostsForCollection retrieves posts that have any of the tags in a collection.
func (d *DB) GetPostsForCollection(collectionID int64, page, perPage int, currentUserID *int64) ([]*models.Post, int, error) {
	// Get tag IDs in collection
	rows, err := d.Query(`SELECT tag_id FROM collection_tags WHERE collection_id = ?`, collectionID)
	if err != nil {
		return nil, 0, err
	}

	var tagIDs []int64
	for rows.Next() {
		var tagID int64
		if err := rows.Scan(&tagID); err != nil {
			rows.Close()
			return nil, 0, err
		}
		tagIDs = append(tagIDs, tagID)
	}
	rows.Close()

	if len(tagIDs) == 0 {
		return nil, 0, nil
	}

	// Build tag ID list for IN clause
	tagMap := make(map[int64]bool)
	for _, id := range tagIDs {
		tagMap[id] = true
	}

	// Get total count
	var total int
	err = d.QueryRow(`
		SELECT COUNT(DISTINCT p.id)
		FROM posts p
		JOIN post_tags pt ON p.id = pt.post_id
		WHERE p.is_deleted = FALSE AND pt.tag_id IN (
			SELECT tag_id FROM collection_tags WHERE collection_id = ?
		)
	`, collectionID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Get posts
	offset := (page - 1) * perPage
	postRows, err := d.Query(`
		SELECT DISTINCT p.id, p.user_id, p.title, p.url, p.body, p.created_at, p.updated_at,
		       p.score, p.is_deleted, u.username,
		       (SELECT COUNT(*) FROM comments WHERE post_id = p.id AND is_deleted = FALSE) as comment_count,
		       COALESCE(p.source_type, 'user') as source_type, p.source_id,
		       COALESCE(rf.title, rf.url) as source_name
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN rss_feeds rf ON p.source_type = 'rss' AND p.source_id = rf.id
		JOIN post_tags pt ON p.id = pt.post_id
		WHERE p.is_deleted = FALSE AND pt.tag_id IN (
			SELECT tag_id FROM collection_tags WHERE collection_id = ?
		)
		ORDER BY p.created_at DESC
		LIMIT ? OFFSET ?
	`, collectionID, perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer postRows.Close()

	return d.scanPostsWithSource(postRows, currentUserID, total)
}
