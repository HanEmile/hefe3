package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"git.emile.space/r2wars-web/internal/model"
)

// InsertBattle creates a battle and links owner as its sole initial owner,
// atomically - either both happen or neither does.
func (s *Store) InsertBattle(battle model.Battle, owner model.User) (int, error) {
	var id int64
	err := s.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`
			INSERT INTO battles
			VALUES(NULL,?,?,?,?,?,?)
			`, time.Now(),
			battle.Name,
			battle.Public,
			battle.RawOutput,
			battle.MaxRounds,
			battle.ArenaSize)
		if err != nil {
			return fmt.Errorf("inserting battle: %w", err)
		}

		id, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("reading new battle id: %w", err)
		}

		if _, err := tx.Exec("INSERT INTO owner_battle_rel VALUES (?, ?)", owner.ID, id); err != nil {
			return fmt.Errorf("linking owner to battle: %w", err)
		}
		return nil
	})
	if err != nil {
		return -1, fmt.Errorf("store: %w", err)
	}
	return int(id), nil
}

func (s *Store) UpdateBattle(battle model.Battle) error {
	_, err := s.db.Exec(`
		UPDATE battles
		SET name=?, public=?, arena_size=?, max_rounds=?
		WHERE id=?`,
		battle.Name,
		battle.Public,
		battle.ArenaSize,
		battle.MaxRounds,
		battle.ID)
	if err != nil {
		return fmt.Errorf("store: updating battle: %w", err)
	}
	return nil
}

func (s *Store) LinkBotBattle(botID, battleID int) error {
	_, err := s.db.Exec("INSERT INTO bot_battle_rel VALUES (?, ?)", botID, battleID)
	if err != nil {
		return fmt.Errorf("store: linking bot to battle: %w", err)
	}
	return nil
}

// UnlinkAllBotsForUserFromBattle removes every bot owned by userID from
// battleID, so a resubmission can start clean.
func (s *Store) UnlinkAllBotsForUserFromBattle(userID, battleID int) error {
	_, err := s.db.Exec(`
	DELETE FROM bot_battle_rel
	WHERE battle_id=? AND bot_id IN
		(SELECT b.id
		 FROM bots b
		 JOIN user_bot_rel ub_rel ON ub_rel.bot_id = b.id
		 WHERE ub_rel.user_id=?)`, battleID, userID)
	if err != nil {
		return fmt.Errorf("store: unlinking user's bots from battle: %w", err)
	}
	return nil
}

func (s *Store) LinkArchIDsToBattle(battleID int, archIDs []int) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM arch_battle_rel WHERE battle_id=?", battleID); err != nil {
			return fmt.Errorf("clearing battle archs: %w", err)
		}
		if err := linkMany(tx, "arch_battle_rel", "arch_id", "battle_id", battleID, archIDs); err != nil {
			return fmt.Errorf("linking battle archs: %w", err)
		}
		return nil
	})
}

func (s *Store) LinkBitIDsToBattle(battleID int, bitIDs []int) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM bit_battle_rel WHERE battle_id=?", battleID); err != nil {
			return fmt.Errorf("clearing battle bits: %w", err)
		}
		if err := linkMany(tx, "bit_battle_rel", "bit_id", "battle_id", battleID, bitIDs); err != nil {
			return fmt.Errorf("linking battle bits: %w", err)
		}
		return nil
	})
}

func (s *Store) LinkOwnerIDsToBattle(battleID int, ownerIDs []int) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM owner_battle_rel WHERE battle_id=?", battleID); err != nil {
			return fmt.Errorf("clearing battle owners: %w", err)
		}
		if err := linkMany(tx, "owner_battle_rel", "user_id", "battle_id", battleID, ownerIDs); err != nil {
			return fmt.Errorf("linking battle owners: %w", err)
		}
		return nil
	})
}

func (s *Store) GetAllBattles() ([]model.Battle, error) {
	rows, err := s.db.Query("SELECT id, name FROM battles;")
	if err != nil {
		return nil, fmt.Errorf("store: getting all battles: %w", err)
	}
	defer rows.Close()

	battles := []model.Battle{}
	for rows.Next() {
		var b model.Battle
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, fmt.Errorf("store: scanning battle: %w", err)
		}
		battles = append(battles, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating battles: %w", err)
	}
	return battles, nil
}

// GetBattleByIDDeep fetches a battle along with every bot, user, arch, bit
// and owner linked to it.
//
// TODO: go deeper - battle -> bot -> arch (the bot's own archs/bits aren't
// fetched here, only their id/name).
// GetBattleByIDDeep fetches a battle along with every bot, arch, bit and
// owner linked to it.
//
// This used to be one giant query joining all four relations at once and
// pulling each side out with GROUP_CONCAT(DISTINCT ...). That silently
// breaks the moment two related rows share a display name (e.g. two bots
// both named "bot"): DISTINCT collapses the name list but not the id list,
// desyncing them and panicking on the zip. Archs/bits/users all happen to
// have a UNIQUE name constraint, which is what hid this for everything but
// bots. Running one query per relation costs a few more round trips but
// can't suffer that class of bug.
//
// TODO: go deeper - battle -> bot -> arch (the bot's own archs/bits aren't
// fetched here, only their id/name).
func (s *Store) GetBattleByIDDeep(id int) (model.Battle, error) {
	var battle model.Battle

	// COALESCE provides defaults for battles created before max_rounds/
	// arena_size existed or were left NULL.
	err := s.db.QueryRow(`
		SELECT id, name, public,
			COALESCE(raw_output, ""),
			COALESCE(max_rounds, 100),
			COALESCE(arena_size, 4096)
		FROM battles
		WHERE id=?`, id,
	).Scan(&battle.ID, &battle.Name, &battle.Public, &battle.RawOutput, &battle.MaxRounds, &battle.ArenaSize)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Battle{}, ErrNotFound
	}
	if err != nil {
		return model.Battle{}, fmt.Errorf("store: getting battle %d: %w", id, err)
	}

	bots, err := s.getBattleBots(id)
	if err != nil {
		return model.Battle{}, err
	}
	battle.Bots = bots

	archs, err := s.getBattleArchs(id)
	if err != nil {
		return model.Battle{}, err
	}
	battle.Archs = archs

	bits, err := s.getBattleBits(id)
	if err != nil {
		return model.Battle{}, err
	}
	battle.Bits = bits

	owners, err := s.getBattleOwners(id)
	if err != nil {
		return model.Battle{}, err
	}
	battle.Owners = owners

	return battle, nil
}

func (s *Store) getBattleBots(battleID int) ([]model.Bot, error) {
	rows, err := s.db.Query(`
		SELECT bo.id, bo.name
		FROM bot_battle_rel bb
		JOIN bots bo ON bo.id = bb.bot_id
		WHERE bb.battle_id=?`, battleID)
	if err != nil {
		return nil, fmt.Errorf("store: getting battle %d bots: %w", battleID, err)
	}
	defer rows.Close()

	bots := []model.Bot{}
	for rows.Next() {
		var b model.Bot
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, fmt.Errorf("store: scanning battle bot: %w", err)
		}
		bots = append(bots, b)
	}
	return bots, rows.Err()
}

func (s *Store) getBattleArchs(battleID int) ([]model.Arch, error) {
	rows, err := s.db.Query(`
		SELECT ar.id, ar.name
		FROM arch_battle_rel ab
		JOIN archs ar ON ar.id = ab.arch_id
		WHERE ab.battle_id=?`, battleID)
	if err != nil {
		return nil, fmt.Errorf("store: getting battle %d archs: %w", battleID, err)
	}
	defer rows.Close()

	archs := []model.Arch{}
	for rows.Next() {
		var a model.Arch
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return nil, fmt.Errorf("store: scanning battle arch: %w", err)
		}
		a.Enabled = true
		archs = append(archs, a)
	}
	return archs, rows.Err()
}

func (s *Store) getBattleBits(battleID int) ([]model.Bit, error) {
	rows, err := s.db.Query(`
		SELECT bi.id, bi.name
		FROM bit_battle_rel bb
		JOIN bits bi ON bi.id = bb.bit_id
		WHERE bb.battle_id=?`, battleID)
	if err != nil {
		return nil, fmt.Errorf("store: getting battle %d bits: %w", battleID, err)
	}
	defer rows.Close()

	bits := []model.Bit{}
	for rows.Next() {
		var b model.Bit
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, fmt.Errorf("store: scanning battle bit: %w", err)
		}
		b.Enabled = true
		bits = append(bits, b)
	}
	return bits, rows.Err()
}

func (s *Store) getBattleOwners(battleID int) ([]model.User, error) {
	rows, err := s.db.Query(`
		SELECT u.id, u.name
		FROM owner_battle_rel ob
		JOIN users u ON u.id = ob.user_id
		WHERE ob.battle_id=?`, battleID)
	if err != nil {
		return nil, fmt.Errorf("store: getting battle %d owners: %w", battleID, err)
	}
	defer rows.Close()

	owners := []model.User{}
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Name); err != nil {
			return nil, fmt.Errorf("store: scanning battle owner: %w", err)
		}
		owners = append(owners, u)
	}
	return owners, rows.Err()
}

func (s *Store) UpdateBattleRawOutput(battleID int, rawOutput string) error {
	_, err := s.db.Exec("UPDATE battles SET raw_output=? WHERE id=?", rawOutput, battleID)
	if err != nil {
		return fmt.Errorf("store: saving battle output: %w", err)
	}
	return nil
}

// DeleteBattleByID deletes a battle and every relation row pointing at it.
func (s *Store) DeleteBattleByID(battleID int) error {
	return s.withTx(func(tx *sql.Tx) error {
		for _, stmt := range []string{
			"DELETE FROM user_battle_rel WHERE battle_id = ?",
			"DELETE FROM owner_battle_rel WHERE battle_id = ?",
			"DELETE FROM bot_battle_rel WHERE battle_id = ?",
			"DELETE FROM arch_battle_rel WHERE battle_id = ?",
			"DELETE FROM bit_battle_rel WHERE battle_id = ?",
			"DELETE FROM battles WHERE id = ?",
		} {
			if _, err := tx.Exec(stmt, battleID); err != nil {
				return fmt.Errorf("deleting battle %d (%q): %w", battleID, stmt, err)
			}
		}
		return nil
	})
}
