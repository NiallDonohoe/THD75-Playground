// The d75aprs map: the shared base map (web/map) with station markers,
// tracks and labels on it. Each marker's ring shows where its position
// came from: RF, IP, both, or aprs.fi (dashed).

import { BaseMap, LABEL_FONT } from "./map/basemap.js";
import { ambiguityHalfDeg } from "./map/geo.js";
import { symbolInfo } from "./symbols.js";

const STALE_MS = 60 * 60 * 1000;

export class MapView extends BaseMap {
  constructor(canvas, opts) {
    super(canvas, opts);
    this.stations = [];
    this.me = null;
    this.selected = null;
  }

  setData({ stations, me, selected }) {
    this.stations = stations;
    this.me = me;
    this.selected = selected;
    this.render();
  }

  drawOverlay(ctx, view) {
    const { w, h, color, screen } = view;
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

}

function ring(ctx, x, y, r, from, to, colour) {
  ctx.beginPath();
  ctx.arc(x, y, r, from, to);
  ctx.strokeStyle = colour;
  ctx.stroke();
}
