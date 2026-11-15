package store

// schema is the canonical DDL for the r2wars-web database. It is reproduced
// faithfully from the original db.go `create` const, including the archs/bits
// seed data. All statements are idempotent (IF NOT EXISTS / INSERT OR IGNORE)
// so applying it on every New() is safe.
const schema = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER NOT NULL PRIMARY KEY,
	created_at DATETIME NOT NULL,
	name TEXT,
	passwordHash TEXT
);
CREATE TABLE IF NOT EXISTS bots (
	id INTEGER NOT NULL PRIMARY KEY,
	created_at DATETIME NOT NULL,
	name TEXT,
	source TEXT
);
CREATE TABLE IF NOT EXISTS battles (
	id INTEGER NOT NULL PRIMARY KEY,
	created_at DATETIME NOT NULL,
	name TEXT,
	public BOOLEAN,
	raw_output TEXT,
	max_rounds INTEGER,
	arena_size INTEGER
);
CREATE TABLE IF NOT EXISTS archs (
	id INTEGER NOT NULL PRIMARY KEY,
	name TEXT,
    UNIQUE(name)
);
INSERT OR IGNORE INTO archs (name) VALUES
	("null"), ("6502"), ("6502.cs"), ("8051"), ("alpha"), ("amd29k"),
	("any.as"), ("any.vasm"), ("arm.nz"), ("arm"), ("avr"), ("bf"), ("bpf.mr"),
	("bpf"), ("chip8"), ("cr16"), ("cris"), ("dalvik"), ("dis"), ("ebc"),
	("evm"), ("fslsp"), ("gb"), ("h8300"), ("i4004"), ("i8080"), ("java"),
	("jdh8"), ("kvx"), ("lh5801"), ("lm32"), ("m680x"), ("m68k"), ("mcore"),
	("mcs96"), ("mips"), ("msp430"), ("nios2"), ("or1k"), ("pic"), ("ppc"),
	("propeller"), ("pyc"), ("riscv"), ("riscv.cs"), ("rsp"), ("s390"),
	("sh"), ("sh.cs"), ("snes"), ("sparc"), ("tms320"), ("tricore"),
	("tricore.cs"), ("v850"), ("vax"), ("wasm"), ("ws"), ("x86"), ("x86.nz"),
	("xap"), ("xcore"), ("arm.gnu"), ("lanai"), ("loongarch"), ("m68k.gnu"),
	("mips.gnu"), ("nds32"), ("pdp11"), ("ppc.gnu"), ("s390.gnu"),
	("sparc.gnu"), ("xtensa"), ("z80")
;

CREATE TABLE IF NOT EXISTS bits (
	id INTEGER NOT NULL PRIMARY KEY,
	name TEXT,
    UNIQUE(name)
);
INSERT OR IGNORE INTO bits (name) VALUES
	("8"), ("16"), ("32"), ("64")
;

CREATE TABLE IF NOT EXISTS user_bot_rel (
	user_id INTEGER,
	bot_id INTEGER,
	PRIMARY KEY(user_id, bot_id)
);
CREATE TABLE IF NOT EXISTS arch_bot_rel (
	arch_id INTEGER,
	bot_id INTEGER,
	PRIMARY KEY(arch_id, bot_id)
);
CREATE TABLE IF NOT EXISTS bit_bot_rel (
	bit_id INTEGER,
	bot_id INTEGER,
	PRIMARY KEY(bit_id, bot_id)
);

CREATE TABLE IF NOT EXISTS user_battle_rel (
	user_id INTEGER,
	battle_id INTEGER,
	PRIMARY KEY(user_id, battle_id)
);
CREATE TABLE IF NOT EXISTS owner_battle_rel (
	user_id INTEGER,
	battle_id INTEGER,
	PRIMARY KEY(user_id, battle_id)
);
CREATE TABLE IF NOT EXISTS bot_battle_rel (
	bot_id INTEGER,
	battle_id INTEGER,
	PRIMARY KEY(bot_id, battle_id)
);
CREATE TABLE IF NOT EXISTS arch_battle_rel (
	arch_id INTEGER,
	battle_id INTEGER,
	PRIMARY KEY(arch_id, battle_id)
);
CREATE TABLE IF NOT EXISTS bit_battle_rel (
	bit_id INTEGER,
	battle_id INTEGER,
	PRIMARY KEY(bit_id, battle_id)
);
`
