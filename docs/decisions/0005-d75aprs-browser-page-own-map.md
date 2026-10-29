# 0005 — d75aprs UI: a browser page, our own map, plain JavaScript, server-sent events

Date: 2026-10-29 · Status: accepted

Two options were compared: Gio (a native Go GUI) and a page served by the Go
program on localhost.

- **Gio**: all Go and a real window. But it needs cgo on macOS, has no 1.0
  release (upgrades sometimes break the API), and has no map widget. Every
  part of the UI would be hand-built in its immediate-mode style.
- **Browser page**: forms, text and caching come free. It shares d75scope's
  stack, so the two pages could be merged later, and it suits a headless
  Raspberry Pi later.

The owner chose the browser page:

- **Our own map** on a canvas rather than Leaflet: Web Mercator, OpenStreetMap
  raster tiles, pan, zoom and markers. That keeps the page free of
  third-party code.
- **Tiles go through the Go server.** It caches them on disk, honours their
  cache headers and identifies itself to the tile server. This keeps to
  OpenStreetMap's tile usage policy and lets cached tiles work offline.
- **Plain JavaScript (ES modules), with no build step.** d75scope's plan is
  TypeScript with esbuild; this page is small and standalone, so the build
  step is not worth it yet. Port it if the pages merge.
- **Server-sent events for live updates, JSON POSTs for actions.** The page
  only needs a push stream from the server, and `net/http` already does
  that, so no WebSocket package is needed.

Consequences: no new Go or npm dependencies. The map (tiles, projection,
gestures, markers) is ours to write and test. Projection and tile maths are
unit-tested in the browser-free parts.
