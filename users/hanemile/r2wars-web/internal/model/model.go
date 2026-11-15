// Package model holds the plain data types shared by the store, battle
// engine and web layers. Keeping them dependency-free avoids import cycles
// between those packages.
package model

// User is a registered player. There is no password: accounts authenticate
// by email magic link (see internal/store's magic link methods), so Email
// is required for any user that needs to log in.
type User struct {
	ID    int
	Name  string
	Email string
}

// Arch is a radare2-supported architecture (e.g. "x86", "arm").
type Arch struct {
	ID      int
	Name    string
	Enabled bool // only set when rendering a form against a specific bot/battle
}

// Bit is a word size supported by radare2 (8, 16, 32, 64).
type Bit struct {
	ID      int
	Name    string
	Enabled bool // only set when rendering a form against a specific bot/battle
}

// Bot is a piece of submitted assembly source plus the archs/bits it has
// been validated against.
type Bot struct {
	ID     int
	Name   string
	Source string
	Users  []User

	Archs []Arch
	Bits  []Bit
}

// Battle is an arena bots can be submitted to and run against each other.
type Battle struct {
	ID        int
	Name      string
	Bots      []Bot
	Owners    []User
	Public    bool
	Archs     []Arch
	Bits      []Bit
	RawOutput string
	MaxRounds int
	ArenaSize int
}
