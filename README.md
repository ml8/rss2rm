# RSS to reMarkable (rss2rm)

Fetches RSS feeds, converts articles to PDF, and uploads them to a reMarkable tablet or other destinations.

## Features

- Polls RSS feeds on a schedule and extracts article content
- Converts articles to PDF using pandoc and weasyprint
- Uploads to reMarkable Cloud by default; other destinations (local filesystem, email, Gmail, Dropbox, Notion, GCP) available via `-destinations` flag
- Digests: groups feeds into scheduled combined PDFs
- Retention policy: keep last N deliveries per feed or digest
- Web UI and CLI for managing feeds, destinations, and digests
- Multi-user authentication with session tokens

## Prerequisites

- Go 1.25+
- Pandoc
- Weasyprint
- Node.js (for frontend development only)

## Getting Started

### CLI (single-user)

The CLI operates directly against a local SQLite database. No server, no authentication — a local user is created automatically.

```bash
make build

# Set up a destination (where PDFs go)
./rss2rm dest add file "Local PDFs"              # save to local filesystem
./rss2rm dest add remarkable "My reMarkable"     # or upload to reMarkable Cloud
./rss2rm dest default <id>                       # set as default destination

# Add feeds and poll
./rss2rm add --url "https://example.com/rss"     # add a feed
./rss2rm list                                     # list feeds
./rss2rm poll                                     # fetch, convert, deliver

# Digests (optional) — combine multiple feeds into one PDF
./rss2rm digest add --name "Morning" --schedule 07:00
./rss2rm digest add-feed <digest-id> <feed-id>
./rss2rm digest generate <digest-id>
```

Data is stored in `rss2rm.db` in the current directory. Use `-db-dsn` to change the path.

### Server (multi-user, web UI)

For background polling and browser-based management, start the HTTP server:

```bash
make all                                  # build frontend + backend
./rss2rm serve -poll -web-dir web/dist    # start server with web UI
```

The server starts on port 8080 with background polling every 30 minutes. Register a user through the web UI, or via CLI:

```bash
./rss2rm user add --email=you@example.com --password=yourpassword
```

#### Docker

```bash
make docker-build
docker run -d -p 8080:8080 -p 9090:9090 -v $(pwd)/data:/data rss2rm
```

#### Administration

The admin API runs on port 9090. Create users, manage accounts:

```bash
curl http://localhost:9090/admin/
```

Registration is open by default. For production, use `-registration=closed` and create users via the admin API.

#### GKE Deployment

See [deploy/README.md](deploy/README.md) for deploying to GKE with Cloud SQL.

## Development

```bash
make build          # Build Go binary
make test           # Run tests
make vet            # Run go vet
make fmt            # Format code
make all            # Build frontend + backend
cd web && npm run dev  # Frontend dev server (proxies API to localhost:8080)
```

## Documentation

- [Technical Docs & API Reference](docs/README.md)
- [Deployment Guide](deploy/README.md)
- [AGENTS.md](AGENTS.md) — Conventions and architecture for AI agents

## License

MIT
