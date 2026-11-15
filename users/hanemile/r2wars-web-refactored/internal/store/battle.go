package store

import (
	"git.emile.space/r2wars-web/internal/model"
)

// InsertBattle inserts a battle row and its owner link in a single
// transaction. This fixes the original bug where the owner link used
// `battle.ID` (always 0 on a fresh struct) instead of the newly-inserted id.
func (s *Store) InsertBattle(b model.Battle, ownerID int) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return -1, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO battles
		VALUES(NULL,datetime('now'),?,?,?,?,?)`,
		b.Name, b.Public, b.RawOutput, b.MaxRounds, b.ArenaSize)
	if err != nil {
		return -1, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return -1, err
	}

	// Link the owner using the freshly-created battle id.
	if _, err := tx.Exec("INSERT INTO owner_battle_rel VALUES (?, ?)", ownerID, id); err != nil {
		return -1, err
	}

	if err := tx.Commit(); err != nil {
		return -1, err
	}
	return int(id), nil
}

// UpdateBattle updates a battle's editable fields.
func (s *Store) UpdateBattle(b model.Battle) error {
	_, err := s.db.Exec(`
		UPDATE battles
		SET name=?, public=?, arena_size=?, max_rounds=?
		WHERE id=?`,
		b.Name, b.Public, b.ArenaSize, b.MaxRounds, b.ID)
	return err
}

// BattleByID returns a battle with its Bots (id+name), member users, Owners,
// Archs and Bits populated. Each relation is fetched with a dedicated JOIN
// query and scanned row-by-row (no group_concat/Split). COALESCE supplies the
// same defaults the original used for raw_output/max_rounds/arena_size.
func (s *Store) BattleByID(id int) (model.Battle, error) {
	b := model.Battle{
		Bots:   []model.Bot{},
		Owners: []model.User{},
		Archs:  []model.Arch{},
		Bits:   []model.Bit{},
	}

	err := s.db.QueryRow(`
		SELECT
			id, name, public,
			COALESCE(raw_output, ""),
			COALESCE(max_rounds, 100),
			COALESCE(arena_size, 4096)
		FROM battles
		WHERE id=?`, id).
		Scan(&b.ID, &b.Name, &b.Public, &b.RawOutput, &b.MaxRounds, &b.ArenaSize)
	if err != nil {
		return model.Battle{}, err
	}

	// bots in the battle (id+name)
	botRows, err := s.db.Query(`
		SELECT bo.id, bo.name
		FROM bots bo
		JOIN bot_battle_rel bb ON bb.bot_id = bo.id
		WHERE bb.battle_id=?`, id)
	if err != nil {
		return model.Battle{}, err
	}
	for botRows.Next() {
		var bot model.Bot
		if err := botRows.Scan(&bot.ID, &bot.Name); err != nil {
			botRows.Close()
			return model.Battle{}, err
		}
		b.Bots = append(b.Bots, bot)
	}
	botRows.Close()
	if err := botRows.Err(); err != nil {
		return model.Battle{}, err
	}

	// Note: model.Battle has no "members" field, so the user_battle_rel
	// relation the original fetched is not surfaced here; only Owners are.

	// owners (owner_battle_rel)
	owners, err := s.battleRelUsers("owner_battle_rel", id)
	if err != nil {
		return model.Battle{}, err
	}
	if owners != nil {
		b.Owners = owners
	}

	// archs
	archRows, err := s.db.Query(`
		SELECT a.id, a.name
		FROM archs a
		JOIN arch_battle_rel ab ON ab.arch_id = a.id
		WHERE ab.battle_id=?`, id)
	if err != nil {
		return model.Battle{}, err
	}
	archs, err := scanArchs(archRows)
	archRows.Close()
	if err != nil {
		return model.Battle{}, err
	}
	if archs != nil {
		b.Archs = archs
	}

	// bits
	bitRows, err := s.db.Query(`
		SELECT bi.id, bi.name
		FROM bits bi
		JOIN bit_battle_rel bb ON bb.bit_id = bi.id
		WHERE bb.battle_id=?`, id)
	if err != nil {
		return model.Battle{}, err
	}
	bits, err := scanBits(bitRows)
	bitRows.Close()
	if err != nil {
		return model.Battle{}, err
	}
	if bits != nil {
		b.Bits = bits
	}

	return b, nil
}

// battleRelUsers returns the users linked to a battle through one of the
// user-id relation tables (user_battle_rel or owner_battle_rel).
func (s *Store) battleRelUsers(table string, battleID int) ([]model.User, error) {
	// table is a caller-controlled constant, never user input.
	rows, err := s.db.Query(`
		SELECT u.id, u.name
		FROM users u
		JOIN `+table+` r ON r.user_id = u.id
		WHERE r.battle_id=?`, battleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsers(rows)
}

// AllBattles returns every battle (id+name only).
func (s *Store) AllBattles() ([]model.Battle, error) {
	rows, err := s.db.Query("SELECT id, name FROM battles;")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var battles []model.Battle
	for rows.Next() {
		var b model.Battle
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return battles, err
		}
		battles = append(battles, b)
	}
	return battles, rows.Err()
}

// LinkBattleBot links a bot into a battle.
func (s *Store) LinkBattleBot(botID, battleID int) error {
	_, err := s.db.Exec("INSERT INTO bot_battle_rel VALUES (?, ?)", botID, battleID)
	return err
}

// UnlinkUserBotsFromBattle removes every bot owned by the given user from the
// given battle. Ported from the original join-based DELETE, but scoped to the
// single battle (the original deleted the user's bots from *all* battles,
// which was a latent bug given the method name and call site).
func (s *Store) UnlinkUserBotsFromBattle(userID, battleID int) error {
	_, err := s.db.Exec(`
		DELETE FROM bot_battle_rel
		WHERE battle_id=?
		  AND bot_id IN (
			SELECT ub.bot_id
			FROM user_bot_rel ub
			WHERE ub.user_id=?
		  )`, battleID, userID)
	return err
}

// LinkBattleArchs replaces a battle's arch links in one transaction.
func (s *Store) LinkBattleArchs(battleID int, archIDs []int) error {
	return s.replaceLinks("arch_battle_rel", "arch_id", "battle_id", battleID, archIDs)
}

// LinkBattleBits replaces a battle's bit links in one transaction.
func (s *Store) LinkBattleBits(battleID int, bitIDs []int) error {
	return s.replaceLinks("bit_battle_rel", "bit_id", "battle_id", battleID, bitIDs)
}

// LinkBattleOwners replaces a battle's owner links in one transaction.
func (s *Store) LinkBattleOwners(battleID int, ownerIDs []int) error {
	return s.replaceLinks("owner_battle_rel", "user_id", "battle_id", battleID, ownerIDs)
}

// SaveBattleRawOutput stores the transcript of the most recent run.
func (s *Store) SaveBattleRawOutput(battleID int, raw string) error {
	_, err := s.db.Exec("UPDATE battles SET raw_output=? WHERE id=?", raw, battleID)
	return err
}

// DeleteBattle removes a battle and all of its relation rows in one
// transaction. This fixes the original bug which referenced a non-existent
// `battleid` column (the actual columns are `id` / `battle_id`).
func (s *Store) DeleteBattle(battleID int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		"DELETE FROM battles WHERE id=?",
		"DELETE FROM user_battle_rel WHERE battle_id=?",
		"DELETE FROM owner_battle_rel WHERE battle_id=?",
		"DELETE FROM bot_battle_rel WHERE battle_id=?",
		"DELETE FROM arch_battle_rel WHERE battle_id=?",
		"DELETE FROM bit_battle_rel WHERE battle_id=?",
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q, battleID); err != nil {
			return err
		}
	}

	return tx.Commit()
}
