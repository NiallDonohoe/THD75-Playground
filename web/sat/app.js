// d75sat page: follows the server's event stream and draws the map, the
// next pass, the pass table, the satellite list and the selected
// satellite with its sky plot and transmitters. Names and descriptions
// come from the network, so they only ever reach the page through
// textContent.

import { SatMap } from "./satmap.js";
import { locator, fromLocator, formatAge } from "./map/geo.js";
import { doppler, skyXY, countdown, mhz, shift, passState, nextPass, dueAlerts, compass8 } from "./sat.js";

const STALE_ELEMENTS_MS = 14 * 86400e3;

const state = {
  info: null,
  now: Date.now(), // the program's clock, from the last tick
  cfg: null,
  sats: new Map(),
  passes: [],
  live: new Map(),
  status: {},
  selected: null,
  selPass: null, // the pass the sky plot shows: {pass, points}
  track: null,
  trackAt: 0,
  tab: "passes",
  minEl: 10,
  watchedOnly: false,
  search: "",
  follow: false,
  alerted: new Set(),
  passSig: "",
  connected: false,
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

async function postJSON(path, body) {
  const r = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || `HTTP ${r.status}`);
  return j;
}

// ---- Time --------------------------------------------------------------

const pad = (n) => String(n).padStart(2, "0");

/** "18:04:12" in UTC, with the weekday when it is not the clock's day. */
function utc(iso) {
  const d = new Date(iso), now = new Date(state.now);
  const t = `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`;
  if (d.toISOString().slice(0, 10) === now.toISOString().slice(0, 10)) return t;
  return `${d.toLocaleDateString("en-GB", { weekday: "short", timeZone: "UTC" })} ${t}`;
}

function length(p) {
  const s = Math.round((Date.parse(p.los) - Date.parse(p.aos)) / 1000);
  return `${Math.floor(s / 60)}:${pad(s % 60)}`;
}

// ---- Events ------------------------------------------------------------

function connect() {
  const es = new EventSource("/events");
  const on = (type, fn) => es.addEventListener(type, (e) => fn(JSON.parse(e.data)));
  on("hello", ({ info, snapshot }) => {
    state.info = info;
    state.connected = true;
    setCatalog(snapshot.sats);
    state.status = snapshot.status;
    state.cfg = snapshot.config;
    state.passes = snapshot.passes ?? [];
    state.now = Date.parse(snapshot.now);
    setLive(snapshot.live);
    startMap();
    renderAll();
    if (!state.cfg.location) openLocation("Set your location to see passes.");
  });
  on("tick", ({ now, live }) => {
    state.now = Date.parse(now);
    setLive(live);
    onTick();
  });
  on("passes", ({ passes }) => {
    state.passes = passes ?? [];
    state.passSig = "";
    renderPasses();
    renderSats();
    renderNext();
    if (state.selected) refreshSky();
  });
  on("catalog", ({ sats, status }) => {
    setCatalog(sats);
    state.status = status;
    renderAll();
  });
  on("config", (cfg) => {
    const moved = JSON.stringify(cfg.location) !== JSON.stringify(state.cfg?.location);
    state.cfg = cfg;
    renderHeader();
    renderAlertSettings();
    renderDetail();
    renderPasses();
    renderSats();
    if (moved) {
      if (cfg.location) map?.centreOn(cfg.location.lat, cfg.location.lon);
      refreshSky();
    }
  });
  on("status", (status) => {
    state.status = status;
    renderHeader();
  });
  es.onerror = () => {
    state.connected = false;
    renderConn();
  };
  es.onopen = () => renderConn();
}

function setCatalog(sats) {
  state.sats = new Map(sats.map((s) => [s.id, s]));
  if (state.selected && !state.sats.has(state.selected)) select(null);
}

function setLive(live) {
  state.live = new Map((live ?? []).map((l) => [l.id, l]));
}

// ---- Map ---------------------------------------------------------------

let map = null;

function startMap() {
  if (map) return;
  map = new SatMap($("map"), {
    tiles: state.info.tiles,
    onSelect: (id) => select(id),
    onHover: hover,
    onClickMap: (p) => {
      if (map.picking) pickDone(p);
    },
  });
  map.minZoom = 2;
  const loc = state.cfg.location;
  if (loc) map.setView(loc.lat, loc.lon, 3);
  else map.setView(25, 0, 2);
  $("tiles-off").hidden = state.info.tiles;
  $("zoom-in").onclick = () => map.zoomBy(1);
  $("zoom-out").onclick = () => map.zoomBy(-1);
  $("centre-me").onclick = () => {
    const l = state.cfg.location;
    if (l) map.centreOn(l.lat, l.lon);
    else openLocation();
  };
  $("centre-sat").onclick = () => {
    state.follow = !state.follow;
    $("centre-sat").setAttribute("aria-pressed", String(state.follow));
    followSat();
  };
}

function followSat() {
  const l = state.live.get(state.selected);
  if (state.follow && l) map.centreOn(l.lat, l.lon);
}

function renderMap() {
  if (!map) return;
  map.setData({
    live: [...state.live.values()],
    names: new Map([...state.sats.values()].map((s) => [s.id, s.name])),
    track: state.track,
    selected: state.selected,
    station: state.cfg?.location ?? null,
  });
}

function hover(id, x, y) {
  const tip = $("tooltip");
  if (!id) {
    tip.hidden = true;
    return;
  }
  const s = state.sats.get(id), l = state.live.get(id);
  tip.textContent = `${s?.name ?? id}${l && state.cfg.location ? ` · ${l.el > 0 ? `in view, ${l.el.toFixed(0)}°` : "below your horizon"}` : ""}`;
  tip.style.left = `${x + 14}px`;
  tip.style.top = `${y + 10}px`;
  tip.hidden = false;
}

// ---- Selection, tracks and the sky plot -------------------------------

function select(id, pass = null) {
  state.selected = id;
  state.track = null;
  state.selPass = null;
  renderSats();
  renderPasses();
  renderDetail();
  renderMap();
  if (!id) return;
  refreshTrack();
  refreshSky(pass);
  followSat();
}

async function refreshTrack() {
  const id = state.selected;
  if (!id) return;
  state.trackAt = performance.now();
  try {
    const r = await fetch(`/api/track?id=${encodeURIComponent(id)}`);
    if (!r.ok || state.selected !== id) return;
    state.track = await r.json();
    renderMap();
  } catch {
    // The next tick tries again.
  }
}

/** Fetches the sky track of a given pass, or of the selected satellite's current or next one. */
async function refreshSky(pass = null) {
  const id = state.selected;
  if (!id || !state.cfg?.location) return;
  const at = pass ? new Date(Date.parse(pass.aos) - 1000).toISOString() : new Date(state.now).toISOString();
  try {
    const r = await fetch(`/api/pass?id=${encodeURIComponent(id)}&at=${encodeURIComponent(at)}`);
    if (state.selected !== id) return;
    state.selPass = r.ok ? await r.json() : null;
  } catch {
    state.selPass = null;
  }
  renderPasses();
  renderDetail();
}

function drawSky() {
  const cv = $("sky"), ctx = cv.getContext("2d");
  const dpr = window.devicePixelRatio || 1, size = 220;
  cv.width = size * dpr;
  cv.height = size * dpr;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, size, size);
  const css = getComputedStyle(document.documentElement);
  const color = (n) => css.getPropertyValue(n).trim();
  const R = 92, cx = size / 2, cy = size / 2;
  ctx.strokeStyle = color("--border");
  ctx.lineWidth = 1;
  for (const el of [0, 30, 60]) {
    ctx.beginPath();
    ctx.arc(cx, cy, (R * (90 - el)) / 90, 0, 2 * Math.PI);
    ctx.stroke();
  }
  ctx.beginPath();
  ctx.moveTo(cx - R, cy);
  ctx.lineTo(cx + R, cy);
  ctx.moveTo(cx, cy - R);
  ctx.lineTo(cx, cy + R);
  ctx.stroke();
  ctx.fillStyle = color("--muted");
  ctx.font = "11px ui-monospace, Menlo, monospace";
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  for (const [t, az] of [["N", 0], ["E", 90], ["S", 180], ["W", 270]]) {
    const p = skyXY(az, -8, R);
    ctx.fillText(t, cx + p.x * 1.1, cy + p.y * 1.1);
  }
  ctx.font = "9px ui-monospace, Menlo, monospace";
  ctx.fillText("30°", cx + 4 + (R * 60) / 90, cy - 6);
  ctx.fillText("60°", cx + 4 + (R * 30) / 90, cy - 6);

  const sp = state.selPass;
  if (!sp?.points?.length) return;
  const pts = sp.points.map((p) => skyXY(p.az, p.el, R));
  ctx.beginPath();
  pts.forEach((p, i) => (i ? ctx.lineTo(cx + p.x, cy + p.y) : ctx.moveTo(cx + p.x, cy + p.y)));
  ctx.strokeStyle = color("--accent");
  ctx.lineWidth = 2;
  ctx.stroke();
  const mark = (p, fill, label) => {
    ctx.beginPath();
    ctx.arc(cx + p.x, cy + p.y, 3.5, 0, 2 * Math.PI);
    ctx.fillStyle = fill;
    ctx.fill();
    if (label) {
      ctx.fillStyle = color("--text");
      ctx.font = "10px ui-monospace, Menlo, monospace";
      ctx.fillText(label, cx + p.x * 0.82, cy + p.y * 0.82);
    }
  };
  mark(pts[0], color("--up"), "AOS");
  mark(pts[pts.length - 1], color("--muted"), "LOS");
  // The satellite now, if this pass is under way.
  const l = state.live.get(state.selected);
  if (l && passState(sp.pass, state.now) === "up" && l.el > 0) {
    const p = skyXY(l.az, l.el, R);
    ctx.beginPath();
    ctx.arc(cx + p.x, cy + p.y, 6, 0, 2 * Math.PI);
    ctx.fillStyle = color("--accent");
    ctx.fill();
    ctx.lineWidth = 2;
    ctx.strokeStyle = color("--halo");
    ctx.stroke();
  }
}

// ---- Rendering ---------------------------------------------------------

function renderAll() {
  renderHeader();
  renderNext();
  renderPasses();
  renderSats();
  renderDetail();
  renderAlertSettings();
  renderMap();
}

function renderConn() {
  const c = $("conn");
  c.dataset.live = String(state.connected);
  c.textContent = state.connected ? "live" : "reconnecting…";
}

function renderHeader() {
  renderConn();
  $("demo-badge").hidden = !state.info?.demo;
  if (state.info?.demo) $("demo-badge").textContent = state.info.speed > 1 ? `Demo · clock ×${state.info.speed}` : "Demo";
  const loc = state.cfg?.location, btn = $("loc-btn");
  btn.dataset.set = String(!!loc);
  const src = { browser: "Mac location", map: "picked on the map", locator: "locator", flags: "command line", demo: "demo default" };
  $("loc-text").textContent = loc ? `${locator(loc.lat, loc.lon)} · ${src[loc.source] ?? loc.source}` : "Set your location";
  for (const node of document.querySelectorAll(".src")) {
    const st = state.status?.[node.dataset.src] ?? {};
    let text = "", kind = "";
    if (state.info?.demo) {
      text = "demo data";
      kind = "ok";
    } else if (st.off) {
      text = st.fetched ? `cached, ${formatAge(Date.now() - Date.parse(st.fetched))} old` : "off";
      kind = "stale";
    } else if (st.error) {
      text = st.fetched ? `cached, ${formatAge(Date.now() - Date.parse(st.fetched))} old` : "unavailable";
      kind = st.fetched ? "stale" : "error";
      node.title = st.error;
    } else if (st.fetched) {
      text = `${formatAge(Date.now() - Date.parse(st.fetched))} old`;
      kind = "ok";
    }
    node.dataset.state = kind;
    node.querySelector(".state").textContent = text;
  }
  renderClock();
}

function renderClock() {
  const d = new Date(state.now);
  $("clock").textContent = `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())} UTC`;
}

function renderNext() {
  const box = $("next");
  box.replaceChildren();
  if (!state.cfg?.location) {
    box.append(el("div", { class: "label" }, "Next pass"), el("div", { class: "sub" }, "Set your location to see passes."));
    return;
  }
  const p = nextPass(state.passes, state.now, state.minEl);
  if (!p) {
    box.append(el("div", { class: "label" }, "Next pass"), el("div", { class: "sub" }, `None above ${state.minEl}° in the next day.`));
    return;
  }
  const up = passState(p, state.now) === "up";
  box.dataset.state = up ? "up" : "upcoming";
  const when = up ? `in view · sets in ${countdown(Date.parse(p.los) - state.now)}` : `rises in ${countdown(Date.parse(p.aos) - state.now)}`;
  box.append(
    el("div", { class: "label" }, up ? "In view now" : "Next pass"),
    el("div", { class: "big" }, el("button", { class: "link name", onclick: () => select(p.id, p) }, p.name), el("span", { class: "when" }, when)),
    el("div", { class: "sub" }, `AOS ${utc(p.aos)} UTC from ${compass8(p.aosAz)} · peak ${p.maxEl.toFixed(0)}° · LOS towards ${compass8(p.losAz)}`),
  );
}

function visiblePasses() {
  return state.passes.filter((p) => p.maxEl >= state.minEl && Date.parse(p.los) >= state.now && (!state.watchedOnly || state.cfg?.alerts?.[p.id]));
}

function renderPasses() {
  const rows = $("pass-rows");
  const list = visiblePasses();
  $("count-passes").textContent = list.length || "";
  rows.replaceChildren(...list.slice(0, 300).map((p) => {
    const st = passState(p, state.now);
    const selected = state.selPass && state.selPass.pass.id === p.id && state.selPass.pass.aos.slice(0, 19) === p.aos.slice(0, 19);
    return el("tr", { "data-state": st, "aria-selected": selected ? "true" : "false", tabindex: "0", onclick: () => select(p.id, p), onkeydown: (e) => e.key === "Enter" && select(p.id, p) },
      el("td", { class: "mono" }, st === "up" ? "in view" : utc(p.aos)),
      el("td", { class: state.cfg?.alerts?.[p.id] ? "bell-on" : null }, p.name),
      el("td", { class: "mono" }, length(p)),
      el("td", { class: "mono" }, `${p.maxEl.toFixed(0)}°`),
      el("td", { class: "mono" }, `${compass8(p.aosAz)} → ${compass8(p.losAz)}`),
    );
  }));
  const empty = $("pass-empty");
  empty.hidden = list.length > 0;
  empty.textContent = !state.cfg?.location ? "Set your location to see passes." : `No passes peaking at ${state.minEl}° or more in the next day.`;
  state.passSig = passSignature();
}

/** What the pass table shows that changes with time: which passes are up, and which are over. */
function passSignature() {
  return visiblePasses().map((p) => `${p.id}${passState(p, state.now)}`).join(",");
}

function renderSats() {
  const q = state.search.trim().toLowerCase();
  const sats = [...state.sats.values()].filter((s) => !q || s.name.toLowerCase().includes(q) || s.id.includes(q));
  $("count-sats").textContent = state.sats.size || "";
  $("sat-list").replaceChildren(...sats.map((s) => {
    const l = state.live.get(s.id);
    let sub;
    if (!s.predicted) sub = el("span", { class: "sub np", title: s.problem }, "not predicted");
    else if (l && state.cfg?.location && l.el > 0) sub = el("span", { class: "sub up" }, `in view, ${l.el.toFixed(0)}°`);
    else {
      const p = state.passes.find((p) => p.id === s.id && Date.parse(p.los) >= state.now);
      sub = el("span", { class: "sub" }, p ? `next ${utc(p.aos)} · ${p.maxEl.toFixed(0)}°` : state.cfg?.location ? "no pass today" : "");
    }
    return el("li", {}, el("button", { "aria-current": s.id === state.selected ? "true" : "false", onclick: () => select(s.id) },
      el("span", { class: state.cfg?.alerts?.[s.id] ? "nm bell-on" : "nm" }, s.name), sub));
  }));
}

function renderDetail() {
  const box = $("detail");
  const s = state.sats.get(state.selected);
  box.hidden = !s;
  if (!s) return;
  $("d-name").textContent = s.name;
  const watched = !!state.cfg?.alerts?.[s.id];
  $("d-alert").setAttribute("aria-pressed", String(watched));
  $("d-alert").disabled = !s.predicted;
  renderDetailLive();
  const links = $("d-links");
  links.replaceChildren();
  if (s.satnogs) links.append("More about it: ", el("a", { href: s.satnogs, target: "_blank", rel: "noopener noreferrer" }, "SatNOGS DB entry"));
}

function renderDetailLive() {
  const s = state.sats.get(state.selected);
  if (!s) return;
  const l = state.live.get(s.id), loc = state.cfg?.location;
  const facts = [["Catalogue", s.id]];
  const age = state.now - Date.parse(s.epoch);
  facts.push(["Elements", `${formatAge(Math.abs(age))} old${age > STALE_ELEMENTS_MS ? " — stale, predictions may be off" : ""}`]);
  if (!s.predicted) facts.push(["Orbit", s.problem]);
  if (s.periodMin) facts.push(["Period", `${s.periodMin} min`]);
  if (l) {
    facts.push(["Height", `${l.alt.toFixed(0)} km`]);
    facts.push(["Over", `${Math.abs(l.lat).toFixed(2)}°${l.lat >= 0 ? "N" : "S"} ${Math.abs(l.lon).toFixed(2)}°${l.lon >= 0 ? "E" : "W"}`]);
    if (loc) facts.push(["From you", `az ${l.az.toFixed(0)}° (${compass8(l.az)}) · el ${l.el.toFixed(1)}° · ${l.range.toFixed(0)} km`]);
  }
  $("d-facts").replaceChildren(el("dl", { class: "facts" }, facts.flatMap(([k, v]) => [el("dt", {}, k), el("dd", {}, v)])));

  const sp = state.selPass;
  const pf = $("d-pass");
  if (!loc) pf.textContent = "Set your location to see its passes.";
  else if (!sp) pf.textContent = s.predicted ? "No pass in the next day." : "";
  else {
    const st = passState(sp.pass, state.now);
    const lead = st === "up" ? `In view now, sets in ${countdown(Date.parse(sp.pass.los) - state.now)}` : st === "upcoming" ? `Rises in ${countdown(Date.parse(sp.pass.aos) - state.now)}` : "Pass over";
    pf.textContent = `${lead} · AOS ${utc(sp.pass.aos)} · peak ${sp.pass.maxEl.toFixed(0)}° at ${utc(sp.pass.tca)} · LOS ${utc(sp.pass.los)} UTC`;
  }
  drawSky();

  const inView = l && loc && l.el > 0;
  $("d-tx").replaceChildren(...s.transmitters.map((t) => {
    const band = t.downlinkTop ? `${(t.downlink / 1e6).toFixed(3)}–${(t.downlinkTop / 1e6).toFixed(3)} MHz` : mhz(t.downlink);
    const mode = `${t.mode}${t.mode.toUpperCase() !== t.radioMode ? ` (${t.radioMode})` : ""}${t.invert ? ", inverting" : ""}`;
    let heard;
    if (inView) {
      const f = doppler(t.downlink, l.rr);
      heard = [t.downlinkTop ? `${(f / 1e6).toFixed(4)}–${(doppler(t.downlinkTop, l.rr) / 1e6).toFixed(4)}` : mhz(f), el("span", { class: "shift" }, shift(f - t.downlink))];
    } else heard = [el("span", { class: "dim" }, loc ? "not in view" : "—")];
    const title = t.uplink ? `Uplink ${(t.uplink / 1e6).toFixed(3)}${t.uplinkTop ? `–${(t.uplinkTop / 1e6).toFixed(3)}` : ""} MHz, set by hand` : null;
    return el("tr", { title }, el("td", {}, el("div", { class: "mono" }, band), el("div", { class: "note" }, `${t.description} · ${mode}`)), el("td", {}, heard));
  }));
}

function renderAlertSettings() {
  if (!state.cfg) return;
  if (document.activeElement?.closest?.("#alert-form") == null) {
    $("alert-min").value = state.cfg.alertMinutes;
    $("alert-el").value = state.cfg.alertMinEl;
  }
  const n = "Notification" in window ? Notification.permission : "unsupported";
  const text = {
    granted: "Chrome notifications are allowed; alerts also show at the top of this page.",
    denied: "Chrome notifications are blocked for this page, so alerts only show at the top of it.",
    default: "Turning an alert on asks Chrome to allow notifications. The page has to stay open.",
    unsupported: "This browser has no notifications; alerts show at the top of the page.",
  };
  $("notify-state").textContent = text[n];
}

// ---- Every tick --------------------------------------------------------

function onTick() {
  renderClock();
  renderNext();
  if (passSignature() !== state.passSig) {
    renderPasses();
    renderSats();
  }
  if (state.selected) {
    renderDetailLive();
    if (performance.now() - state.trackAt > 20000) refreshTrack();
    if (state.selPass && passState(state.selPass.pass, state.now) === "over") refreshSky();
    followSat();
  }
  renderMap();
  checkAlerts();
}

function checkAlerts() {
  if (!state.cfg) return;
  for (const { pass: p, key } of dueAlerts(state.passes, state.cfg, state.now, state.alerted)) {
    state.alerted.add(key);
    const title = `${p.name} rises in ${countdown(Date.parse(p.aos) - state.now)}`;
    const body = `AOS ${utc(p.aos)} UTC from ${compass8(p.aosAz)}, peak ${p.maxEl.toFixed(0)}°`;
    $("alert-text").textContent = `${title} · ${body}`;
    $("alert-banner").hidden = false;
    if ("Notification" in window && Notification.permission === "granted") {
      const n = new Notification(title, { body, tag: key });
      n.onclick = () => {
        window.focus();
        select(p.id, p);
      };
    }
  }
}

// ---- Location ----------------------------------------------------------

function openLocation(message = "") {
  $("loc-panel").hidden = false;
  setLocMsg(message);
}

function setLocMsg(text, kind = "") {
  const m = $("loc-msg");
  m.textContent = text;
  m.dataset.kind = kind;
}

async function setLocation(body) {
  try {
    await postJSON("/api/location", body);
    setLocMsg("");
    $("loc-panel").hidden = true;
  } catch (e) {
    setLocMsg(e.message, "error");
  }
}

function useMacLocation() {
  if (!navigator.geolocation || !window.isSecureContext) {
    setLocMsg("This browser cannot give the Mac's location here. Pick on the map or type a locator.", "error");
    return;
  }
  setLocMsg("Asking Chrome for this Mac's location…");
  navigator.geolocation.getCurrentPosition(
    (pos) => setLocation({ lat: pos.coords.latitude, lon: pos.coords.longitude, altM: pos.coords.altitude ?? 0, source: "browser" }),
    (err) => {
      const why = {
        1: "Chrome was not allowed to give this page your location. Allow it for this site in Chrome's settings (and for Chrome in macOS Location Services), or pick on the map.",
        2: "The Mac could not work out where it is. Pick on the map or type a locator.",
        3: "The Mac took too long to find its location. Try again, or pick on the map.",
      };
      setLocMsg(why[err.code] ?? err.message, "error");
    },
    { enableHighAccuracy: false, timeout: 20000, maximumAge: 10 * 60000 },
  );
}

function pickStart() {
  $("loc-panel").hidden = true;
  $("pick-hint").hidden = false;
  map.setPicking(true);
}

function pickEnd() {
  $("pick-hint").hidden = true;
  map.setPicking(false);
}

function pickDone(p) {
  pickEnd();
  setLocation({ lat: p.lat, lon: p.lon, altM: 0, source: "map" });
}

// ---- Controls ----------------------------------------------------------

function setTab(tab) {
  state.tab = tab;
  for (const t of ["passes", "sats"]) {
    $(`tabbtn-${t}`).setAttribute("aria-selected", String(t === tab));
    $(`tab-${t}`).hidden = t !== tab;
  }
}

function bind() {
  $("tabbtn-passes").onclick = () => setTab("passes");
  $("tabbtn-sats").onclick = () => setTab("sats");
  setTab("passes");
  $("min-el").onchange = (e) => {
    state.minEl = Number(e.target.value);
    renderPasses();
    renderNext();
  };
  $("watched-only").onchange = (e) => {
    state.watchedOnly = e.target.checked;
    renderPasses();
  };
  $("search").oninput = (e) => {
    state.search = e.target.value;
    renderSats();
  };
  $("d-close").onclick = () => select(null);
  $("d-alert").onclick = async () => {
    const id = state.selected, on = !state.cfg?.alerts?.[id];
    if (on && "Notification" in window && Notification.permission === "default") {
      await Notification.requestPermission();
      renderAlertSettings();
    }
    try {
      await postJSON("/api/alert", { id, on });
    } catch (e) {
      $("notify-state").textContent = e.message;
    }
  };
  $("alert-form").onsubmit = async (e) => {
    e.preventDefault();
    try {
      await postJSON("/api/alert-timing", { minutes: Number($("alert-min").value), minEl: Number($("alert-el").value) });
      $("alert-min").blur();
      $("alert-el").blur();
    } catch (err) {
      $("notify-state").textContent = err.message;
    }
  };
  $("alert-dismiss").onclick = () => ($("alert-banner").hidden = true);
  $("loc-btn").onclick = () => ($("loc-panel").hidden ? openLocation() : ($("loc-panel").hidden = true));
  $("loc-close").onclick = () => ($("loc-panel").hidden = true);
  $("loc-mac").onclick = useMacLocation;
  $("loc-pick").onclick = pickStart;
  $("pick-cancel").onclick = pickEnd;
  $("loc-form").onsubmit = (e) => {
    e.preventDefault();
    const c = fromLocator($("loc-locator").value);
    if (!c) {
      setLocMsg("A locator is two letters A–R, two digits and optionally two letters A–X, such as IO63 or IO63wi.", "error");
      return;
    }
    setLocation({ lat: c.lat, lon: c.lon, altM: 0, source: "locator" });
  };
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      $("loc-panel").hidden = true;
      if (map?.picking) pickEnd();
    }
  });
}

bind();
connect();
