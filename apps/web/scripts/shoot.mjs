// Screenshots of the running app (real API, real tiles) for review and the README.
// Usage: node scripts/shoot.mjs [baseURL] [outDir]
import { chromium, devices } from "@playwright/test";

const base = process.argv[2] ?? "http://localhost:5173";
const out = process.argv[3] ?? "../../docs/screenshots";
const route = "/route?to=22.6549,75.8722&toName=Bilawali%20Lake&from=22.6893,75.8529&fromName=Choithram%20Mandi";

const browser = await chromium.launch();
for (const theme of ["light", "dark"]) {
  const ctx = await browser.newContext({
    ...devices["Pixel 7"],
    geolocation: { latitude: 22.6893, longitude: 75.8529 },
    permissions: ["geolocation"],
    timezoneId: "Asia/Kolkata",
    serviceWorkers: "block",
  });
  await ctx.addInitScript((t) => {
    localStorage.setItem("pd.settings", JSON.stringify({ lang: "en", theme: t, regionId: "indore", recents: [], home: null, work: null, muted: true, vehicle: null }));
  }, theme);
  const page = await ctx.newPage();
  page.on("pageerror", (e) => console.error("pageerror:", e.message));
  await page.goto(base + "/");
  await page.waitForTimeout(4000);
  await page.screenshot({ path: `${out}/home-${theme}.png` });
  await page.goto(base + route);
  await page.getByTestId("ride").waitFor({ timeout: 20000 });
  await page.waitForTimeout(3000);
  await page.screenshot({ path: `${out}/route-${theme}.png` });
  await ctx.close();
}
const desk = await browser.newContext({ viewport: { width: 1440, height: 900 }, timezoneId: "Asia/Kolkata", serviceWorkers: "block" });
await desk.addInitScript(() => localStorage.setItem("pd.settings", JSON.stringify({ lang: "en", theme: "light", regionId: "indore", recents: [], muted: true })));
const dp = await desk.newPage();
await dp.goto(base + route);
await dp.getByTestId("ride").waitFor({ timeout: 20000 });
await dp.waitForTimeout(3000);
await dp.screenshot({ path: `${out}/route-desktop.png` });
await browser.close();
console.log("screenshots written to", out);
