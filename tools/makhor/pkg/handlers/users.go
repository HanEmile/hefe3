// User profile handlers.
package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"makhor/pkg/imaging"
	"makhor/pkg/middleware"
	"makhor/pkg/models"
)

const maxAvatarSize = 256 * 1024 // 256KB
const avatarSize = 100           // 100x100 pixels

// UserProfilePage shows a user's profile.
func (h *Handler) UserProfilePage(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/users/")
	parts := strings.Split(path, "/")
	username := parts[0]

	if username == "" {
		h.renderError(w, r, http.StatusBadRequest, "Invalid username")
		return
	}

	profileUser, err := h.DB.GetUserByUsername(username)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "User not found")
		return
	}

	// Check if viewing own profile
	currentUser := middleware.GetUser(r)
	isOwnProfile := currentUser != nil && currentUser.ID == profileUser.ID

	// Handle sub-routes for own profile
	if len(parts) > 1 && isOwnProfile {
		switch parts[1] {
		case "settings":
			if r.Method == http.MethodPost {
				h.userSettingsSubmit(w, r, profileUser)
			} else {
				h.userProfileSection(w, r, profileUser, "settings")
			}
			return
		case "avatar":
			if r.Method == http.MethodPost {
				h.userAvatarSubmit(w, r, profileUser)
			} else {
				h.render(w, r, "upload_avatar.html", map[string]interface{}{
					"ProfileUser": profileUser,
				})
			}
			return
		case "invites":
			if r.Method == http.MethodPost {
				h.userCreateInvite(w, r, profileUser)
			} else {
				h.userProfileSection(w, r, profileUser, "invites")
			}
			return
		}
	}

	// Get section from query param
	section := r.URL.Query().Get("section")
	if section == "" {
		section = "posts"
	}

	// Only allow own profile sections
	if !isOwnProfile && (section == "settings" || section == "invites" || section == "hats") {
		section = "posts"
	}

	h.userProfileSection(w, r, profileUser, section)
}

// userProfileSection renders a specific section of the user profile.
func (h *Handler) userProfileSection(w http.ResponseWriter, r *http.Request, profileUser *models.User, section string) {
	currentUser := middleware.GetUser(r)
	isOwnProfile := currentUser != nil && currentUser.ID == profileUser.ID

	data := map[string]interface{}{
		"ProfileUser":  profileUser,
		"IsOwnProfile": isOwnProfile,
		"Section":      section,
		"BaseURL":      h.BaseURL,
	}

	// Get inviter
	inviter, _ := h.DB.GetUserInviter(profileUser.ID)
	data["Inviter"] = inviter

	// Get hats
	hats, _ := h.DB.GetUserHats(profileUser.ID)
	data["Hats"] = hats

	// Check if current user can ban this user (admin or inviter)
	if currentUser != nil && currentUser.ID != profileUser.ID {
		canBan := currentUser.IsAdmin || h.DB.IsInvitedBy(profileUser.ID, currentUser.ID)
		data["CanBan"] = canBan
	}

	// Section-specific data
	switch section {
	case "settings":
		// No extra data needed, profileUser already has About
	case "invites":
		if isOwnProfile {
			invites, _ := h.DB.GetUserInvites(profileUser.ID)
			data["Invites"] = invites
		}
	case "hats":
		// Already have hats
	default: // "posts"
		posts, _, _ := h.DB.GetUserPosts(profileUser.ID, 1, 20)
		data["Posts"] = posts
		comments, _, _ := h.DB.GetUserComments(profileUser.ID, 1, 20)
		data["Comments"] = comments
	}

	h.render(w, r, "user.html", data)
}

// userSettingsSubmit handles settings form submission from profile page.
// It handles both the about field and optional avatar upload.
func (h *Handler) userSettingsSubmit(w http.ResponseWriter, r *http.Request, profileUser *models.User) {
	// Parse multipart form for avatar upload support
	if err := r.ParseMultipartForm(maxAvatarSize); err != nil {
		// Fall back to regular form parsing if no file
		if err := r.ParseForm(); err != nil {
			h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
			return
		}
	}

	var errorMsg string

	// Handle about field
	about := strings.TrimSpace(r.FormValue("about"))
	if len(about) > 1000 {
		about = about[:1000]
	}

	if err := h.DB.UpdateUser(profileUser.ID, about); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not update profile")
		return
	}
	h.logAction(r, models.ActionUserUpdate, "user", profileUser.ID, "about updated")

	// Handle optional avatar upload
	file, header, err := r.FormFile("avatar")
	if err == nil {
		defer file.Close()

		// Check file size
		if header.Size > maxAvatarSize {
			errorMsg = "Avatar file too large (max 256KB)"
		} else {
			// Read file
			data, err := io.ReadAll(io.LimitReader(file, maxAvatarSize+1))
			if err != nil || len(data) > maxAvatarSize {
				errorMsg = "Could not read avatar file"
			} else if len(data) > 0 {
				// Resize image
				resizedData, err := imaging.ResizeToSquare(data, avatarSize)
				if err != nil {
					errorMsg = "Invalid image format (use PNG, JPEG, or GIF)"
				} else {
					// Save avatar
					if err := h.DB.UpdateUserAvatar(profileUser.ID, resizedData, "image/png"); err != nil {
						errorMsg = "Could not save avatar"
					} else {
						h.logAction(r, models.ActionUserUpdate, "user", profileUser.ID, "avatar updated")
					}
				}
			}
		}
	}

	// Refresh user data
	profileUser, _ = h.DB.GetUserByID(profileUser.ID)

	data := map[string]interface{}{
		"ProfileUser":  profileUser,
		"IsOwnProfile": true,
		"Section":      "settings",
		"BaseURL":      h.BaseURL,
	}

	if errorMsg != "" {
		data["Error"] = errorMsg
	} else {
		data["Success"] = "Settings saved"
	}

	hats, _ := h.DB.GetUserHats(profileUser.ID)
	data["Hats"] = hats
	inviter, _ := h.DB.GetUserInviter(profileUser.ID)
	data["Inviter"] = inviter

	h.render(w, r, "user.html", data)
}

// userAvatarSubmit handles avatar upload from profile page.
func (h *Handler) userAvatarSubmit(w http.ResponseWriter, r *http.Request, profileUser *models.User) {
	// Parse multipart form
	if err := r.ParseMultipartForm(maxAvatarSize); err != nil {
		h.render(w, r, "upload_avatar.html", map[string]interface{}{
			"Error":       "File too large (max 256KB)",
			"ProfileUser": profileUser,
		})
		return
	}

	file, header, err := r.FormFile("avatar")
	if err != nil {
		h.render(w, r, "upload_avatar.html", map[string]interface{}{
			"Error":       "Please select a file",
			"ProfileUser": profileUser,
		})
		return
	}
	defer file.Close()

	if header.Size > maxAvatarSize {
		h.render(w, r, "upload_avatar.html", map[string]interface{}{
			"Error":       "File too large (max 256KB)",
			"ProfileUser": profileUser,
		})
		return
	}

	data, err := io.ReadAll(io.LimitReader(file, maxAvatarSize+1))
	if err != nil || len(data) > maxAvatarSize {
		h.render(w, r, "upload_avatar.html", map[string]interface{}{
			"Error":       "Could not read file",
			"ProfileUser": profileUser,
		})
		return
	}

	// Resize image to 100x100
	resizedData, err := imaging.ResizeToSquare(data, avatarSize)
	if err != nil {
		h.render(w, r, "upload_avatar.html", map[string]interface{}{
			"Error":       "Invalid image format (use PNG, JPEG, or GIF)",
			"ProfileUser": profileUser,
		})
		return
	}

	if err := h.DB.UpdateUserAvatar(profileUser.ID, resizedData, "image/png"); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not save avatar")
		return
	}

	h.logAction(r, models.ActionUserUpdate, "user", profileUser.ID, "avatar updated")

	h.redirect(w, r, "/users/"+profileUser.Username+"?section=settings")
}

// userCreateInvite creates a new invite from profile page.
func (h *Handler) userCreateInvite(w http.ResponseWriter, r *http.Request, profileUser *models.User) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	note := strings.TrimSpace(r.FormValue("note"))

	// Limit pending invites
	pendingCount, _ := h.DB.GetPendingInviteCount(profileUser.ID)
	if pendingCount >= 5 {
		invites, _ := h.DB.GetUserInvites(profileUser.ID)
		hats, _ := h.DB.GetUserHats(profileUser.ID)
		inviter, _ := h.DB.GetUserInviter(profileUser.ID)
		h.render(w, r, "user.html", map[string]interface{}{
			"ProfileUser":  profileUser,
			"IsOwnProfile": true,
			"Section":      "invites",
			"Invites":      invites,
			"Hats":         hats,
			"Inviter":      inviter,
			"Error":        "You have too many pending invites. Wait for some to be used.",
			"BaseURL":      h.BaseURL,
		})
		return
	}

	invite, err := h.DB.CreateInvite(profileUser.ID, note)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not create invite")
		return
	}

	h.logAction(r, models.ActionInviteCreate, "invite", invite.ID, note)

	h.redirect(w, r, "/users/"+profileUser.Username+"?section=invites")
}

// UserSettingsPage shows the user's settings.
func (h *Handler) UserSettingsPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	h.render(w, r, "settings.html", nil)
}

// UserSettingsSubmit handles settings form submission.
func (h *Handler) UserSettingsSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	about := strings.TrimSpace(r.FormValue("about"))

	// Limit about length
	if len(about) > 1000 {
		about = about[:1000]
	}

	if err := h.DB.UpdateUser(user.ID, about); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not update profile")
		return
	}

	h.logAction(r, models.ActionUserUpdate, "user", user.ID, "about updated")

	h.render(w, r, "settings.html", map[string]interface{}{
		"Success": "Profile updated",
	})
}

// UploadAvatarPage shows the avatar upload form.
func (h *Handler) UploadAvatarPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	h.render(w, r, "upload_avatar.html", nil)
}

// UploadAvatarSubmit handles avatar upload.
func (h *Handler) UploadAvatarSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(maxAvatarSize); err != nil {
		h.render(w, r, "upload_avatar.html", map[string]interface{}{
			"Error": "File too large (max 256KB)",
		})
		return
	}

	file, header, err := r.FormFile("avatar")
	if err != nil {
		h.render(w, r, "upload_avatar.html", map[string]interface{}{
			"Error": "Please select a file",
		})
		return
	}
	defer file.Close()

	// Check file size
	if header.Size > maxAvatarSize {
		h.render(w, r, "upload_avatar.html", map[string]interface{}{
			"Error": "File too large (max 256KB)",
		})
		return
	}

	// Read file
	data, err := io.ReadAll(io.LimitReader(file, maxAvatarSize+1))
	if err != nil || len(data) > maxAvatarSize {
		h.render(w, r, "upload_avatar.html", map[string]interface{}{
			"Error": "Could not read file",
		})
		return
	}

	// Resize image to 100x100
	resizedData, err := imaging.ResizeToSquare(data, avatarSize)
	if err != nil {
		h.render(w, r, "upload_avatar.html", map[string]interface{}{
			"Error": "Invalid image format (use PNG, JPEG, or GIF)",
		})
		return
	}

	// Save avatar
	if err := h.DB.UpdateUserAvatar(user.ID, resizedData, "image/png"); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not save avatar")
		return
	}

	h.logAction(r, models.ActionUserUpdate, "user", user.ID, "avatar updated")

	h.redirect(w, r, "/settings")
}

// ServeAvatar serves a user's avatar image.
func (h *Handler) ServeAvatar(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/avatar/")
	username = strings.TrimSuffix(username, ".png")
	username = strings.TrimSuffix(username, ".jpg")
	username = strings.TrimSuffix(username, ".gif")
	username = strings.TrimSuffix(username, ".svg")

	// Special case for RSS icon
	if username == "_rss" {
		h.serveRSSIcon(w)
		return
	}

	if username == "" {
		h.serveDefaultAvatar(w, "")
		return
	}

	user, err := h.DB.GetUserByUsername(username)
	if err != nil || user.Avatar == nil || len(user.Avatar) == 0 {
		h.serveDefaultAvatar(w, username)
		return
	}

	// Generate ETag from avatar content length (simple but effective)
	etag := fmt.Sprintf(`"%d-%d"`, user.ID, len(user.Avatar))

	// Check If-None-Match
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Set cache headers - must-revalidate ensures browser checks ETag
	w.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", user.AvatarType)
	w.Write(user.Avatar)
}

// serveDefaultAvatar serves a random colored SVG circle based on username.
func (h *Handler) serveDefaultAvatar(w http.ResponseWriter, username string) {
	// Generate a color based on username hash
	color := generateColorFromString(username)

	// Get initials (first letter or "?" if empty)
	initial := "?"
	if len(username) > 0 {
		initial = strings.ToUpper(string(username[0]))
	}

	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32">
  <circle cx="16" cy="16" r="16" fill="` + color + `"/>
  <text x="16" y="21" text-anchor="middle" font-family="sans-serif" font-size="14" font-weight="bold" fill="white">` + initial + `</text>
</svg>`

	// Use ETag "default" for default avatars - browser will revalidate
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
	w.Header().Set("ETag", `"default"`)
	w.Write([]byte(svg))
}

// serveRSSIcon serves the RSS feed icon.
func (h *Handler) serveRSSIcon(w http.ResponseWriter) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32">
  <circle cx="16" cy="16" r="16" fill="#f26522"/>
  <circle cx="10" cy="22" r="3" fill="white"/>
  <path d="M7 11.5c7.18 0 13 5.82 13 13h-3.5c0-5.24-4.26-9.5-9.5-9.5v-3.5z" fill="white"/>
  <path d="M7 5c10.49 0 19 8.51 19 19h-3.5c0-8.56-6.94-15.5-15.5-15.5V5z" fill="white"/>
</svg>`

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write([]byte(svg))
}

// generateColorFromString generates a consistent color from a string.
func generateColorFromString(s string) string {
	if s == "" {
		return "#888888"
	}

	// Simple hash
	var hash uint32
	for _, c := range s {
		hash = hash*31 + uint32(c)
	}

	// Generate HSL color with good saturation and lightness
	hue := hash % 360
	// Use fixed saturation and lightness for pleasant colors
	return hslToHex(int(hue), 60, 45)
}

// hslToHex converts HSL to hex color.
func hslToHex(h, s, l int) string {
	// Convert to 0-1 range
	hf := float64(h) / 360
	sf := float64(s) / 100
	lf := float64(l) / 100

	var r, g, b float64

	if sf == 0 {
		r, g, b = lf, lf, lf
	} else {
		var q float64
		if lf < 0.5 {
			q = lf * (1 + sf)
		} else {
			q = lf + sf - lf*sf
		}
		p := 2*lf - q
		r = hueToRgb(p, q, hf+1.0/3)
		g = hueToRgb(p, q, hf)
		b = hueToRgb(p, q, hf-1.0/3)
	}

	ri := int(r * 255)
	gi := int(g * 255)
	bi := int(b * 255)

	return "#" + toHex(ri) + toHex(gi) + toHex(bi)
}

func hueToRgb(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	if t < 1.0/6 {
		return p + (q-p)*6*t
	}
	if t < 0.5 {
		return q
	}
	if t < 2.0/3 {
		return p + (q-p)*(2.0/3-t)*6
	}
	return p
}

func toHex(n int) string {
	hex := "0123456789abcdef"
	return string(hex[n/16]) + string(hex[n%16])
}
