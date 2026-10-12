// Performance budget (SPEC §10.3): initial JS ≤ 200 KB gzip.
// "Initial" = the entry chunk plus its static imports; lazy chunks (map,
// navigation, draw, admin) are excluded.
import { readFileSync } from "node:fs";
import { gzipSync } from "node:zlib";
import { join } from "node:path";

const dist = new URL("../dist/", import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1");
const html = readFileSync(join(dist, "index.html"), "utf8");
const entries = [...html.matchAll(/<script[^>]+src="\/([^"]+\.js)"/g)].map((m) => m[1]);
const preloads = [...html.matchAll(/<link[^>]+rel="modulepreload"[^>]+href="\/([^"]+\.js)"/g)].map((m) => m[1]);
const files = [...new Set([...entries, ...preloads])];
let total = 0;
for (const f of files) {
  const gz = gzipSync(readFileSync(join(dist, f))).length;
  total += gz;
  console.log(`${(gz / 1024).toFixed(1).padStart(7)} KB  ${f}`);
}
const kb = total / 1024;
console.log(`initial JS: ${kb.toFixed(1)} KB gzip (budget 200 KB)`);
if (kb > 200) process.exit(1);
