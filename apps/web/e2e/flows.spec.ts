import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";
import { DEST, mockAll } from "./mocks";

async function axe(page: Page, name: string) {
  const r = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).exclude(".maplibregl-canvas").analyze();
  expect(r.violations.map((v) => `${name}: ${v.id} (${v.nodes.length})`)).toEqual([]);
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    // Skip the explain-before-prompt dialog: permission is already granted in tests.
    try {
      localStorage.setItem("pd.settings", JSON.stringify({ lang: "en", theme: "light", regionId: "indore", recents: [], home: null, work: null, muted: true, vehicle: null }));
    } catch {}
  });
});

test("search → compare → ride → arrive → post-ride prompt", async ({ page, context }) => {
  const state = await mockAll(page, { signedIn: true });
  await page.goto("/");
  await expect(page.getByTestId("where-to")).toBeVisible();
  await axe(page, "home");

  await page.getByTestId("where-to").click();
  await page.getByTestId("search-input").fill("Bila");
  await expect(page.getByText("Bilawali Lake View")).toBeVisible();
  await axe(page, "search");
  await page.getByText("Bilawali Lake View").click();

  // Route compare: usual way vs pagdandi, honest labels.
  await expect(page.getByTestId("option-sc1")).toBeVisible();
  await expect(page.getByTestId("option-sc1").getByText("Recommended")).toBeVisible();
  await expect(page.getByTestId("option-sc1").getByText("saves 7 min")).toBeVisible();
  await expect(page.getByText("Kaccha road · 1.2 km").first()).toBeVisible();
  await expect(page.getByText(/Confirmed 3 days ago by 4 riders/)).toBeVisible();
  await expect(page.getByText("Unverified roads can change. Ride at your own judgement.")).toBeVisible();
  await axe(page, "route");
  await page.getByTestId("option-std").click();
  await expect(page.getByTestId("option-std")).toHaveAttribute("aria-checked", "true");
  await page.getByTestId("option-sc1").click();

  // Ride: maneuver banner from the mocked GPS fix.
  await page.getByTestId("ride").click();
  await expect(page.getByTestId("maneuver")).toContainText(/rider shortcut|Head south/);
  await axe(page, "navigation");

  // Arrive: move the mocked GPS to the destination.
  await context.setGeolocation({ latitude: DEST.lat, longitude: DEST.lng });
  await expect(page.getByTestId("arrival")).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText("Was the shortcut passable?")).toBeVisible();
  await page.getByRole("button", { name: "Yes" }).click();
  await expect(page.getByText("Thanks. Your answer helps the next rider.")).toBeVisible();
  expect(state.reports).toEqual([{ verdict: "passable" }]);
});

test("draw a shortcut", async ({ page }) => {
  const state = await mockAll(page, { signedIn: true });
  await page.goto("/add");
  await expect(page.getByText("Tap the map to trace the path.", { exact: false })).toBeVisible();
  const canvas = page.locator(".maplibregl-canvas");
  await expect(canvas).toBeVisible();
  const box = (await canvas.boundingBox())!;
  for (const [fx, fy] of [[0.3, 0.2], [0.45, 0.25], [0.6, 0.3]]) {
    await page.mouse.click(box.x + box.width * fx, box.y + box.height * fy);
  }
  await expect(page.getByTestId("draw-count")).toContainText("3 points");
  await expect(page.getByText("Joins the road").first()).toBeVisible();
  await page.getByTestId("add-next").click();
  await page.getByRole("button", { name: "Gate" }).click();
  await page.getByLabel("Name (optional)").fill("Behind the mandi");
  await axe(page, "add-details");
  await page.getByRole("button", { name: "Submit" }).click();
  await expect(page).toHaveURL(/\/s\/sc-new$/);
  const body = state.created[0] as { points: unknown[]; flags: string[]; title: string; source: string };
  expect(body.source).toBe("drawn");
  expect(body.points).toHaveLength(3);
  expect(body.flags).toEqual(["gate"]);
  expect(body.title).toBe("Behind the mandi");
  await axe(page, "shortcut-detail");
});

test("offline banner", async ({ page, context }) => {
  await mockAll(page);
  await page.goto("/");
  await expect(page.getByTestId("where-to")).toBeVisible();
  await context.setOffline(true);
  await expect(page.getByTestId("offline-banner")).toBeVisible();
  await context.setOffline(false);
  await expect(page.getByTestId("offline-banner")).toBeHidden();
});

test("other screens pass axe", async ({ page }) => {
  await mockAll(page, { signedIn: true });
  for (const [path, marker] of [["/explore", "Bilawali Lake"], ["/me", "Asha"], ["/signin", "Email me a sign-in link"], ["/offline", "Offline maps"]] as const) {
    await page.goto(path);
    await expect(page.getByText(marker).filter({ visible: true }).first()).toBeVisible();
    await axe(page, path);
  }
});

for (const theme of ["light", "dark"] as const) {
  test(`route compare looks right (${theme})`, async ({ page }) => {
    await mockAll(page);
    await page.addInitScript((t) => {
      localStorage.setItem("pd.settings", JSON.stringify({ lang: "en", theme: t, regionId: "indore", recents: [], home: null, work: null, muted: true, vehicle: null }));
    }, theme);
    await page.goto(`/route?to=${DEST.lat},${DEST.lng}&toName=Bilawali%20Lake&from=22.6893,75.8529&fromName=Choithram%20Mandi`);
    await expect(page.getByTestId("option-sc1")).toBeVisible();
    await page.waitForTimeout(800); // sheet spring + route draw-on
    await expect(page.locator("[data-vaul-drawer], aside").first()).toHaveScreenshot(`route-compare-${theme}.png`, {
      mask: [page.getByText(/days? ago/)],
    });
  });
}

for (const lang of ["hi", "sv"] as const) {
  test(`no text overflow in ${lang} (SPEC §11.7)`, async ({ page }) => {
    await mockAll(page, { signedIn: true });
    await page.addInitScript((l) => {
      localStorage.setItem("pd.settings", JSON.stringify({ lang: l, theme: "light", regionId: "indore", recents: [], home: null, work: null, muted: true, vehicle: null }));
    }, lang);
    for (const path of [`/route?to=${DEST.lat},${DEST.lng}&toName=Bilawali&from=22.6893,75.8529&fromName=Choithram`, "/me", "/add", "/explore"]) {
      await page.goto(path);
      await page.waitForTimeout(800);
      const overflow = await page.evaluate(() => {
        const bad: string[] = [];
        for (const el of document.querySelectorAll<HTMLElement>("button, [role=radio], h1, h2, p, li, label, span")) {
          if (el.closest(".maplibregl-map, .sr-only") || getComputedStyle(el).overflowX === "auto") continue;
          const cs = getComputedStyle(el);
          if (cs.textOverflow === "ellipsis") continue;
          if (el.scrollWidth > el.clientWidth + 1 && el.clientWidth > 0) bad.push(`${el.tagName}: ${el.textContent?.slice(0, 40)}`);
        }
        return { bad: bad.slice(0, 5), pageScroll: document.documentElement.scrollWidth > innerWidth + 1 };
      });
      expect(overflow, path).toEqual({ bad: [], pageScroll: false });
    }
  });
}
