package db

// SeedDefaultFeeds is a no-op placeholder.
// RSS feeds should be added manually by administrators through the web interface.
func (d *DB) SeedDefaultFeeds() error {
	// No default feeds - administrators can add feeds through the UI
	return nil
}
