// The map: OpenStreetMap raster tiles (through the Go server's cache) on a
// canvas, in Web Mercator, with station markers, tracks and labels. Each
// marker's ring shows where its position came from: RF, IP, both, or
// aprs.fi (dashed).

import { TILE, project, unproject, visibleTiles, ambiguityHalfDeg } from "./geo.js";
import { symbolInfo } from "./symbols.js";

const MIN_ZOOM = 2, MAX_ZOOM = 18;
const TILE_CACHE = 400;
const STALE_MS = 60 * 60 * 1000;
const LABEL_FONT = "600 11px ui-monospace, SFMono-Regular, Menlo, monospace";

export class MapView {
  constructor(canvas, { tiles, onSelect, onHover }) {
    this.canvas = canvas;
    this.ctx = canvas.getContext("2d");
    this.tilesOn = tiles;
    this.onSelect = onSelect;
    this.onHover = onHover;
    this.lat = 0;
    this.lon = 0;
    this.zoom = 10;
    this.stations = [];
    this.me = null;
    this.selected = null;
    this.tiles = new Map();
    this.clock = 0;
    this.hits = [];
    this.queued = false;
    this.bind();
    new ResizeObserver(() => this.render()).observe(canvas);
    matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => this.render());
  }

  setView(lat, lon, zoom = this.zoom) {
    this.lat = Math.max(-85, Math.min(85, lat));
    this.lon = ((((lon + 180) % 360) + 360) % 360) - 180;
    this.zoom = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, Math.round(zoom)));
    this.render();
  }

  /** Centres on a point, zooming in to at least minZoom. */
  centreOn(lat, lon, minZoom = 0) {
    this.setView(lat, lon, Math.max(this.zoom, minZoom));
  }

  setData({ stations, me, selected }) {
    this.stations = stations;
    this.me = me;
    this.selected = selected;
    this.render();
  }

  zoomBy(dz, sx, sy) {
    const w = this.canvas.clientWidth, h = this.canvas.clientHeight;
    sx ??= w / 2;
    sy ??= h / 2;
    const z = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, this.zoom + dz));
    if (z === this.zoom) return;
    // Keep the point under (sx, sy) where it is.
    const c = project(this.lat, this.lon, this.zoom);
    const p = unproject(c.x - w / 2 + sx, c.y - h / 2 + sy, this.zoom);
    const q = project(p.lat, p.lon, z);
    const nc = unproject(q.x - sx + w / 2, q.y - sy + h / 2, z);
    this.setView(nc.lat, nc.lon, z);
  }

  panBy(dx, dy) {
    const c = project(this.lat, this.lon, this.zoom);
    const p = unproject(c.x - dx, c.y - dy, this.zoom);
    this.setView(p.lat, p.lon);
  }

  render() {
    if (this.queued) return;
    this.queued = true;
    requestAnimationFrame(() => {
      this.queued = false;
      this.draw();
    });
  }

  bind() {
    const cv = this.canvas;
    let down = null;
    let wheel = 0;
    cv.addEventListener("pointerdown", (e) => {
      cv.setPointerCapture(e.pointerId);
      down = { x: e.offsetX, y: e.offsetY, lx: e.offsetX, ly: e.offsetY, drag: false };
    });
    cv.addEventListener("pointermove", (e) => {
      if (down) {
        if (!down.drag && Math.hypot(e.offsetX - down.x, e.offsetY - down.y) > 4) down.drag = true;
        if (down.drag) {
          this.panBy(e.offsetX - down.lx, e.offsetY - down.ly);
          down.lx = e.offsetX;
          down.ly = e.offsetY;
        }
        return;
      }
      const hit = this.hitTest(e.offsetX, e.offsetY);
      cv.style.cursor = hit ? "pointer" : "grab";
      this.onHover?.(hit, e.offsetX, e.offsetY);
    });
    cv.addEventListener("pointerup", (e) => {
      if (down && !down.drag) {
        const hit = this.hitTest(e.offsetX, e.offsetY);
        if (hit) this.onSelect?.(hit);
      }
      down = null;
    });
    cv.addEventListener("pointerleave", () => this.onHover?.(null));
    cv.addEventListener("dblclick", (e) => this.zoomBy(1, e.offsetX, e.offsetY));
    cv.addEventListener("wheel", (e) => {
      e.preventDefault();
      wheel += e.deltaY;
      if (Math.abs(wheel) >= 60) {
        this.zoomBy(wheel < 0 ? 1 : -1, e.offsetX, e.offsetY);
        wheel = 0;
      }
    }, { passive: false });
    cv.addEventListener("keydown", (e) => {
      const step = 120;
      const keys = {
        "+": () => this.zoomBy(1), "=": () => this.zoomBy(1), "-": () => this.zoomBy(-1),
        ArrowLeft: () => this.panBy(step, 0), ArrowRight: () => this.panBy(-step, 0),
        ArrowUp: () => this.panBy(0, step), ArrowDown: () => this.panBy(0, -step),
      };
      if (keys[e.key]) {
        e.preventDefault();
        keys[e.key]();
      }
    });
  }

  hitTest(x, y) {
    let best = null, bestD = Infinity;
    for (const h of this.hits) {
      const d = Math.hypot(h.x - x, h.y - y);
      if (d <= h.r + 4 && d < bestD) {
        best = h.key;
        bestD = d;
      }
    }
    return best;
  }

  tile(z, x, y) {
    const key = `${z}/${x}/${y}`;
    let t = this.tiles.get(key);
    if (!t) {
      const img = new Image();
      t = { img, ok: false };
      img.onload = () => {
        t.ok = true;
        this.render();
      };
      img.src = `/tiles/${key}`;
      this.tiles.set(key, t);
      if (this.tiles.size > TILE_CACHE) this.evict();
    }
    t.used = ++this.clock;
    return t;
  }

  evict() {
    const entries = [...this.tiles.entries()].sort((a, b) => a[1].used - b[1].used);
    for (const [k] of entries.slice(0, entries.length - TILE_CACHE)) this.tiles.delete(k);
  }

  /** A loaded ancestor of a tile, to stretch over the gap while it loads. */
  ancestor(z, x, y) {
    for (let dz = 1; dz <= 4 && z - dz >= 0; dz++) {
      const t = this.tiles.get(`${z - dz}/${x >> dz}/${y >> dz}`);
      if (t?.ok) {
        const size = TILE >> dz;
        return { img: t.img, sx: (x - ((x >> dz) << dz)) * size, sy: (y - ((y >> dz) << dz)) * size, size };
      }
    }
    return null;
  }

  draw() {
    const cv = this.canvas, ctx = this.ctx;
    const dpr = window.devicePixelRatio || 1;
    const w = cv.clientWidth, h = cv.clientHeight;
    if (cv.width !== Math.round(w * dpr) || cv.height !== Math.round(h * dpr)) {
      cv.width = Math.round(w * dpr);
      cv.height = Math.round(h * dpr);
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    const css = getComputedStyle(document.documentElement);
    const color = (name) => css.getPropertyValue(name).trim();
    const dark = matchMedia("(prefers-color-scheme: dark)").matches;

    ctx.fillStyle = color("--map-bg");
    ctx.fillRect(0, 0, w, h);

    const z = this.zoom;
    const c = project(this.lat, this.lon, z);
    const left = c.x - w / 2, top = c.y - h / 2;
    const world = TILE * 2 ** z;

    this.drawGrid(ctx, left, top, w, h, z, color("--grid"), color("--muted"));

    if (this.tilesOn) {
      ctx.filter = dark ? "invert(1) hue-rotate(180deg) brightness(0.9) contrast(0.85) saturate(0.5)" : "none";
      for (const t of visibleTiles(left, top, w, h, z)) {
        const tile = this.tile(t.z, t.x, t.y);
        if (tile.ok) {
          ctx.drawImage(tile.img, t.left, t.top, TILE, TILE);
        } else {
          const a = this.ancestor(t.z, t.x, t.y);
          if (a) ctx.drawImage(a.img, a.sx, a.sy, a.size, a.size, t.left, t.top, TILE, TILE);
        }
      }
      ctx.filter = "none";
    }

    // Screen position of a point, choosing the copy of the world nearest the view.
    const screen = (lat, lon) => {
      const p = project(lat, lon, z);
      let x = p.x - left;
      if (x < -w) x += world;
      else if (x > 2 * w) x -= world;
      return { x, y: p.y - top };
    };
    const colours = { rf: color("--rf"), is: color("--is"), aprsfi: color("--fi") };
    const now = Date.now();
    const visible = this.stations.filter((s) => s.pos && !s.killed);

    // Tracks and ambiguity boxes under the markers.
    for (const s of visible) {
      const col = colours[s.pos.sources[0]] ?? colours.rf;
      if (s.track?.length > 1) {
        ctx.beginPath();
        s.track.forEach(([la, lo], i) => {
          const p = screen(la, lo);
          i ? ctx.lineTo(p.x, p.y) : ctx.moveTo(p.x, p.y);
        });
        ctx.strokeStyle = col;
        ctx.globalAlpha = 0.45;
        ctx.lineWidth = 2;
        ctx.stroke();
        ctx.globalAlpha = 1;
      }
      if (s.pos.ambiguity > 0) {
        const d = ambiguityHalfDeg(s.pos.ambiguity);
        const a = screen(s.pos.lat + d, s.pos.lon - d), b = screen(s.pos.lat - d, s.pos.lon + d);
        ctx.fillStyle = col;
        ctx.globalAlpha = 0.12;
        ctx.fillRect(a.x, a.y, b.x - a.x, b.y - a.y);
        ctx.globalAlpha = 1;
      }
    }

    this.hits = [];
    const labels = [];
    // Draw the selected station last, on top.
    visible.sort((a, b) => (a.key === this.selected) - (b.key === this.selected));
    for (const s of visible) {
      const p = screen(s.pos.lat, s.pos.lon);
      if (p.x < -40 || p.y < -40 || p.x > w + 40 || p.y > h + 40) continue;
      const sel = s.key === this.selected;
      const r = sel ? 14 : 11;
      const stale = now - Date.parse(s.pos.at) > STALE_MS;
      ctx.globalAlpha = stale ? 0.55 : 1;
      this.drawMarker(ctx, p.x, p.y, r, s, colours, color("--marker-bg"), color("--text"), sel ? color("--accent") : null);
      ctx.globalAlpha = 1;
      labels.push({ x: p.x + r + 4, y: p.y, text: s.name, fill: color("--text"), rank: sel ? 0 : 2, at: Date.parse(s.pos.at), alpha: stale ? 0.55 : 1 });
      this.hits.push({ x: p.x, y: p.y, r, key: s.key });
    }

    if (this.me) {
      const p = screen(this.me.lat, this.me.lon);
      ctx.beginPath();
      ctx.arc(p.x, p.y, 7, 0, 2 * Math.PI);
      ctx.fillStyle = color("--accent");
      ctx.fill();
      ctx.lineWidth = 3;
      ctx.strokeStyle = color("--halo");
      ctx.stroke();
      labels.push({ x: p.x + 11, y: p.y, text: `${this.me.call} (you)`, fill: color("--accent"), rank: 1, at: Infinity, alpha: 1 });
    }

    // Labels last, most important first; one that would overlap a label
    // already drawn is left out (hovering still names the station).
    labels.sort((a, b) => a.rank - b.rank || b.at - a.at);
    const placed = [];
    ctx.font = LABEL_FONT;
    for (const l of labels) {
      const box = { x0: l.x - 2, y0: l.y - 8, x1: l.x + ctx.measureText(l.text).width + 2, y1: l.y + 8 };
      if (placed.some((b) => box.x0 < b.x1 && box.x1 > b.x0 && box.y0 < b.y1 && box.y1 > b.y0)) continue;
      placed.push(box);
      ctx.globalAlpha = l.alpha;
      this.drawLabel(ctx, l.x, l.y, l.text, l.fill, color("--halo"));
      ctx.globalAlpha = 1;
    }
  }

  drawMarker(ctx, x, y, r, s, colours, bg, text, highlight) {
    const src = new Set(s.pos.sources);
    if (highlight) {
      ctx.beginPath();
      ctx.arc(x, y, r + 5, 0, 2 * Math.PI);
      ctx.fillStyle = highlight;
      ctx.globalAlpha *= 0.35;
      ctx.fill();
      ctx.globalAlpha /= 0.35;
    }
    ctx.beginPath();
    ctx.arc(x, y, r, 0, 2 * Math.PI);
    ctx.fillStyle = bg;
    ctx.fill();

    ctx.lineWidth = 3;
    if (src.has("rf") && src.has("is")) {
      ring(ctx, x, y, r, Math.PI / 2, (3 * Math.PI) / 2, colours.rf);
      ring(ctx, x, y, r, -Math.PI / 2, Math.PI / 2, colours.is);
    } else if (src.has("rf") || src.has("is")) {
      ring(ctx, x, y, r, 0, 2 * Math.PI, src.has("rf") ? colours.rf : colours.is);
    } else {
      ctx.setLineDash([3, 3]);
      ring(ctx, x, y, r, 0, 2 * Math.PI, colours.aprsfi);
      ctx.setLineDash([]);
    }

    // Course: a tick out from the ring, for anything moving.
    if (s.pos.hasCourse && s.pos.speed >= 1 && s.pos.course > 0) {
      const a = ((s.pos.course - 90) * Math.PI) / 180;
      ctx.beginPath();
      ctx.moveTo(x + Math.cos(a) * r, y + Math.sin(a) * r);
      ctx.lineTo(x + Math.cos(a) * (r + 8), y + Math.sin(a) * (r + 8));
      ctx.lineWidth = 2;
      ctx.strokeStyle = text;
      ctx.stroke();
    }

    const sym = symbolInfo(s.symbol);
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    if (sym.emoji) {
      ctx.font = `${Math.round(r * 1.15)}px system-ui, "Apple Color Emoji", sans-serif`;
      ctx.fillText(sym.emoji, x, y + 1);
    } else {
      ctx.font = `bold ${Math.round(r * 1.1)}px ui-monospace, Menlo, monospace`;
      ctx.fillStyle = text;
      ctx.fillText(sym.fallback, x, y + 1);
    }
    if (sym.overlay) {
      ctx.font = "bold 9px ui-monospace, Menlo, monospace";
      ctx.fillStyle = text;
      ctx.fillText(sym.overlay, x + r * 0.7, y + r * 0.7);
    }
  }

  drawLabel(ctx, x, y, text, fill, halo) {
    ctx.font = LABEL_FONT;
    ctx.textAlign = "left";
    ctx.textBaseline = "middle";
    ctx.lineWidth = 3;
    ctx.strokeStyle = halo;
    ctx.strokeText(text, x, y);
    ctx.fillStyle = fill;
    ctx.fillText(text, x, y);
  }

  drawGrid(ctx, left, top, w, h, z, line, label) {
    const tl = unproject(left, top, z), br = unproject(left + w, top + h, z);
    const span = (w / (TILE * 2 ** z)) * 360;
    const step = [0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 20, 30].find((s) => span / s <= 8) ?? 30;
    ctx.strokeStyle = line;
    ctx.lineWidth = 1;
    ctx.fillStyle = label;
    ctx.font = "10px ui-monospace, Menlo, monospace";
    ctx.textBaseline = "top";
    ctx.textAlign = "left";
    const digits = step < 0.1 ? 2 : step < 1 ? 1 : 0;
    for (let lon = Math.ceil(tl.lon / step) * step; lon <= br.lon; lon += step) {
      const x = project(0, lon, z).x - left;
      ctx.beginPath();
      ctx.moveTo(x, 0);
      ctx.lineTo(x, h);
      ctx.stroke();
      ctx.fillText(`${lon.toFixed(digits)}°`, x + 3, 3);
    }
    for (let lat = Math.ceil(br.lat / step) * step; lat <= tl.lat; lat += step) {
      const y = project(lat, 0, z).y - top;
      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(w, y);
      ctx.stroke();
      ctx.fillText(`${lat.toFixed(digits)}°`, 3, y + 3);
    }
  }
}

function ring(ctx, x, y, r, from, to, colour) {
  ctx.beginPath();
  ctx.arc(x, y, r, from, to);
  ctx.strokeStyle = colour;
  ctx.stroke();
}
