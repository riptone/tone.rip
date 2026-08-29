// @ts-check

import cloudflare from "@astrojs/cloudflare";
import { defineConfig } from "astro/config";

// https://astro.build/config
export default defineConfig({
  site: "https://dash.tone.rip",
  output: "server",
  integrations: [],
  // Nothing here uses astro:assets: the app icons are remote files served by
  // whatever CDN each Access application names, requested by the browser
  // directly (see index.astro). That was already written down - what was
  // missing is the config that acts on it. The adapter's default is
  // `cloudflare-binding`, which provisions a Cloudflare Images binding on
  // every deploy, so this app was carrying a binding to a paid product with
  // nothing on either end of it. `passthrough` serves the original bytes and
  // binds nothing.
  //
  // apps/web says `compile` for the same reason from the other direction: it
  // does have images, pre-optimises them at build time, and so also needs no
  // runtime binding.

  // No sessions either, and this is the off switch for them. Both Workers
  // pinned a KV namespace id in wrangler.jsonc with a comment saying the
  // adapter provisions one "regardless" - it does not, it provisions one
  // unless told the app has no sessions. Both namespaces have since been
  // deleted from the account; see apps/web/astro.config.mjs.
  session: false,
  adapter: cloudflare({ imageService: "passthrough" }),
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
