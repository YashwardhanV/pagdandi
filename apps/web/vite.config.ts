/// <reference types="vitest/config" />
import { createReadStream, existsSync, statSync } from "node:fs";
import { resolve } from "node:path";
import type { Connect, Plugin } from "vite";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwind from "@tailwindcss/vite";
import { VitePWA } from "vite-plugin-pwa";

const tilesDir = resolve(import.meta.dirname, "../../data/build/tiles");

// Serves data/build/tiles at /tiles with HTTP range requests in dev, like Caddy does in prod.
function devTiles(): Plugin {
  const handler: Connect.NextHandleFunction = (req, res, next) => {
    if (!req.url?.startsWith("/tiles/")) return next();
    const rel = decodeURIComponent(req.url.slice("/tiles/".length).split("?")[0] ?? "");
    const file = resolve(tilesDir, rel);
    if (!file.startsWith(tilesDir) || !existsSync(file)) {
      res.statusCode = 404;
      return res.end();
    }
    const size = statSync(file).size;
    res.setHeader("Accept-Ranges", "bytes");
    res.setHeader("Access-Control-Allow-Origin", "*");
    res.setHeader("Content-Type", rel.endsWith(".json") ? "application/json" : "application/octet-stream");
    const m = /bytes=(\d+)-(\d*)/.exec(req.headers.range ?? "");
    if (!m) {
      res.setHeader("Content-Length", size);
      return createReadStream(file).pipe(res);
    }
    const start = Number(m[1]);
    const end = m[2] ? Math.min(Number(m[2]), size - 1) : size - 1;
    res.statusCode = 206;
    res.setHeader("Content-Range", `bytes ${start}-${end}/${size}`);
    res.setHeader("Content-Length", end - start + 1);
    createReadStream(file, { start, end }).pipe(res);
  };
  return {
    name: "pagdandi-dev-tiles",
    configureServer: (s) => void s.middlewares.use(handler),
    configurePreviewServer: (s) => void s.middlewares.use(handler),
  };
}

export default defineConfig({
  plugins: [
    react(),
    tailwind(),
    devTiles(),
    VitePWA({
      registerType: "autoUpdate",
      injectRegister: false,
      includeAssets: ["favicon.svg", "icons/*.png"],
      manifest: {
        name: "Pagdandi",
        short_name: "Pagdandi",
        description: "Shorter routes through the lanes riders actually use.",
        theme_color: "#F6F5F2",
        background_color: "#F6F5F2",
        display: "standalone",
        orientation: "portrait",
        start_url: "/",
        icons: [
          { src: "/icons/icon-192.png", sizes: "192x192", type: "image/png" },
          { src: "/icons/icon-512.png", sizes: "512x512", type: "image/png" },
          { src: "/icons/icon-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
        ],
      },
      workbox: {
        globPatterns: ["**/*.{js,css,html,svg,png,woff2}"],
        navigateFallback: "/index.html",
        navigateFallbackDenylist: [/^\/v1\//, /^\/tiles\//],
        maximumFileSizeToCacheInBytes: 3 * 1024 * 1024,
        runtimeCaching: [
          {
            // Offline packs: served from the cache with HTTP range support (PMTiles reads by range).
            urlPattern: ({ url }) => url.pathname.startsWith("/tiles/packs/"),
            handler: "CacheOnly",
            options: { cacheName: "pd-offline-packs", rangeRequests: true },
          },
          {
            // Map fonts and sprites for the basemap.
            urlPattern: /^https:\/\/protomaps\.github\.io\/basemaps-assets\//,
            handler: "CacheFirst",
            options: { cacheName: "map-assets", expiration: { maxEntries: 400, maxAgeSeconds: 60 * 60 * 24 * 60 } },
          },
          {
            urlPattern: ({ url }) => url.pathname.startsWith("/v1/regions"),
            handler: "StaleWhileRevalidate",
            options: { cacheName: "api-regions" },
          },
        ],
      },
    }),
  ],
  server: {
    port: 5173,
    proxy: {
      "/v1": { target: "http://localhost:8080", changeOrigin: false },
      "/healthz": "http://localhost:8080",
    },
  },
  preview: {
    proxy: { "/v1": { target: "http://localhost:8080", changeOrigin: false } },
  },
  build: {
    target: "es2022",
    // MapLibre is one ~1 MB lazy chunk by nature; the initial-JS budget is checked by scripts/check-size.mjs.
    chunkSizeWarningLimit: 1200,
    sourcemap: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("maplibre-gl") || id.includes("react-map-gl") || id.includes("pmtiles") || id.includes("@protomaps")) return "map";
        },
      },
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    css: false,
  },
});
