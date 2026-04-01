// Admin handlers: moderation, logs, hat management.
package handlers

import (
	"makhor/pkg/db"
	"makhor/pkg/middleware"
	"makhor/pkg/models"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// AdminLogPage shows the action log (admin only).
func (h *Handler) AdminLogPage(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) == nil {
		return
	}

	page := getIntParam(r, "page", 1)
	perPage := AdminLogsPerPage

	// Get filter parameters
	category := r.URL.Query().Get("category")
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	targetType := r.URL.Query().Get("target_type")
	action := r.URL.Query().Get("action")

	filter := db.ActionLogFilter{
		Category:   category,
		Username:   username,
		TargetType: targetType,
		Action:     action,
	}

	logs, total, err := h.DB.GetActionLogFiltered(page, perPage, filter)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load action log")
		return
	}

	pagination := models.NewPagination(page, perPage, total)

	h.render(w, r, "admin_log.html", map[string]interface{}{
		"Logs":       logs,
		"Pagination": pagination,
		"Category":   category,
		"Username":   username,
		"TargetType": targetType,
		"Action":     action,
	})
}

// ModerationLogPage shows the public moderation log.
func (h *Handler) ModerationLogPage(w http.ResponseWriter, r *http.Request) {
	page := getIntParam(r, "page", 1)
	perPage := AdminLogsPerPage

	logs, pagination, err := h.DB.GetModerationLog(page, perPage)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load moderation log")
		return
	}

	h.render(w, r, "modlog.html", map[string]interface{}{
		"Logs":       logs,
		"Pagination": pagination,
	})
}

// AdminHatsPage shows all hats.
func (h *Handler) AdminHatsPage(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) == nil {
		return
	}

	hats, err := h.DB.GetAllHats()
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load hats")
		return
	}

	h.render(w, r, "admin_hats.html", map[string]interface{}{
		"Hats": hats,
	})
}

// AdminGrantHatPage shows the hat grant form.
func (h *Handler) AdminGrantHatPage(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) == nil {
		return
	}

	h.render(w, r, "admin_grant_hat.html", nil)
}

// AdminGrantHatSubmit handles granting a hat.
func (h *Handler) AdminGrantHatSubmit(w http.ResponseWriter, r *http.Request) {
	user := h.requireAdmin(w, r)
	if user == nil {
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	name := strings.TrimSpace(r.FormValue("name"))
	organization := strings.TrimSpace(r.FormValue("organization"))
	link := strings.TrimSpace(r.FormValue("link"))

	// Validation
	if username == "" || name == "" || organization == "" {
		h.render(w, r, "admin_grant_hat.html", map[string]interface{}{
			"Error":        "Username, name, and organization are required",
			"Username":     username,
			"Name":         name,
			"Organization": organization,
			"Link":         link,
		})
		return
	}

	targetUser, err := h.DB.GetUserByUsername(username)
	if err != nil {
		h.render(w, r, "admin_grant_hat.html", map[string]interface{}{
			"Error":        "User not found",
			"Username":     username,
			"Name":         name,
			"Organization": organization,
			"Link":         link,
		})
		return
	}

	hat, err := h.DB.CreateHat(targetUser.ID, name, organization, link, &user.ID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not grant hat")
		return
	}

	h.logAction(r, models.ActionHatGrant, "hat", hat.ID, username)
	h.DB.LogModeration(user.ID, "grant_hat", &targetUser.ID, nil, nil, name+" @ "+organization)

	h.redirect(w, r, "/admin/hats")
}

// AdminRevokeHat handles revoking a hat.
func (h *Handler) AdminRevokeHat(w http.ResponseWriter, r *http.Request) {
	user := h.requireAdmin(w, r)
	if user == nil {
		return
	}

	hatIDStr := r.URL.Query().Get("id")
	hatID, err := strconv.ParseInt(hatIDStr, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid hat ID")
		return
	}

	if err := h.DB.RevokeHat(hatID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not revoke hat")
		return
	}

	h.logAction(r, models.ActionHatRevoke, "hat", hatID, "")
	h.DB.LogModeration(user.ID, "revoke_hat", nil, nil, nil, "")

	h.redirect(w, r, "/admin/hats")
}

// AdminEditHatPage shows the hat edit form.
func (h *Handler) AdminEditHatPage(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) == nil {
		return
	}

	hatIDStr := r.URL.Query().Get("id")
	hatID, err := strconv.ParseInt(hatIDStr, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid hat ID")
		return
	}

	hat, err := h.DB.GetHatByID(hatID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Hat not found")
		return
	}

	// Get the username for display
	hatUser, err := h.DB.GetUserByID(hat.UserID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load user")
		return
	}

	h.render(w, r, "admin_edit_hat.html", map[string]interface{}{
		"Hat":      hat,
		"Username": hatUser.Username,
	})
}

// AdminEditHatSubmit handles hat update submission.
func (h *Handler) AdminEditHatSubmit(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) == nil {
		return
	}

	hatIDStr := r.URL.Query().Get("id")
	hatID, err := strconv.ParseInt(hatIDStr, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid hat ID")
		return
	}

	hat, err := h.DB.GetHatByID(hatID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Hat not found")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	organization := strings.TrimSpace(r.FormValue("organization"))
	link := strings.TrimSpace(r.FormValue("link"))

	// Validation
	if name == "" || organization == "" {
		hatUser, _ := h.DB.GetUserByID(hat.UserID)
		username := ""
		if hatUser != nil {
			username = hatUser.Username
		}
		h.render(w, r, "admin_edit_hat.html", map[string]interface{}{
			"Error":        "Name and organization are required",
			"Hat":          hat,
			"Username":     username,
			"Name":         name,
			"Organization": organization,
			"Link":         link,
		})
		return
	}

	if err := h.DB.UpdateHat(hatID, name, organization, link); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not update hat")
		return
	}

	h.logAction(r, models.ActionHatGrant, "hat", hatID, "updated")

	h.redirect(w, r, "/admin/hats")
}

// AdminReactivateHat handles reactivating a revoked hat.
func (h *Handler) AdminReactivateHat(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) == nil {
		return
	}

	hatIDStr := r.URL.Query().Get("id")
	hatID, err := strconv.ParseInt(hatIDStr, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid hat ID")
		return
	}

	if err := h.DB.ReactivateHat(hatID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not reactivate hat")
		return
	}

	h.logAction(r, models.ActionHatGrant, "hat", hatID, "reactivated")

	h.redirect(w, r, "/admin/hats")
}

// AdminBanUserPage shows the ban form (GET).
// Admins can ban anyone. Inviters can ban users they invited.
func (h *Handler) AdminBanUserPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.renderError(w, r, http.StatusForbidden, "Forbidden")
		return
	}

	username := r.URL.Query().Get("username")
	if username == "" {
		h.renderError(w, r, http.StatusBadRequest, "Missing username")
		return
	}

	targetUser, err := h.DB.GetUserByUsername(username)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "User not found")
		return
	}

	// Prevent self-ban
	if targetUser.ID == user.ID {
		h.renderError(w, r, http.StatusBadRequest, "Cannot ban yourself")
		return
	}

	// Check if user can ban: must be admin OR inviter of this user
	canBan := user.IsAdmin || h.DB.IsInvitedBy(targetUser.ID, user.ID)
	if !canBan {
		h.renderError(w, r, http.StatusForbidden, "Forbidden")
		return
	}

	h.render(w, r, "admin_ban.html", map[string]interface{}{
		"TargetUser": targetUser,
	})
}

// AdminBanUserSubmit handles banning a user (POST).
// Admins can ban anyone. Inviters can ban users they invited.
func (h *Handler) AdminBanUserSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.renderError(w, r, http.StatusForbidden, "Forbidden")
		return
	}

	username := r.URL.Query().Get("username")
	if username == "" {
		h.renderError(w, r, http.StatusBadRequest, "Missing username")
		return
	}

	targetUser, err := h.DB.GetUserByUsername(username)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "User not found")
		return
	}

	// Prevent self-ban
	if targetUser.ID == user.ID {
		h.renderError(w, r, http.StatusBadRequest, "Cannot ban yourself")
		return
	}

	// Check if user can ban: must be admin OR inviter of this user
	canBan := user.IsAdmin || h.DB.IsInvitedBy(targetUser.ID, user.ID)
	if !canBan {
		h.renderError(w, r, http.StatusForbidden, "Forbidden")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	reason := strings.TrimSpace(r.FormValue("reason"))
	durationStr := strings.TrimSpace(r.FormValue("duration"))

	// Parse duration - empty or "permanent" means no expiration
	var expiresAt *time.Time
	if durationStr != "" && durationStr != "permanent" {
		days, err := strconv.Atoi(durationStr)
		if err != nil || days < 1 {
			h.render(w, r, "admin_ban.html", map[string]interface{}{
				"TargetUser": targetUser,
				"Error":      "Invalid duration - must be a positive number of days or empty for permanent",
				"Reason":     reason,
				"Duration":   durationStr,
			})
			return
		}
		t := time.Now().AddDate(0, 0, days)
		expiresAt = &t
	}

	if err := h.DB.BanUser(targetUser.ID, reason, expiresAt, user.ID); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not ban user")
		return
	}

	details := reason
	if expiresAt != nil {
		details += " (expires: " + expiresAt.Format("2006-01-02") + ")"
	}
	h.logAction(r, models.ActionModBan, "user", targetUser.ID, username)
	h.DB.LogModeration(user.ID, "ban", &targetUser.ID, nil, nil, details)

	h.redirect(w, r, "/users/"+username)
}

// AdminUnbanUser handles unbanning a user.
func (h *Handler) AdminUnbanUser(w http.ResponseWriter, r *http.Request) {
	user := h.requireAdmin(w, r)
	if user == nil {
		return
	}

	username := r.URL.Query().Get("username")
	if username == "" {
		h.renderError(w, r, http.StatusBadRequest, "Missing username")
		return
	}

	targetUser, err := h.DB.GetUserByUsername(username)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "User not found")
		return
	}

	if err := h.DB.SetUserBanned(targetUser.ID, false); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not unban user")
		return
	}

	h.logAction(r, models.ActionModUnban, "user", targetUser.ID, username)
	h.DB.LogModeration(user.ID, "unban", &targetUser.ID, nil, nil, "")

	h.redirect(w, r, "/users/"+username)
}
