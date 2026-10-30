// d75sat page helpers with no DOM, so Node can test them (sat.test.mjs).

const C_KMS = 299792.458;
const EARTH_KM = 6378.137;
const rad = (d) => (d * Math.PI) / 180;

/** The frequency heard on the ground from a transmitter on hz, for a range rate in km/s (positive receding). */
export function doppler(hz, rangeRate) {
  return hz * (1 - rangeRate / C_KMS);
}

/**
 * Ground distance (km) from the point below a satellite at altKm to the
 * edge of the area where it is above minEl degrees: its radio horizon for
 * 0°. The same formula as orbit.FootprintRadius.
 */
export function footprintRadiusKm(altKm, minEl = 0) {
  const e = rad(minEl);
  return EARTH_KM * (Math.acos((EARTH_KM * Math.cos(e)) / (EARTH_KM + altKm)) - e);
}

/**
 * Position on a polar sky plot of radius r: the zenith at the centre, the
 * horizon on the rim, north up and east to the right (as on a map).
 */
export function skyXY(az, el, r) {
  const d = (r * (90 - Math.max(0, Math.min(90, el)))) / 90;
  return { x: d * Math.sin(rad(az)), y: -d * Math.cos(rad(az)) };
}

/** "45 s", "4:05", "1 h 12 min", "2 d 3 h". */
export function countdown(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}:${String(s % 60).padStart(2, "0")}`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} h ${m % 60} min`;
  return `${Math.floor(h / 24)} d ${h % 24} h`;
}

/** "145.8000 MHz", or "29.4085 MHz". */
export function mhz(hz) {
  return `${(hz / 1e6).toFixed(4)} MHz`;
}

/** A shift in Hz as "+2.31 kHz", "−0.48 kHz" or "0.00 kHz". */
export function shift(hz) {
  const k = hz / 1000;
  const v = Math.abs(k).toFixed(2);
  if (v === "0.00") return "0.00 kHz";
  return `${k > 0 ? "+" : "−"}${v} kHz`;
}

/** Where a pass stands at now (ms): "upcoming", "up" or "over". */
export function passState(p, now) {
  if (now < Date.parse(p.aos)) return "upcoming";
  if (now <= Date.parse(p.los)) return "up";
  return "over";
}

/** The next pass at or after now that has not ended, or null. */
export function nextPass(passes, now, minEl = 0) {
  return passes.find((p) => p.maxEl >= minEl && Date.parse(p.los) >= now) ?? null;
}

/**
 * The passes whose alert is due at now: the satellite is watched, the
 * pass peaks high enough, its AOS is within the lead time and not yet
 * past, and it has not already been alerted (seen holds alerted keys).
 */
export function dueAlerts(passes, cfg, now, seen) {
  const lead = (cfg.alertMinutes || 5) * 60000;
  const out = [];
  for (const p of passes) {
    if (!cfg.alerts?.[p.id] || p.maxEl < cfg.alertMinEl) continue;
    const aos = Date.parse(p.aos);
    const key = `${p.id}@${p.aos}`;
    if (now >= aos - lead && now < aos && !seen.has(key)) out.push({ pass: p, key });
  }
  return out;
}

/** "N", "NE" … for an azimuth. */
export function compass8(az) {
  return ["N", "NE", "E", "SE", "S", "SW", "W", "NW"][Math.round((((az % 360) + 360) % 360) / 45) % 8];
}
