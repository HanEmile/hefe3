// Comment handlers: creating, editing, deleting comments.
package handlers

import (
	"fmt"
	"makhor/pkg/middleware"
	"makhor/pkg/models"
	"net/http"
	"strconv"
	"strings"
)

// canModerateComment checks if a user can moderate a comment (author, admin, or tag moderator).
func (h *Handler) canModerateComment(user *models.User, comment *models.Comment) bool {
	if user == nil {
		return false
	}
	if comment.UserID == user.ID || user.IsAdmin {
		return true
	}
	return h.isCommentModerator(user, comment)
}

// isCommentModerator checks if user has moderator rights on a comment (admin or tag moderator, not author).
func (h *Handler) isCommentModerator(user *models.User, comment *models.Comment) bool {
	if user == nil {
		return false
	}
	if user.IsAdmin {
		return true
	}
	// Check tag moderation permissions
	post, err := h.DB.GetPostByID(comment.PostID, nil)
	if err != nil || post == nil {
		return false
	}
	for _, tag := range post.Tags {
		if h.DB.CanModerateTag(tag.ID, user.ID) {
			return true
		}
	}
	return false
}

// SubmitComment handles comment submission.
func (h *Handler) SubmitComment(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	postID, err := getPathID(r, "/posts/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid post ID")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	body := strings.TrimSpace(r.FormValue("body"))
	parentIDStr := r.FormValue("parent_id")
	hatIDStr := r.FormValue("hat_id")

	if len(body) < 1 {
		h.redirect(w, r, fmt.Sprintf("/posts/%d", postID))
		return
	}

	// Parse optional parent ID (for replies)
	var parentID *int64
	if parentIDStr != "" {
		if pid, err := strconv.ParseInt(parentIDStr, 10, 64); err == nil {
			parentID = &pid
		}
	}

	// Parse and verify optional hat ID
	hatID := h.verifyUserHat(hatIDStr, user.ID)

	comment, err := h.DB.CreateComment(postID, user.ID, parentID, hatID, body)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not create comment")
		return
	}

	h.logAction(r, models.ActionCommentCreate, "comment", comment.ID, truncate(body, 100))

	h.redirect(w, r, fmt.Sprintf("/posts/%d#c%d", postID, comment.ID))
}

// EditCommentPage shows the comment edit form.
func (h *Handler) EditCommentPage(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	commentID, err := getPathID(r, "/comments/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid comment ID")
		return
	}

	comment, err := h.DB.GetCommentByID(commentID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Comment not found")
		return
	}

	if comment.UserID != user.ID && !user.IsAdmin {
		h.renderError(w, r, http.StatusForbidden, "You cannot edit this comment")
		return
	}

	h.render(w, r, "edit_comment.html", map[string]interface{}{
		"Comment": comment,
	})
}

// EditCommentSubmit handles comment edit submission.
func (h *Handler) EditCommentSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	commentID, err := getPathID(r, "/comments/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid comment ID")
		return
	}

	comment, err := h.DB.GetCommentByID(commentID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Comment not found")
		return
	}

	if comment.UserID != user.ID && !user.IsAdmin {
		h.renderError(w, r, http.StatusForbidden, "You cannot edit this comment")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	body := strings.TrimSpace(r.FormValue("body"))

	if len(body) < 1 {
		h.render(w, r, "edit_comment.html", map[string]interface{}{
			"Comment": comment,
			"Error":   "Comment cannot be empty",
		})
		return
	}

	if err := h.DB.UpdateComment(commentID, body); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not update comment")
		return
	}

	h.logAction(r, models.ActionCommentUpdate, "comment", commentID, "")

	h.redirect(w, r, fmt.Sprintf("/posts/%d#c%d", comment.PostID, commentID))
}

// DeleteComment handles comment deletion.
func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	commentID, err := getPathID(r, "/comments/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid comment ID")
		return
	}

	comment, err := h.DB.GetCommentByID(commentID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Comment not found")
		return
	}

	if comment.UserID != user.ID && !user.IsAdmin {
		h.renderError(w, r, http.StatusForbidden, "You cannot delete this comment")
		return
	}

	if err := h.DB.DeleteComment(commentID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not delete comment")
		return
	}

	h.logAction(r, models.ActionCommentDelete, "comment", commentID, "")

	h.redirect(w, r, fmt.Sprintf("/posts/%d", comment.PostID))
}

// VoteComment handles voting on a comment.
func (h *Handler) VoteComment(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	commentID, err := getPathID(r, "/comments/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid comment ID")
		return
	}

	comment, err := h.DB.GetCommentByID(commentID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Comment not found")
		return
	}

	_, voted, err := h.DB.VoteComment(user.ID, commentID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not vote")
		return
	}

	if voted {
		h.logAction(r, models.ActionVoteComment, "comment", commentID, "upvote")
	}

	// Redirect back to post (with open redirect protection)
	if safeURL := safeRedirectURL(r.Header.Get("Referer"), r); safeURL != "" {
		h.redirect(w, r, safeURL)
	} else {
		h.redirect(w, r, fmt.Sprintf("/posts/%d#c%d", comment.PostID, commentID))
	}
}

// RecentCommentsPage shows recent comments across all posts.
func (h *Handler) RecentCommentsPage(w http.ResponseWriter, r *http.Request) {
	comments, err := h.DB.GetRecentComments(50)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load comments")
		return
	}

	h.render(w, r, "recent_comments.html", map[string]interface{}{
		"Comments": comments,
	})
}

// ReplyPage shows a form to reply to a specific comment.
func (h *Handler) ReplyPage(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	commentID, err := getPathID(r, "/comments/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid comment ID")
		return
	}

	comment, err := h.DB.GetCommentByID(commentID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Comment not found")
		return
	}

	post, err := h.DB.GetPostByID(comment.PostID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Post not found")
		return
	}

	hats, _ := h.DB.GetUserHats(user.ID)

	h.render(w, r, "reply.html", map[string]interface{}{
		"Comment": comment,
		"Post":    post,
		"Hats":    hats,
	})
}

// DeleteCommentTree handles deleting a comment and all its replies.
func (h *Handler) DeleteCommentTree(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	commentID, err := getPathID(r, "/comments/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid comment ID")
		return
	}

	comment, err := h.DB.GetCommentByID(commentID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Comment not found")
		return
	}

	if !h.canModerateComment(user, comment) {
		h.renderError(w, r, http.StatusForbidden, "You cannot delete this comment tree")
		return
	}

	if err := h.DB.DeleteCommentTree(commentID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not delete comment tree")
		return
	}

	h.logAction(r, models.ActionCommentTreeDel, "comment", commentID, "")

	h.redirect(w, r, fmt.Sprintf("/posts/%d", comment.PostID))
}

// BlurComment handles blurring a comment (moving to bottom).
func (h *Handler) BlurComment(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	commentID, err := getPathID(r, "/comments/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid comment ID")
		return
	}

	comment, err := h.DB.GetCommentByID(commentID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Comment not found")
		return
	}

	if !h.isCommentModerator(user, comment) {
		h.renderError(w, r, http.StatusForbidden, "You cannot blur this comment")
		return
	}

	if err := h.DB.BlurComment(commentID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not blur comment")
		return
	}

	h.logAction(r, models.ActionCommentBlur, "comment", commentID, "")

	h.redirect(w, r, fmt.Sprintf("/posts/%d#c%d", comment.PostID, commentID))
}

// UnblurComment handles unblurring a comment.
func (h *Handler) UnblurComment(w http.ResponseWriter, r *http.Request) {
	user := h.requireLogin(w, r)
	if user == nil {
		return
	}

	commentID, err := getPathID(r, "/comments/")
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid comment ID")
		return
	}

	comment, err := h.DB.GetCommentByID(commentID, nil)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Comment not found")
		return
	}

	if !h.isCommentModerator(user, comment) {
		h.renderError(w, r, http.StatusForbidden, "You cannot unblur this comment")
		return
	}

	if err := h.DB.UnblurComment(commentID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not unblur comment")
		return
	}

	h.logAction(r, models.ActionCommentUnblur, "comment", commentID, "")

	h.redirect(w, r, fmt.Sprintf("/posts/%d#c%d", comment.PostID, commentID))
}
