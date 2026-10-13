// Renders public/favicon.svg to the PWA PNG icons (run: node scripts/make-icons.mjs).
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { Resvg } from "@resvg/resvg-js";

const svg = readFileSync(new URL("../public/favicon.svg", import.meta.url), "utf8");
// Maskable icons need a safe zone: shrink the mark onto a full-bleed background.
const maskable = svg
  .replace('<rect width="64" height="64" rx="16" fill="#0B0F15"/>', '<rect width="64" height="64" fill="#0B0F15"/><g transform="translate(9.6 9.6) scale(0.7)">')
  .replace("</svg>", "</g></svg>");

mkdirSync(new URL("../public/icons/", import.meta.url), { recursive: true });
const out = (name, src, size) => {
  const png = new Resvg(src, { fitTo: { mode: "width", value: size } }).render().asPng();
  writeFileSync(new URL(`../public/icons/${name}`, import.meta.url), png);
  console.log(name, png.length, "bytes");
};
out("icon-192.png", svg, 192);
out("icon-512.png", svg, 512);
out("icon-maskable-512.png", maskable, 512);
