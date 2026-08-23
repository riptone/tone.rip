import { SELF_HOSTED_APPS } from "@repo/content";
import { createAstroSecurityMiddleware } from "@repo/hono-middleware/astro-security";

// Access control is delegated to Cloudflare Zero Trust in front of this
// Worker (an Access policy on dash.tone.rip), not app code - matches what
// main-menu originally did. This middleware only adds the baseline nonce'd
// CSP + security-header set every app in this monorepo shares, which is also
// a hard requirement for BaseHead.astro - it reads Astro.locals.cspNonce to
// nonce its inline theme script.
export const onRequest = createAstroSecurityMiddleware({
  devHostnames: ["localhost", "127.0.0.1"],
  connectSrc: [
    // status-client.ts fetches apps/api's /status endpoint client-side.
    "https://api.tone.rip",
    // client-probe.ts pings each tile's own origin directly from the
    // visitor's browser (see its own comment for why) - every app in the
    // registry needs to be allowed, derived here so this can't drift out of
    // sync with SELF_HOSTED_APPS the way two hand-maintained lists would.
    ...new Set(SELF_HOSTED_APPS.map((app) => new URL(app.href).origin)),
  ],
  imgSrc: ["https://cdn.jsdelivr.net"],
});
