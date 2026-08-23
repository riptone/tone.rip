// @ts-check

import cloudflare from "@astrojs/cloudflare";
import { defineConfig } from "astro/config";

// https://astro.build/config
export default defineConfig({
  site: "https://dash.tone.rip",
  output: "server",
  integrations: [],
  // No `image` config and no imageService override: nothing in this app uses
  // astro:assets any more. The app icons are remote .webp files served by a
  // CDN and are requested by the browser directly (see index.astro), so
  // there is no image pipeline left to configure.
  adapter: cloudflare(),
  vite: {
    build: {
      // Keep media queries in `max-width` form rather than the Level 4 range
      // syntax the minifier prefers, which browsers older than Chrome 104 /
      // Safari 16.4 drop entirely - taking the responsive layout with them.
      // The reasoning in full is in apps/web/astro.config.mjs; this app shares
      // the stylesheets, so it has to share the target.
      cssTarget: ["chrome111", "edge111", "firefox113", "safari16.2"],
    },
  },
});
