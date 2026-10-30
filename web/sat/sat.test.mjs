// node --test web/sat/sat.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { doppler, footprintRadiusKm, skyXY, countdown, mhz, shift, passState, nextPass, dueAlerts, compass8 } from "./sat.js";

const near = (a, b, tol, msg) => assert.ok(Math.abs(a - b) <= tol, `${msg}: ${a} not within ${tol} of ${b}`);

test("doppler matches the Go formula", () => {
  // 7 km/s receding at 435 MHz is about −10.157 kHz (orbit's TestDoppler).
  near(doppler(435e6, 7) - 435e6, -10157.1, 1, "receding");
  assert.ok(doppler(145.8e6, -7) > 145.8e6, "approaching is higher");
});

test("footprint radius", () => {
  // ISS height: about 2250 km to the horizon, less at 10°.
  near(footprintRadiusKm(420), 2253, 15, "horizon");
  assert.ok(footprintRadiusKm(420, 10) < footprintRadiusKm(420) - 500, "10° much smaller");
  near(footprintRadiusKm(420, 90), 0, 1e-6, "zenith only");
});

test("sky plot: zenith at the centre, north up, east right", () => {
  const z = skyXY(123, 90, 100);
  near(z.x, 0, 1e-9, "zenith x");
  near(z.y, 0, 1e-9, "zenith y");
  const n = skyXY(0, 0, 100);
  near(n.y, -100, 1e-9, "north on top");
  const e = skyXY(90, 0, 100);
  near(e.x, 100, 1e-9, "east on the right");
  const below = skyXY(180, -5, 100);
  near(below.y, 100, 1e-9, "below the horizon stays on the rim");
});

test("formatting", () => {
  assert.equal(countdown(45_000), "45 s");
  assert.equal(countdown(245_000), "4:05");
  assert.equal(countdown(72 * 60_000), "1 h 12 min");
  assert.equal(countdown(51 * 3600_000), "2 d 3 h");
  assert.equal(mhz(145.8e6), "145.8000 MHz");
  assert.equal(shift(2310), "+2.31 kHz");
  assert.equal(shift(-480), "−0.48 kHz");
  assert.equal(shift(1), "0.00 kHz");
  assert.equal(compass8(44), "NE");
  assert.equal(compass8(359), "N");
});

const T = Date.parse("2026-10-30T18:00:00Z");
const at = (min) => new Date(T + min * 60000).toISOString();
const passes = [
  { id: "1", aos: at(1), los: at(10), maxEl: 60 },
  { id: "2", aos: at(4), los: at(9), maxEl: 5 },
  { id: "3", aos: at(30), los: at(40), maxEl: 40 },
];

test("pass state and next pass", () => {
  assert.equal(passState(passes[0], T), "upcoming");
  assert.equal(passState(passes[0], T + 5 * 60000), "up");
  assert.equal(passState(passes[0], T + 11 * 60000), "over");
  assert.equal(nextPass(passes, T + 11 * 60000).id, "3");
  assert.equal(nextPass(passes, T + 5 * 60000, 10).id, "1", "a pass under way still counts");
  assert.equal(nextPass(passes, T + 41 * 60000), null);
});

test("alerts: watched, high enough, within the lead time, once", () => {
  const cfg = { alerts: { 1: true, 2: true }, alertMinutes: 5, alertMinEl: 10 };
  const seen = new Set();
  let due = dueAlerts(passes, cfg, T, seen);
  assert.deepEqual(due.map((d) => d.pass.id), ["1"], "2 peaks too low, 3 is not watched");
  for (const d of due) seen.add(d.key);
  assert.equal(dueAlerts(passes, cfg, T + 30000, seen).length, 0, "not twice");
  assert.equal(dueAlerts(passes, cfg, T + 2 * 60000, new Set()).length, 0, "not once it has risen");
  cfg.alerts[3] = true;
  assert.equal(dueAlerts(passes, cfg, T + 24 * 60000, seen).length, 0, "too early");
  assert.equal(dueAlerts(passes, cfg, T + 26 * 60000, seen)[0].pass.id, "3");
});
