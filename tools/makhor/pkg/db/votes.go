// Vote-related database operations.
package db

import (
	"database/sql"
	"fmt"
)

// voteTarget represents either a post or comment for voting.
type voteTarget struct {
	idColumn    string // "post_id" or "comment_id"
	scoreTable  string // "posts" or "comments"
	entityName  string // "post" or "comment" for error messages
}

var (
	postTarget = voteTarget{
		idColumn:   "post_id",
		scoreTable: "posts",
		entityName: "post",
	}
	commentTarget = voteTarget{
		idColumn:   "comment_id",
		scoreTable: "comments",
		entityName: "comment",
	}
)

// toggleVote adds or removes a vote on a target (post or comment).
// Returns the new score and whether the user is now voted.
func (d *DB) toggleVote(userID, targetID int64, target voteTarget) (int, bool, error) {
	tx, err := d.Begin()
	if err != nil {
		return 0, false, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Check if already voted
	var existingID int64
	err = tx.QueryRow(
		fmt.Sprintf(`SELECT id FROM votes WHERE user_id = ? AND %s = ?`, target.idColumn),
		userID, targetID,
	).Scan(&existingID)

	var newScore int
	var voted bool

	if err == nil {
		// Already voted - remove vote
		_, err = tx.Exec(`DELETE FROM votes WHERE id = ?`, existingID)
		if err != nil {
			return 0, false, fmt.Errorf("removing vote: %w", err)
		}

		// Update score and get new value atomically
		err = tx.QueryRow(
			fmt.Sprintf(`UPDATE %s SET score = score - 1 WHERE id = ? RETURNING score`, target.scoreTable),
			targetID,
		).Scan(&newScore)
		if err != nil {
			return 0, false, fmt.Errorf("updating %s score: %w", target.entityName, err)
		}
		voted = false
	} else if err == sql.ErrNoRows {
		// Add new vote
		_, err = tx.Exec(
			fmt.Sprintf(`INSERT INTO votes (user_id, %s, value) VALUES (?, ?, 1)`, target.idColumn),
			userID, targetID,
		)
		if err != nil {
			return 0, false, fmt.Errorf("inserting vote: %w", err)
		}

		// Update score and get new value atomically
		err = tx.QueryRow(
			fmt.Sprintf(`UPDATE %s SET score = score + 1 WHERE id = ? RETURNING score`, target.scoreTable),
			targetID,
		).Scan(&newScore)
		if err != nil {
			return 0, false, fmt.Errorf("updating %s score: %w", target.entityName, err)
		}
		voted = true
	} else {
		return 0, false, fmt.Errorf("checking existing vote: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, false, fmt.Errorf("committing transaction: %w", err)
	}

	return newScore, voted, nil
}

// VotePost adds or removes a vote on a post.
// Returns the new score and whether the user is now voted.
func (d *DB) VotePost(userID, postID int64) (int, bool, error) {
	return d.toggleVote(userID, postID, postTarget)
}

// VoteComment adds or removes a vote on a comment.
// Returns the new score and whether the user is now voted.
func (d *DB) VoteComment(userID, commentID int64) (int, bool, error) {
	return d.toggleVote(userID, commentID, commentTarget)
}

// hasVoted checks if a user has voted on a target.
func (d *DB) hasVoted(userID, targetID int64, target voteTarget) bool {
	var count int
	err := d.QueryRow(
		fmt.Sprintf(`SELECT COUNT(*) FROM votes WHERE user_id = ? AND %s = ?`, target.idColumn),
		userID, targetID,
	).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// HasVotedPost checks if a user has voted on a post.
func (d *DB) HasVotedPost(userID, postID int64) bool {
	return d.hasVoted(userID, postID, postTarget)
}

// HasVotedComment checks if a user has voted on a comment.
func (d *DB) HasVotedComment(userID, commentID int64) bool {
	return d.hasVoted(userID, commentID, commentTarget)
}
