// The shared map: OpenStreetMap raster tiles (through the Go server's
// cache) on a canvas, in Web Mercator, with a latitude–longitude grid, pan
// and zoom. Each page subclasses it and draws its own things in
// drawOverlay. Used by d75aprs and d75sat.

import { TILE, project, unproject, visibleTiles } from "./geo.js";

const MIN_ZOOM = 2, MAX_ZOOM = 18;
const TILE_CACHE = 400;
export const LABEL_FONT = "600 11px ui-monospace, SFMono-Regular, Menlo, monospace";

export class BaseMap {
  /**
   * onSelect(key) is called when a hit target is clicked, onHover(key, x, y)
   * as the pointer moves, and onClickMap({lat, lon}) when the map itself
   * is clicked (not dragged) away from any target.
   */
  constructor(canvas, { tiles, onSelect, onHover, onClickMap }) {
    this.canvas = canvas;
    this.ctx = canvas.getContext("2d");
    this.tilesOn = tiles;
    this.onSelect = onSelect;
    this.onHover = onHover;
    this.onClickMap = onClickMap;
    this.lat = 0;
    this.lon = 0;
    this.zoom = 10;
    this.minZoom = MIN_ZOOM;
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
    this.zoom = Math.max(this.minZoom, Math.min(MAX_ZOOM, Math.round(zoom)));
    this.render();
  }

  /** Centres on a point, zooming in to at least minZoom. */
  centreOn(lat, lon, minZoom = 0) {
    this.setView(lat, lon, Math.max(this.zoom, minZoom));
  }

  zoomBy(dz, sx, sy) {
    const w = this.canvas.clientWidth, h = this.canvas.clientHeight;
    sx ??= w / 2;
    sy ??= h / 2;
    const z = Math.max(this.minZoom, Math.min(MAX_ZOOM, this.zoom + dz));
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

  /** The point under view coordinates (x, y). */
  pointAt(x, y) {
    const w = this.canvas.clientWidth, h = this.canvas.clientHeight;
    const c = project(this.lat, this.lon, this.zoom);
    const p = unproject(c.x - w / 2 + x, c.y - h / 2 + y, this.zoom);
    return { lat: p.lat, lon: ((((p.lon + 180) % 360) + 360) % 360) - 180 };
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
      cv.style.cursor = hit ? "pointer" : this.cursor ?? "grab";
      this.onHover?.(hit, e.offsetX, e.offsetY);
    });
    cv.addEventListener("pointerup", (e) => {
      if (down && !down.drag) {
        const hit = this.hitTest(e.offsetX, e.offsetY);
        if (hit) this.onSelect?.(hit);
        else this.onClickMap?.(this.pointAt(e.offsetX, e.offsetY));
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

    const view = {
      w, h, z, left, top, world, color,
      // Screen position of a point, choosing the copy of the world nearest the view.
      screen: (lat, lon) => {
        const p = project(lat, lon, z);
        let x = p.x - left;
        if (x < -w) x += world;
        else if (x > 2 * w) x -= world;
        return { x, y: p.y - top };
      },
      // Screen position without choosing a copy, for lines whose
      // longitudes run on past ±180 (see unwrap in geo.js).
      raw: (lat, lon) => {
        const p = project(lat, lon, z);
        return { x: p.x - left, y: p.y - top };
      },
    };
    this.hits = [];
    this.drawOverlay(ctx, view);
  }

  /** Subclasses draw here, over the tiles; push click targets to this.hits. */
  drawOverlay(ctx, view) {}

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
      // Past the antimeridian the line is the same meridian as −180…180.
      const shown = ((((lon + 180) % 360) + 360) % 360) - 180;
      ctx.fillText(`${shown.toFixed(digits)}°`, x + 3, 3);
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
