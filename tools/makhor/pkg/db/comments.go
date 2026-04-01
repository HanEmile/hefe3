// Comment-related database operations.
package db

import (
	"database/sql"
	"fmt"
	"makhor/pkg/models"
	"strings"
)

// CreateComment creates a new comment on a post.
func (d *DB) CreateComment(postID, userID int64, parentID, hatID *int64, body string) (*models.Comment, error) {
	tx, err := d.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`INSERT INTO comments (post_id, user_id, parent_id, hat_id, body, score) VALUES (?, ?, ?, ?, ?, 0)`,
		postID, userID, parentID, hatID, body,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting comment: %w", err)
	}

	commentID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting comment id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return d.GetCommentByID(commentID, nil)
}

// GetCommentByID retrieves a comment by ID.
func (d *DB) GetCommentByID(id int64, currentUserID *int64) (*models.Comment, error) {
	comment := &models.Comment{}
	var parentID, hatID sql.NullInt64
	var isBlurred sql.NullBool

	err := d.QueryRow(`
		SELECT c.id, c.post_id, c.user_id, c.parent_id, c.body, c.created_at, c.updated_at,
		       c.score, c.is_deleted, c.is_blurred, c.hat_id, u.username, p.title
		FROM comments c
		JOIN users u ON c.user_id = u.id
		JOIN posts p ON c.post_id = p.id
		WHERE c.id = ?
	`, id).Scan(
		&comment.ID, &comment.PostID, &comment.UserID, &parentID, &comment.Body,
		&comment.CreatedAt, &comment.UpdatedAt, &comment.Score, &comment.IsDeleted,
		&isBlurred, &hatID, &comment.Username, &comment.PostTitle,
	)

	if err == sql.ErrNoRows {
		return nil, ErrCommentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying comment: %w", err)
	}

	if parentID.Valid {
		comment.ParentID = &parentID.Int64
	}
	if hatID.Valid {
		comment.HatID = &hatID.Int64
		comment.Hat, _ = d.GetHatByID(hatID.Int64)
	}
	if isBlurred.Valid {
		comment.IsBlurred = isBlurred.Bool
	}
	comment.BodyLen = len(comment.Body)

	// Check if current user voted
	if currentUserID != nil {
		var count int
		d.QueryRow(
			`SELECT COUNT(*) FROM votes WHERE user_id = ? AND comment_id = ?`,
			*currentUserID, id,
		).Scan(&count)
		comment.UserVoted = count > 0
	}

	return comment, nil
}

// GetUserVotedComments batch checks which comments a user has voted on.
// Returns a map of commentID -> voted. This eliminates N+1 queries.
func (d *DB) GetUserVotedComments(userID int64, commentIDs []int64) (map[int64]bool, error) {
	if len(commentIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(commentIDs))
	args := make([]interface{}, len(commentIDs)+1)
	args[0] = userID
	for i, id := range commentIDs {
		placeholders[i] = "?"
		args[i+1] = id
	}

	query := fmt.Sprintf(`
		SELECT comment_id FROM votes
		WHERE user_id = ? AND comment_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]bool)
	for rows.Next() {
		var commentID int64
		if err := rows.Scan(&commentID); err != nil {
			return nil, err
		}
		result[commentID] = true
	}

	return result, nil
}

// GetHatsForComments batch loads hats for multiple comments.
// Returns a map of hatID -> hat. This eliminates N+1 queries.
func (d *DB) GetHatsForComments(hatIDs []int64) (map[int64]*models.Hat, error) {
	if len(hatIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(hatIDs))
	args := make([]interface{}, len(hatIDs))
	for i, id := range hatIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT h.id, h.user_id, h.name, h.organization, h.link, h.granted_by, h.granted_at, h.is_active, u.username
		FROM hats h
		JOIN users u ON h.user_id = u.id
		WHERE h.id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]*models.Hat)
	for rows.Next() {
		hat := &models.Hat{}
		var grantedBy sql.NullInt64
		if err := rows.Scan(&hat.ID, &hat.UserID, &hat.Name, &hat.Organization, &hat.Link,
			&grantedBy, &hat.GrantedAt, &hat.IsActive, &hat.Username); err != nil {
			return nil, err
		}
		if grantedBy.Valid {
			hat.GrantedBy = &grantedBy.Int64
		}
		result[hat.ID] = hat
	}

	return result, nil
}

// GetPostComments retrieves all comments for a post in threaded order.
// Blurred comments are moved to the end.
func (d *DB) GetPostComments(postID int64, currentUserID *int64) ([]*models.Comment, error) {
	rows, err := d.Query(`
		SELECT c.id, c.post_id, c.user_id, c.parent_id, c.body, c.created_at, c.updated_at,
		       c.score, c.is_deleted, c.is_blurred, c.hat_id, u.username
		FROM comments c
		JOIN users u ON c.user_id = u.id
		WHERE c.post_id = ?
		ORDER BY c.created_at ASC
	`, postID)
	if err != nil {
		return nil, fmt.Errorf("querying comments: %w", err)
	}
	defer rows.Close()

	// Build a map of all comments
	commentMap := make(map[int64]*models.Comment)
	var allComments []*models.Comment
	var commentIDs []int64
	var hatIDs []int64

	for rows.Next() {
		comment := &models.Comment{}
		var parentID, hatID sql.NullInt64
		var isBlurred sql.NullBool

		err := rows.Scan(
			&comment.ID, &comment.PostID, &comment.UserID, &parentID, &comment.Body,
			&comment.CreatedAt, &comment.UpdatedAt, &comment.Score, &comment.IsDeleted,
			&isBlurred, &hatID, &comment.Username,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning comment: %w", err)
		}

		if parentID.Valid {
			comment.ParentID = &parentID.Int64
		}
		if hatID.Valid {
			comment.HatID = &hatID.Int64
			hatIDs = append(hatIDs, hatID.Int64)
		}
		if isBlurred.Valid {
			comment.IsBlurred = isBlurred.Bool
		}
		comment.BodyLen = len(comment.Body)

		commentMap[comment.ID] = comment
		allComments = append(allComments, comment)
		commentIDs = append(commentIDs, comment.ID)
	}

	// Batch load hats (fixes N+1)
	if len(hatIDs) > 0 {
		hats, _ := d.GetHatsForComments(hatIDs)
		for _, comment := range allComments {
			if comment.HatID != nil {
				comment.Hat = hats[*comment.HatID]
			}
		}
	}

	// Batch load votes for current user (fixes N+1)
	if currentUserID != nil && len(commentIDs) > 0 {
		votedComments, _ := d.GetUserVotedComments(*currentUserID, commentIDs)
		for _, comment := range allComments {
			comment.UserVoted = votedComments[comment.ID]
		}
	}

	// Build tree structure, separating blurred top-level comments
	var rootComments []*models.Comment
	var blurredRootComments []*models.Comment

	for _, comment := range allComments {
		if comment.ParentID == nil {
			comment.Depth = 0
			if comment.IsBlurred {
				blurredRootComments = append(blurredRootComments, comment)
			} else {
				rootComments = append(rootComments, comment)
			}
		} else {
			if parent, ok := commentMap[*comment.ParentID]; ok {
				comment.Depth = parent.Depth + 1
				parent.Children = append(parent.Children, comment)
			}
		}
	}

	// Flatten tree for display (depth-first), blurred at end
	result := flattenComments(rootComments)
	result = append(result, flattenComments(blurredRootComments)...)
	return result, nil
}

// flattenComments converts a tree of comments to a flat slice with depth info.
func flattenComments(comments []*models.Comment) []*models.Comment {
	var result []*models.Comment
	for _, comment := range comments {
		result = append(result, comment)
		if len(comment.Children) > 0 {
			result = append(result, flattenComments(comment.Children)...)
		}
	}
	return result
}

// GetUserComments retrieves all comments by a user.
func (d *DB) GetUserComments(userID int64, page, perPage int) ([]*models.Comment, int, error) {
	var total int
	err := d.QueryRow(
		`SELECT COUNT(*) FROM comments WHERE user_id = ? AND is_deleted = FALSE`,
		userID,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting user comments: %w", err)
	}

	rows, err := d.Query(`
		SELECT c.id, c.post_id, c.user_id, c.parent_id, c.body, c.created_at, c.updated_at,
		       c.score, c.is_deleted, c.hat_id, u.username, p.title
		FROM comments c
		JOIN users u ON c.user_id = u.id
		JOIN posts p ON c.post_id = p.id
		WHERE c.user_id = ? AND c.is_deleted = FALSE
		ORDER BY c.created_at DESC
		LIMIT ? OFFSET ?
	`, userID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("querying user comments: %w", err)
	}
	defer rows.Close()

	var comments []*models.Comment
	for rows.Next() {
		comment := &models.Comment{}
		var parentID, hatID sql.NullInt64

		err := rows.Scan(
			&comment.ID, &comment.PostID, &comment.UserID, &parentID, &comment.Body,
			&comment.CreatedAt, &comment.UpdatedAt, &comment.Score, &comment.IsDeleted,
			&hatID, &comment.Username, &comment.PostTitle,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning comment: %w", err)
		}

		if parentID.Valid {
			comment.ParentID = &parentID.Int64
		}
		if hatID.Valid {
			comment.HatID = &hatID.Int64
			comment.Hat, _ = d.GetHatByID(hatID.Int64)
		}

		comments = append(comments, comment)
	}

	return comments, total, nil
}

// UpdateComment updates an existing comment.
func (d *DB) UpdateComment(id int64, body string) error {
	_, err := d.Exec(
		`UPDATE comments SET body = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		body, id,
	)
	return err
}

// DeleteComment soft-deletes a comment.
func (d *DB) DeleteComment(id int64) error {
	_, err := d.Exec(`UPDATE comments SET is_deleted = TRUE WHERE id = ?`, id)
	return err
}

// DeleteCommentTree soft-deletes a comment and all its descendants.
func (d *DB) DeleteCommentTree(id int64) error {
	// First get all descendant IDs using recursive CTE
	rows, err := d.Query(`
		WITH RECURSIVE descendants AS (
			SELECT id FROM comments WHERE id = ?
			UNION ALL
			SELECT c.id FROM comments c
			JOIN descendants d ON c.parent_id = d.id
		)
		SELECT id FROM descendants
	`, id)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var cid int64
		if err := rows.Scan(&cid); err != nil {
			return err
		}
		ids = append(ids, cid)
	}

	// Delete all in one statement
	for _, cid := range ids {
		if _, err := d.Exec(`UPDATE comments SET is_deleted = TRUE WHERE id = ?`, cid); err != nil {
			return err
		}
	}
	return nil
}

// BlurComment marks a comment as blurred (moved to bottom).
func (d *DB) BlurComment(id int64) error {
	_, err := d.Exec(`UPDATE comments SET is_blurred = TRUE WHERE id = ?`, id)
	return err
}

// UnblurComment removes the blurred status from a comment.
func (d *DB) UnblurComment(id int64) error {
	_, err := d.Exec(`UPDATE comments SET is_blurred = FALSE WHERE id = ?`, id)
	return err
}

// GetRecentComments retrieves the most recent comments across all posts.
func (d *DB) GetRecentComments(limit int) ([]*models.Comment, error) {
	rows, err := d.Query(`
		SELECT c.id, c.post_id, c.user_id, c.parent_id, c.body, c.created_at, c.updated_at,
		       c.score, c.is_deleted, c.hat_id, u.username, p.title
		FROM comments c
		JOIN users u ON c.user_id = u.id
		JOIN posts p ON c.post_id = p.id
		WHERE c.is_deleted = FALSE
		ORDER BY c.created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("querying recent comments: %w", err)
	}
	defer rows.Close()

	var comments []*models.Comment
	for rows.Next() {
		comment := &models.Comment{}
		var parentID, hatID sql.NullInt64

		err := rows.Scan(
			&comment.ID, &comment.PostID, &comment.UserID, &parentID, &comment.Body,
			&comment.CreatedAt, &comment.UpdatedAt, &comment.Score, &comment.IsDeleted,
			&hatID, &comment.Username, &comment.PostTitle,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning comment: %w", err)
		}

		if parentID.Valid {
			comment.ParentID = &parentID.Int64
		}
		if hatID.Valid {
			comment.HatID = &hatID.Int64
			comment.Hat, _ = d.GetHatByID(hatID.Int64)
		}

		comments = append(comments, comment)
	}

	return comments, nil
}
