package store

import (
	"fmt"

	"git.emile.space/r2wars-web/internal/model"
)

func (s *Store) GetAllArchs() ([]model.Arch, error) {
	rows, err := s.db.Query("SELECT id, name FROM archs")
	if err != nil {
		return nil, fmt.Errorf("store: getting archs: %w", err)
	}
	defer rows.Close()

	archs := []model.Arch{}
	for rows.Next() {
		var a model.Arch
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return nil, fmt.Errorf("store: scanning arch: %w", err)
		}
		archs = append(archs, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating archs: %w", err)
	}
	return archs, nil
}
