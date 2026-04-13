# rss2rm Documentation

## Architecture

`rss2rm` is a single binary that can run as either a CLI tool or an HTTP server.

### Components

*   **`cmd/rss2rm`**: Unified entry point. Subcommands include `serve` (HTTP server), `add`, `list`, `poll`, `remove`, `dest`, and `digest`.
*   **`internal/service`**: Business logic layer. Defines the `Service` interface implemented by both Local and Remote backends.
*   **`internal/api`**: HTTP handlers for the REST API and Server-Sent Events (SSE).
*   **`internal/client`**: HTTP client implementation of the `Service` interface for remote mode.
*   **`internal/db`**: SQLite and MySQL database management.
*   **`internal/destinations`**: Pluggable destination implementations (ReMarkable, File, Email, Gmail, GCP, Dropbox, Notion).
*   **`internal/gmail`**: Gmail API integration with OAuth2 support.
*   **`internal/uploader`**: ReMarkable cloud integration using `github.com/juruen/rmapi`.
*   **`internal/importer`**: RSS feed fetching using `github.com/mmcdole/gofeed`.
*   **`internal/processor`**: Article content extraction using `go-readability`.
*   **`internal/converter`**: HTML generation and PDF conversion via external tools.
*   **`web/`**: Single Page Application (SPA) frontend built with Vanilla JS and Vite.

## Data Flow

1.  **Startup**: CLI loads DB. If `serve` command, starts HTTP server + optional background poller.
2.  **Destinations**: User configures destinations via CLI or UI. Each destination stores its own config (including auth tokens for ReMarkable).
3.  **Poll Loop**:
    *   Fetches RSS items from active feeds.
    *   Extracts article content (Readability).
    *   Converts to PDF (WeasyPrint).
    *   Resolves destination (feed-specific or system default).
    *   Uploads to destination.
4.  **Client/UI**:
    *   CLI or Web UI sends commands to API.
    *   API updates DB and triggers events via SSE.

## Database Schema

```sql
CREATE TABLE feeds (
    id TEXT PRIMARY KEY,
    url TEXT,
    name TEXT,
    last_polled TIMESTAMP,
    active BOOLEAN,
    backfill INTEGER DEFAULT 5,
    user_id TEXT
);

CREATE TABLE feed_delivery (
    feed_id TEXT PRIMARY KEY,
    directory TEXT,
    destination_id TEXT,
    last_delivered_id INTEGER,
    retain INTEGER DEFAULT 0,
    user_id TEXT,
    FOREIGN KEY(feed_id) REFERENCES feeds(id),
    FOREIGN KEY(destination_id) REFERENCES destinations(id)
);

CREATE TABLE digests (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE,
    directory TEXT,
    schedule TEXT,            -- daily time, e.g. "07:00"
    destination_id TEXT,
    last_generated TIMESTAMP,
    last_delivered_id INTEGER,
    active BOOLEAN DEFAULT 1,
    retain INTEGER DEFAULT 0,
    user_id TEXT,
    FOREIGN KEY(destination_id) REFERENCES destinations(id)
);

CREATE TABLE digest_feeds (
    digest_id TEXT,
    feed_id TEXT,
    PRIMARY KEY(digest_id, feed_id),
    FOREIGN KEY(digest_id) REFERENCES digests(id),
    FOREIGN KEY(feed_id) REFERENCES feeds(id)
);

CREATE TABLE destinations (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE,
    type TEXT,
    config TEXT,  -- JSON blob with type-specific configuration
    is_default BOOLEAN,
    user_id TEXT
);

CREATE TABLE entries (
    id INTEGER PRIMARY KEY,
    feed_id TEXT,
    entry_id TEXT,
    title TEXT,
    url TEXT,
    published TIMESTAMP,
    rendered TEXT,  -- path to rendered PDF
    user_id TEXT,
    UNIQUE(feed_id, entry_id),
    FOREIGN KEY(feed_id) REFERENCES feeds(id)
);

CREATE TABLE IF NOT EXISTS delivered_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    delivery_type TEXT NOT NULL,
    delivery_ref TEXT NOT NULL,
    entry_id INTEGER DEFAULT 0,
    remote_path TEXT NOT NULL,
    destination_id TEXT NOT NULL,
    delivered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT
);

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    verified BOOLEAN DEFAULT 1,
    verify_token TEXT,
    verify_expires TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sessions (
    token TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    FOREIGN KEY(user_id) REFERENCES users(id)
);
```

Note: `url` is NOT unique in `feeds` table. Adding the same URL creates a new feed row. Individual delivery config lives in `feed_delivery` (row exists = deliver individually). Digest membership is M:N via `digest_feeds`. Entry tracking is cursor-based via `last_delivered_id` fields.

## Destination Types

| Type | Config Fields | Description |
|------|--------------|-------------|
| `remarkable` | `user_token`, `device_token` | ReMarkable cloud via rmapi |
| `file` | `path` | Local filesystem directory |
| `email` | `server`, `port`, `username`, `password`, `to_email`, `from_email` | SMTP email attachment |
| `gmail` | `client_id`, `client_secret`, `to_email`, `access_token`*, `refresh_token`*, `token_expiry`* | Gmail API with OAuth2 |
| `gcp` | `bucket`, `credentials` | Google Cloud Storage |
| `dropbox` | `app_key`, `app_secret`, `folder_path`, `access_token`*, `refresh_token`*, `token_expiry`* | Dropbox with OAuth2 |
| `notion` | `client_id`, `client_secret`, `parent_page_id`, `access_token`*, `refresh_token`* | Notion pages with OAuth2 |

*\* OAuth tokens are populated automatically after completing the authorization flow.*

### Gmail Setup

1. Create OAuth 2.0 credentials in [Google Cloud Console](https://console.cloud.google.com/apis/credentials)
2. Set application type to "Web application"
3. Add authorized redirect URI: `http://localhost:8080/api/v1/oauth/callback`
4. Create Gmail destination with client_id, client_secret, and to_email
5. Complete OAuth flow by clicking "Authorize" in web UI or running `rss2rm dest auth <id>`

### Dropbox Setup

1. Create an app in [Dropbox App Console](https://www.dropbox.com/developers/apps)
2. Set permissions: `files.content.write` and `files.content.read`
3. Add redirect URI: `http://localhost:8080/api/v1/oauth/callback`
4. Create Dropbox destination with app_key, app_secret, and folder_path (e.g., `/rss2rm`)
5. Complete OAuth flow by clicking "Authorize" in web UI or running `rss2rm dest auth <id>`

### Notion Setup

1. Create an integration in [Notion Integrations](https://www.notion.so/my-integrations)
2. Set as "Public" integration to enable OAuth
3. Add redirect URI: `http://localhost:8080/api/v1/oauth/callback`
4. Create Notion destination with client_id (OAuth client ID), client_secret, and parent_page_id
5. Complete OAuth flow by clicking "Authorize" in web UI or running `rss2rm dest auth <id>`

**Note:** Notion doesn't support direct file uploads. Articles are created as Notion pages with title, feed name, and date. The PDF content is not transferred.

## External Dependencies

*   **WeasyPrint**: Converts HTML to PDF. Handles WebP images and modern CSS.

## Substack Authentication

Substack RSS feeds truncate paywalled posts unless fetched with a valid
subscriber session cookie. rss2rm supports authenticated fetching for
subscribers.

### Setup

1. **Log in** to [substack.com](https://substack.com) in your browser.
2. Open **Developer Tools** (F12, or right-click → Inspect).
3. Go to **Application** (Chrome) or **Storage** (Firefox) → **Cookies** →
   `https://substack.com`.
4. Find the cookie named `substack.sid` and copy its value.

### In rss2rm

1. Go to the **Credentials** section in the web UI.
2. Create a new credential with type "Substack Cookie" and paste the SID.
3. When adding or editing a feed, select this credential from the dropdown.

The cookie is valid for approximately 3 months. When it expires, update the
credential with a fresh cookie — all linked feeds will pick up the change
automatically. The UI shows a warning when a credential is approaching expiry.

## API Reference

Base URL: `http://localhost:8080/api/v1`

### Feeds

#### List Feeds
*   **GET** `/feeds`
*   **Response** `200 OK`:
    ```json
    [
      {
        "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
        "url": "https://example.com/feed",
        "name": "Example Feed",
        "directory": "Example_Feed",
        "last_polled": "2026-01-20T10:00:00Z",
        "active": true,
        "backfill": 5,
        "deliver_individually": true,
        "digest_names": ["Morning Reading"]
      }
    ]
    ```
    Returns `[]` if no feeds. `digest_names` is omitted when empty. `directory` comes from `feed_delivery` if individual delivery is enabled.

#### Add Feed
*   **POST** `/feeds`
*   **Body**:
    ```json
    {
      "url": "https://example.com/newfeed",
      "name": "My New Feed",
      "directory": "my_new_feed",
      "backfill": 5
    }
    ```
    Only `url` is required.
*   **Response**:
    *   `201 Created`: `{"status": "created", "url": "..."}`
    *   `400 Bad Request`: Invalid input

#### Edit Feed
*   **PUT** `/feeds/{id}`
*   **Body**:
    ```json
    {"name": "New Name", "directory": "new_dir", "deliver_individually": true}
    ```
    All fields optional. `deliver_individually` is a boolean — `true` creates/updates a `feed_delivery` row, `false` removes it.
*   **Response**: `200 OK`

#### Remove Feed
*   **DELETE** `/feeds/{id}`
*   **Response**:
    *   `204 No Content`: Successfully removed (deactivated)
    *   `404 Not Found`: Feed not found

### Polling

#### Poll Feeds
*   **POST** `/poll`
*   **Body** (optional):
    ```json
    {
      "urls": ["https://example.com/feed"],
      "backfill": 5
    }
    ```
    Omit or pass empty array to poll all feeds. `backfill` overrides the count for this poll only.
*   **Response**:
    *   `202 Accepted`: `{"status": "polling_started"}`
    *   `409 Conflict`: Poll already in progress

#### Poll Events (SSE)
*   **GET** `/poll/events`
*   **Content-Type**: `text/event-stream`
*   Events:
    ```
    data: {"FeedURL":"...","Type":"START","Message":"..."}
    data: {"FeedURL":"...","Type":"ITEM_FOUND","ItemTitle":"..."}
    data: {"FeedURL":"...","Type":"ITEM_UPLOADED","ItemTitle":"..."}
    data: {"FeedURL":"...","Type":"ERROR","Message":"..."}
    data: {"FeedURL":"...","Type":"FINISH","Message":"..."}
    data: {"Type":"POLL_COMPLETE","Message":"All feeds processed"}
    ```

### Destinations

#### List Registered Destination Types
*   **GET** `/destination-types`
*   **Response** `200 OK`:
    ```json
    ["remarkable", "file", "email"]
    ```
    Returns the list of destination types enabled via the `-destinations` flag.

#### List Destinations
*   **GET** `/destinations`
*   **Response** `200 OK`:
    ```json
    [
      {
        "ID": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
        "Name": "My ReMarkable",
        "Type": "remarkable",
        "Config": "{...}",
        "IsDefault": true
      }
    ]
    ```

#### Add Destination
*   **POST** `/destinations`
*   **Body**:
    ```json
    {
      "type": "file",
      "name": "Local Files",
      "config": {"path": "/tmp/pdfs"}
    }
    ```
*   **Response**: `201 Created`: `{"status": "created", "id": "c3d4e5f6-a7b8-9012-cdef-123456789012"}`

#### Remove Destination
*   **DELETE** `/destinations/{id}`
*   **Response**: `204 No Content`

#### Set Default Destination
*   **PUT** `/destinations/{id}/default`
*   **Response**: `200 OK`

#### Test Destination
*   **POST** `/destinations/{id}/test`
*   **Response**: `200 OK` or error

#### Get OAuth Authorization URL
*   **GET** `/destinations/{id}/auth-url`
*   **Response** `200 OK`:
    ```json
    {"auth_url": "https://accounts.google.com/o/oauth2/auth?..."}
    ```
    Only valid for OAuth destination types (Gmail, Dropbox, Notion).

#### OAuth Callback
*   **GET** `/oauth/callback?code=...&state=...`
*   Called by the OAuth provider after user authorization. Not called directly by clients.

### Digests

#### List Digests
*   **GET** `/digests`
*   **Response** `200 OK`:
    ```json
    [
      {
        "ID": "d4e5f6a7-b8c9-0123-def0-123456789abc",
        "Name": "Morning Reading",
        "Directory": "Morning_Reading",
        "Schedule": "07:00",
        "DestinationID": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
        "LastGenerated": "2026-02-21T07:00:00Z",
        "LastDeliveredID": 42,
        "Active": true
      }
    ]
    ```
    `DestinationID` is nullable. `LastGenerated` is zero-value if never generated.

#### Create Digest
*   **POST** `/digests`
*   **Body**:
    ```json
    {
      "name": "Morning Reading",
      "schedule": "07:00",
      "destination_id": "b2c3d4e5-f6a7-8901-bcde-f12345678901"
    }
    ```
    `name` is required. `schedule` defaults to `"07:00"` if omitted. `destination_id` is optional (uses system default if not set).
*   **Response**: `201 Created`: `{"status": "created", "id": "d4e5f6a7-b8c9-0123-def0-123456789abc"}`

#### Edit Digest
*   **PUT** `/digests/{id}`
*   **Body**:
    ```json
    {"name": "New Name", "schedule": "08:00", "directory": "new_dir"}
    ```
    All fields optional. Empty `directory` clears the value.
*   **Response**: `200 OK`

#### Remove Digest
*   **DELETE** `/digests/{id}`
*   **Response**: `204 No Content`

#### List Digest Feeds
*   **GET** `/digests/{id}/feeds`
*   **Response** `200 OK`: Array of feed objects (same shape as List Feeds response).

#### Add Feed to Digest
*   **POST** `/digests/{id}/feeds`
*   **Body**:
    ```json
    {
      "feed_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "also_individual": false
    }
    ```
    `feed_id` is required. `also_individual` defaults to `false`.
*   **Response**: `200 OK`

#### Remove Feed from Digest
*   **DELETE** `/digests/{id}/feeds/{feed_id}`
*   **Response**: `204 No Content`

#### Generate Digest
*   **POST** `/digests/{id}/generate`
*   Triggers immediate digest generation.
*   **Response**: `200 OK`: `{"status": "generated"}` or error

### Deliveries

#### List Recent Deliveries
*   **GET** `/deliveries`
*   Returns the 25 most recent deliveries for the authenticated user, newest first.
*   **Response** `200 OK`:
    ```json
    [
      {
        "id": 42,
        "delivery_type": "individual",
        "title": "How DNS Works",
        "feed_name": "Julia Evans",
        "url": "https://jvns.ca/blog/how-dns-works/",
        "dest_name": "My ReMarkable",
        "dest_type": "remarkable",
        "delivered_at": "2026-03-12T07:30:00Z"
      },
      {
        "id": 41,
        "delivery_type": "digest",
        "title": "Morning Reading",
        "dest_name": "My ReMarkable",
        "dest_type": "remarkable",
        "delivered_at": "2026-03-12T07:00:00Z"
      }
    ]
    ```
    `feed_name` and `url` are omitted for digest deliveries.

### Authentication

Register via the web UI or `POST /api/v1/auth/register` with `{"email": "...", "password": "..."}`. Login returns a session token, also set as an HttpOnly cookie. API requests require an `Authorization: Bearer <token>` header or session cookie. All API routes except auth, health, and OAuth callback require authentication.

Registration mode is configurable via the `-registration` flag: `open` (default), `closed`, or `allowlist`. Email verification is optional (enabled with `-verify-email` flag; requires SMTP configuration). Admin-created users are verified by default.

#### Register
*   **POST** `/auth/register`
*   **Body**: `{"email": "user@example.com", "password": "secret"}`
*   **Response**: `201 Created`: `{"id": "f47ac10b-58cc-4372-a567-0e02b2c3d479", "email": "user@example.com"}`

#### Login
*   **POST** `/auth/login`
*   **Body**: `{"email": "user@example.com", "password": "secret"}`
*   **Response**: `200 OK`: `{"token": "...", "user": {"id": "f47ac10b-58cc-4372-a567-0e02b2c3d479", "email": "..."}}`

#### Logout
*   **POST** `/auth/logout`
*   **Response**: `204 No Content`

#### Get Current User
*   **GET** `/auth/me`
*   **Response**: `200 OK`: `{"id": "f47ac10b-58cc-4372-a567-0e02b2c3d479", "email": "user@example.com"}`

#### Change Password
*   **POST** `/auth/change-password`
*   **Body**: `{"current_password": "old", "new_password": "new"}`
*   **Response**: `200 OK`: `{"status": "password changed"}`

#### Verify Email
*   **GET** `/auth/verify?token=NONCE`
*   **Response**: `200 OK`: `{"status": "verified"}` or error if token is invalid/expired.

### Credentials

Credentials store authentication tokens for fetching content from paywalled
sources. A credential can be linked to one or more feeds via `credential_id`.

#### List Credentials
*   **GET** `/credentials`
*   **Response** `200 OK`: Array of credentials with config values redacted.

#### Add Credential
*   **POST** `/credentials`
*   **Body**:
    ```json
    {
      "name": "My Substack",
      "type": "substack_cookie",
      "config": {"substack_sid": "your-session-cookie-here"}
    }
    ```
*   **Response** `201 Created`: `{"status": "created", "id": "..."}`

#### Update Credential
*   **PUT** `/credentials/{id}`
*   **Body**: Same as Add.
*   **Response** `200 OK`: `{"status": "ok"}`

#### Delete Credential
*   **DELETE** `/credentials/{id}`
*   **Response** `204 No Content`. Feeds referencing this credential have their `credential_id` cleared.

### Health

#### Health Check
*   **GET** `/health`
*   **Response**: `{"status": "ok"}`

## Admin API

Runs on a separate port (default 9090). If `-admin-token` is set, all API endpoints require an `Authorization: Bearer <token>` header. The admin web page at `GET /admin/` is accessible without a token (it prompts for the token in-browser). See [deploy/README.md](../deploy/README.md) for access instructions.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/admin/` | Admin web page (no token required) |
| GET | `/admin/users` | List all users |
| POST | `/admin/users` | Create user |
| DELETE | `/admin/users/{id}` | Delete user and all their data |
| POST | `/admin/users/{id}/verify` | Manually verify a user |
