package store

import (
	"errors"
	"testing"

	"git.emile.space/r2wars-web/internal/model"
)

func mustCreateBattle(t *testing.T, st *Store, name string, owner model.User) model.Battle {
	t.Helper()
	id, err := st.InsertBattle(model.Battle{Name: name, MaxRounds: 100, ArenaSize: 4096}, owner)
	if err != nil {
		t.Fatalf("InsertBattle(%q) error = %v", name, err)
	}
	return model.Battle{ID: id, Name: name}
}

// TestInsertBattleLinksOwnerToNewBattle guards against a regression where
// InsertBattle linked the owner to `battle.ID` from the caller-supplied,
// not-yet-persisted struct (always 0) instead of the id SQLite actually
// assigned.
func TestInsertBattleLinksOwnerToNewBattle(t *testing.T) {
	st := newTestStore(t)
	owner := mustCreateUser(t, st, "alice")

	battle := mustCreateBattle(t, st, "first", owner)
	// A second battle to make sure a wrong, hardcoded id (e.g. 0 or 1)
	// wouldn't accidentally pass.
	second := mustCreateBattle(t, st, "second", owner)

	for _, b := range []model.Battle{battle, second} {
		deep, err := st.GetBattleByIDDeep(b.ID)
		if err != nil {
			t.Fatalf("GetBattleByIDDeep(%d) error = %v", b.ID, err)
		}
		if len(deep.Owners) != 1 || deep.Owners[0].ID != owner.ID {
			t.Fatalf("battle %d owners = %+v, want [%+v]", b.ID, deep.Owners, owner)
		}
	}
}

func TestDeleteBattleByID(t *testing.T) {
	st := newTestStore(t)
	owner := mustCreateUser(t, st, "alice")
	battle := mustCreateBattle(t, st, "doomed", owner)

	if err := st.DeleteBattleByID(battle.ID); err != nil {
		t.Fatalf("DeleteBattleByID() error = %v", err)
	}

	if _, err := st.GetBattleByIDDeep(battle.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetBattleByIDDeep() after delete: error = %v, want ErrNotFound", err)
	}

	all, err := st.GetAllBattles()
	if err != nil {
		t.Fatalf("GetAllBattles() error = %v", err)
	}
	for _, b := range all {
		if b.ID == battle.ID {
			t.Fatalf("battle %d still present in GetAllBattles() after delete", battle.ID)
		}
	}
}

func TestBattleArchBitOwnerLinking(t *testing.T) {
	st := newTestStore(t)
	alice := mustCreateUser(t, st, "alice")
	bob := mustCreateUser(t, st, "bob")
	battle := mustCreateBattle(t, st, "arena", alice)

	x86ID, bits32ID := firstArchBitIDs(t, st, "x86", "32")
	armID, bits64ID := firstArchBitIDs(t, st, "arm", "64")

	if err := st.LinkArchIDsToBattle(battle.ID, []int{x86ID, armID}); err != nil {
		t.Fatalf("LinkArchIDsToBattle() error = %v", err)
	}
	if err := st.LinkBitIDsToBattle(battle.ID, []int{bits32ID, bits64ID}); err != nil {
		t.Fatalf("LinkBitIDsToBattle() error = %v", err)
	}
	if err := st.LinkOwnerIDsToBattle(battle.ID, []int{alice.ID, bob.ID}); err != nil {
		t.Fatalf("LinkOwnerIDsToBattle() error = %v", err)
	}

	deep, err := st.GetBattleByIDDeep(battle.ID)
	if err != nil {
		t.Fatalf("GetBattleByIDDeep() error = %v", err)
	}
	if len(deep.Archs) != 2 {
		t.Fatalf("battle.Archs = %+v, want 2 entries", deep.Archs)
	}
	if len(deep.Bits) != 2 {
		t.Fatalf("battle.Bits = %+v, want 2 entries", deep.Bits)
	}
	if len(deep.Owners) != 2 {
		t.Fatalf("battle.Owners = %+v, want 2 entries", deep.Owners)
	}
}

// TestBattleSupportsMoreThanTwoBots guards the store layer against ever
// reintroducing an assumption that a battle has exactly two participants -
// the HTTP battle-run handler used to hardcode a 2-element array and panic
// on a 3rd submission.
func TestBattleSupportsMoreThanTwoBots(t *testing.T) {
	st := newTestStore(t)
	owner := mustCreateUser(t, st, "alice")
	battle := mustCreateBattle(t, st, "free-for-all", owner)

	for i := 0; i < 3; i++ {
		botID, err := st.InsertBot(model.Bot{Name: "bot", Source: "nop"})
		if err != nil {
			t.Fatalf("InsertBot() error = %v", err)
		}
		if err := st.LinkBotBattle(botID, battle.ID); err != nil {
			t.Fatalf("LinkBotBattle() error = %v", err)
		}
	}

	deep, err := st.GetBattleByIDDeep(battle.ID)
	if err != nil {
		t.Fatalf("GetBattleByIDDeep() error = %v", err)
	}
	if len(deep.Bots) != 3 {
		t.Fatalf("battle.Bots = %+v, want 3 entries", deep.Bots)
	}
}

func TestUnlinkAllBotsForUserFromBattle(t *testing.T) {
	st := newTestStore(t)
	alice := mustCreateUser(t, st, "alice")
	bob := mustCreateUser(t, st, "bob")
	battle := mustCreateBattle(t, st, "arena", alice)

	aliceBot, err := st.InsertBot(model.Bot{Name: "alice-bot", Source: "nop"})
	if err != nil {
		t.Fatalf("InsertBot() error = %v", err)
	}
	if err := st.LinkUserBot(alice.Name, aliceBot); err != nil {
		t.Fatalf("LinkUserBot() error = %v", err)
	}
	bobBot, err := st.InsertBot(model.Bot{Name: "bob-bot", Source: "nop"})
	if err != nil {
		t.Fatalf("InsertBot() error = %v", err)
	}
	if err := st.LinkUserBot(bob.Name, bobBot); err != nil {
		t.Fatalf("LinkUserBot() error = %v", err)
	}

	if err := st.LinkBotBattle(aliceBot, battle.ID); err != nil {
		t.Fatalf("LinkBotBattle(aliceBot) error = %v", err)
	}
	if err := st.LinkBotBattle(bobBot, battle.ID); err != nil {
		t.Fatalf("LinkBotBattle(bobBot) error = %v", err)
	}

	if err := st.UnlinkAllBotsForUserFromBattle(alice.ID, battle.ID); err != nil {
		t.Fatalf("UnlinkAllBotsForUserFromBattle() error = %v", err)
	}

	deep, err := st.GetBattleByIDDeep(battle.ID)
	if err != nil {
		t.Fatalf("GetBattleByIDDeep() error = %v", err)
	}
	if len(deep.Bots) != 1 || deep.Bots[0].ID != bobBot {
		t.Fatalf("battle.Bots after unlink = %+v, want only bob's bot (%d)", deep.Bots, bobBot)
	}
}
