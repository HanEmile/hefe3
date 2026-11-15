// Package model holds the core domain types shared across the store, engine
// and web layers. These are plain data carriers with no behaviour and no
// dependencies on the rest of the application, so every other package can
// import them without creating import cycles.
package model

// User is a player. PasswordHash is only populated where it is actually
// needed (login / registration); list and join queries leave it nil.
type User struct {
	ID           int
	Name         string
	PasswordHash []byte
}

// Bot is a program submitted by a user. A bot is assembled for exactly one
// arch/bits combination when it fights, but the schema allows linking several
// so the UI can offer a choice.
type Bot struct {
	ID     int
	Name   string
	Source string
	Users  []User
	Archs  []Arch
	Bits   []Bit
}

// Arch is a radare2 assembler architecture (e.g. "x86").
type Arch struct {
	ID      int
	Name    string
	Enabled bool // set by the web layer when rendering selection lists
}

// Bit is a radare2 word size (8/16/32/64), stored as its string name.
type Bit struct {
	ID      int
	Name    string
	Enabled bool // set by the web layer when rendering selection lists
}

// Battle is an arena in which bots fight. Owners may edit it; Bots are the
// programs that have been submitted to it. RawOutput holds the transcript of
// the most recent run.
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
