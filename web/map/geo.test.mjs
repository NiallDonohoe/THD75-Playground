// node --test web/map
import { test } from "node:test";
import assert from "node:assert/strict";
import { project, unproject, visibleTiles, distanceKm, bearing, compass, locator, formatAge, ambiguityHalfDeg, TILE, unwrap, footprintRing, fromLocator } from "./geo.js";

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

test("unwrap keeps longitudes continuous across the antimeridian", () => {
  const u = unwrap([[0, 170], [1, 179], [2, -175], [3, -160], [4, 175]]);
  assert.deepEqual(u.map((p) => p[1]), [170, 179, 185, 200, 175]);
});

test("footprint ring: every point at the radius", () => {
  const { points, polar } = footprintRing(53.35, -6.26, 2200, 36);
  assert.equal(polar, false);
  assert.equal(points.length, 36);
  for (const [lat, lon] of points) near(distanceKm({ lat: 53.35, lon: -6.26 }, { lat, lon }), 2200, 15, "distance");
});

test("footprint ring across the antimeridian stays continuous", () => {
  const { points } = footprintRing(0, 179, 2000, 36);
  for (let i = 1; i < points.length; i++) assert.ok(Math.abs(points[i][1] - points[i - 1][1]) < 30, "jump");
});

test("footprint ring round a pole closes along the map edge", () => {
  const { points, polar } = footprintRing(80, 20, 2600, 36);
  assert.equal(polar, true);
  assert.equal(points.length, 38);
  near(points[36][0], 85.0511287798, 1e-6, "edge");
});

test("locator: round trip with locator()", () => {
  assert.equal(fromLocator("nope"), null);
  for (const [lat, lon] of [[53.35, -6.26], [-33.85, 151.2], [41.71, -72.73]]) {
    const loc = locator(lat, lon);
    const c = fromLocator(loc);
    assert.equal(locator(c.lat, c.lon), loc);
    near(c.lat, lat, 1 / 24, "lat");
    near(c.lon, lon, 2 / 24, "lon");
  }
  const io63 = fromLocator("io63");
  near(io63.lat, 53.5, 1e-9, "IO63 lat");
  near(io63.lon, -7, 1e-9, "IO63 lon");
});
