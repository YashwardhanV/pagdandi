/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";

const API = process.env.PAGDANDI_API ?? "http://localhost:8080";

export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: "autoUpdate",
      injectRegister: "script-defer",
      includeAssets: ["favicon.svg", "icons/*.png"],
      manifest: {
        name: "Pagdandi — the other way",
        short_name: "Pagdandi",
        description: "Maps for the road less taken: shortcuts, kaccha tracks, quiet lanes, scenic detours and new roads around Indore.",
        theme_color: "#0B0F15",
        background_color: "#0B0F15",
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
        maximumFileSizeToCacheInBytes: 4 * 1024 * 1024,
        runtimeCaching: [
          {
            urlPattern: ({ url }) => url.pathname.startsWith("/tiles/"),
            handler: "CacheFirst",
            options: { cacheName: "pd-tiles", rangeRequests: true, expiration: { maxEntries: 4 } },
          },
          {
            urlPattern: ({ url }) => url.hostname.includes("protomaps.github.io"),
            handler: "CacheFirst",
            options: { cacheName: "pd-glyphs", expiration: { maxEntries: 400, maxAgeSeconds: 60 * 60 * 24 * 60 } },
          },
          {
            urlPattern: ({ url }) => url.hostname.includes("elevation-tiles-prod") || url.hostname.includes("arcgisonline"),
            handler: "CacheFirst",
            options: { cacheName: "pd-raster", expiration: { maxEntries: 1500, maxAgeSeconds: 60 * 60 * 24 * 30 } },
          },
        ],
      },
    }),
  ],
  server: {
    port: 5173,
    proxy: {
      "/v1": { target: API, changeOrigin: true },
      "/tiles": { target: API, changeOrigin: true },
    },
  },
  preview: {
    proxy: {
      "/v1": { target: API, changeOrigin: true },
      "/tiles": { target: API, changeOrigin: true },
    },
  },
  build: {
    target: "es2022",
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        manualChunks: (id: string) => (id.includes("maplibre-gl") ? "maplibre" : id.includes("@protomaps") || id.includes("pmtiles") ? "basemaps" : undefined),
      },
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
  },
});
