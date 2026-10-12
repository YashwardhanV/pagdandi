// Anti-vibe-code gate (SPEC §11.7): components use design tokens only.
// Fails on raw hex/rgb colours, Tailwind default palette classes, gradients,
// glow shadows and backdrop blur outside the files allowed to hold colours.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const root = new URL("../src/", import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1");
const allowed = new Set(["styles/tokens.css", "features/map/style.ts"]);
const rules = [
  [/#[0-9a-fA-F]{3,8}\b(?![\w-])/, "raw hex colour"],
  [/\brgba?\(/, "raw rgb colour"],
  [/\b(?:bg|text|border|from|to|via|ring|fill|stroke)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}\b/, "Tailwind default palette"],
  [/\bbg-gradient-|linear-gradient\(|radial-gradient\(/, "gradient"],
  [/\bbackdrop-blur|backdrop-filter/, "glassmorphism"],
  [/drop-shadow-|shadow-(?:lg|xl|2xl)\b/, "glow/large shadow"],
];

const files = [];
(function walk(d) {
  for (const f of readdirSync(d)) {
    const p = join(d, f);
    if (statSync(p).isDirectory()) walk(p);
    else if (/\.(tsx?|css)$/.test(f) && !/\.test\./.test(f) && !f.endsWith("api-types.ts")) files.push(p);
  }
})(root);

let bad = 0;
for (const f of files) {
  const rel = relative(root, f).replace(/\\/g, "/");
  if (allowed.has(rel)) continue;
  const lines = readFileSync(f, "utf8").split("\n");
  lines.forEach((line, i) => {
    if (rel === "styles/base.css" && /skeleton|linear-gradient/.test(line)) return; // shimmer uses tokens only
    for (const [re, what] of rules) {
      if (re.test(line)) {
        console.error(`${rel}:${i + 1}: ${what}: ${line.trim()}`);
        bad++;
      }
    }
  });
}
if (bad) {
  console.error(`\n${bad} design-token violation(s).`);
  process.exit(1);
}
console.log(`design tokens: ${files.length} files clean`);
