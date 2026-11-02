// The d75sat map: the shared base map with satellites, the selected
// satellite's ground track and footprint, footprints of satellites in
// view, and the station with its visibility circle for the selected
// satellite's height.

import { BaseMap, LABEL_FONT } from "./map/basemap.js";
import { unwrap, footprintRing } from "./map/geo.js";
import { footprintRadiusKm } from "./sat.js";

export class SatMap extends BaseMap {
  constructor(canvas, opts) {
    super(canvas, opts);
    this.live = [];
    this.names = new Map();
    this.track = null;
    this.selected = null;
    this.station = null;
    this.picking = false;
  }

  setData({ live, names, track, selected, station }) {
    Object.assign(this, { live, names, track, selected, station });
    this.render();
  }

  setPicking(on) {
    this.picking = on;
    this.cursor = on ? "crosshair" : null;
    this.canvas.style.cursor = on ? "crosshair" : "grab";
  }

  /**
   * Draws a path of unwrapped [lat, lon] points once for every copy of the
   * world it shows up in.
   */
  path(ctx, view, pts, { closed = false, fill = null, stroke = null, width = 2, dash = null, alpha = 1 }) {
    if (pts.length < 2) return;
    const xy = pts.map(([la, lo]) => view.raw(la, lo));
    let lo = Infinity, hi = -Infinity;
    for (const p of xy) {
      lo = Math.min(lo, p.x);
      hi = Math.max(hi, p.x);
    }
    const k0 = Math.floor(-hi / view.world), k1 = Math.ceil((view.w - lo) / view.world);
    ctx.save();
    ctx.globalAlpha = alpha;
    ctx.setLineDash(dash ?? []);
    for (let k = k0; k <= k1; k++) {
      const dx = k * view.world;
      ctx.beginPath();
      xy.forEach((p, i) => (i ? ctx.lineTo(p.x + dx, p.y) : ctx.moveTo(p.x + dx, p.y)));
      if (closed) ctx.closePath();
      if (fill) {
        ctx.fillStyle = fill;
        ctx.fill();
      }
      if (stroke) {
        ctx.strokeStyle = stroke;
        ctx.lineWidth = width;
        ctx.stroke();
      }
    }
    ctx.restore();
  }

  ring(ctx, view, lat, lon, radiusKm, colour, { fillAlpha = 0.1, dash = null, width = 1.5 } = {}) {
    const { points } = footprintRing(lat, lon, radiusKm);
    this.path(ctx, view, points, { closed: true, fill: colour, alpha: fillAlpha });
    this.path(ctx, view, points, { closed: true, stroke: colour, width, dash });
  }

  drawOverlay(ctx, view) {
    const { w, h, color, screen } = view;
    const accent = color("--accent"), up = color("--up"), muted = color("--muted"), halo = color("--halo"), text = color("--text");
    const sel = this.live.find((s) => s.id === this.selected);

    // Footprints of everything in view, then the selected one on top.
    for (const s of this.live) {
      if (s.el > 0 && s.id !== this.selected) this.ring(ctx, view, s.lat, s.lon, footprintRadiusKm(s.alt), up, { fillAlpha: 0.06 });
    }
    if (this.track && sel) {
      this.path(ctx, view, unwrap(this.track.past), { stroke: accent, width: 1.5, dash: [4, 4], alpha: 0.5 });
      this.path(ctx, view, unwrap([[sel.lat, sel.lon], ...this.track.future]), { stroke: accent, width: 2, alpha: 0.85 });
    }
    if (sel) this.ring(ctx, view, sel.lat, sel.lon, footprintRadiusKm(sel.alt), accent, { fillAlpha: 0.12, width: 2 });

    // The station, and where the selected satellite has to be for the
    // station to see it.
    const labels = [];
    if (this.station) {
      if (sel) this.ring(ctx, view, this.station.lat, this.station.lon, footprintRadiusKm(sel.alt), accent, { fillAlpha: 0, dash: [6, 4], width: 1.2 });
      const p = screen(this.station.lat, this.station.lon);
      ctx.beginPath();
      ctx.arc(p.x, p.y, 6, 0, 2 * Math.PI);
      ctx.fillStyle = accent;
      ctx.fill();
      ctx.lineWidth = 3;
      ctx.strokeStyle = halo;
      ctx.stroke();
      labels.push({ x: p.x + 10, y: p.y, text: "you", fill: accent, rank: 1 });
    }

    // Satellites, the selected one last.
    const order = [...this.live].sort((a, b) => (a.id === this.selected) - (b.id === this.selected) || (a.el > 0) - (b.el > 0));
    for (const s of order) {
      const p = screen(s.lat, s.lon);
      if (p.x < -20 || p.y < -20 || p.x > w + 20 || p.y > h + 20) continue;
      const isSel = s.id === this.selected, inView = s.el > 0;
      const r = isSel ? 7 : inView ? 6 : 4;
      ctx.beginPath();
      ctx.arc(p.x, p.y, r, 0, 2 * Math.PI);
      ctx.fillStyle = isSel ? accent : inView ? up : muted;
      ctx.fill();
      ctx.lineWidth = 2;
      ctx.strokeStyle = halo;
      ctx.stroke();
      this.hits.push({ x: p.x, y: p.y, r: Math.max(r, 6), key: s.id });
      labels.push({ x: p.x + r + 4, y: p.y, text: this.names.get(s.id) ?? s.id, fill: isSel ? accent : inView ? up : text, rank: isSel ? 0 : inView ? 2 : 3 });
    }

    // Labels, most important first, skipping any that would overlap.
    labels.sort((a, b) => a.rank - b.rank);
    const placed = [];
    ctx.font = LABEL_FONT;
    for (const l of labels) {
      const box = { x0: l.x - 2, y0: l.y - 8, x1: l.x + ctx.measureText(l.text).width + 2, y1: l.y + 8 };
      if (placed.some((b) => box.x0 < b.x1 && box.x1 > b.x0 && box.y0 < b.y1 && box.y1 > b.y0)) continue;
      placed.push(box);
      ctx.globalAlpha = l.rank === 3 ? 0.75 : 1;
      this.drawLabel(ctx, l.x, l.y, l.text, l.fill, halo);
      ctx.globalAlpha = 1;
    }
  }
}
