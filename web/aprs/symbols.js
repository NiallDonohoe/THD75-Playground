// APRS symbols drawn as emoji, so no icon set has to be embedded (and
// licensed). Only common symbols are mapped; the rest are drawn as their
// symbol character in a disc. Overlay tables (A–Z, 0–9) use the alternate
// table's symbol with the overlay character on top.

const PRIMARY = {
  "!": ["🚓", "police"], "#": ["📡", "digipeater"], "$": ["☎️", "phone"], "&": ["🌐", "HF gateway"],
  "'": ["🛩️", "small aircraft"], "+": ["⛑️", "Red Cross"], "-": ["🏠", "house"], ";": ["⛺", "campground"],
  "<": ["🏍️", "motorcycle"], "=": ["🚆", "train"], ">": ["🚗", "car"], "O": ["🎈", "balloon"],
  "R": ["🚐", "RV"], "U": ["🚌", "bus"], "X": ["🚁", "helicopter"], "Y": ["⛵", "yacht"],
  "[": ["🚶", "person"], "^": ["✈️", "aircraft"], "_": ["🌦️", "weather station"], "a": ["🚑", "ambulance"],
  "b": ["🚲", "bicycle"], "f": ["🚒", "fire truck"], "h": ["🏥", "hospital"], "j": ["🚙", "jeep"],
  "k": ["🚚", "truck"], "l": ["💻", "laptop"], "r": ["📡", "antenna"], "s": ["🚢", "ship"],
  "u": ["🚛", "truck"], "v": ["🚐", "van"], "w": ["💧", "water station"], "y": ["🏡", "house with yagi"],
};

const ALTERNATE = {
  "#": ["📡", "digipeater"], "&": ["🌐", "gateway"], "-": ["🏠", "house"], ">": ["🚗", "car"],
  "_": ["🌦️", "weather station"], "k": ["🚙", "SUV"], "n": ["🔺", "triangle"], "u": ["🚛", "truck"],
};

/**
 * How to draw a two-character symbol: an emoji (or null), the overlay
 * character (or ""), the character to draw when there is no emoji, and a name.
 */
export function symbolInfo(sym) {
  const table = sym?.[0] ?? "", code = sym?.[1] ?? "";
  const overlay = /[A-Z0-9]/.test(table) ? table : "";
  const entry = table === "/" ? PRIMARY[code] : ALTERNATE[code];
  return {
    emoji: entry ? entry[0] : null,
    name: entry ? entry[1] : sym ? `symbol ${sym}` : "no symbol",
    overlay,
    fallback: code || "•",
  };
}
