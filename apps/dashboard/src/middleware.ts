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
    // One wildcard rather than a list of origins, and this is the only
    // interesting decision in this file.
    //
    // client-probe.ts pings each tile's own origin directly from the
    // visitor's browser - it has to, because those hosts resolve to Tailscale
    // CGNAT addresses that Cloudflare's edge cannot route to, so only a
    // browser already on the tailnet can reach them. Every one of those
    // origins therefore has to be in connect-src, alongside api.tone.rip for
    // status-client.ts.
    //
    // That list used to be derived here from a hardcoded registry. The
    // registry is gone - tiles now come from Cloudflare Access at runtime
    // (apps/api's /apps) - which leaves the origins unknown at build time.
    // Deriving them per request would put a credentialed fetch on the path of
    // every page render, and a fetch that fails would silently produce a
    // policy that blocks every probe and greys out the whole board.
    //
    // Every application is a subdomain of tone.rip, so one wildcard covers
    // them, covers api.tone.rip, and covers whatever is added next year. It
    // is broader than an exact list - it permits a fetch to any tone.rip
    // subdomain - but every one of those is ours, so what the extra breadth
    // buys an attacker is nothing, and it removes a whole failure mode.
    //
    // Note it does *not* match the apex: `*.tone.rip` never matches
    // `tone.rip`. That is correct here, and worth knowing before adding a
    // call to the site itself and wondering why it is blocked.
    "https://*.tone.rip",
  ],
  // Tile icons are whatever URL each Access application carries in its
  // logo_url, so their host is not knowable here. They are already
  // constrained to https at the API boundary (@repo/validation's
  // toSelfHostedApps drops anything else, because a non-https image on an
  // https page is blocked anyway). An icon is decoration; the strict half of
  // this policy is script-src and connect-src.
  imgSrc: ["https:"],
});
