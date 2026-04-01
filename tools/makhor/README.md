# Makhor

Minimal link aggregator. Pure Go, SQLite, no JavaScript.

## Quick Start

```bash
# Build
go build -o makhor ./cmd/makhor

# Run with initial admin
./makhor -db makhor.db -create-admin "admin:admin@example.com"

# Run with email enabled
./makhor -db makhor.db -config config.json
```

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:8080` | HTTP listen address |
| `-db` | `makhor.db` | SQLite database path |
| `-base-url` | `http://localhost:8080` | Base URL for links |
| `-templates` | `templates` | Templates directory |
| `-config` | | Path to config file (JSON) |
| `-create-admin` | | Create admin user (format: `username:email`) |
| `-enable-rss` | `true` | Enable RSS feed polling |
| `-rss-interval` | `5m` | RSS polling interval |
| `-backup` | | Create backup at path and exit |
| `-restore` | | Restore from backup and exit |
| `-backup-dir` | `./backups` | Directory for automatic backups |
| `-backup-interval` | `0` | Auto-backup interval (e.g., `24h`). 0 = disabled |
| `-backup-retain` | `7` | Number of backups to retain |

## Config File

```json
{
  "email": {
    "host": "smtp.example.com",
    "port": 587,
    "username": "user@example.com",
    "password": "secret",
    "from": "noreply@example.com"
  }
}
```

## Endpoints

### Web Routes

| Path | Description | Auth |
|------|-------------|------|
| `/` | Home page (hot/top/new posts) | - |
| `/newest` | Latest posts | - |
| `/about` | About page with statistics | - |
| `/metrics` | Prometheus metrics | Admin |
| `/tags` | Browse tags | - |
| `/tags/{name}` | Posts by tag | - |
| `/search` | Search posts | - |
| `/comments` | Recent comments | - |
| `/submit` | Submit new post | User |
| `/posts/{id}` | View post and comments | - |
| `/posts/{id}/edit` | Edit post | Author |
| `/posts/{id}/history` | Post revision history | - |
| `/users/{name}` | User profile | - |
| `/login` | Login page | - |
| `/register` | Register (requires invite) | - |
| `/collections` | Browse collections | - |
| `/invite-tree` | Invite tree visualization | - |
| `/modlog` | Public moderation log | - |
| `/admin/log` | Action log | Admin |
| `/admin/hats` | Manage hats | Admin |
| `/admin/ban` | Ban users | Admin |

### API Routes

All API routes require authentication and return JSON.

| Path | Method | Description |
|------|--------|-------------|
| `/api/posts` | GET | List posts |
| `/api/posts` | POST | Create post |
| `/api/posts/{id}` | GET | Get post |
| `/api/comments` | GET | List comments |
| `/api/comments` | POST | Create comment |
| `/api/tags` | GET | List tags |
| `/api/users/{name}` | GET | Get user |
| `/api/feeds` | GET | List RSS feeds |

### RSS Feeds

| Path | Description |
|------|-------------|
| `/rss.xml` | Latest posts |
| `/rss/comments.xml` | Recent comments |
| `/rss/tag/{name}.xml` | Posts by tag |
| `/rss/tags.xml?tags=go,rust` | Posts from multiple tags |
| `/rss/user/{name}.xml` | Posts by user |
| `/rss/log.xml` | Action log |

## Backup & Restore

### Manual Backup

```bash
./makhor -db makhor.db -backup /path/to/backup.db
```

### Manual Restore

```bash
./makhor -db makhor.db -restore /path/to/backup.db
```

### Automatic Backups

```bash
./makhor -db makhor.db -backup-interval 24h -backup-dir ./backups -backup-retain 7
```

Creates daily backups, keeping the 7 most recent.

## Administration

### makhor-admin CLI

```bash
# Build
go build -o makhor-admin ./cmd/makhor-admin

# Commands
makhor-admin -db makhor.db list-users
makhor-admin -db makhor.db list-admins
makhor-admin -db makhor.db user-info alice
makhor-admin -db makhor.db promote alice
makhor-admin -db makhor.db demote alice
makhor-admin -db makhor.db ban alice
makhor-admin -db makhor.db unban alice
```

## Project Structure

```
cmd/
  makhor/         Main server
  makhor-admin/   Admin CLI tool
pkg/
  api/            JSON API handlers
  config/         Configuration loading
  db/             Database operations
  handlers/       HTTP handlers
  mail/           Email sending
  middleware/     HTTP middleware
  models/         Data models
  rsspoll/        RSS feed polling
templates/        HTML templates
```
