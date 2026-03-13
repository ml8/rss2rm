# RSS2RM Web Frontend

Web interface for managing feeds, destinations, and digests.

## Stack

- Vanilla JS (no framework), bundled with Vite
- [Pico.css](https://picocss.com/) v1.x for styling
- Server-Sent Events (SSE) for real-time poll notifications

## Files

- `index.html` — Page layout, forms, edit modals (`<dialog>` elements), login/register section
- `src/main.js` — All application logic:
  - **Auth** (top): `checkAuth`, `showApp`/`showAuth`, login/register form handlers, 401 fetch interceptor
  - **API calls**: `fetchFeeds`, `addFeed`, `removeFeed`, `fetchDestinations`, `addDestination`, `fetchDigests`, etc.
  - **Rendering**: `renderFeeds` (feed table with delivery status), `renderDestinations` (destination table with dynamic type tabs fetched from `/api/v1/destination-types`), `renderDigests` (digest cards with feed badges)
  - **Event delegation**: Click handlers on feed/destination/digest tables via `data-*` attributes
  - **Modals**: Edit feed/digest dialogs, password change modal
  - **Add feed form**: Includes "Deliver individually" checkbox
  - **SSE**: `initSSE` listens for poll events, shows toast notifications
- `vite.config.js` — Dev server proxy (`/api` → `localhost:8080`)
- `package.json` — Only dev dependency is `vite`

## Development

```bash
cd web
npm install
npm run dev     # Dev server on :5173, proxies API to :8080
npm run build   # Production build → dist/
```

The Go server must be running on port 8080 for the dev proxy to work.
