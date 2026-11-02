// Pure helpers for the d75winlink page, tested with node --test.

/** "SMTP:name@example.com" shows as the bare address; callsigns as they are. */
export function showAddress(a) {
  return a.startsWith("SMTP:") ? a.slice(5) : a;
}

/** Bytes as "512 B", "1.4 kB", "120 kB". */
export function formatSize(n) {
  if (n < 1000) return `${n} B`;
  const kb = n / 1024;
  return kb < 10 ? `${kb.toFixed(1)} kB` : `${Math.round(kb)} kB`;
}

/** Seconds as "≈ 8 s", "≈ 2 min 10 s", "≈ 1 h 5 min". */
export function formatDuration(secs) {
  const s = Math.max(1, Math.round(secs));
  if (s < 60) return `≈ ${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return s % 60 ? `≈ ${m} min ${s % 60} s` : `≈ ${m} min`;
  return `≈ ${Math.floor(m / 60)} h ${m % 60} min`;
}

/** Characters that Winlink's ISO-8859-1 bodies cannot carry, each once. */
export function notLatin1(text) {
  const bad = new Set();
  for (const ch of text) {
    if (ch.codePointAt(0) > 0xff) bad.add(ch);
  }
  return [...bad];
}

/** A reply's subject: "Re: " once. */
export function replySubject(subject) {
  return /^re:/i.test(subject.trim()) ? subject.trim() : `Re: ${subject.trim()}`;
}

/** A reply's body: a blank line for the answer, then the original quoted. */
export function replyBody(from, date, body) {
  const when = date ? new Date(date).toUTCString() : "";
  const quoted = body.replace(/\r\n/g, "\n").replace(/\n+$/, "").split("\n").map((l) => `> ${l}`).join("\n");
  return `\n\n${showAddress(from)} wrote${when ? ` (${when})` : ""}:\n${quoted}\n`;
}

/**
 * A message's time for the list: the time today, the weekday this week,
 * otherwise the date.
 */
export function formatWhen(iso, now = new Date()) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1990) return "";
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  if (now - d < 6 * 86400e3 && now > d) return d.toLocaleDateString([], { weekday: "short", hour: "2-digit", minute: "2-digit" });
  return d.toLocaleDateString([], { year: "numeric", month: "short", day: "numeric" });
}

/** Base64 of an ArrayBuffer, for the JSON a draft is sent in. */
export function toBase64(buf) {
  const bytes = new Uint8Array(buf);
  let s = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(s);
}
