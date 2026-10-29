// node --test web/aprs
import { test } from "node:test";
import assert from "node:assert/strict";
import { project, unproject, visibleTiles, distanceKm, bearing, compass, locator, formatAge, ambiguityHalfDeg, TILE } from "./geo.js";

const near = (a, b, tol, msg) => assert.ok(Math.abs(a - b) <= tol, `${msg}: ${a} not within ${tol} of ${b}`);

test("projection: corners and centre", () => {
  const c = project(0, 0, 0);
  near(c.x, 128, 1e-9, "x");
  near(c.y, 128, 1e-9, "y");
  const ne = project(85.0511287798, 180, 0);
  near(ne.x, 256, 1e-9, "x");
  near(ne.y, 0, 1e-6, "y");
  // Beyond the Mercator limit clamps rather than running off to infinity.
  assert.equal(project(90, 0, 3).y, project(85.0511287798, 0, 3).y);
});

test("projection round trip", () => {
  for (const [lat, lon] of [[53.35, -6.26], [-33.85, 151.2], [41.71, -72.73], [0, 0], [80, 179.9]]) {
    for (const z of [0, 5, 12, 18]) {
      const p = project(lat, lon, z);
      const q = unproject(p.x, p.y, z);
      near(q.lat, lat, 1e-9, "lat");
      near(q.lon, lon, 1e-9, "lon");
    }
  }
});

test("visible tiles wrap round the world and stop at the poles", () => {
  const tiles = visibleTiles(-100, -50, 400, 300, 1);
  // Zoom 1 is 2 × 2 tiles; the view spans tile columns -1..1 and rows 0..0 (row -1 is beyond the pole).
  assert.deepEqual(tiles.map((t) => [t.x, t.y]), [[1, 0], [0, 0], [1, 0]]);
  assert.equal(tiles[0].left, -TILE + 100);
  assert.equal(tiles[0].top, 50);
});

test("distance and bearing, London to Paris", () => {
  const london = { lat: 51.5074, lon: -0.1278 }, paris = { lat: 48.8566, lon: 2.3522 };
  near(distanceKm(london, paris), 343.5, 1, "km");
  near(bearing(london, paris), 148.1, 0.5, "bearing");
  assert.equal(compass(148.1), "SSE");
  assert.equal(compass(359), "N");
  assert.equal(compass(-90), "W");
});

test("Maidenhead locators", () => {
  assert.equal(locator(41.714775, -72.72726), "FN31pr"); // W1AW
  assert.equal(locator(53.35, -6.26), "IO63ui");
  assert.equal(locator(-33.85, 151.2), "QF56od");
  assert.equal(locator(90, 180), "RR99xx");
});

test("ages", () => {
  assert.equal(formatAge(3000), "now");
  assert.equal(formatAge(45000), "45 s");
  assert.equal(formatAge(12 * 60000), "12 min");
  assert.equal(formatAge(3 * 3600000 + 5 * 60000), "3 h 5 min");
  assert.equal(formatAge(2 * 3600000), "2 h");
  assert.equal(formatAge(50 * 3600000), "2 d");
  assert.equal(formatAge(-5000), "now");
});

test("ambiguity boxes", () => {
  assert.equal(ambiguityHalfDeg(0), 0);
  near(ambiguityHalfDeg(4), 0.5, 1e-12, "level 4");
});
