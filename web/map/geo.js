// Map and geography helpers. No DOM here, so Node can test it
// (geo.test.mjs).

export const TILE = 256;
export const MAX_LAT = 85.0511287798; // where Web Mercator stops

const rad = (d) => (d * Math.PI) / 180;
const deg = (r) => (r * 180) / Math.PI;

/** World pixel coordinates of a point at zoom z (256 px tiles). */
export function project(lat, lon, z) {
  const n = TILE * 2 ** z;
  const s = Math.sin(rad(Math.max(-MAX_LAT, Math.min(MAX_LAT, lat))));
  return {
    x: ((lon + 180) / 360) * n,
    y: (0.5 - Math.log((1 + s) / (1 - s)) / (4 * Math.PI)) * n,
  };
}

/** The point at world pixel coordinates x, y at zoom z. */
export function unproject(x, y, z) {
  const n = TILE * 2 ** z;
  return {
    lat: deg(Math.atan(Math.sinh(Math.PI * (1 - (2 * y) / n)))),
    lon: (x / n) * 360 - 180,
  };
}

/**
 * The tiles covering a w × h view whose top-left corner is at world pixel
 * (left, top). Each has its tile coordinates (x wrapped round the world) and
 * where to draw it in the view. Rows beyond the poles are left out.
 */
export function visibleTiles(left, top, w, h, z) {
  const n = 2 ** z;
  const tiles = [];
  const x0 = Math.floor(left / TILE), x1 = Math.floor((left + w) / TILE);
  const y0 = Math.max(0, Math.floor(top / TILE)), y1 = Math.min(n - 1, Math.floor((top + h) / TILE));
  for (let ty = y0; ty <= y1; ty++) {
    for (let tx = x0; tx <= x1; tx++) {
      tiles.push({ z, x: ((tx % n) + n) % n, y: ty, left: tx * TILE - left, top: ty * TILE - top });
    }
  }
  return tiles;
}

/** Great-circle distance in km. */
export function distanceKm(a, b) {
  const R = 6371.0088;
  const dLat = rad(b.lat - a.lat), dLon = rad(b.lon - a.lon);
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(rad(a.lat)) * Math.cos(rad(b.lat)) * Math.sin(dLon / 2) ** 2;
  return 2 * R * Math.asin(Math.min(1, Math.sqrt(h)));
}

/** Initial bearing from a to b, in degrees from north, 0 to 360. */
export function bearing(a, b) {
  const y = Math.sin(rad(b.lon - a.lon)) * Math.cos(rad(b.lat));
  const x = Math.cos(rad(a.lat)) * Math.sin(rad(b.lat)) - Math.sin(rad(a.lat)) * Math.cos(rad(b.lat)) * Math.cos(rad(b.lon - a.lon));
  return (deg(Math.atan2(y, x)) + 360) % 360;
}

const POINTS = ["N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"];

/** The 16-point compass name of a bearing. */
export function compass(d) {
  return POINTS[Math.round((((d % 360) + 360) % 360) / 22.5) % 16];
}

/** Six-character Maidenhead locator. */
export function locator(lat, lon) {
  const x = Math.min(359.9999999, Math.max(0, lon + 180));
  const y = Math.min(179.9999999, Math.max(0, lat + 90));
  const A = "A".charCodeAt(0), a = "a".charCodeAt(0);
  return (
    String.fromCharCode(A + Math.floor(x / 20), A + Math.floor(y / 10)) +
    String(Math.floor((x % 20) / 2)) + String(Math.floor(y % 10)) +
    String.fromCharCode(a + Math.floor((x % 2) * 12), a + Math.floor((y % 1) * 24))
  );
}

/** A short age: "now", "45 s", "12 min", "3 h 5 min", "2 d". */
export function formatAge(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 10) return "now";
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  if (h < 24) return m % 60 ? `${h} h ${m % 60} min` : `${h} h`;
  return `${Math.floor(h / 24)} d`;
}

/**
 * Half the size, in degrees of latitude, of the box an ambiguous position
 * stands for: blanking 1 to 4 digits leaves 0.1′, 1′, 10′ or 60′ open.
 */
export function ambiguityHalfDeg(level) {
  return [0, 0.1, 1, 10, 60][level] / 60 / 2;
}

/**
 * The same points with longitudes made continuous: each one within 180°
 * of the last, so a line crossing the antimeridian runs on past ±180
 * instead of jumping across the world. Draw with a raw (unwrapped)
 * projection.
 */
export function unwrap(points) {
  const out = [];
  let prev = null;
  for (const [lat, lon0] of points) {
    let lon = lon0;
    if (prev !== null) {
      while (lon - prev > 180) lon -= 360;
      while (lon - prev < -180) lon += 360;
    }
    out.push([lat, lon]);
    prev = lon;
  }
  return out;
}

const EARTH_KM = 6378.137;

/**
 * The circle of points radiusKm (along the ground) from a centre, on a
 * spherical Earth, as an unwrapped ring of n points. A circle round a
 * pole cannot be a closed ring in Mercator; it is closed along the edge
 * of the map instead, and polar is set.
 */
export function footprintRing(lat, lon, radiusKm, n = 90) {
  const r = radiusKm / EARTH_KM;
  const la = rad(lat), lo = rad(lon);
  const pts = [];
  for (let i = 0; i < n; i++) {
    const b = (2 * Math.PI * i) / n;
    const pl = Math.asin(Math.sin(la) * Math.cos(r) + Math.cos(la) * Math.sin(r) * Math.cos(b));
    const pn = lo + Math.atan2(Math.sin(b) * Math.sin(r) * Math.cos(la), Math.cos(r) - Math.sin(la) * Math.sin(pl));
    pts.push([deg(pl), ((((deg(pn) + 180) % 360) + 360) % 360) - 180]);
  }
  const ring = unwrap(pts);
  // Going all the way round in longitude means the circle holds a pole.
  const turn = ring[ring.length - 1][1] - ring[0][1] + (ring[1][1] - ring[0][1]);
  if (Math.abs(turn) > 180) {
    const edge = lat > 0 ? MAX_LAT : -MAX_LAT;
    const last = ring[ring.length - 1][1];
    ring.push([edge, last], [edge, ring[0][1]]);
    return { points: ring, polar: true };
  }
  return { points: ring, polar: false };
}

/** The centre of a 4- or 6-character Maidenhead locator, or null. */
export function fromLocator(s) {
  const m = /^([A-R]{2})([0-9]{2})([A-X]{2})?$/i.exec(s.trim());
  if (!m) return null;
  const u = m[1].toUpperCase();
  let lon = (u.charCodeAt(0) - 65) * 20 + Number(m[2][0]) * 2 - 180;
  let lat = (u.charCodeAt(1) - 65) * 10 + Number(m[2][1]) - 90;
  if (m[3]) {
    const sub = m[3].toUpperCase();
    lon += ((sub.charCodeAt(0) - 65) + 0.5) * (2 / 24);
    lat += ((sub.charCodeAt(1) - 65) + 0.5) * (1 / 24);
  } else {
    lon += 1;
    lat += 0.5;
  }
  return { lat, lon };
}
