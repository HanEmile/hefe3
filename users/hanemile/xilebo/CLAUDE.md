# Xilebo

Self-hostable communication platform (text + video) built on the Signal protocol.

## Project Identity

- **License**: MIT — all dependencies must be MIT-compatible (no GPL)
- **Philosophy**: Minimal but functional. Security and reliability above all else.
- **Monorepo**: Go backend (`cmd/`, `internal/`, `pkg/`) + vanilla JS frontend (`web/`) + Go TUI (`cmd/xilebo-tui/`)

## Architecture

Single `xilebo-server` binary. No microservices. All components in-process:
- HTTP API + WebSocket hub (net/http)
- Federation server (mTLS)
- SFU for video (Ion-SFU / Pion)
- Background worker pool
- Storage layer (SQLite default, PostgreSQL optional)

## Tech Stack

### Backend (Go)
- **Crypto**: `status-im/doubleratchet` (Double Ratchet), `farazdagi/x3dh` (X3DH key agreement), Sender Keys from spec
- **WebRTC**: `pion/webrtc` v4, `pion/ion-sfu`
- **Storage**: `sqlc` (SQL-first codegen), `goose` (migrations)
- **Config**: TOML + env vars

### Frontend (Vanilla JS)
- Pure JavaScript ES modules — zero dependencies, zero build step
- Served directly by the Go server from `web/`
- No npm, no bundler, no transpilation
- WebCrypto API + IndexedDB for client-side encryption and key storage
- Native RTCPeerConnection for WebRTC

### TUI Client (Go)
- bubbletea + lipgloss + bubbles
- Uses `pkg/client` Go client library
- Login, room switching, messaging — works over SSH and low-bandwidth connections

## Core Design Decisions

### Identity
- Ed25519 keypair-based cryptographic identity
- Human-readable handle: `@username@server.tld`
- Cross-instance profile (signed, portable), per-instance overrides (display name, avatar, tags)

### Encryption (Signal Protocol)
- **1:1**: X3DH → Double Ratchet
- **Groups**: Sender Keys distributed via pairwise Double Ratchet channels
- **Video**: WebRTC Insertable Streams (AES-128-GCM) — SFU forwards opaque ciphertext
- **Pre-keys**: uploaded to homeserver, fetched cross-server via federation API
- **Fault tolerance**: MKSKIPPED dictionary (MAX_SKIP=1000), ratchet state backup, re-keying on desync

### Federation
- Discovery: `/.well-known/xilebo-server`
- Auth: Ed25519 request signing over `METHOD\nPATH\nTIMESTAMP\nBODY_HASH`
- Transport: mTLS (default), relay-assisted (fallback, configurable)
- Ordering: vector clocks for causal consistency
- Delivery: persistent outbound queue with exponential backoff

### Video/Audio
- SFU mode: Ion-SFU embedded, for 4+ participants
- P2P mode: direct DTLS-SRTP, for 1:1 calls
- Automatic fallback between modes
- Signaling over existing WebSocket (`m.call.*` events)

### Permissions
- Bitwise `uint64` flags (instance-scoped + room-scoped)
- Numeric power levels 0–100
- Hierarchy enforcement: cannot modify users at or above your level

### Storage
- `sqlc` generates type-safe Go from plain SQL — no ORM
- Dual-dialect: SQLite (default, WAL) + PostgreSQL behind `Store` interface
- Migrations via `goose`

## API Namespacing

- Client-Server: `/_xb/client/v1/`
- Federation: `/_xb/federation/v1/`
- WebSocket: `/_xb/client/v1/ws`

## Frontend Layout

4-column layout:
1. **Server list** (joined instances)
2. **Room list** (chats + video rooms with presence indicators beneath)
3. **Main view** (chat messages or video call)
4. **Meta panel** (member list, group info, room settings)

## Component Naming

- Web Components use `xb-` prefix: `<xb-app>`, `<xb-room-list>`, `<xb-message-list>`, etc.
- All extend `XbElement` base class (`web/src/components/base.ts`)

## Build & Test

```sh
make build         # go build (server + TUI)
make test-unit     # go test ./internal/... ./pkg/... + vitest
make test-int      # go test ./test/integration/...
make test-e2e      # go test ./test/e2e/... (playwright)
make lint          # golangci-lint + eslint
make generate      # sqlc generate
make migrate       # goose up
```

## Key Constraints

- **No GPL dependencies** — MIT license requires it
- **Server never sees plaintext** — all message content is E2E encrypted
- **Single binary** — self-hosters should not need to orchestrate services
- **No build step** — frontend is plain JS served directly, no npm/bundler/transpiler
- **No ORM** — sqlc for explicit query control
- **Fault-tolerant federation** — queue-and-retry, never drop events silently
