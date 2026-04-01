// Tag-related database operations.
package db

import (
	"database/sql"
	"fmt"
	"makhor/pkg/models"
)

// CreateTag creates a new tag with the given creator.
func (d *DB) CreateTag(name, description string, creatorID int64) (*models.Tag, error) {
	result, err := d.Exec(
		`INSERT INTO tags (name, description, creator_id) VALUES (?, ?, ?)`,
		name, description, creatorID,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting tag: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting tag id: %w", err)
	}

	return d.GetTagByID(id)
}

// GetTagByID retrieves a tag by its ID with ownership info.
func (d *DB) GetTagByID(id int64) (*models.Tag, error) {
	tag := &models.Tag{}
	var creatorID sql.NullInt64
	var creatorUsername sql.NullString
	var createdAt sql.NullTime

	err := d.QueryRow(`
		SELECT t.id, t.name, t.description, t.creator_id, t.created_at, t.is_active,
		       u.username
		FROM tags t
		LEFT JOIN users u ON t.creator_id = u.id
		WHERE t.id = ?
	`, id).Scan(&tag.ID, &tag.Name, &tag.Description, &creatorID, &createdAt, &tag.IsActive, &creatorUsername)
	if err != nil {
		return nil, err
	}

	if creatorID.Valid {
		tag.CreatorID = &creatorID.Int64
	}
	if creatorUsername.Valid {
		tag.CreatorUsername = creatorUsername.String
	}
	if createdAt.Valid {
		tag.CreatedAt = createdAt.Time
	}

	// Get admins
	tag.Admins, _ = d.GetTagAdmins(id)

	return tag, nil
}

// GetTagByNameWithOwnership retrieves a tag by name with creator and admin info.
func (d *DB) GetTagByNameWithOwnership(name string) (*models.Tag, error) {
	tag := &models.Tag{}
	var creatorID sql.NullInt64
	var creatorUsername sql.NullString
	var createdAt sql.NullTime

	err := d.QueryRow(`
		SELECT t.id, t.name, t.description, t.creator_id, t.created_at, t.is_active,
		       u.username
		FROM tags t
		LEFT JOIN users u ON t.creator_id = u.id
		WHERE t.name = ? AND t.is_active = TRUE
	`, name).Scan(&tag.ID, &tag.Name, &tag.Description, &creatorID, &createdAt, &tag.IsActive, &creatorUsername)
	if err != nil {
		return nil, err
	}

	if creatorID.Valid {
		tag.CreatorID = &creatorID.Int64
	}
	if creatorUsername.Valid {
		tag.CreatorUsername = creatorUsername.String
	}
	if createdAt.Valid {
		tag.CreatedAt = createdAt.Time
	}

	// Get admins
	tag.Admins, _ = d.GetTagAdmins(tag.ID)

	return tag, nil
}

// GetTagAdmins retrieves all admins for a tag.
func (d *DB) GetTagAdmins(tagID int64) ([]models.TagAdmin, error) {
	rows, err := d.Query(`
		SELECT ta.id, ta.tag_id, ta.user_id, ta.granted_by, ta.granted_at,
		       u.username, COALESCE(g.username, '') as granted_by_name
		FROM tag_admins ta
		JOIN users u ON ta.user_id = u.id
		LEFT JOIN users g ON ta.granted_by = g.id
		WHERE ta.tag_id = ?
		ORDER BY ta.granted_at ASC
	`, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var admins []models.TagAdmin
	for rows.Next() {
		var admin models.TagAdmin
		var grantedBy sql.NullInt64

		err := rows.Scan(&admin.ID, &admin.TagID, &admin.UserID, &grantedBy, &admin.GrantedAt,
			&admin.Username, &admin.GrantedByName)
		if err != nil {
			return nil, err
		}

		if grantedBy.Valid {
			admin.GrantedBy = &grantedBy.Int64
		}

		admins = append(admins, admin)
	}

	return admins, nil
}

// AddTagAdmin adds a user as an admin for a tag.
func (d *DB) AddTagAdmin(tagID, userID, grantedBy int64) error {
	_, err := d.Exec(
		`INSERT OR IGNORE INTO tag_admins (tag_id, user_id, granted_by) VALUES (?, ?, ?)`,
		tagID, userID, grantedBy,
	)
	return err
}

// RemoveTagAdmin removes a user as an admin for a tag.
func (d *DB) RemoveTagAdmin(tagID, userID int64) error {
	_, err := d.Exec(`DELETE FROM tag_admins WHERE tag_id = ? AND user_id = ?`, tagID, userID)
	return err
}

// IsTagCreator checks if a user is the creator of a tag.
func (d *DB) IsTagCreator(tagID, userID int64) bool {
	var count int
	d.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ? AND creator_id = ?`, tagID, userID).Scan(&count)
	return count > 0
}

// IsTagAdmin checks if a user is an admin for a tag.
func (d *DB) IsTagAdmin(tagID, userID int64) bool {
	var count int
	d.QueryRow(`SELECT COUNT(*) FROM tag_admins WHERE tag_id = ? AND user_id = ?`, tagID, userID).Scan(&count)
	return count > 0
}

// CanModerateTag checks if a user can moderate a tag (is creator, admin, or site admin).
func (d *DB) CanModerateTag(tagID, userID int64) bool {
	// Check if site admin
	var isAdmin bool
	d.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&isAdmin)
	if isAdmin {
		return true
	}

	// Check if tag creator
	if d.IsTagCreator(tagID, userID) {
		return true
	}

	// Check if tag admin
	return d.IsTagAdmin(tagID, userID)
}

// CanManageTagAdmins checks if a user can add/remove admins for a tag (only creator or site admin).
func (d *DB) CanManageTagAdmins(tagID, userID int64) bool {
	// Check if site admin
	var isAdmin bool
	d.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&isAdmin)
	if isAdmin {
		return true
	}

	// Only creator can manage admins
	return d.IsTagCreator(tagID, userID)
}

// TagExists checks if a tag with the given name exists.
func (d *DB) TagExists(name string) bool {
	var count int
	d.QueryRow(`SELECT COUNT(*) FROM tags WHERE name = ?`, name).Scan(&count)
	return count > 0
}

// GetParentTagName returns the parent tag name for a hierarchical tag.
// e.g., "programming::languages::go" -> "programming::languages"
func GetParentTagName(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == ':' && i > 0 && name[i-1] == ':' {
			return name[:i-1]
		}
	}
	return ""
}

// SeedTagCreators sets the creator_id for all tags that don't have one.
// Uses the root user (first admin user) as the creator.
func (d *DB) SeedTagCreators() error {
	// Find the first admin user as root
	var rootUserID int64
	err := d.QueryRow(`SELECT id FROM users WHERE is_admin = TRUE ORDER BY id ASC LIMIT 1`).Scan(&rootUserID)
	if err != nil {
		// No admin user found, skip seeding
		return nil
	}

	// Update all tags without a creator
	_, err = d.Exec(`UPDATE tags SET creator_id = ? WHERE creator_id IS NULL`, rootUserID)
	return err
}

// RemoveTagFromPost removes a tag from a post.
func (d *DB) RemoveTagFromPost(postID, tagID int64) error {
	_, err := d.Exec(`DELETE FROM post_tags WHERE post_id = ? AND tag_id = ?`, postID, tagID)
	return err
}

// UpdateTagDescription updates a tag's description.
func (d *DB) UpdateTagDescription(tagID int64, description string) error {
	_, err := d.Exec(`UPDATE tags SET description = ? WHERE id = ?`, description, tagID)
	return err
}

// GetPostTagIDs returns the tag IDs for a post.
func (d *DB) GetPostTagIDs(postID int64) ([]int64, error) {
	rows, err := d.Query(`SELECT tag_id FROM post_tags WHERE post_id = ?`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tagIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		tagIDs = append(tagIDs, id)
	}
	return tagIDs, nil
}

// CanCreateSubtag checks if a user can create a subtag under a parent tag.
// Returns true if user is site admin, or if user can moderate the parent tag
// (i.e., is creator or admin of the parent tag, recursively up the hierarchy).
func (d *DB) CanCreateSubtag(parentName string, userID int64) bool {
	// Site admins can create any subtag
	var isAdmin bool
	d.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&isAdmin)
	if isAdmin {
		return true
	}

	// Check if user can moderate the parent tag
	parentTag, err := d.GetTagByName(parentName)
	if err != nil {
		return false
	}

	// Check if user is creator or admin of the parent tag
	if d.IsTagCreator(parentTag.ID, userID) || d.IsTagAdmin(parentTag.ID, userID) {
		return true
	}

	// Recursively check parent tags (user with access to "programming" can create "programming::web::new")
	grandparent := GetParentTagName(parentName)
	if grandparent != "" {
		return d.CanCreateSubtag(grandparent, userID)
	}

	return false
}

// DeleteTag marks a tag as inactive and removes it from all posts.
// Only the tag creator or site admin can delete a tag.
func (d *DB) DeleteTag(tagID int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Remove tag from all posts
	_, err = tx.Exec(`DELETE FROM post_tags WHERE tag_id = ?`, tagID)
	if err != nil {
		return fmt.Errorf("removing tag from posts: %w", err)
	}

	// Delete any RSS feeds associated with this tag
	_, err = tx.Exec(`DELETE FROM rss_feeds WHERE tag_id = ?`, tagID)
	if err != nil {
		return fmt.Errorf("removing tag feeds: %w", err)
	}

	// Delete tag admins
	_, err = tx.Exec(`DELETE FROM tag_admins WHERE tag_id = ?`, tagID)
	if err != nil {
		return fmt.Errorf("removing tag admins: %w", err)
	}

	// Mark tag as inactive (soft delete)
	_, err = tx.Exec(`UPDATE tags SET is_active = FALSE WHERE id = ?`, tagID)
	if err != nil {
		return fmt.Errorf("deactivating tag: %w", err)
	}

	return tx.Commit()
}

// CanDeleteTag checks if a user can delete a tag.
// Only the tag creator or site admin can delete a tag.
func (d *DB) CanDeleteTag(tagID, userID int64) bool {
	// Site admins can delete any tag
	var isAdmin bool
	d.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&isAdmin)
	if isAdmin {
		return true
	}

	// Only creator can delete
	return d.IsTagCreator(tagID, userID)
}

// GetTagPostCount returns the number of posts with a given tag.
func (d *DB) GetTagPostCount(tagID int64) int {
	var count int
	d.QueryRow(`SELECT COUNT(*) FROM post_tags WHERE tag_id = ?`, tagID).Scan(&count)
	return count
}

// GetTagChildCount returns the number of child tags for a given tag.
func (d *DB) GetTagChildCount(tagName string) int {
	var count int
	pattern := tagName + "::%"
	d.QueryRow(`SELECT COUNT(*) FROM tags WHERE name LIKE ? AND is_active = TRUE`, pattern).Scan(&count)
	return count
}
