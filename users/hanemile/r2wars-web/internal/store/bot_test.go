package store

import (
	"errors"
	"sort"
	"testing"

	"git.emile.space/r2wars-web/internal/model"
)

func firstArchBitIDs(t *testing.T, st *Store, archName, bitName string) (archID, bitID int) {
	t.Helper()
	archs, err := st.GetAllArchs()
	if err != nil {
		t.Fatalf("GetAllArchs() error = %v", err)
	}
	for _, a := range archs {
		if a.Name == archName {
			archID = a.ID
		}
	}
	bits, err := st.GetAllBits()
	if err != nil {
		t.Fatalf("GetAllBits() error = %v", err)
	}
	for _, b := range bits {
		if b.Name == bitName {
			bitID = b.ID
		}
	}
	if archID == 0 || bitID == 0 {
		t.Fatalf("could not find seeded arch %q / bit %q", archName, bitName)
	}
	return archID, bitID
}

func TestGetBotByIDNotFound(t *testing.T) {
	st := newTestStore(t)
	_, err := st.GetBotByID(999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetBotByID() error = %v, want ErrNotFound", err)
	}
}

func TestBotArchBitLinkingRoundTrip(t *testing.T) {
	st := newTestStore(t)
	user := mustCreateUser(t, st, "alice")

	botID, err := st.InsertBot(model.Bot{Name: "amaterasu", Source: "nop"})
	if err != nil {
		t.Fatalf("InsertBot() error = %v", err)
	}
	if err := st.LinkUserBot(user.Name, botID); err != nil {
		t.Fatalf("LinkUserBot() error = %v", err)
	}

	x86ID, bits32ID := firstArchBitIDs(t, st, "x86", "32")

	if err := st.LinkArchIDsToBot(botID, []int{x86ID}); err != nil {
		t.Fatalf("LinkArchIDsToBot() error = %v", err)
	}
	if err := st.LinkBitIDsToBot(botID, []int{bits32ID}); err != nil {
		t.Fatalf("LinkBitIDsToBot() error = %v", err)
	}

	bot, err := st.GetBotByID(botID)
	if err != nil {
		t.Fatalf("GetBotByID() error = %v", err)
	}
	if len(bot.Archs) != 1 || bot.Archs[0].Name != "x86" {
		t.Fatalf("bot.Archs = %+v, want [x86]", bot.Archs)
	}
	if len(bot.Bits) != 1 || bot.Bits[0].Name != "32" {
		t.Fatalf("bot.Bits = %+v, want [32]", bot.Bits)
	}
	if len(bot.Users) != 1 || bot.Users[0].ID != user.ID {
		t.Fatalf("bot.Users = %+v, want [%+v]", bot.Users, user)
	}

	// Relinking must replace, not accumulate - a bot edited from x86/32 to
	// arm/64 shouldn't end up valid for both afterwards.
	armID, bits64ID := firstArchBitIDs(t, st, "arm", "64")
	if err := st.LinkArchIDsToBot(botID, []int{armID}); err != nil {
		t.Fatalf("re-LinkArchIDsToBot() error = %v", err)
	}
	if err := st.LinkBitIDsToBot(botID, []int{bits64ID}); err != nil {
		t.Fatalf("re-LinkBitIDsToBot() error = %v", err)
	}

	bot, err = st.GetBotByID(botID)
	if err != nil {
		t.Fatalf("GetBotByID() after relink error = %v", err)
	}
	if len(bot.Archs) != 1 || bot.Archs[0].Name != "arm" {
		t.Fatalf("bot.Archs after relink = %+v, want [arm]", bot.Archs)
	}
	if len(bot.Bits) != 1 || bot.Bits[0].Name != "64" {
		t.Fatalf("bot.Bits after relink = %+v, want [64]", bot.Bits)
	}
}

func TestGetAllBotsWithUsers(t *testing.T) {
	st := newTestStore(t)
	alice := mustCreateUser(t, st, "alice")
	bob := mustCreateUser(t, st, "bob")

	botID, err := st.InsertBot(model.Bot{Name: "shared-bot", Source: "nop"})
	if err != nil {
		t.Fatalf("InsertBot() error = %v", err)
	}
	if err := st.LinkUserBot(alice.Name, botID); err != nil {
		t.Fatalf("LinkUserBot(alice) error = %v", err)
	}
	if err := st.LinkUserBot(bob.Name, botID); err != nil {
		t.Fatalf("LinkUserBot(bob) error = %v", err)
	}

	bots, err := st.GetAllBotsWithUsers()
	if err != nil {
		t.Fatalf("GetAllBotsWithUsers() error = %v", err)
	}
	if len(bots) != 1 {
		t.Fatalf("GetAllBotsWithUsers() returned %d bots, want 1", len(bots))
	}
	names := []string{bots[0].Users[0].Name, bots[0].Users[1].Name}
	sort.Strings(names)
	if names[0] != "alice" || names[1] != "bob" {
		t.Fatalf("bot users = %v, want [alice bob]", names)
	}
}
