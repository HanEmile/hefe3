package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"git.emile.space/r2wars-web/internal/model"
)

func (s *Store) InsertBot(bot model.Bot) (int, error) {
	res, err := s.db.Exec("INSERT INTO bots VALUES(NULL,?,?,?);", time.Now(), bot.Name, bot.Source)
	if err != nil {
		return 0, fmt.Errorf("store: inserting bot: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: reading new bot id: %w", err)
	}
	return int(id), nil
}

func (s *Store) UpdateBot(bot model.Bot) error {
	_, err := s.db.Exec("UPDATE bots SET name=?, source=? WHERE id=?", bot.Name, bot.Source, bot.ID)
	if err != nil {
		return fmt.Errorf("store: updating bot: %w", err)
	}
	return nil
}

// GetBotByID fetches a bot along with the users that own it and the archs
// and bits it has been configured for.
func (s *Store) GetBotByID(id int) (model.Bot, error) {
	var bot model.Bot
	var ownerIDs, ownerNames string
	var archIDs, archNames string
	var bitIDs, bitNames string

	err := s.db.QueryRow(`
	SELECT
		bo.id, bo.name, bo.source,
		COALESCE(group_concat(DISTINCT ub.user_id), ""),
		COALESCE(group_concat(DISTINCT us.name), ""),
		COALESCE(group_concat(DISTINCT ab.arch_id), ""),
		COALESCE(group_concat(DISTINCT ar.name), ""),
		COALESCE(group_concat(DISTINCT bb.bit_id), ""),
		COALESCE(group_concat(DISTINCT bi.name), "")
	FROM bots bo

	LEFT JOIN user_bot_rel ub ON ub.bot_id = bo.id
	LEFT JOIN users us ON us.id = ub.user_id

	LEFT JOIN arch_bot_rel ab ON ab.bot_id = bo.id
	LEFT JOIN archs ar ON ar.id = ab.arch_id

	LEFT JOIN bit_bot_rel bb ON bb.bot_id = bo.id
	LEFT JOIN bits bi ON bi.id = bb.bit_id

	WHERE bo.id=?
	GROUP BY bo.id;
	`, id).Scan(&bot.ID, &bot.Name, &bot.Source,
		&ownerIDs, &ownerNames,
		&archIDs, &archNames,
		&bitIDs, &bitNames)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Bot{}, ErrNotFound
	}
	if err != nil {
		return model.Bot{}, fmt.Errorf("store: getting bot %d: %w", id, err)
	}

	users, err := zipUsers(ownerIDs, ownerNames)
	if err != nil {
		return model.Bot{}, err
	}
	bot.Users = users

	archs, err := zipArchs(archIDs, archNames)
	if err != nil {
		return model.Bot{}, err
	}
	bot.Archs = archs

	bits, err := zipBits(bitIDs, bitNames)
	if err != nil {
		return model.Bot{}, err
	}
	bot.Bits = bits

	return bot, nil
}

func (s *Store) GetAllBots() ([]model.Bot, error) {
	// source is selected (even though bot listing pages don't show it) so
	// this can share scanBots with every other caller that does need it,
	// rather than duplicating the scan loop for a 2-vs-3-column mismatch.
	rows, err := s.db.Query("SELECT id, name, source FROM bots;")
	if err != nil {
		return nil, fmt.Errorf("store: getting all bots: %w", err)
	}
	defer rows.Close()
	return scanBots(rows)
}

// GetAllBotsWithUsers returns every bot along with the users that own it,
// for listing pages.
func (s *Store) GetAllBotsWithUsers() ([]model.Bot, error) {
	rows, err := s.db.Query(`SELECT
		b.id, b.name, b.source,
		COALESCE(group_concat(DISTINCT ub.user_id), ""),
		COALESCE(group_concat(DISTINCT u.name), "")
	FROM bots b
	LEFT JOIN user_bot_rel ub ON ub.bot_id = b.id
	LEFT JOIN users u ON ub.user_id = u.id
	GROUP BY b.id;`)
	if err != nil {
		return nil, fmt.Errorf("store: getting bots with users: %w", err)
	}
	defer rows.Close()

	bots := []model.Bot{}
	for rows.Next() {
		var bot model.Bot
		var userIDs, userNames string
		if err := rows.Scan(&bot.ID, &bot.Name, &bot.Source, &userIDs, &userNames); err != nil {
			return nil, fmt.Errorf("store: scanning bot with users: %w", err)
		}
		users, err := zipUsers(userIDs, userNames)
		if err != nil {
			return nil, err
		}
		bot.Users = users
		bots = append(bots, bot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating bots with users: %w", err)
	}
	return bots, nil
}

// LinkArchIDsToBot replaces the set of archs a bot is validated against.
func (s *Store) LinkArchIDsToBot(botID int, archIDs []int) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM arch_bot_rel WHERE bot_id=?;", botID); err != nil {
			return fmt.Errorf("store: clearing bot archs: %w", err)
		}
		if err := linkMany(tx, "arch_bot_rel", "arch_id", "bot_id", botID, archIDs); err != nil {
			return fmt.Errorf("store: linking bot archs: %w", err)
		}
		return nil
	})
}

// LinkBitIDsToBot replaces the set of bits a bot is validated against.
func (s *Store) LinkBitIDsToBot(botID int, bitIDs []int) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM bit_bot_rel WHERE bot_id=?;", botID); err != nil {
			return fmt.Errorf("store: clearing bot bits: %w", err)
		}
		if err := linkMany(tx, "bit_bot_rel", "bit_id", "bot_id", botID, bitIDs); err != nil {
			return fmt.Errorf("store: linking bot bits: %w", err)
		}
		return nil
	})
}

func zipUsers(ids, names string) ([]model.User, error) {
	idList, err := splitIDList(ids)
	if err != nil {
		return nil, err
	}
	nameList := splitNameList(names)
	users := make([]model.User, len(idList))
	for i := range idList {
		users[i] = model.User{ID: idList[i], Name: nameList[i]}
	}
	return users, nil
}

func zipArchs(ids, names string) ([]model.Arch, error) {
	idList, err := splitIDList(ids)
	if err != nil {
		return nil, err
	}
	nameList := splitNameList(names)
	archs := make([]model.Arch, len(idList))
	for i := range idList {
		archs[i] = model.Arch{ID: idList[i], Name: nameList[i], Enabled: true}
	}
	return archs, nil
}

func zipBits(ids, names string) ([]model.Bit, error) {
	idList, err := splitIDList(ids)
	if err != nil {
		return nil, err
	}
	nameList := splitNameList(names)
	bits := make([]model.Bit, len(idList))
	for i := range idList {
		bits[i] = model.Bit{ID: idList[i], Name: nameList[i], Enabled: true}
	}
	return bits, nil
}
