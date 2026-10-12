import { defineConfig, devices } from "@playwright/test";

// E2E on phone viewports with mocked GPS and a mocked API (SPEC §14).
export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  expect: { timeout: 10_000, toHaveScreenshot: { maxDiffPixelRatio: 0.02 } },
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : [["list"]],
  // Baselines are per platform; a missing one is recorded instead of failing.
  updateSnapshots: "missing",
  use: {
    baseURL: "http://localhost:4173",
    trace: "retain-on-failure",
    // A registered service worker would answer fetches itself and bypass the API mocks.
    serviceWorkers: "block",
    geolocation: { latitude: 22.6893, longitude: 75.8529 },
    permissions: ["geolocation"],
    locale: "en-IN",
    timezoneId: "Asia/Kolkata",
  },
  projects: [
    { name: "pixel7", use: { ...devices["Pixel 7"] } },
    { name: "iphone14", use: { ...devices["iPhone 14"] } },
  ],
  webServer: {
    command: "npm run preview -- --strictPort",
    url: "http://localhost:4173",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
