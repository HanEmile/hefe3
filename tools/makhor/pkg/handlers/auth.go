// Authentication handlers: login, logout, registration.
package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"makhor/pkg/db"
	"makhor/pkg/middleware"
	"makhor/pkg/models"
	"net/http"
	"regexp"
	"strings"
)

var (
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	usernameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{2,19}$`)
)

// LoginPage shows the login form.
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if middleware.GetUser(r) != nil {
		h.redirect(w, r, "/")
		return
	}

	h.render(w, r, "login.html", nil)
}

// LoginSubmit handles login form submission.
// Sends a magic link to the user's email.
func (h *Handler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))

	if !emailRegex.MatchString(email) {
		h.render(w, r, "login.html", map[string]interface{}{
			"Error": "Please enter a valid email address",
			"Email": email,
		})
		return
	}

	// Check if user exists
	user, err := h.DB.GetUserByEmail(email)
	if err == db.ErrUserNotFound {
		// Don't reveal whether email exists - show same message
		h.render(w, r, "login.html", map[string]interface{}{
			"Message": "If an account exists with this email, a login link has been sent.",
		})
		return
	}
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Database error")
		return
	}

	if user.IsBanned {
		h.render(w, r, "login.html", map[string]interface{}{
			"Error": "This account has been banned",
		})
		return
	}

	// Create login token
	token, err := h.DB.CreateLoginToken(email)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not create login token")
		return
	}

	// Send email with login link
	loginURL := fmt.Sprintf("%s/login/verify?token=%s", h.BaseURL, token)
	emailBody := fmt.Sprintf(`Hello %s,

Click the link below to log in to makhor:

%s

This link will expire in 15 minutes.

If you did not request this login, you can safely ignore this email.
`, user.Username, loginURL)

	if h.MailFunc != nil {
		if err := h.MailFunc(email, "Login to makhor", emailBody); err != nil {
			// Log error but don't reveal to user
			log.Printf("Error sending email to %s: %v", email, err)
		}
	} else {
		// Development mode - print to console
		log.Printf("LOGIN LINK FOR %s: %s", email, loginURL)
	}

	h.render(w, r, "login.html", map[string]interface{}{
		"Message": "If an account exists with this email, a login link has been sent.",
	})
}

// LoginVerify handles the magic link verification.
func (h *Handler) LoginVerify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		h.renderError(w, r, http.StatusBadRequest, "Missing token")
		return
	}

	email, err := h.DB.ValidateLoginToken(token)
	if err == db.ErrInvalidToken {
		h.render(w, r, "login.html", map[string]interface{}{
			"Error": "Invalid or expired login link. Please request a new one.",
		})
		return
	}
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Database error")
		return
	}

	user, err := h.DB.GetUserByEmail(email)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "User not found")
		return
	}

	// Create session
	sessionToken, err := h.DB.CreateSession(user.ID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not create session")
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   strings.HasPrefix(h.BaseURL, "https"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60, // 30 days
	})

	h.logAction(r, models.ActionUserLogin, "user", user.ID, "")

	h.redirect(w, r, "/")
}

// Logout logs the user out.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		h.DB.DeleteSession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	h.redirect(w, r, "/")
}

// RegisterPage shows the registration form.
func (h *Handler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	if middleware.GetUser(r) != nil {
		h.redirect(w, r, "/")
		return
	}

	inviteCode := r.URL.Query().Get("invite")

	h.render(w, r, "register.html", map[string]interface{}{
		"InviteCode": inviteCode,
	})
}

// RegisterSubmit handles registration form submission.
func (h *Handler) RegisterSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	inviteCode := strings.TrimSpace(r.FormValue("invite_code"))

	// Validation
	var errors []string

	if !usernameRegex.MatchString(username) {
		errors = append(errors, "Username must be 3-20 characters, start with a letter, and contain only letters, numbers, and underscores")
	}

	if !emailRegex.MatchString(email) {
		errors = append(errors, "Please enter a valid email address")
	}

	// Validate invite code - required for registration
	var inviterID *int64
	if inviteCode == "" {
		errors = append(errors, "An invite code is required to register")
	} else {
		invite, err := h.DB.GetInviteByCode(inviteCode)
		if err == db.ErrInvalidInvite || (invite != nil && invite.UsedBy != nil) {
			errors = append(errors, "Invalid or already used invite code")
		} else if err != nil {
			errors = append(errors, "Could not verify invite code")
		} else {
			inviterID = &invite.CreatedBy
		}
	}

	if len(errors) > 0 {
		h.render(w, r, "register.html", map[string]interface{}{
			"Errors":     errors,
			"Username":   username,
			"Email":      email,
			"InviteCode": inviteCode,
		})
		return
	}

	// Create user
	user, err := h.DB.CreateUser(username, email, inviterID)
	if err == db.ErrUserExists {
		h.render(w, r, "register.html", map[string]interface{}{
			"Errors":     []string{"Username or email already taken"},
			"Username":   username,
			"Email":      email,
			"InviteCode": inviteCode,
		})
		return
	}
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not create account")
		return
	}

	// Mark invite as used
	if inviteCode != "" {
		h.DB.UseInvite(inviteCode, user.ID)
	}

	h.logAction(r, models.ActionUserCreate, "user", user.ID, fmt.Sprintf("invited_by=%v", inviterID))

	// Create login token and send email
	token, err := h.DB.CreateLoginToken(email)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not create login token")
		return
	}

	loginURL := fmt.Sprintf("%s/login/verify?token=%s", h.BaseURL, token)
	emailBody := fmt.Sprintf(`Welcome to makhor, %s!

Your account has been created. Click the link below to log in:

%s

This link will expire in 15 minutes.
`, username, loginURL)

	if h.MailFunc != nil {
		if err := h.MailFunc(email, "Welcome to makhor", emailBody); err != nil {
			log.Printf("Error sending welcome email to %s: %v", email, err)
		}
	} else {
		log.Printf("WELCOME EMAIL FOR %s: %s", email, loginURL)
	}

	h.render(w, r, "register.html", map[string]interface{}{
		"Success": "Account created! Check your email for a login link.",
	})
}

// InvitesPage shows the user's invites.
func (h *Handler) InvitesPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	invites, err := h.DB.GetUserInvites(user.ID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load invites")
		return
	}

	h.render(w, r, "invites.html", map[string]interface{}{
		"Invites": invites,
		"BaseURL": h.BaseURL,
	})
}

// CreateInvite creates a new invite code.
func (h *Handler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	note := strings.TrimSpace(r.FormValue("note"))

	// Limit pending invites (optional, adjust as needed)
	pendingCount, _ := h.DB.GetPendingInviteCount(user.ID)
	if pendingCount >= 5 {
		h.render(w, r, "invites.html", map[string]interface{}{
			"Error": "You have too many pending invites. Wait for some to be used.",
		})
		return
	}

	invite, err := h.DB.CreateInvite(user.ID, note)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not create invite")
		return
	}

	h.logAction(r, models.ActionInviteCreate, "invite", invite.ID, note)

	h.redirect(w, r, "/invites")
}

// InviteTreePage shows the invite tree.
func (h *Handler) InviteTreePage(w http.ResponseWriter, r *http.Request) {
	// Find the root user (first user, or specified user)
	userIDStr := r.URL.Query().Get("user")
	var rootUserID int64 = 1

	if userIDStr != "" {
		if id, err := getPathID(r, ""); err == nil {
			rootUserID = id
		}
	}

	tree, err := h.DB.GetInviteTree(rootUserID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not load invite tree")
		return
	}

	h.render(w, r, "invite_tree.html", map[string]interface{}{
		"Tree": tree,
	})
}

// ChangeEmailSubmit initiates email change by sending verification to old email.
func (h *Handler) ChangeEmailSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		h.redirect(w, r, "/login")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	newEmail := strings.TrimSpace(strings.ToLower(r.FormValue("new_email")))

	if !emailRegex.MatchString(newEmail) {
		h.userSettingsError(w, r, user, "Please enter a valid email address")
		return
	}

	if newEmail == user.Email {
		h.userSettingsError(w, r, user, "New email is the same as your current email")
		return
	}

	// Check if email is already taken
	_, err := h.DB.GetUserByEmail(newEmail)
	if err == nil {
		h.userSettingsError(w, r, user, "This email is already in use")
		return
	} else if err != db.ErrUserNotFound {
		h.renderError(w, r, http.StatusInternalServerError, "Database error")
		return
	}

	// Generate token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not generate token")
		return
	}
	token := hex.EncodeToString(tokenBytes)

	// Store token
	if err := h.DB.CreateEmailChangeToken(user.ID, newEmail, token); err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Could not create token")
		return
	}

	// Send verification email to OLD email address
	verifyURL := fmt.Sprintf("%s/settings/email/verify?token=%s", h.BaseURL, token)
	emailBody := fmt.Sprintf(`Hello %s,

You requested to change your email address to: %s

Click the link below to confirm this change:

%s

This link will expire in 24 hours.

If you did not request this change, you can safely ignore this email and your email will remain unchanged.
`, user.Username, newEmail, verifyURL)

	if h.MailFunc != nil {
		if err := h.MailFunc(user.Email, "Confirm email change - makhor", emailBody); err != nil {
			log.Printf("Error sending email change verification to %s: %v", user.Email, err)
		}
	} else {
		log.Printf("EMAIL CHANGE VERIFICATION FOR %s: %s", user.Email, verifyURL)
	}

	h.logAction(r, models.ActionUserUpdate, "user", user.ID, fmt.Sprintf("email change requested to %s", newEmail))

	h.userSettingsSuccess(w, r, user, "A verification link has been sent to your current email address.")
}

// VerifyEmailChange processes the email change verification token.
func (h *Handler) VerifyEmailChange(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		h.renderError(w, r, http.StatusBadRequest, "Missing token")
		return
	}

	err := h.DB.UseEmailChangeToken(token)
	if err != nil {
		h.render(w, r, "message.html", map[string]interface{}{
			"Title":   "Email Change Failed",
			"Message": "Invalid or expired email change link. Please request a new one.",
			"IsError": true,
		})
		return
	}

	h.render(w, r, "message.html", map[string]interface{}{
		"Title":   "Email Changed",
		"Message": "Your email address has been updated successfully.",
	})
}

// userSettingsError renders the settings section with an error message.
func (h *Handler) userSettingsError(w http.ResponseWriter, r *http.Request, user *models.User, errMsg string) {
	hats, _ := h.DB.GetUserHats(user.ID)
	inviter, _ := h.DB.GetUserInviter(user.ID)
	h.render(w, r, "user.html", map[string]interface{}{
		"ProfileUser":  user,
		"IsOwnProfile": true,
		"Section":      "settings",
		"Hats":         hats,
		"Inviter":      inviter,
		"Error":        errMsg,
		"BaseURL":      h.BaseURL,
	})
}

// userSettingsSuccess renders the settings section with a success message.
func (h *Handler) userSettingsSuccess(w http.ResponseWriter, r *http.Request, user *models.User, msg string) {
	hats, _ := h.DB.GetUserHats(user.ID)
	inviter, _ := h.DB.GetUserInviter(user.ID)
	h.render(w, r, "user.html", map[string]interface{}{
		"ProfileUser":  user,
		"IsOwnProfile": true,
		"Section":      "settings",
		"Hats":         hats,
		"Inviter":      inviter,
		"Success":      msg,
		"BaseURL":      h.BaseURL,
	})
}
