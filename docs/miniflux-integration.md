# Miniflux / External Reader Integration

Researched 2026-03-24. **Implemented: Options A + C.**

## Problem

A Miniflux user already has a feed reader managing their subscriptions.
They want to use rss2rm as a delivery pipeline (article → PDF →
reMarkable) without duplicating feed management. Today, rss2rm is both
the feed reader AND the delivery pipeline. These two roles are coupled.

This applies to any external reader (Miniflux, FreshRSS, Tiny Tiny RSS,
Wallabag, Newsblur), not just Miniflux.

## How Other Projects Handle This

**remarkable-sync** (Go, archived, 10⭐):
Directly polls Miniflux and Wallabag APIs for articles, converts them,
and uploads to reMarkable. Separate tool, not integrated into a larger
system. https://github.com/vlaborie/remarkable-sync

**wallabag2remarkable** (Ruby, 1⭐):
Syncs Wallabag articles to reMarkable. Same pattern — dedicated bridge
script. https://github.com/danto7/wallabag2remarkable

**goosepaper**: No reader integration. It has its own source plugins
(RSS, weather, Wikipedia) but no external reader API support.

**Pattern**: Every integration is a bespoke bridge script. There is no
standard protocol for "send this article to my e-reader."

## Miniflux Integration Points

Miniflux provides two integration mechanisms:

1. **Webhooks** (push): POST to a URL when new entries arrive or when
   the user saves an entry. Events: `new_entries`, `save_entry`. Payload
   includes full HTML content, title, URL, feed metadata. HMAC-SHA256
   signed.

2. **REST API** (pull): Official Go client at `miniflux.app/v2/client`.
   Can query entries by status, starred/bookmarked, feed, category.

## Implementation Alternatives

### A. Webhook Receiver Endpoint

Add a `POST /api/v1/webhook/miniflux` endpoint to rss2rm. Miniflux
pushes `save_entry` events (user stars/saves an article). rss2rm
processes and delivers the article.

**How it works**:
1. User configures Miniflux webhook URL → rss2rm endpoint
2. User saves an article in Miniflux
3. Miniflux POSTs the entry (with HTML content) to rss2rm
4. rss2rm converts to PDF and uploads to the user's default destination

**Pros**:
- Real-time delivery (no polling delay)
- Miniflux-native — uses Miniflux's own webhook system
- User curates what to send (save = deliver)
- Minimal state in rss2rm (no feed list needed for webhook articles)
- Webhook payload already contains article HTML content

**Cons**:
- rss2rm must be network-reachable from Miniflux
- HMAC signature validation needed (straightforward)
- Only works with Miniflux (though the pattern is generic enough to
  extend to other webhook sources)
- No digest support — each article arrives individually
- Need to decide on deduplication strategy

**Effort**: Small-medium. One new HTTP handler, HMAC validation, and
a call into the existing processing/delivery pipeline.

### B. Miniflux API Poller

Add a "Miniflux source" that periodically polls the Miniflux API for
starred entries. Uses the official Go client (`miniflux.app/v2/client`).

**How it works**:
1. User configures Miniflux URL + API token in rss2rm
2. rss2rm periodically polls for starred/bookmarked entries
3. New entries are processed and delivered
4. rss2rm un-stars them after delivery (optional)

**Pros**:
- Pull model — rss2rm doesn't need to be network-exposed
- Works with rss2rm's existing polling architecture
- Could support digests (batch starred articles into a digest)
- Official Go client available

**Cons**:
- Polling delay (depends on interval)
- New dependency (`miniflux.app/v2/client`)
- Couples rss2rm to Miniflux's API specifically
- More complex config (URL, token, poll interval, what to query)
- Duplicates some of rss2rm's existing feed management concepts

**Effort**: Medium. New package for Miniflux client, config storage,
polling loop integration.

### C. Generic Article Ingest Endpoint

Add a `POST /api/v1/articles` endpoint that accepts articles directly
(title, URL, HTML content, optional metadata). Any external system can
push articles to it. Not Miniflux-specific.

**How it works**:
1. External system (Miniflux webhook, script, browser extension, etc.)
   POSTs `{"title": "...", "url": "...", "content": "<p>...</p>"}`
2. rss2rm processes, converts to PDF, delivers to default destination
3. Optional: specify destination, directory, include in digest

**Pros**:
- Generic — works with any source, not just Miniflux
- Simple API contract (title + URL + content)
- A Miniflux user writes a 10-line webhook forwarder, or rss2rm
  supports Miniflux webhook format directly
- Browser extensions, scripts, n8n, Zapier can all use it
- Could later support other webhook formats (FreshRSS, Wallabag)

**Cons**:
- Caller must provide content (or rss2rm fetches from URL)
- No built-in feed tracking or deduplication for ingested articles
- Need to decide: does ingest go through readability, or accept
  pre-processed HTML?

**Effort**: Small. One new endpoint. Most of the delivery pipeline
already exists.

### D. Do Nothing — External Bridge Script

Don't change rss2rm. Instead, document how to write a bridge script
that reads from Miniflux API and calls rss2rm's existing API.

**How it works**:
1. User writes a script (Go, Python, shell+curl)
2. Script polls Miniflux for starred entries
3. Script calls rss2rm's `POST /api/v1/poll` or adds feeds via API
4. Script runs on a cron

**Pros**:
- Zero changes to rss2rm
- Users can customize the integration however they want
- Follows the pattern of remarkable-sync and wallabag2remarkable

**Cons**:
- Extra component to deploy and maintain
- Every user reinvents the same bridge
- Not discoverable — users won't know it's possible

**Effort**: Trivial (documentation only).

### E. Feed Source Abstraction

Refactor rss2rm's importer to support pluggable feed sources. An RSS
source fetches from URLs (current behavior). A Miniflux source fetches
from the Miniflux API. A Wallabag source fetches from Wallabag. Etc.

**Pros**:
- Clean architecture for multiple input sources
- Each source is a separate package
- Extensible to FreshRSS, Wallabag, Pocket, etc.

**Cons**:
- Over-engineering for a hobby project
- Significant refactor of the importer/service layer
- Each source has different semantics (polling vs. push, feeds vs.
  saved articles, entry tracking)
- Abstraction may not fit well — RSS feeds and "starred articles in
  Miniflux" are fundamentally different concepts

**Effort**: High. Architectural change.

## Comparison

| | Webhook (A) | API Poller (B) | Ingest API (C) | Bridge Script (D) | Abstraction (E) |
|---|---|---|---|---|---|
| Effort | Small-Med | Medium | Small | Trivial | High |
| Generic | Miniflux-only | Miniflux-only | Any source | Any source | Extensible |
| Real-time | Yes | No (polling) | Yes | No | Depends |
| Digest support | No | Yes | Possible | Possible | Yes |
| Network exposure | Required | Not required | Required | Not required | Depends |
| New dependencies | None | miniflux client | None | None | Per-source |
| Complexity | Low | Medium | Low | None | High |

## Recommendation

**Start with C (Generic Article Ingest), then add A (Miniflux webhook
support) as a recognized format.**

Reasoning:

1. **C is the simplest useful change.** A `POST /api/v1/articles`
   endpoint that accepts `{title, url, content}` and delivers it. This
   works for any source and requires no Miniflux-specific code. It
   separates "reading" from "delivering" — which is the real
   architectural insight here.

2. **A is a thin layer on top of C.** Miniflux's `save_entry` webhook
   payload contains title, url, and content. A handler that validates
   the HMAC signature and maps the Miniflux payload to the article
   ingest format is ~50 lines. The user configures their Miniflux
   webhook URL as `https://rss2rm.example.com/api/v1/webhook/miniflux`.

3. **Together, A+C give you**: Miniflux "save" → webhook → rss2rm →
   PDF → reMarkable. The user stars an article in Miniflux, and it
   appears on their reMarkable within seconds.

4. **B and E are over-engineering** for a hobby project. D is fine but
   leaves value on the table — the integration is too common to not
   support directly.

5. **The ingest endpoint naturally extends** to FreshRSS (via its
   webhook support), n8n workflows, browser extensions, or curl
   one-liners.

### Suggested API

```
POST /api/v1/articles
Content-Type: application/json
Authorization: Bearer <token>

{
  "title": "How DNS Works",
  "url": "https://jvns.ca/blog/how-dns-works/",
  "content": "<p>Optional pre-extracted HTML</p>",
  "destination_id": "optional-dest-uuid",
  "directory": "optional-folder"
}
```

If `content` is omitted, rss2rm fetches the URL through readability.
If `content` is provided, it's used directly (skips fetch + extraction).

```
POST /api/v1/webhook/miniflux
Content-Type: application/json
X-Miniflux-Signature: <hmac>
X-Miniflux-Event-Type: save_entry

{ ...miniflux save_entry payload... }
```

Maps to the article ingest internally. Requires webhook secret config.

## References

- Miniflux webhook docs: https://miniflux.app/docs/webhooks.html
- Miniflux API docs: https://miniflux.app/docs/api.html
- Miniflux Go client: https://pkg.go.dev/miniflux.app/v2/client
- remarkable-sync (Miniflux+Wallabag→reMarkable): https://github.com/vlaborie/remarkable-sync
- wallabag2remarkable: https://github.com/danto7/wallabag2remarkable
- FreshRSS Google Reader API: https://freshrss.github.io/FreshRSS/en/developers/06_GoogleReader_API.html
