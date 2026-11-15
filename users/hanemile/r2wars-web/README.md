# r2wars

## nix

So I use nix for all kinds of things nowadays and so here as well.
You don't have to if you don't want to and that's ok.

The nix package lives right in this repo: `./default.nix`.

The NixOS module consuming it (`services.r2wars-web`) and the VM config
running https://r2wa.rs live in the `hefe3` repo:
`ops/modules/r2wars-web.nix` and `ops/vms/x86/r2wars/default.nix`.

If you want to clone from git.emile.space, you can currently do so like this:

```
git clone git://git.emile.space/r2wars-web.git
git clone git://git.emile.space/hefe.git
```

## Usage

```
; CGO_ENABLED=0 SESSION_KEY=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa go run ./cmd/r2wars-web --help
Usage of r2wars-web:
  -databasepath string
    	The path to the main database (default "./main.db")
  -h string
    	The host to listen on (shorthand) (default "127.0.0.1")
  -host string
    	The host to listen on (default "127.0.0.1")
  -logfilepath string
    	The path to the log file (default "./server.log")
  -p int
    	The port to listen on (shorthand) (default 8080)
  -port int
    	The port to listen on (default 8080)
  -sessiondbpath string
    	The path to the session database (default "./sessions.db")
  -templates string
    	The path to the templates used (default "./templates")
  -backupdir string
    	Directory periodic database backups are written to (default "./backups")
  -backupinterval duration
    	How often to back up the database, 0 disables backups (default 1h0m0s)
  -backupkeep int
    	How many database backups to retain (default 24)
  -baseurl string
    	Public base URL, used to build the links emailed for login (default "http://127.0.0.1:8080")
  -magiclinkttl duration
    	How long an emailed login link stays valid (default 15m0s)
  -smtphost string
    	SMTP server host used to send login-link emails (required)
  -smtpport int
    	SMTP server port (default 587)
  -smtpusername string
    	SMTP auth username (required)
  -smtpfrom string
    	From address for login-link emails (defaults to -smtpusername)
  -smtppasswordfile string
    	Path to a file containing the SMTP auth password (required)
  -devmode
    	DANGER: disables login and logs emails instead of sending them. Local/offline testing only.
```

There is no password login: accounts authenticate by clicking a one-time link
emailed to them (see "Login" below). `SESSION_KEY` is required outside of
`-devmode`. `VERSION` (shown in the footer) is an optional environment variable.

CGO must stay disabled (`CGO_ENABLED=0`): `r2pipe-go` otherwise tries to link
against a native `libr_core`, which isn't what this project uses (it talks to
`r2` over a pipe, spawning the `r2`/`rasm2` binaries from `$PATH`).

### Login

Registering/logging in asks only for an email; the server mails a single-use
link (valid for `-magiclinkttl`, default 15m) that starts a session when
clicked. There's nothing to hash or leak - `-smtphost`/`-smtpusername`/
`-smtppasswordfile` configure the SMTP server links are sent through.

### Running the whole thing locally with zero setup

```
go run ./cmd/r2wars-web -devmode
```

`-devmode` is a bundle of conveniences for exactly this - running or testing
the entire app with no external dependencies:

- Every page that normally requires login works with no session at all,
  authenticated as a fixed, auto-created "dev" user.
- If `-smtphost` isn't set, emails are logged instead of sent (so the real
  login flow - register, get a link, click it - is still testable, just by
  reading the log instead of an inbox).
- If `SESSION_KEY` isn't set, a throwaway one is generated.

Radare2 itself isn't stubbed by `-devmode` (running a battle still shells out
to `r2`/`rasm2` for real), but `internal/web.Server.OpenR2` is an injectable
`func(target string) (r2client.Pipe, error)` precisely so tests can swap in a
fake - see `internal/web/e2e_test.go`'s `TestFullOfflineUserJourney`, which
drives the entire app (create a bot, assemble it, create a battle, submit,
run it, view the result) over real HTTP with no login, no SMTP server, and no
radare2 binary at all.

**Never set `-devmode` in production** - it's a complete authentication
bypass. The NixOS module (`ops/modules/r2wars-web.nix` in the hefe3 repo)
doesn't expose it as an option at all, on purpose.

### Running tests

```
go test ./...
```

Nothing under `go test ./...` needs radare2, a network, or a real mail
server: the battle engine and the web layer's battle-running tests run
against a scripted fake of the r2 pipe interface (`internal/web/e2e_test.go`,
`internal/battle/engine_test.go`), login tests run against a fake mailer that
just records what would have been sent, and the store tests run against real
(temp-file) SQLite databases.

### Operating in production

A few things that used to make this unstable, and what now covers them:

- **Crash recovery**: a panic inside any HTTP handler is caught and turned
  into a 500 instead of taking down the process (`internal/web/middleware.go`).
- **Schema recovery**: database startup runs `CREATE TABLE IF NOT EXISTS` /
  `INSERT OR IGNORE` unconditionally, so a missing or freshly-created
  `main.db` is brought back to a usable state automatically.
- **Concurrent writers**: SQLite runs in WAL mode with a busy timeout, so
  concurrent requests retry instead of immediately failing with
  "database is locked".
- **Backups**: the server periodically snapshots the database with SQLite's
  `VACUUM INTO` into `-backupdir` (default `./backups`), keeping the last
  `-backupkeep` snapshots. To restore, stop the server and copy a backup file
  over `-databasepath`.
- **Graceful shutdown**: SIGINT/SIGTERM (what systemd sends on stop/restart)
  waits for in-flight requests - including a running battle - to finish
  before exiting, instead of cutting them off.
- **Process-level restart**: see `contrib/systemd/r2wars-web.service` for a
  unit with `Restart=on-failure`, so the process itself comes back up if it
  ever does die.

Not done here, flagged as a follow-up rather than being fixed
opportunistically: there's no CSRF protection on the HTML forms.

## Architecture

There are essentially the following objects which are all linked to each other (using a table joining their ids):

- User
  - You, the player
- Bots
  - The bots to be run within a battle
- Battles
  - An arena in which bots can be placed and run. Constraints on what bots can be added are defined here
- Architectures
  - The archs supported by r2 in order to be used by bots and battles
- Bits
  - The bits (8, 16, 32, 64) supported by r2 in order to be used by bots and battles

## TODO

- [ ] Add user creating battle as default owner
- [ ] Allow adding other users as owners to battles
- [x] Implement submitting bots
- [x] Implement running the battle
- [x] Add a "start battle now" button
- [ ] Add a "battle starts at this time" field into the battle
- [ ] Figure out how time is stored and restored with the db
- [x] Do some magic to display the current fight backlog with all info
- [ ] After having added a bot to a battle with the right arch, the arch can be changed
      When updating the bot, make sure that it is still valid in all currently linked battles
