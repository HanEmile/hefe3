package protocol

import "testing"

func TestParseHandle(t *testing.T) {
	tests := []struct {
		input string
		user  string
		server string
		valid bool
	}{
		{"@alice@example.com", "alice", "example.com", true},
		{"@bob@chat.xilebo.io", "bob", "chat.xilebo.io", true},
		{"@user-name@server.tld", "user-name", "server.tld", true},
		{"@user.name@server.tld", "user.name", "server.tld", true},
		{"@user_name@server.tld", "user_name", "server.tld", true},
		{"@a@b.c", "a", "b.c", true},
		// Invalid
		{"alice@example.com", "", "", false},     // no leading @
		{"@Alice@example.com", "", "", false},    // uppercase
		{"@@example.com", "", "", false},         // empty user
		{"@alice@", "", "", false},               // empty server
		{"@alice", "", "", false},                // no server
		{"", "", "", false},                      // empty
		{"@a@b", "", "", false},                  // single char server
	}

	for _, tc := range tests {
		h, err := ParseHandle(tc.input)
		if tc.valid {
			if err != nil {
				t.Errorf("ParseHandle(%q) unexpected error: %v", tc.input, err)
				continue
			}
			if h.User != tc.user {
				t.Errorf("ParseHandle(%q).User = %q, want %q", tc.input, h.User, tc.user)
			}
			if h.Server != tc.server {
				t.Errorf("ParseHandle(%q).Server = %q, want %q", tc.input, h.Server, tc.server)
			}
			if h.String() != tc.input {
				t.Errorf("Handle.String() = %q, want %q", h.String(), tc.input)
			}
		} else {
			if err == nil {
				t.Errorf("ParseHandle(%q) expected error, got %+v", tc.input, h)
			}
		}
	}
}

func TestHandleIsLocal(t *testing.T) {
	h, _ := ParseHandle("@alice@example.com")
	if !h.IsLocal("example.com") {
		t.Error("expected IsLocal=true for example.com")
	}
	if h.IsLocal("other.com") {
		t.Error("expected IsLocal=false for other.com")
	}
}

func TestMakeHandle(t *testing.T) {
	h := MakeHandle("alice", "example.com")
	if h != "@alice@example.com" {
		t.Errorf("MakeHandle = %q, want @alice@example.com", h)
	}
}

func TestValidateHandle(t *testing.T) {
	if !ValidateHandle("@alice@example.com") {
		t.Error("expected valid")
	}
	if ValidateHandle("not-a-handle") {
		t.Error("expected invalid")
	}
}
