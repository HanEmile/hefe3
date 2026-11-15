package store

import (
	"path/filepath"
	"testing"

	"git.emile.space/r2wars-web/internal/model"
)

// newTestStore opens a fresh Store backed by a temp-file DB.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func containsArch(archs []model.Arch, name string) bool {
	for _, a := range archs {
		if a.Name == name {
			return true
		}
	}
	return false
}

func containsBit(bits []model.Bit, name string) bool {
	for _, b := range bits {
		if b.Name == name {
			return true
		}
	}
	return false
}

func TestSchemaSeed(t *testing.T) {
	s := newTestStore(t)

	archs, err := s.AllArchs()
	if err != nil {
		t.Fatalf("AllArchs: %v", err)
	}
	if len(archs) == 0 {
		t.Fatal("expected seeded archs, got none")
	}
	if !containsArch(archs, "x86") {
		t.Error("expected archs to contain x86")
	}

	bits, err := s.AllBits()
	if err != nil {
		t.Fatalf("AllBits: %v", err)
	}
	if len(bits) == 0 {
		t.Fatal("expected seeded bits, got none")
	}
	if !containsBit(bits, "32") {
		t.Error("expected bits to contain 32")
	}
}

func TestNewIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idem.db")
	s1, err := New(path)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	s1.Close()

	// Applying the schema again (INSERT OR IGNORE seeds) must not error or
	// duplicate the reference data.
	s2, err := New(path)
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	defer s2.Close()

	bits, err := s2.AllBits()
	if err != nil {
		t.Fatalf("AllBits: %v", err)
	}
	if len(bits) != 4 {
		t.Errorf("expected 4 bits after reopen, got %d", len(bits))
	}
}

func TestUserCRUD(t *testing.T) {
	s := newTestStore(t)

	id, err := s.InsertUser("alice", []byte("hash1"))
	if err != nil {
		t.Fatalf("InsertUser: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	if c, err := s.UsernameCount("alice"); err != nil || c != 1 {
		t.Errorf("UsernameCount alice = %d, %v; want 1, nil", c, err)
	}
	if c, _ := s.UsernameCount("nobody"); c != 0 {
		t.Errorf("UsernameCount nobody = %d; want 0", c)
	}

	if !s.CheckUserPassword("alice", []byte("hash1")) {
		t.Error("CheckUserPassword should succeed with correct hash")
	}
	if s.CheckUserPassword("alice", []byte("wrong")) {
		t.Error("CheckUserPassword should fail with wrong hash")
	}

	u, err := s.UserByID(id)
	if err != nil || u.Name != "alice" {
		t.Errorf("UserByID = %+v, %v", u, err)
	}
	u, err = s.UserByName("alice")
	if err != nil || u.ID != id {
		t.Errorf("UserByName = %+v, %v", u, err)
	}

	if err := s.UpdateUsername(id, "alice2"); err != nil {
		t.Fatalf("UpdateUsername: %v", err)
	}
	if u, _ := s.UserByID(id); u.Name != "alice2" {
		t.Errorf("after rename UserByID.Name = %q; want alice2", u.Name)
	}

	if err := s.UpdateUserPasswordHash("alice2", []byte("hash2")); err != nil {
		t.Fatalf("UpdateUserPasswordHash: %v", err)
	}
	if !s.CheckUserPassword("alice2", []byte("hash2")) {
		t.Error("CheckUserPassword should succeed after hash update")
	}

	users, err := s.AllUsers()
	if err != nil || len(users) != 1 {
		t.Errorf("AllUsers len = %d, %v; want 1", len(users), err)
	}
}

func TestBotWithLinks(t *testing.T) {
	s := newTestStore(t)

	userID, err := s.InsertUser("bob", []byte("h"))
	if err != nil {
		t.Fatalf("InsertUser: %v", err)
	}

	botID, err := s.InsertBot("warrior", "mov ax, bx")
	if err != nil {
		t.Fatalf("InsertBot: %v", err)
	}

	if err := s.LinkUserBot("bob", botID); err != nil {
		t.Fatalf("LinkUserBot: %v", err)
	}

	// pick a couple of arch/bit ids from the seeded reference data
	archs, _ := s.AllArchs()
	bits, _ := s.AllBits()
	var x86ID, bit32ID int
	for _, a := range archs {
		if a.Name == "x86" {
			x86ID = a.ID
		}
	}
	for _, b := range bits {
		if b.Name == "32" {
			bit32ID = b.ID
		}
	}

	if err := s.LinkBotArchs(botID, []int{x86ID}); err != nil {
		t.Fatalf("LinkBotArchs: %v", err)
	}
	if err := s.LinkBotBits(botID, []int{bit32ID}); err != nil {
		t.Fatalf("LinkBotBits: %v", err)
	}

	bot, err := s.BotByID(botID)
	if err != nil {
		t.Fatalf("BotByID: %v", err)
	}
	if bot.Name != "warrior" || bot.Source != "mov ax, bx" {
		t.Errorf("bot basic fields wrong: %+v", bot)
	}
	if len(bot.Users) != 1 || bot.Users[0].ID != userID {
		t.Errorf("bot.Users = %+v; want one user id %d", bot.Users, userID)
	}
	if len(bot.Archs) != 1 || bot.Archs[0].Name != "x86" {
		t.Errorf("bot.Archs = %+v; want [x86]", bot.Archs)
	}
	if !bot.Archs[0].Enabled {
		t.Error("linked arch should be Enabled")
	}
	if len(bot.Bits) != 1 || bot.Bits[0].Name != "32" {
		t.Errorf("bot.Bits = %+v; want [32]", bot.Bits)
	}

	// replacing links should not accumulate duplicates
	if err := s.LinkBotArchs(botID, []int{x86ID}); err != nil {
		t.Fatalf("re-LinkBotArchs: %v", err)
	}
	bot, _ = s.BotByID(botID)
	if len(bot.Archs) != 1 {
		t.Errorf("after re-link bot.Archs len = %d; want 1", len(bot.Archs))
	}

	// emptying links
	if err := s.LinkBotBits(botID, nil); err != nil {
		t.Fatalf("LinkBotBits(nil): %v", err)
	}
	bot, _ = s.BotByID(botID)
	if len(bot.Bits) != 0 {
		t.Errorf("after clearing, bot.Bits len = %d; want 0", len(bot.Bits))
	}

	// UpdateBot
	if err := s.UpdateBot(botID, "warrior2", "die 0"); err != nil {
		t.Fatalf("UpdateBot: %v", err)
	}
	bot, _ = s.BotByID(botID)
	if bot.Name != "warrior2" || bot.Source != "die 0" {
		t.Errorf("after UpdateBot: %+v", bot)
	}

	// AllBots
	bots, err := s.AllBots()
	if err != nil || len(bots) != 1 {
		t.Errorf("AllBots len = %d, %v; want 1", len(bots), err)
	}

	// AllBotsWithUsers
	withUsers, err := s.AllBotsWithUsers()
	if err != nil {
		t.Fatalf("AllBotsWithUsers: %v", err)
	}
	if len(withUsers) != 1 || len(withUsers[0].Users) != 1 || withUsers[0].Users[0].Name != "bob" {
		t.Errorf("AllBotsWithUsers = %+v; want one bot owned by bob", withUsers)
	}

	// BotsForUsername / BotsForUserID
	if bs, _ := s.BotsForUsername("bob"); len(bs) != 1 {
		t.Errorf("BotsForUsername len = %d; want 1", len(bs))
	}
	if bs, _ := s.BotsForUserID(userID); len(bs) != 1 {
		t.Errorf("BotsForUserID len = %d; want 1", len(bs))
	}
}

func TestAllBotsWithUsersNoOwner(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.InsertBot("orphan", "src"); err != nil {
		t.Fatalf("InsertBot: %v", err)
	}
	bots, err := s.AllBotsWithUsers()
	if err != nil {
		t.Fatalf("AllBotsWithUsers: %v", err)
	}
	if len(bots) != 1 {
		t.Fatalf("want 1 bot, got %d", len(bots))
	}
	if len(bots[0].Users) != 0 {
		t.Errorf("orphan bot should have no users, got %+v", bots[0].Users)
	}
}

func TestBattleLifecycle(t *testing.T) {
	s := newTestStore(t)

	ownerID, err := s.InsertUser("owner", []byte("h"))
	if err != nil {
		t.Fatalf("InsertUser: %v", err)
	}

	battleID, err := s.InsertBattle(model.Battle{
		Name:      "arena1",
		Public:    true,
		MaxRounds: 200,
		ArenaSize: 8192,
	}, ownerID)
	if err != nil {
		t.Fatalf("InsertBattle: %v", err)
	}

	// the owner-link fix: owner must be retrievable via deep fetch
	b, err := s.BattleByID(battleID)
	if err != nil {
		t.Fatalf("BattleByID: %v", err)
	}
	if b.Name != "arena1" || !b.Public || b.MaxRounds != 200 || b.ArenaSize != 8192 {
		t.Errorf("battle basic fields wrong: %+v", b)
	}
	if len(b.Owners) != 1 || b.Owners[0].ID != ownerID {
		t.Fatalf("InsertBattle owner-link fix failed: owners = %+v", b.Owners)
	}

	// link a bot
	botID, _ := s.InsertBot("b1", "src")
	if err := s.LinkBattleBot(botID, battleID); err != nil {
		t.Fatalf("LinkBattleBot: %v", err)
	}

	// link archs / bits / additional owner via the replace-methods
	archs, _ := s.AllArchs()
	bits, _ := s.AllBits()
	if err := s.LinkBattleArchs(battleID, []int{archs[0].ID, archs[1].ID}); err != nil {
		t.Fatalf("LinkBattleArchs: %v", err)
	}
	if err := s.LinkBattleBits(battleID, []int{bits[0].ID}); err != nil {
		t.Fatalf("LinkBattleBits: %v", err)
	}

	secondOwner, _ := s.InsertUser("owner2", []byte("h"))
	if err := s.LinkBattleOwners(battleID, []int{ownerID, secondOwner}); err != nil {
		t.Fatalf("LinkBattleOwners: %v", err)
	}

	b, err = s.BattleByID(battleID)
	if err != nil {
		t.Fatalf("BattleByID deep: %v", err)
	}
	if len(b.Bots) != 1 || b.Bots[0].ID != botID {
		t.Errorf("battle bots = %+v; want one bot id %d", b.Bots, botID)
	}
	if len(b.Archs) != 2 {
		t.Errorf("battle archs len = %d; want 2", len(b.Archs))
	}
	if len(b.Bits) != 1 {
		t.Errorf("battle bits len = %d; want 1", len(b.Bits))
	}
	if len(b.Owners) != 2 {
		t.Errorf("battle owners len = %d; want 2", len(b.Owners))
	}

	// UpdateBattle
	b.Name = "arena1-renamed"
	b.MaxRounds = 50
	if err := s.UpdateBattle(b); err != nil {
		t.Fatalf("UpdateBattle: %v", err)
	}
	got, _ := s.BattleByID(battleID)
	if got.Name != "arena1-renamed" || got.MaxRounds != 50 {
		t.Errorf("after UpdateBattle: %+v", got)
	}

	// SaveBattleRawOutput
	if err := s.SaveBattleRawOutput(battleID, "the transcript"); err != nil {
		t.Fatalf("SaveBattleRawOutput: %v", err)
	}
	got, _ = s.BattleByID(battleID)
	if got.RawOutput != "the transcript" {
		t.Errorf("raw_output = %q; want 'the transcript'", got.RawOutput)
	}

	// AllBattles
	all, err := s.AllBattles()
	if err != nil || len(all) != 1 {
		t.Errorf("AllBattles len = %d, %v; want 1", len(all), err)
	}
}

func TestUnlinkUserBotsFromBattle(t *testing.T) {
	s := newTestStore(t)

	userID, _ := s.InsertUser("u", []byte("h"))
	battleID, _ := s.InsertBattle(model.Battle{Name: "ar"}, userID)

	botA, _ := s.InsertBot("a", "")
	botB, _ := s.InsertBot("b", "")
	s.LinkUserBot("u", botA)
	s.LinkUserBot("u", botB)
	s.LinkBattleBot(botA, battleID)
	s.LinkBattleBot(botB, battleID)

	// a bot NOT owned by the user should survive
	other, _ := s.InsertUser("other", []byte("h"))
	botC, _ := s.InsertBot("c", "")
	s.LinkUserBot("other", botC)
	s.LinkBattleBot(botC, battleID)
	_ = other

	if b, _ := s.BattleByID(battleID); len(b.Bots) != 3 {
		t.Fatalf("setup: expected 3 bots, got %d", len(b.Bots))
	}

	if err := s.UnlinkUserBotsFromBattle(userID, battleID); err != nil {
		t.Fatalf("UnlinkUserBotsFromBattle: %v", err)
	}

	b, _ := s.BattleByID(battleID)
	if len(b.Bots) != 1 || b.Bots[0].ID != botC {
		t.Errorf("after unlink, battle bots = %+v; want only botC (%d)", b.Bots, botC)
	}
}

func TestDeleteBattle(t *testing.T) {
	s := newTestStore(t)

	ownerID, _ := s.InsertUser("o", []byte("h"))
	battleID, _ := s.InsertBattle(model.Battle{Name: "doomed"}, ownerID)

	botID, _ := s.InsertBot("b", "")
	s.LinkBattleBot(botID, battleID)
	archs, _ := s.AllArchs()
	bits, _ := s.AllBits()
	s.LinkBattleArchs(battleID, []int{archs[0].ID})
	s.LinkBattleBits(battleID, []int{bits[0].ID})

	if err := s.DeleteBattle(battleID); err != nil {
		t.Fatalf("DeleteBattle: %v", err)
	}

	if _, err := s.BattleByID(battleID); err == nil {
		t.Error("expected error fetching deleted battle")
	}

	// verify relation rows are gone
	assertCount := func(query string) int {
		var n int
		if err := s.db.QueryRow(query, battleID).Scan(&n); err != nil {
			t.Fatalf("count query %q: %v", query, err)
		}
		return n
	}
	if n := assertCount("SELECT COUNT(*) FROM owner_battle_rel WHERE battle_id=?"); n != 0 {
		t.Errorf("owner_battle_rel rows = %d; want 0", n)
	}
	if n := assertCount("SELECT COUNT(*) FROM bot_battle_rel WHERE battle_id=?"); n != 0 {
		t.Errorf("bot_battle_rel rows = %d; want 0", n)
	}
	if n := assertCount("SELECT COUNT(*) FROM arch_battle_rel WHERE battle_id=?"); n != 0 {
		t.Errorf("arch_battle_rel rows = %d; want 0", n)
	}
	if n := assertCount("SELECT COUNT(*) FROM bit_battle_rel WHERE battle_id=?"); n != 0 {
		t.Errorf("bit_battle_rel rows = %d; want 0", n)
	}
}
