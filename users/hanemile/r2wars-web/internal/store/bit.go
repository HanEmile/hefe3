package store

import (
	"fmt"

	"git.emile.space/r2wars-web/internal/model"
)

func (s *Store) GetAllBits() ([]model.Bit, error) {
	rows, err := s.db.Query("SELECT id, name FROM bits")
	if err != nil {
		return nil, fmt.Errorf("store: getting bits: %w", err)
	}
	defer rows.Close()

	bits := []model.Bit{}
	for rows.Next() {
		var b model.Bit
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, fmt.Errorf("store: scanning bit: %w", err)
		}
		bits = append(bits, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating bits: %w", err)
	}
	return bits, nil
}
