import { test } from "node:test";
import assert from "node:assert/strict";
import { showAddress, formatSize, formatDuration, notLatin1, replySubject, replyBody, formatWhen, toBase64 } from "./format.js";

test("addresses", () => {
  assert.equal(showAddress("SMTP:a@example.com"), "a@example.com");
  assert.equal(showAddress("Q1ABC"), "Q1ABC");
});

test("sizes", () => {
  assert.equal(formatSize(0), "0 B");
  assert.equal(formatSize(999), "999 B");
  assert.equal(formatSize(1434), "1.4 kB");
  assert.equal(formatSize(120 * 1024), "120 kB");
});

test("durations", () => {
  assert.equal(formatDuration(0.2), "≈ 1 s");
  assert.equal(formatDuration(42), "≈ 42 s");
  assert.equal(formatDuration(120), "≈ 2 min");
  assert.equal(formatDuration(130), "≈ 2 min 10 s");
  assert.equal(formatDuration(3900), "≈ 1 h 5 min");
});

test("Latin-1 check", () => {
  assert.deepEqual(notLatin1("Café, 73!"), []);
  assert.deepEqual(notLatin1("73 📻 and 📻 — ok"), ["📻", "—"]);
});

test("replies", () => {
  assert.equal(replySubject("Net tonight"), "Re: Net tonight");
  assert.equal(replySubject("RE: Net tonight"), "RE: Net tonight");
  assert.equal(
    replyBody("SMTP:a@example.com", "", "one\r\ntwo\r\n"),
    "\n\na@example.com wrote:\n> one\n> two\n",
  );
});

test("when", () => {
  const now = new Date(2026, 10, 2, 15, 0);
  assert.match(formatWhen(new Date(2026, 10, 2, 9, 41).toISOString(), now), /09:41|9:41/);
  assert.equal(formatWhen("0001-01-01T00:00:00Z", now), "");
  assert.match(formatWhen(new Date(2025, 0, 5).toISOString(), now), /2025/);
});

test("base64", () => {
  assert.equal(toBase64(new Uint8Array([0, 1, 2, 255]).buffer), "AAEC/w==");
  const big = new Uint8Array(100000).fill(65);
  assert.equal(atob(toBase64(big.buffer)).length, 100000);
});
