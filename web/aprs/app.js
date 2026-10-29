// d75aprs page: follows the server's event stream and draws the map, the
// station list, the packet log and the messages. Text from packets comes
// from the air, so it only ever reaches the page through textContent.

import { MapView } from "./map.js";
import { distanceKm, bearing, compass, locator, formatAge } from "./geo.js";
import { symbolInfo } from "./symbols.js";

const MAX_PACKETS = 1000;

const state = {
  info: null,
  stations: new Map(),
  packets: [], // oldest first
  packetById: new Map(),
  messages: new Map(),
  status: {},
  tab: "stations",
  filter: "all",
  search: "",
  selected: null,
  paused: false,
  pausedCount: 0,
  seenMessage: 0,
  live: false,
};

const $ = (id) => document.getElementById(id);

/** Builds an element. Strings become text nodes, never HTML. */
function el(tag, props = {}, ...children) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (v == null || v === false) continue;
    if (k === "class") e.className = v;
    else if (k.startsWith("on")) e.addEventListener(k.slice(2), v);
    else if (k === "text") e.textContent = v;
    else e.setAttribute(k, v === true ? "" : v);
  }
  for (const c of children.flat()) {
    if (c == null || c === false) continue;
    e.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return e;
}

// ---- Source tags --------------------------------------------------------

/** Tag chips for a list of sources: RF, IP, RF+IP, aprs.fi. */
function tags(sources, extra = "") {
  const s = new Set(sources);
  const chips = [];
  if (s.has("rf") && s.has("is")) chips.push(el("span", { class: "tag both", title: "Heard on RF and on APRS-IS" }, "RF+IP"));
  else if (s.has("rf")) chips.push(el("span", { class: "tag rf", title: "Heard on RF" }, "RF"));
  else if (s.has("is")) chips.push(el("span", { class: "tag is", title: "Received from APRS-IS" }, "IP"));
  if (s.has("aprsfi")) chips.push(el("span", { class: "tag fi", title: "From an aprs.fi lookup" }, "aprs.fi" + extra));
  return el("span", { class: "tags" }, chips);
}

function stationSources(st) {
  const s = [];
  if (st.lastRF) s.push("rf");
  if (st.lastIS) s.push("is");
  if (st.lastAPRSFI) s.push("aprsfi");
  return s;
}

/** The second line under a packet: how it travelled before it reached us. */
function origin(p) {
  const parts = [];
  if (p.rf) {
    if (p.rf.gateway) parts.push(`RF: third-party, gated from APRS-IS by ${p.rf.gateway}`);
    else if (p.rf.direct) parts.push("RF: heard direct");
    else parts.push(`RF: via ${p.rf.digis.join(", ")}`);
  }
  if (p.is) {
    if (p.is.fromRF) parts.push(`IP: igated by ${p.is.via || "an IGate"} (${p.is.q})`);
    else if (p.is.q === "qAC" || p.is.q === "qAX" || p.is.q === "qAU") parts.push(`IP: sent over the internet (${p.is.q}${p.is.via ? " via " + p.is.via : ""})`);
    else if (p.is.q) parts.push(`IP: ${p.is.q}${p.is.via ? " via " + p.is.via : ""}`);
    else parts.push("IP: no q construct");
  }
  return parts.join(" · ");
}

const matchesFilter = (sources) => state.filter === "all" || sources.includes(state.filter);

// ---- Formatting --------------------------------------------------------

const ms = (t) => (t ? Date.parse(t) : 0);
const ago = (t) => (t ? formatAge(Date.now() - ms(t)) : "never");
const since = (t) => (!t ? "never" : ago(t) === "now" ? "just now" : `${ago(t)} ago`);
const clock = (t) => new Date(t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
const lastActivity = (st) => Math.max(ms(st.lastHeard), ms(st.lastAPRSFI));

function whereFromMe(pos) {
  const me = state.info;
  if (!me || !pos) return "";
  const a = { lat: me.lat, lon: me.lon };
  const km = distanceKm(a, pos);
  return `${km < 10 ? km.toFixed(1) : Math.round(km)} km ${compass(bearing(a, pos))}`;
}

function glyph(sym) {
  const s = symbolInfo(sym);
  return el("span", { class: "glyph", title: s.name, "aria-hidden": "true" }, s.emoji ?? s.fallback);
}

// ---- Rendering ---------------------------------------------------------

let renderQueued = false;
function render() {
  if (renderQueued) return;
  renderQueued = true;
  setTimeout(() => {
    renderQueued = false;
    renderNow();
  }, 150);
}

function renderNow() {
  renderHeader();
  for (const t of ["stations", "packets", "messages"]) {
    $(`tab-${t}`).hidden = state.tab !== t;
    $(`tabbtn-${t}`).setAttribute("aria-selected", String(state.tab === t));
  }
  $("count-stations").textContent = String(state.stations.size);
  const unread = [...state.messages.values()].filter((m) => !m.bulletin && m.kind === "message" && m.id > state.seenMessage).length;
  $("count-messages").textContent = unread ? String(unread) : "";
  $("count-messages").hidden = !unread;
  if (state.tab === "stations") renderStations();
  if (state.tab === "packets") renderPackets();
  if (state.tab === "messages") renderMessages();
  for (const b of document.querySelectorAll(".filter")) b.setAttribute("aria-pressed", String(b.dataset.filter === state.filter));
  $("aprsfi-credit").hidden = ![...state.stations.values()].some((s) => s.lastAPRSFI);

  const stations = [...state.stations.values()].filter((s) => s.key === state.selected || matchesFilter(stationSources(s)));
  map.setData({ stations, me: state.info && { lat: state.info.lat, lon: state.info.lon, call: state.info.myCall }, selected: state.selected });
}

function renderHeader() {
  const info = state.info;
  $("mycall").textContent = info?.myCall ?? "";
  $("demo-badge").hidden = !info?.demo;
  const tx = $("tx-badge");
  tx.textContent = info?.rfTransmit ? "RF transmit ON" : "RF transmit off";
  tx.className = "badge " + (info?.rfTransmit ? "tx-on" : "tx-off");
  for (const node of document.querySelectorAll(".src")) {
    const st = state.status[node.dataset.src];
    node.dataset.state = st?.state ?? "off";
    node.querySelector(".state").textContent = st?.state ?? "off";
    node.title = st?.detail ?? "";
  }
  const conn = $("conn");
  conn.textContent = state.live ? "live" : "reconnecting…";
  conn.dataset.live = String(state.live);
}

function renderStations() {
  const detail = state.selected && state.stations.get(state.selected);
  $("station-list-wrap").hidden = !!detail;
  $("station-detail").hidden = !detail;
  if (detail) return renderDetail(detail);

  const q = state.search.trim().toUpperCase();
  const list = [...state.stations.values()]
    .filter((s) => matchesFilter(stationSources(s)) && (!q || s.name.toUpperCase().includes(q)))
    .sort((a, b) => lastActivity(b) - lastActivity(a));
  const ul = $("station-list");
  ul.replaceChildren(...list.map((s) =>
    el("li", {},
      el("button", { class: "row", onclick: () => select(s.key, true) },
        glyph(s.symbol),
        el("span", { class: "row-main" },
          el("span", { class: "row-top" },
            el("span", { class: "call" }, s.name),
            s.kind !== "station" ? el("span", { class: "kind" }, s.kind) : null,
            tags(stationSources(s)),
            el("span", { class: "age" }, formatAge(Date.now() - lastActivity(s)))),
          el("span", { class: "row-sub" },
            el("span", { class: "comment" }, s.status || s.comment || (s.pos ? "" : "no position")),
            el("span", { class: "dist" }, whereFromMe(s.pos))))))));
  $("station-empty").hidden = list.length > 0;
}

function renderDetail(s) {
  const pos = s.pos;
  const rows = [];
  const row = (k, ...v) => rows.push(el("dt", {}, k), el("dd", {}, ...v));
  if (s.kind !== "station") row("Kind", `${s.kind}${s.owner ? " from " + s.owner : ""}${s.killed ? " (deleted)" : ""}`);
  if (pos) {
    row("Position", el("span", { class: "mono" }, `${pos.lat.toFixed(5)}, ${pos.lon.toFixed(5)}`), " · ", el("span", { class: "mono" }, locator(pos.lat, pos.lon)),
      pos.ambiguity ? ` · ambiguous (${pos.ambiguity} digit${pos.ambiguity > 1 ? "s" : ""})` : "");
    row("Position from", tags(pos.sources), ` ${since(pos.at)}`);
    if (state.info) row("From you", whereFromMe(pos));
    if (pos.hasCourse && pos.speed > 0) row("Moving", `${Math.round(pos.speed)} kn (${Math.round(pos.speed * 1.852)} km/h)${pos.course ? ` at ${pos.course}°` : ""}`);
    if (pos.hasAlt) row("Altitude", `${Math.round(pos.alt)} m`);
  } else {
    row("Position", "none heard yet");
  }
  row("Symbol", symbolInfo(s.symbol).name);
  if (s.micE) row("Mic-E", s.micE);
  if (s.comment) row("Comment", s.comment);
  if (s.status) row("Status", s.status);
  row("Last on RF", since(s.lastRF));
  row("Last on APRS-IS", since(s.lastIS));
  if (s.lastAPRSFI) row("aprs.fi report", `${ago(s.lastAPRSFI)} old`);
  row("Packets", String(s.packets));

  const packets = state.packets.filter((p) => p.station === s.key || p.src.toUpperCase() === s.key).slice(-30).reverse();
  $("station-detail-body").replaceChildren(...[
    el("div", { class: "detail-head" },
      glyph(s.symbol),
      el("h2", { class: "call" }, s.name),
      tags(stationSources(s))),
    el("div", { class: "actions" },
      pos ? el("button", { onclick: () => map.centreOn(pos.lat, pos.lon, 12) }, "Show on map") : null,
      state.info?.aprsfi && s.kind === "station" ? el("button", { onclick: () => lookup(s.name) }, "Look up on aprs.fi") : null,
      el("a", { href: `https://aprs.fi/#!call=a%2F${encodeURIComponent(s.name)}`, target: "_blank", rel: "noopener noreferrer" }, "Open aprs.fi ↗")),
    el("dl", { class: "facts" }, rows),
    s.lastAPRSFI ? el("p", { class: "credit" }, "Location data from ", el("a", { href: "https://aprs.fi/", target: "_blank", rel: "noopener noreferrer" }, "aprs.fi")) : null,
    el("h3", {}, "Recent packets"),
    packets.length ? el("ol", { class: "packets" }, packets.map(packetItem)) : el("p", { class: "empty" }, "None in this session."),
  ].filter(Boolean));
}

function packetItem(p) {
  return el("li", { class: "packet" + (p.kind === "malformed" ? " bad" : "") },
    el("div", { class: "packet-top" },
      el("time", { datetime: p.at }, clock(p.at)),
      tags(p.sources),
      el("button", { class: "link call", onclick: () => select(p.station, true) }, p.station.startsWith(";") ? p.station.slice(1) : p.src),
      p.dupes ? el("span", { class: "dupes", title: "Further copies from the same source" }, `+${p.dupes}`) : null),
    el("div", { class: "summary" }, p.summary),
    el("div", { class: "origin" }, origin(p)),
    el("details", {}, el("summary", {}, "raw"), el("code", {}, p.raw)));
}

function renderPackets() {
  $("pause").textContent = state.paused ? `Resume (${state.pausedCount} new)` : "Pause";
  if (state.paused) return;
  const list = state.packets.filter((p) => matchesFilter(p.sources)).slice(-300).reverse();
  $("packet-list").replaceChildren(...list.map(packetItem));
}

function renderMessages() {
  const msgs = [...state.messages.values()].filter((m) => matchesFilter(m.sources));
  const threads = new Map();
  for (const m of msgs) {
    const k = m.bulletin ? "Bulletins" : m.from;
    if (!threads.has(k)) threads.set(k, []);
    threads.get(k).push(m);
  }
  const ordered = [...threads.entries()].sort((a, b) => ms(b[1].at(-1).at) - ms(a[1].at(-1).at));
  $("threads").replaceChildren(...ordered.map(([who, list]) =>
    el("section", { class: "thread" },
      el("h3", {}, el("span", { class: "call" }, who)),
      el("ol", {}, list.map((m) =>
        el("li", { class: "msg " + m.kind },
          el("div", { class: "packet-top" },
            el("time", { datetime: m.at }, clock(m.at)),
            tags(m.sources),
            m.bulletin ? el("span", { class: "call" }, `${m.from} → ${m.to}`) : null,
            m.msgId ? el("span", { class: "msgid" }, `#${m.msgId}`) : null),
          el("div", { class: "text" }, m.kind === "message" ? m.text : `${m.kind} for #${m.msgId}`)))))));
  $("messages-empty").hidden = ordered.length > 0;
  state.seenMessage = Math.max(state.seenMessage, ...[...state.messages.values()].map((m) => m.id));
}

// ---- Actions -----------------------------------------------------------

function select(key, show) {
  state.selected = key;
  state.tab = "stations";
  const st = state.stations.get(key);
  if (show && st?.pos) map.centreOn(st.pos.lat, st.pos.lon);
  render();
}

async function lookup(call) {
  const msg = $("lookup-msg");
  msg.textContent = `Asking aprs.fi about ${call}…`;
  msg.dataset.kind = "";
  try {
    const r = await fetch("/api/lookup", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ call }) });
    const body = await r.json();
    if (!r.ok) throw new Error(body.error || `HTTP ${r.status}`);
    if (!body.keys.length) {
      msg.textContent = `aprs.fi has no position for ${call}.`;
      return;
    }
    msg.textContent = `Found ${body.keys.length === 1 ? call : body.keys.length + " entries"} on aprs.fi.`;
    // The station event may arrive after the reply; select once it has.
    const key = body.keys[0];
    const pick = () => (state.stations.has(key) ? select(key, true) : setTimeout(pick, 100));
    pick();
  } catch (e) {
    msg.textContent = String(e.message || e);
    msg.dataset.kind = "error";
  }
}

// ---- Events ------------------------------------------------------------

function addPacket(p) {
  const old = state.packetById.get(p.id);
  if (old) {
    Object.assign(old, p);
  } else {
    state.packets.push(p);
    state.packetById.set(p.id, p);
    if (state.packets.length > MAX_PACKETS) state.packetById.delete(state.packets.shift().id);
    if (state.paused) state.pausedCount++;
  }
}

function connect() {
  const es = new EventSource("/events");
  es.addEventListener("hello", (e) => {
    const { info, snapshot } = JSON.parse(e.data);
    const first = !state.info;
    state.info = info;
    state.stations = new Map(snapshot.stations.map((s) => [s.key, s]));
    state.packets = [];
    state.packetById = new Map();
    snapshot.packets.forEach(addPacket);
    state.messages = new Map(snapshot.messages.map((m) => [m.id, m]));
    state.status = snapshot.status;
    state.live = true;
    map.tilesOn = info.tiles;
    $("tiles-off").hidden = info.tiles;
    $("lookup-btn").disabled = !info.aprsfi;
    $("lookup-btn").title = info.aprsfi ? "" : "aprs.fi lookups are off: set D75APRS_APRSFI_KEY";
    if (first) map.setView(info.lat, info.lon, 11);
    render();
  });
  es.addEventListener("packet", (e) => {
    addPacket(JSON.parse(e.data));
    render();
  });
  es.addEventListener("station", (e) => {
    const s = JSON.parse(e.data);
    state.stations.set(s.key, s);
    render();
  });
  es.addEventListener("message", (e) => {
    const m = JSON.parse(e.data);
    state.messages.set(m.id, m);
    render();
  });
  es.addEventListener("status", (e) => {
    Object.assign(state.status, JSON.parse(e.data));
    render();
  });
  es.onopen = () => {
    state.live = true;
    render();
  };
  es.onerror = () => {
    state.live = false;
    render();
  };
}

// ---- Wiring ------------------------------------------------------------

const tooltip = $("tooltip");
const map = new MapView($("map"), {
  tiles: true,
  onSelect: (key) => select(key, false),
  onHover: (key, x, y) => {
    const s = key && state.stations.get(key);
    if (!s) {
      tooltip.hidden = true;
      return;
    }
    tooltip.replaceChildren(el("span", { class: "call" }, s.name), " ", tags(s.pos?.sources ?? []), " ", el("span", { class: "age" }, ago(s.pos?.at)));
    tooltip.style.left = `${x + 16}px`;
    tooltip.style.top = `${y + 12}px`;
    tooltip.hidden = false;
  },
});

for (const t of ["stations", "packets", "messages"]) {
  $(`tabbtn-${t}`).addEventListener("click", () => {
    state.tab = t;
    if (t !== "stations") state.selected = null;
    render();
  });
}
for (const b of document.querySelectorAll(".filter")) {
  b.addEventListener("click", () => {
    state.filter = b.dataset.filter;
    render();
  });
}
$("back").addEventListener("click", () => {
  state.selected = null;
  render();
});
$("search").addEventListener("input", (e) => {
  state.search = e.target.value;
  render();
});
$("lookup").addEventListener("submit", (e) => {
  e.preventDefault();
  const call = $("search").value.trim().toUpperCase();
  if (call) lookup(call);
});
$("pause").addEventListener("click", () => {
  state.paused = !state.paused;
  state.pausedCount = 0;
  render();
});
$("zoom-in").addEventListener("click", () => map.zoomBy(1));
$("zoom-out").addEventListener("click", () => map.zoomBy(-1));
$("centre-me").addEventListener("click", () => state.info && map.centreOn(state.info.lat, state.info.lon));
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && state.selected) {
    state.selected = null;
    render();
  }
});
setInterval(render, 15000); // keep the ages current

connect();
