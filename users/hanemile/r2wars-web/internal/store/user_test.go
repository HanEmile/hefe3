package store

import (
	"errors"
	"strings"
	"testing"

	"git.emile.space/r2wars-web/internal/model"
)

func TestInsertUserDuplicateNameRejected(t *testing.T) {
	st := newTestStore(t)
	mustCreateUser(t, st, "alice")

	_, err := st.InsertUser(model.User{Name: "alice", Email: "someone-else@example.com"})
	if err == nil {
		t.Fatal("InsertUser() with duplicate name: want error, got nil")
	}
	if !IsDuplicateUser(err) {
		t.Fatalf("IsDuplicateUser(%v) = false, want true", err)
	}
}

func TestInsertUserDuplicateEmailRejected(t *testing.T) {
	st := newTestStore(t)
	alice := mustCreateUser(t, st, "alice")

	_, err := st.InsertUser(model.User{Name: "someone-else", Email: alice.Email})
	if err == nil {
		t.Fatal("InsertUser() with duplicate email: want error, got nil")
	}
	if !IsDuplicateUser(err) {
		t.Fatalf("IsDuplicateUser(%v) = false, want true", err)
	}
}

func TestGetUserByEmail(t *testing.T) {
	st := newTestStore(t)
	alice := mustCreateUser(t, st, "alice")

	got, err := st.GetUserByEmail(alice.Email)
	if err != nil {
		t.Fatalf("GetUserByEmail() error = %v", err)
	}
	if got.ID != alice.ID {
		t.Fatalf("GetUserByEmail().ID = %d, want %d", got.ID, alice.ID)
	}

	// case-insensitive, since email addresses conventionally are
	got, err = st.GetUserByEmail(strings.ToUpper(alice.Email))
	if err != nil {
		t.Fatalf("GetUserByEmail(uppercased) error = %v", err)
	}
	if got.ID != alice.ID {
		t.Fatalf("GetUserByEmail(uppercased).ID = %d, want %d", got.ID, alice.ID)
	}

	if _, err := st.GetUserByEmail("nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetUserByEmail(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestUpdateUserEmail(t *testing.T) {
	st := newTestStore(t)
	alice := mustCreateUser(t, st, "alice")

	if err := st.UpdateUserEmail(alice.ID, "new@example.com"); err != nil {
		t.Fatalf("UpdateUserEmail() error = %v", err)
	}

	if _, err := st.GetUserByEmail(alice.Email); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetUserByEmail(old email) error = %v, want ErrNotFound", err)
	}
	got, err := st.GetUserByEmail("new@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail(new email) error = %v", err)
	}
	if got.ID != alice.ID {
		t.Fatalf("GetUserByEmail(new email).ID = %d, want %d", got.ID, alice.ID)
	}
}

func TestGetUserByUsernameNotFound(t *testing.T) {
	st := newTestStore(t)
	_, err := st.GetUserByUsername("nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetUserByUsername() error = %v, want ErrNotFound", err)
	}
}

func TestUpdateUserNameUpdatesSessionLookup(t *testing.T) {
	st := newTestStore(t)
	user := mustCreateUser(t, st, "alice")

	if err := st.UpdateUserName(user.ID, "alicia"); err != nil {
		t.Fatalf("UpdateUserName() error = %v", err)
	}

	if _, err := st.GetUserByUsername("alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetUserByUsername(old name) error = %v, want ErrNotFound", err)
	}
	got, err := st.GetUserByUsername("alicia")
	if err != nil {
		t.Fatalf("GetUserByUsername(new name) error = %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("GetUserByUsername(new name).ID = %d, want %d", got.ID, user.ID)
	}
}

func TestLinkUserBotAndFetch(t *testing.T) {
	st := newTestStore(t)
	user := mustCreateUser(t, st, "alice")

	botID, err := st.InsertBot(model.Bot{Name: "amaterasu", Source: "nop"})
	if err != nil {
		t.Fatalf("InsertBot() error = %v", err)
	}
	if err := st.LinkUserBot(user.Name, botID); err != nil {
		t.Fatalf("LinkUserBot() error = %v", err)
	}

	byUsername, err := st.GetUserBotsByUsername(user.Name)
	if err != nil {
		t.Fatalf("GetUserBotsByUsername() error = %v", err)
	}
	if len(byUsername) != 1 || byUsername[0].ID != botID {
		t.Fatalf("GetUserBotsByUsername() = %+v, want bot %d", byUsername, botID)
	}

	byID, err := st.GetUserBotsByID(user.ID)
	if err != nil {
		t.Fatalf("GetUserBotsByID() error = %v", err)
	}
	if len(byID) != 1 || byID[0].ID != botID {
		t.Fatalf("GetUserBotsByID() = %+v, want bot %d", byID, botID)
	}
}
