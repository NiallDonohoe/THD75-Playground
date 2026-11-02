// d75winlink page: follows the server's event stream and draws the
// folders, the message list, the reading pane, the compose form and the
// session log. Message text comes from other people, so it only ever
// reaches the page through textContent.

import { showAddress, formatSize, formatDuration, notLatin1, replySubject, replyBody, formatWhen, toBase64 } from "./format.js";

const DRAFT_KEY = "d75winlink.draft";
const FOLDER_NAMES = { in: "Inbox", out: "Outbox", sent: "Sent", archive: "Archive" };

const state = {
  info: null,
  mailbox: [],
  counts: {},
  session: { state: "idle" },
  routes: [],
  folder: "in",
  selected: null, // MID in the reading pane
  view: "empty", // empty, message, compose
  message: null, // the message in the reading pane
  live: false,
  files: [], // the draft's attachments: {name, size, data (base64)}
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

async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const resp = await fetch(path, opts);
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(data.error || `${resp.status} ${resp.statusText}`);
  return data;
}

const running = () => state.session.state === "connecting" || state.session.state === "exchanging";

// ---- Status bar and folders --------------------------------------------

function renderBar() {
  const info = state.info;
  if (info) {
    $("mycall").textContent = info.myCall;
    $("demo-badge").hidden = !info.demo;
    const tx = $("tx-badge");
    tx.textContent = info.rfTransmit ? "RF transmit ON" : "RF transmit off";
    tx.className = `badge ${info.rfTransmit ? "tx-on" : "tx-off"}`;
  }
  const conn = $("conn");
  conn.textContent = state.live ? "live" : "reconnecting…";
  conn.dataset.live = state.live;
}

function renderFolders() {
  for (const b of document.querySelectorAll(".folder")) {
    b.setAttribute("aria-current", String(b.dataset.folder === state.folder && state.view !== "compose"));
  }
  const unread = state.counts.unread || 0;
  const u = document.querySelector('[data-count="unread"]');
  u.textContent = unread ? unread : "";
  u.className = unread ? "count unread" : "count";
  document.querySelector('[data-count="out"]').textContent = state.counts.out || "";

  const sel = $("route");
  if (sel.options.length !== state.routes.length) {
    sel.replaceChildren(...state.routes.map((r) => el("option", { value: r.id }, r.label)));
  }
  const s = state.session;
  $("connect-btn").disabled = running() || !state.routes.length;
  $("stop-btn").disabled = !running();
  sel.disabled = running();
  const st = $("session-state");
  st.dataset.state = s.state;
  st.textContent = {
    idle: "Not connected.",
    connecting: "Connecting…",
    exchanging: "Exchanging mail…",
    done: `Done: ${s.detail || "nothing to do"}.`,
    failed: `Failed: ${s.detail}.`,
    stopped: "Stopped.",
  }[s.state] || s.state;

  const out = state.mailbox.filter((m) => m.folder === "out");
  const bytes = out.reduce((n, m) => n + (m.compressed || 0), 0);
  $("outbox-note").textContent = out.length
    ? `${out.length} waiting, ${formatSize(bytes)} compressed: ${formatDuration(airtime(bytes, 1200))} at 1200 bps.`
    : "Outbox empty: Connect still collects new mail.";
}

/** The server's rough airtime model (winlinkapp.Airtime). */
function airtime(bytes, bps) {
  return bytes / (bps >= 9600 ? 400 : 70);
}

// ---- Message list ------------------------------------------------------

function renderList() {
  const items = state.mailbox.filter((m) => m.folder === state.folder);
  $("list-h").textContent = FOLDER_NAMES[state.folder];
  const empty = $("list-empty");
  empty.hidden = items.length > 0;
  empty.textContent = {
    in: "No mail yet. Connect to collect it.",
    out: "Nothing waiting to send.",
    sent: "Nothing sent yet.",
    archive: "Nothing archived.",
  }[state.folder];

  $("list").replaceChildren(...items.map((m) => {
    const sentish = m.folder === "out" || m.folder === "sent";
    const who = sentish ? `To: ${m.to.map(showAddress).join(", ")}` : showAddress(m.from || "?");
    const sub = [];
    if (m.files) sub.push(el("span", { class: "clip", title: `${m.files} attachment(s)` }, ` ${m.files}`));
    sub.push(el("span", {}, formatSize(m.size)));
    if (m.folder === "out" && m.compressed) {
      sub.push(el("span", { title: "Compressed size and rough transfer time over 1200 bps packet" },
        `${formatSize(m.compressed)} on air, ${formatDuration(airtime(m.compressed, 1200))} at 1200`));
    }
    const row = el("button", {
      class: `row${m.read ? "" : " unread"}`,
      "aria-current": String(m.mid === state.selected && state.view === "message"),
      onclick: () => openMessage(m.mid),
    },
      el("span", { class: "row-top" }, el("span", { class: "who" }, who), el("time", { class: "when", datetime: m.date }, formatWhen(m.date))),
      el("span", { class: `subject${m.broken ? " broken" : ""}` }, m.subject || "(no subject)"),
      el("span", { class: "row-sub" }, sub),
    );
    return el("li", {}, row);
  }));
}

// ---- Reading pane ------------------------------------------------------

function showPane(view) {
  state.view = view;
  if (view === "empty") $("pane-empty").firstElementChild.textContent = "Select a message, or compose one.";
  $("pane-empty").hidden = view !== "empty";
  $("view").hidden = view !== "message";
  $("compose").hidden = view !== "compose";
  renderFolders();
  renderList();
}

async function openMessage(mid) {
  state.selected = mid;
  try {
    state.message = await api("GET", `/api/message/${encodeURIComponent(mid)}`);
  } catch (e) {
    state.message = null;
    state.selected = null;
    showPane("empty");
    $("pane-empty").firstElementChild.textContent = e.message;
    return;
  }
  renderMessage();
  showPane("message");
}

function renderMessage() {
  const m = state.message;
  $("view-subject").textContent = m.subject || "(no subject)";
  const facts = [
    ["From", showAddress(m.from || "?")],
    ["To", m.to.map(showAddress).join(", ")],
  ];
  if (m.cc && m.cc.length) facts.push(["Cc", m.cc.map(showAddress).join(", ")]);
  if (m.date && !m.date.startsWith("0001")) facts.push(["Date", new Date(m.date).toLocaleString()]);
  facts.push(["Size", formatSize(m.size)]);
  facts.push(["Message ID", m.mid]);
  $("view-facts").replaceChildren(...facts.flatMap(([k, v]) => [el("dt", {}, k), el("dd", {}, v)]));
  $("view-files").replaceChildren(...m.files.map((f, i) => el("li", {},
    el("a", { href: `/api/message/${encodeURIComponent(m.mid)}/file/${i}`, download: f.name }, f.name),
    el("span", { class: "size" }, formatSize(f.size)),
  )));
  $("view-body").textContent = m.raw != null ? `This message could not be read; as received:\n\n${m.raw}` : m.body;

  $("reply-btn").hidden = m.folder !== "in" && m.folder !== "archive";
  $("archive-btn").hidden = m.folder !== "in" && m.folder !== "sent";
  $("unarchive-btn").hidden = m.folder !== "archive";
  const del = $("delete-btn");
  del.hidden = m.folder !== "out" && m.folder !== "archive";
  del.textContent = m.folder === "out" ? "Delete (unsend)" : "Delete";
  disarm(del);
}

function disarm(btn) {
  btn.dataset.armed = "false";
  btn.textContent = btn.textContent.replace(/^Click again: /, "");
}

async function moveTo(to) {
  const m = state.message;
  try {
    await api("POST", "/api/move", { mid: m.mid, to });
    state.selected = null;
    showPane("empty");
  } catch (e) {
    flash(e.message);
  }
}

async function deleteMessage() {
  const btn = $("delete-btn");
  if (btn.dataset.armed !== "true") {
    btn.dataset.armed = "true";
    btn.textContent = `Click again: ${btn.textContent}`;
    setTimeout(() => disarm(btn), 4000);
    return;
  }
  try {
    await api("POST", "/api/delete", { mid: state.message.mid });
    state.selected = null;
    showPane("empty");
  } catch (e) {
    disarm(btn);
    flash(e.message);
  }
}

function flash(text) {
  showPane("empty");
  $("pane-empty").firstElementChild.textContent = text;
}

// ---- Compose -----------------------------------------------------------

const fields = () => ({
  to: $("c-to").value,
  cc: $("c-cc").value,
  subject: $("c-subject").value,
  body: $("c-body").value,
});

function draft() {
  return { ...fields(), files: state.files.map((f) => ({ name: f.name, data: f.data })) };
}

function saveDraft() {
  try {
    localStorage.setItem(DRAFT_KEY, JSON.stringify(fields()));
  } catch {
    // Storage is a convenience; the page works without it.
  }
}

function loadDraft() {
  try {
    return JSON.parse(localStorage.getItem(DRAFT_KEY) || "null");
  } catch {
    return null;
  }
}

function clearDraft() {
  try {
    localStorage.removeItem(DRAFT_KEY);
  } catch {
    // as above
  }
}

function openCompose(prefill) {
  const d = prefill || loadDraft() || {};
  $("c-to").value = d.to || "";
  $("c-cc").value = d.cc || "";
  $("c-subject").value = d.subject || "";
  $("c-body").value = d.body || "";
  if (prefill) {
    state.files = [];
    saveDraft();
  }
  renderFiles();
  showPane("compose");
  scheduleEstimate();
  (prefill ? $("c-body") : $("c-to")).focus();
  if (prefill) $("c-body").setSelectionRange(0, 0);
}

function renderFiles() {
  $("c-filelist").replaceChildren(...state.files.map((f, i) => el("li", {},
    el("span", {}, f.name),
    el("span", { class: "size" }, formatSize(f.size)),
    el("button", { class: "link", "aria-label": `Remove ${f.name}`, onclick: (e) => { e.preventDefault(); state.files.splice(i, 1); renderFiles(); scheduleEstimate(); } }, "✕"),
  )));
}

async function addFiles(list) {
  for (const file of list) {
    const buf = await file.arrayBuffer();
    state.files.push({ name: file.name, size: file.size, data: toBase64(buf) });
  }
  renderFiles();
  scheduleEstimate();
}

let estimateTimer = 0;
let estimateSeq = 0;
function scheduleEstimate() {
  clearTimeout(estimateTimer);
  estimateTimer = setTimeout(estimate, 350);
}

async function estimate() {
  const f = fields();
  const notes = [];
  const out = $("c-estimate");
  const bodyBad = notLatin1(f.body);
  if (bodyBad.length) {
    notes.push(el("li", { class: "warn" }, `Winlink cannot carry ${bodyBad.join(" ")} in the message text (it uses ISO-8859-1). Remove ${bodyBad.length > 1 ? "them" : "it"} to send.`));
  }
  if (!f.to.trim()) {
    out.dataset.kind = "";
    out.textContent = "Add a recipient to see the size and airtime.";
    $("c-notes").replaceChildren(...notes);
    return;
  }
  const seq = ++estimateSeq;
  let e;
  try {
    e = await api("POST", "/api/estimate", draft());
  } catch (err) {
    if (seq !== estimateSeq) return;
    // Characters outside ISO-8859-1 are already explained in the notes.
    out.dataset.kind = bodyBad.length ? "" : "error";
    out.textContent = bodyBad.length ? "" : err.message;
    $("c-to").setAttribute("aria-invalid", String(/^To:/.test(err.message)));
    $("c-cc").setAttribute("aria-invalid", String(/^Cc:/.test(err.message)));
    $("c-notes").replaceChildren(...notes);
    return;
  }
  if (seq !== estimateSeq) return;
  $("c-to").setAttribute("aria-invalid", "false");
  $("c-cc").setAttribute("aria-invalid", "false");
  out.dataset.kind = "";
  out.textContent = `${formatSize(e.size)}, ${formatSize(e.compressed)} compressed. Over RF: ${formatDuration(e.secs1200)} at 1200 bps, ${formatDuration(e.secs9600)} at 9600 bps.`;
  if (e.smtp) {
    notes.push(el("li", {}, "Winlink only delivers replies from internet addresses on your account's whitelist (or with //WL2K in the subject). Ask the recipient to reply to this message, which adds them."));
  }
  notes.push(el("li", {}, "Over RF, amateur radio rules apply to everything you send: no business, no music, no obscenity, nothing obscured."));
  $("c-notes").replaceChildren(...notes);
}

async function queue(ev) {
  ev.preventDefault();
  const btn = $("c-queue");
  btn.disabled = true;
  try {
    const sum = await api("POST", "/api/queue", draft());
    clearDraft();
    state.files = [];
    state.folder = "out";
    await openMessage(sum.mid);
  } catch (e) {
    const out = $("c-estimate");
    out.dataset.kind = "error";
    out.textContent = e.message;
  } finally {
    btn.disabled = false;
  }
}

function discard() {
  clearDraft();
  state.files = [];
  for (const id of ["c-to", "c-cc", "c-subject", "c-body"]) $(id).value = "";
  showPane("empty");
}

function reply() {
  const m = state.message;
  openCompose({
    to: showAddress(m.from),
    cc: "",
    subject: replySubject(m.subject || ""),
    body: replyBody(m.from, m.date, m.body || ""),
  });
}

// ---- Session log -------------------------------------------------------

function logItem(l) {
  const arrow = { tx: "→", rx: "←", info: "·" }[l.dir] || "";
  return el("li", { class: l.dir },
    el("time", { datetime: l.t }, new Date(l.t).toLocaleTimeString([], { hour12: false })),
    el("span", { class: "dir", "aria-label": { tx: "sent", rx: "received", info: "" }[l.dir] }, arrow),
    el("span", {}, l.text),
  );
}

function appendLog(l) {
  const log = $("log");
  const atBottom = log.scrollHeight - log.scrollTop - log.clientHeight < 30;
  log.append(logItem(l));
  while (log.childElementCount > 400) log.firstElementChild.remove();
  if (atBottom) log.scrollTop = log.scrollHeight;
}

function renderProgress(p) {
  const box = $("progress");
  if (!p || !running()) {
    box.hidden = true;
    return;
  }
  box.hidden = false;
  $("progress-label").textContent = `${p.sending ? "Sending" : "Receiving"} ${p.mid}: ${formatSize(p.done)} of ${formatSize(p.total)}`;
  const bar = $("progress-bar");
  bar.max = p.total;
  bar.value = p.done;
}

// ---- Events ------------------------------------------------------------

function render() {
  renderBar();
  renderFolders();
  renderList();
}

function connect() {
  const es = new EventSource("/events");
  es.addEventListener("hello", (e) => {
    const { info, snapshot } = JSON.parse(e.data);
    state.info = info;
    state.mailbox = snapshot.mailbox;
    state.counts = snapshot.counts;
    state.session = snapshot.session;
    state.routes = snapshot.routes;
    state.live = true;
    $("log").replaceChildren(...snapshot.log.map(logItem));
    $("log").scrollTop = $("log").scrollHeight;
    renderProgress(snapshot.session.progress);
    render();
  });
  es.addEventListener("mailbox", (e) => {
    const d = JSON.parse(e.data);
    state.mailbox = d.mailbox;
    state.counts = d.counts;
    // The open message may have moved (sent, archived); keep the pane in step.
    if (state.view === "message" && state.message) {
      const now = state.mailbox.find((m) => m.mid === state.message.mid);
      if (!now) {
        state.selected = null;
        showPane("empty");
      } else if (now.folder !== state.message.folder) {
        state.message.folder = now.folder;
        renderMessage();
      }
    }
    render();
  });
  es.addEventListener("session", (e) => {
    state.session = JSON.parse(e.data);
    if (state.session.state === "connecting") $("log").replaceChildren();
    renderProgress(state.session.progress);
    renderFolders();
  });
  es.addEventListener("log", (e) => appendLog(JSON.parse(e.data)));
  es.addEventListener("progress", (e) => renderProgress(JSON.parse(e.data)));
  es.onerror = () => {
    state.live = false;
    renderBar();
  };
}

// ---- Wiring ------------------------------------------------------------

for (const b of document.querySelectorAll(".folder")) {
  b.addEventListener("click", () => {
    state.folder = b.dataset.folder;
    state.selected = null;
    showPane("empty");
  });
}
$("compose-btn").addEventListener("click", () => openCompose());
$("connect-btn").addEventListener("click", async () => {
  try {
    await api("POST", "/api/connect", { route: $("route").value });
  } catch (e) {
    $("session-state").textContent = e.message;
  }
});
$("stop-btn").addEventListener("click", () => api("POST", "/api/stop", {}).catch(() => {}));
$("reply-btn").addEventListener("click", reply);
$("archive-btn").addEventListener("click", () => moveTo("archive"));
$("unarchive-btn").addEventListener("click", () => moveTo("in"));
$("delete-btn").addEventListener("click", deleteMessage);
$("compose").addEventListener("submit", queue);
$("c-discard").addEventListener("click", discard);
$("c-files").addEventListener("change", (e) => {
  addFiles([...e.target.files]);
  e.target.value = "";
});
for (const id of ["c-to", "c-cc", "c-subject", "c-body"]) {
  $(id).addEventListener("input", () => {
    saveDraft();
    scheduleEstimate();
  });
}
$("log-toggle").addEventListener("click", (e) => {
  const s = document.querySelector(".session");
  const collapsed = s.classList.toggle("collapsed");
  e.target.textContent = collapsed ? "Show" : "Hide";
  e.target.setAttribute("aria-expanded", String(!collapsed));
});

connect();
