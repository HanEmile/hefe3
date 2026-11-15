# xilebo

Self-hostable communication platform with text chat and video calls, built on the Signal protocol.

## Features

- End-to-end encrypted messaging (X3DH + Double Ratchet + Sender Keys)
- Video/audio calls with SFU and P2P fallback
- Server federation with mTLS and signed requests
- Homeservers you can self-host
- Per-instance tagging and power level system
- Cross-instance user profiles with Ed25519 keypair identity
- Web frontend — pure vanilla JavaScript, zero build step, zero dependencies
- Terminal UI client (bubbletea) for low-bandwidth / headless environments
- Nix development shell (`nix-shell`) for reproducible builds

## Requirements

- **Go 1.22+** — that's it for the server and TUI

No Node.js, no npm, no bundler. The web frontend is plain JS served directly by the Go server.

## Quick Start

```sh
# Clone
git clone https://github.com/hanemile/xilebo.git
cd xilebo

# Option A: Nix (recommended)
nix-shell
./scripts/dev.sh

# Option B: just Go
./scripts/dev.sh
```

This will:
1. Copy `config.example.toml` to `config.toml` if it doesn't exist
2. Build the server and TUI binaries
3. Start the server on http://localhost:8080 (web UI included)

The server creates a SQLite database (`xilebo.db`) on first run.

## Running the Server

```sh
# Build
go build -o bin/xilebo-server ./cmd/xilebo-server

# Run with default settings (listens on :8080, SQLite at ./xilebo.db)
./bin/xilebo-server

# Run with a config file
./bin/xilebo-server -config config.toml
```

Open http://localhost:8080 in a browser. The web frontend is served directly — no build step.

## Terminal UI Client

For SSH sessions, low-bandwidth connections, or headless servers:

```sh
# Build
go build -o bin/xilebo-tui ./cmd/xilebo-tui

# Run (connects to localhost:8080 by default)
./bin/xilebo-tui

# Connect to a specific server
./bin/xilebo-tui -server https://chat.example.com
```

**TUI keybindings:**

| Key | Action |
|---|---|
| Tab / Shift+Tab | Navigate login fields |
| Enter | Submit / send message |
| Ctrl+N / Ctrl+P | Next / previous channel |
| Ctrl+R | Toggle register / login |
| Ctrl+L | Reload room list |
| Ctrl+C | Quit |

## Nix

A `default.nix` provides a complete development shell:

```sh
nix-shell    # drops you into a shell with Go, Node, sqlc, goose, etc.
```

### Configuration

Copy the example config and edit as needed:

```sh
cp config.example.toml config.toml
```

```toml
[server]
address = ":8080"
hostname = "chat.example.com"    # used in @user@hostname handles

[database]
driver = "sqlite"                # "sqlite" or "postgres"
dsn = "xilebo.db"                # file path or connection string

[federation]
enabled = false
address = ":8448"

[media]
upload_dir = "uploads"
max_size = 52428800              # 50MB

[sfu]
enabled = true
ice_servers = ["stun:stun.l.google.com:19302"]
```

Settings can also be overridden with environment variables:

| Variable | Description |
|---|---|
| `XILEBO_ADDRESS` | Server listen address (e.g. `:8080`) |
| `XILEBO_HOSTNAME` | Public hostname |
| `XILEBO_DB_DRIVER` | `sqlite` or `postgres` |
| `XILEBO_DB_DSN` | Database path or connection string |
| `XILEBO_FEDERATION_ENABLED` | `true` to enable federation |
| `XILEBO_MEDIA_DIR` | Media upload directory |

### Docker (example)

```sh
docker run -v xilebo-data:/data \
  -e XILEBO_DB_DSN=/data/xilebo.db \
  -e XILEBO_HOSTNAME=chat.example.com \
  -p 8080:8080 \
  ghcr.io/hanemile/xilebo:latest
```

## Web Frontend

The web frontend is pure vanilla JavaScript — no build step, no npm, no bundler. The Go server serves the `web/` directory directly.

```
web/
├── index.html       # single HTML shell
├── css/style.css    # all styles in one file
├── js/
│   ├── app.js       # main app (routing, views, rendering)
│   ├── api.js       # API client (plain fetch)
│   ├── ws.js        # WebSocket with auto-reconnect
│   ├── state.js     # reactive state (no library)
│   └── render.js    # DOM helpers, escaping, time formatting
└── public/          # favicon, manifest
```

To modify the frontend, just edit the JS/CSS files and refresh the browser.

## API

All endpoints are under `/_xb/client/v1/`. Authentication uses Bearer tokens.

### Auth

```sh
# Register
curl -X POST http://localhost:8080/_xb/client/v1/register \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"mypassword"}'

# Login
curl -X POST http://localhost:8080/_xb/client/v1/login \
  -H "Content-Type: application/json" \
  -d '{"handle":"@alice@localhost","password":"mypassword"}'
```

Both return `{"user_id":"...","handle":"@alice@localhost","token":"..."}`.

### Rooms

```sh
TOKEN="your-token-here"

# Create room
curl -X POST http://localhost:8080/_xb/client/v1/rooms \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"general","topic":"Main chat"}'

# List your rooms
curl http://localhost:8080/_xb/client/v1/rooms \
  -H "Authorization: Bearer $TOKEN"

# Join a room
curl -X POST http://localhost:8080/_xb/client/v1/rooms/ROOM_ID/join \
  -H "Authorization: Bearer $TOKEN"
```

### Messages

```sh
# Send message
curl -X POST http://localhost:8080/_xb/client/v1/rooms/ROOM_ID/send \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"Hello!"}'

# Get messages (newest first)
curl "http://localhost:8080/_xb/client/v1/rooms/ROOM_ID/messages?limit=50" \
  -H "Authorization: Bearer $TOKEN"
```

### Sync

```sh
# Initial sync
curl "http://localhost:8080/_xb/client/v1/sync" \
  -H "Authorization: Bearer $TOKEN"

# Incremental sync
curl "http://localhost:8080/_xb/client/v1/sync?since=NEXT_BATCH" \
  -H "Authorization: Bearer $TOKEN"
```

### WebSocket

Connect to `ws://localhost:8080/_xb/client/v1/ws?token=YOUR_TOKEN` for real-time events.

### Full endpoint list

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/register` | No | Create account |
| POST | `/login` | No | Authenticate |
| GET | `/sync` | Yes | Incremental sync |
| POST | `/rooms` | Yes | Create room |
| GET | `/rooms` | Yes | List joined rooms |
| GET | `/rooms/{id}` | Yes | Room details |
| POST | `/rooms/{id}/join` | Yes | Join room |
| POST | `/rooms/{id}/leave` | Yes | Leave room |
| POST | `/rooms/{id}/send` | Yes | Send message |
| GET | `/rooms/{id}/messages` | Yes | Message history |
| GET | `/rooms/{id}/members` | Yes | Room members |
| PUT | `/rooms/{id}/state/{type}` | Yes | Set room state |
| GET | `/profile/{handle}` | Yes | Get profile |
| PUT | `/profile` | Yes | Update profile |
| POST | `/keys/upload` | Yes | Upload pre-keys |
| POST | `/keys/claim` | Yes | Claim pre-key bundle |
| POST | `/calls/offer` | Yes | WebRTC SDP offer |
| POST | `/calls/answer` | Yes | WebRTC SDP answer |
| POST | `/calls/candidate` | Yes | ICE candidate |
| POST | `/calls/leave` | Yes | Leave call |
| GET | `/calls/ice-servers` | Yes | ICE server config |
| GET | `/ws` | Yes | WebSocket |

## Scripts

| Script | Description |
|---|---|
| `scripts/dev.sh` | Start server + frontend for development |
| `scripts/build.sh` | Production build (server binary + frontend bundle) |
| `scripts/test.sh` | Run all tests |
| `scripts/generate.sh` | Regenerate sqlc code from SQL schemas |
| `scripts/reset-db.sh` | Delete the local database |
| `scripts/smoke-test.sh` | Run smoke tests against a running server |

## Project Structure

```
xilebo/
├── cmd/
│   ├── xilebo-server/       Homeserver entry point
│   └── xilebo-tui/          Terminal UI client (bubbletea)
├── internal/
│   ├── api/                 HTTP + WebSocket API handlers
│   ├── api/ws/              WebSocket hub and connections
│   ├── config/              TOML + env var configuration
│   ├── crypto/              Signal protocol (X3DH, Double Ratchet, Sender Keys)
│   ├── federation/          Server-to-server federation
│   ├── permissions/         Power level and permission checks
│   ├── sfu/                 WebRTC SFU (Pion)
│   ├── storage/             Store interface + SQLite implementation
│   └── worker/              Background job processing
├── pkg/
│   ├── client/              Go client library (used by TUI)
│   └── protocol/            Shared types, events, handles, permissions
├── sql/                     SQL schemas and queries (sqlc)
├── web/                     Pure vanilla JS frontend (no build step)
│   ├── js/                  ES modules: app, api, ws, state, render
│   └── css/                 Single stylesheet
├── scripts/                 Development and ops scripts
├── default.nix              Nix development shell
└── spec/                    Protocol specification (planned)
```

## Federation

Federation allows xilebo servers to exchange messages. Enable it in config:

```toml
[federation]
enabled = true
address = ":8448"
```

Servers discover each other via `/.well-known/xilebo-server`:

```json
{"server":"chat.example.com:8448","public_key":"..."}
```

Federation requests are signed with Ed25519. The signature covers `METHOD\nPATH\nTIMESTAMP\nBODY_HASH`.

## Security

- Passwords hashed with argon2id
- Messages encrypted end-to-end using the Signal protocol
- Ed25519 keypair identity per user
- Federation requests signed and verified
- mTLS for server-to-server connections
- Video call frames encrypted via WebRTC Insertable Streams
- Server never sees plaintext message content

## License

MIT
