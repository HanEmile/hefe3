package protocol

import (
	"fmt"
	"regexp"
	"strings"
)

var handleRegex = regexp.MustCompile(`^@[a-z0-9._-]{1,64}@[a-z0-9][a-z0-9.-]*[a-z0-9]$`)

// Handle represents a parsed @user@server handle.
type Handle struct {
	User   string
	Server string
}

// ParseHandle parses a string like "@alice@example.com" into a Handle.
func ParseHandle(s string) (Handle, error) {
	if !handleRegex.MatchString(s) {
		return Handle{}, fmt.Errorf("invalid handle: %q", s)
	}

	// Remove leading @, split on @
	parts := strings.SplitN(s[1:], "@", 2)
	return Handle{
		User:   parts[0],
		Server: parts[1],
	}, nil
}

// String returns the handle as @user@server.
func (h Handle) String() string {
	return "@" + h.User + "@" + h.Server
}

// IsLocal returns true if the handle belongs to the given server.
func (h Handle) IsLocal(hostname string) bool {
	return h.Server == hostname
}

// ValidateHandle checks if a handle string is syntactically valid.
func ValidateHandle(s string) bool {
	return handleRegex.MatchString(s)
}

// MakeHandle constructs a handle string from user and server parts.
func MakeHandle(user, server string) string {
	return "@" + user + "@" + server
}
