// Generates the PWA icons (earth-orange square with a winding path) without
// image tooling: a tiny PNG encoder over raw RGBA pixels.
import { writeFileSync, mkdirSync } from "node:fs";
import { deflateSync } from "node:zlib";

const ACCENT = [0xc2, 0x41, 0x0c];
const PATH = [0xff, 0xff, 0xff];

function crc32(buf) {
  let c, crc = 0xffffffff;
  for (let n = 0; n < buf.length; n++) {
    c = (crc ^ buf[n]) & 0xff;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    crc = (crc >>> 8) ^ c;
  }
  return (crc ^ 0xffffffff) >>> 0;
}
function chunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const td = Buffer.concat([Buffer.from(type), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(td));
  return Buffer.concat([len, td, crc]);
}
function png(size, pixel) {
  const raw = Buffer.alloc((size * 4 + 1) * size);
  for (let y = 0; y < size; y++) {
    raw[y * (size * 4 + 1)] = 0;
    for (let x = 0; x < size; x++) {
      const [r, g, b, a] = pixel(x / size, y / size);
      const o = y * (size * 4 + 1) + 1 + x * 4;
      raw[o] = r; raw[o + 1] = g; raw[o + 2] = b; raw[o + 3] = a;
    }
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; ihdr[9] = 6; ihdr[10] = 0; ihdr[11] = 0; ihdr[12] = 0;
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk("IHDR", ihdr), chunk("IDAT", deflateSync(raw)), chunk("IEND", Buffer.alloc(0))]);
}

// A gentle S-curve from bottom-left to top-right: the pagdandi.
const curve = (t) => [0.22 + 0.56 * t, 0.8 - 0.6 * t + 0.12 * Math.sin(t * Math.PI * 2)];
function onPath(u, v, width) {
  for (let i = 0; i <= 200; i++) {
    const [x, y] = curve(i / 200);
    if ((u - x) ** 2 + (v - y) ** 2 < width * width) return true;
  }
  return false;
}
function icon(size, { maskable = false, rounded = true } = {}) {
  const inset = maskable ? 0.12 : 0;
  return png(size, (u, v) => {
    const r = 0.22;
    const inCorner = (cx, cy) => (u - cx) ** 2 + (v - cy) ** 2 > r * r;
    if (rounded && !maskable && ((u < r && v < r && inCorner(r, r)) || (u > 1 - r && v < r && inCorner(1 - r, r)) || (u < r && v > 1 - r && inCorner(r, 1 - r)) || (u > 1 - r && v > 1 - r && inCorner(1 - r, 1 - r))))
      return [0, 0, 0, 0];
    const su = (u - inset) / (1 - 2 * inset), sv = (v - inset) / (1 - 2 * inset);
    return onPath(su, sv, 0.055) ? [...PATH, 255] : [...ACCENT, 255];
  });
}

const out = new URL("../public/icons/", import.meta.url);
mkdirSync(out, { recursive: true });
writeFileSync(new URL("icon-192.png", out), icon(192));
writeFileSync(new URL("icon-512.png", out), icon(512));
writeFileSync(new URL("icon-maskable-512.png", out), icon(512, { maskable: true }));
writeFileSync(
  new URL("../public/favicon.svg", import.meta.url),
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="#C2410C"/><path d="M14 51 C 26 46, 22 30, 34 26 S 46 12, 50 13" fill="none" stroke="#fff" stroke-width="7" stroke-linecap="round"/></svg>\n`,
);
console.log("icons written");
