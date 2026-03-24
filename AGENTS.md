# AGENTS.md

## Ground Rules

1. This is a hobby app. Keep everything simple, clear, and functional. Do not overengineer.
2. Keep code well-commented with brief function descriptions. No inline comments unless the logic is non-obvious.
3. Prefer standard library or widely-used packages. Avoid obscure dependencies.
4. State things plainly. No hyperbole. Ask for clarification when uncertain.
5. Use parameterized SQL queries. Encapsulate queries in functions in `internal/db/`.
6. Build iteratively. Verify each step. Write tests when appropriate.
7. Self-review after implementing. Strive for clear, minimal code.
8. Keep `AGENTS.md`, `README.md`, and `docs/README.md` up to date when making significant changes.
9. Do not push to GitHub.
10. Do not make git commits. Leave all changes unstaged for human review.
11. Celebrate major changes with a cat party. Add a dated `.txt` file to `cat_party/` (e.g., `2026-03-24_feature-name.txt`). ASCII art, kittens, 80 columns max. Run with `python3 cat_party.py`.

## Build & Test

```bash
make build            # Build Go binary
make test             # Run all tests
make vet              # Run go vet
make fmt              # Format code
make all              # Build frontend + backend
make serve-ui         # Build everything and start server with web UI

# Single test
go test ./internal/db -run TestDigestCRUD

# Frontend dev server (proxies API to localhost:8080)
cd web && npm install && npm run dev
```

CGO is required for SQLite (`github.com/mattn/go-sqlite3`). MySQL mode (`--db-driver=mysql`) does not require CGO.

## Architecture

Monolithic Go binary with two modes: CLI and HTTP server. Pipeline: RSS fetch → readability extraction → HTML → PDF (pandoc + weasyprint) → upload to destination.

### Package Structure

```
cmd/rss2rm/
├── main.go              # Entry point, CLI commands, factory registration
├── destinations_cli.go  # Destination management subcommands
└── digest_cli.go        # Digest management subcommands

internal/
├── api/server.go        # HTTP server, routes (Go 1.22 ServeMux), SSE broker
├── service/
│   ├── service.go       # Core business logic, polling, delivery
│   ├── destination.go   # Destination, ConfigUpdater, OAuthDestination interfaces
│   └── factory.go       # Factory registry for destination types
├── db/db.go             # Database layer (SQLite + MySQL support)
├── destinations/        # Destination implementations (remarkable, file, email, gmail, gcp, dropbox, notion)
├── gmail/               # Gmail OAuth2 + API client
├── mailer/              # SMTP email sender for verification
├── uploader/            # reMarkable cloud API client
├── importer/            # RSS feed fetching
├── processor/           # Article content extraction
├── converter/           # HTML generation & PDF conversion
└── client/              # Remote HTTP client (CLI → server)

web/                     # Vanilla JS + Vite + Pico.css frontend
deploy/                  # GKE deployment scripts and K8s manifests
```

### Key Patterns

- **Factory pattern**: Destination types register factories in `main.go` via `service.RegisterDestinationFactory()`. Avoids circular imports.
- **Service interface**: `LocalService` (direct DB) and `RemoteService` (HTTP client) both implement `Service`.
- **SSE broker**: `api.Broker` manages client connections and broadcasts poll events.
- **ConfigUpdater**: Optional interface for destinations that persist config changes (e.g., token refresh).
- **Auth middleware**: `requireAuth` wraps authenticated routes on a separate `ServeMux`. Public routes: `/auth/*`, `/health`, `/oauth/callback`.
- **Admin server**: Separate `http.ServeMux` on admin port. No auth — network-level access control.

### Data Flow

```
Polling:
  for each active feed → importer.Fetch → create entries
  if feed_delivery exists → GetUndeliveredEntries → process → render → upload → advance cursor
  checkDigests → for each due digest → GenerateDigest

Destination resolution:
  feed_delivery.destination_id or digest.destination_id → fallback to system default → error if none
```

## Database Schema

```sql
feeds (id TEXT PK, url, name, last_polled, active, backfill, user_id)
feed_delivery (feed_id TEXT PK, directory, destination_id, last_delivered_id, retain, user_id)
digests (id TEXT PK, name, directory, schedule, destination_id, last_generated, last_delivered_id, active, retain, user_id)
digest_feeds (digest_id, feed_id) -- M:N join
entries (id INTEGER PK auto-increment, feed_id, entry_id, title, url, published, rendered, user_id)
delivered_files (id INTEGER PK, user_id, delivery_type, delivery_ref, entry_id, remote_path, destination_id, delivered_at)
destinations (id TEXT PK, name, type, config, is_default, user_id)
users (id TEXT PK, email, password_hash, verified, verify_token, verify_expires, created_at)
sessions (token TEXT PK, user_id, created_at, expires_at)
settings (key TEXT PK, value)
```

Entity IDs are UUIDs (TEXT). Entry IDs are auto-increment integers (used as monotonic cursors for delivery tracking). All data tables are scoped by `user_id` — every DB query includes `AND user_id = ?` to enforce tenant isolation.

## Key Concepts

- A **feed** is an RSS source. It does not carry delivery config.
- **Individual delivery** config is in `feed_delivery` (1:1 with feed). Row exists = deliver individually.
- **Digest delivery** config is in `digests`. Feeds linked via `digest_feeds` (M:N). A feed can be in multiple digests and also deliver individually.
- **Authentication**: bcrypt passwords, opaque session tokens in `sessions` table. User ID in request context via `userIDFromContext`. Admin API on separate port (default 9090, network-level access). Registration modes: open, closed, allowlist (flag-based). Email verification (optional, requires SMTP). Password change endpoint.
- **Multitenancy**: All data tables have a `user_id` column. Every DB method takes `userID` as a parameter — compiler-enforced tenant scoping. Background polling iterates all users. CLI auto-creates a local user.
- **Database drivers**: SQLite (default) and MySQL. Selected via `--db-driver` flag.
- **Retention**: `retain` field on `feed_delivery` and `digests`. 0=unlimited, N=keep last N. Old deliveries deleted from destination via `Destination.Delete()`.
- **Pluggable destinations**: `-destinations` flag controls which types are registered. Default: remarkable only.

## Quick Reference

| Task | Command |
|------|---------|
| Build | `make build` |
| Test | `make test` |
| Run server | `./rss2rm serve -poll -web-dir web/dist` |
| Add feed | `./rss2rm add --url https://example.com/feed` |
| Create user | `./rss2rm user add --email=x --password=y` |
| Create digest | `./rss2rm digest add --name "Name" --schedule 07:00` |
| Add destination | `./rss2rm dest add <type> <name>` |
| Docker build | `docker build -t rss2rm .` |
| Deploy to GKE | See `deploy/README.md` |
| Admin API | `kubectl port-forward svc/rss2rm-admin -n rss2rm 9090:9090` |
| Change password | `POST /api/v1/auth/change-password` |

## Documentation

- `README.md` — Usage and getting started
- `docs/README.md` — Technical docs, schema details, API reference
- `deploy/README.md` — GKE deployment guide
