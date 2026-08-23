// @ts-check

import cloudflare from "@astrojs/cloudflare";
import sitemap from "@astrojs/sitemap";
import { defineConfig } from "astro/config";

// https://astro.build/config
export default defineConfig({
  site: "https://tone.rip",
  security: {
    allowedDomains: [
      {
        hostname: "tone.rip",
        protocol: "https",
      },
      {
        hostname: "**.tone.rip",
        protocol: "https",
      },
    ],
  },
  output: "server",
  // Every route is public now that /v2 has become the site, so there is
  // nothing left to filter out.
  //
  // `serialize` strips the trailing slash Astro adds by default. The site's
  // own navigation, its canonical tags and the redirect in src/middleware.ts
  // all use the bare form, and a sitemap that advertises the other one sends
  // every crawler through a redirect to find out.
  //
  // Not `trailingSlash: "never"`: that makes Astro's router refuse /cv/
  // outright, turning duplicate content into a 404 for anyone following an
  // older link. Serving both and redirecting one loses nothing.
  integrations: [
    sitemap({
      serialize: (item) => ({
        ...item,
        url: item.url.replace(/(.)\/$/, "$1"),
      }),
    }),
  ],
  adapter: cloudflare({
    imageService: "compile",
  }),
  vite: {
    build: {
      /**
       * Keep media queries in the syntax every browser understands.
       *
       * Without this the CSS minifier rewrites `@media (max-width: 620px)`
       * into `@media (width <= 620px)` - the Media Queries Level 4 range
       * form. It is smaller and it is correct, and it is understood by
       * nobody older than Chrome 104, Firefox 102 or Safari 16.4. A browser
       * that cannot parse the query does not fall back to the base rules
       * with a warning; it drops the whole block, so an older phone gets the
       * desktop layout with the reading column still holding a gap for a
       * field that is 40vw wide.
       *
       * Verified by reading the deployed CSS, which had been shipping the
       * range form. It is also why an SEO audit reported the site "is not
       * using CSS media queries" and scored mobile usability at 29/100 -
       * that parser did not understand the range form either, and neither
       * will the next tool.
       *
       * The floor is set by what the design already needs (`:has()`,
       * `color-mix()`, `100svh`), not by an ambition to support everything.
       * It just should not be *narrower* than that by accident.
       */
      cssTarget: ["chrome111", "edge111", "firefox113", "safari16.2"],
    },
  },
});
