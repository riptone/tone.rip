import { requireCloudflareAccess } from "@repo/hono-middleware";
import { Hono } from "hono";
import type { AppEnv } from "../env";
import { fetchAccessApps } from "../services/access-apps";
import { getTailnetDevice, probeAllApps } from "../services/app-health";

export const statusRoute = new Hono<AppEnv>();

// The Cloudflare Zero Trust team domain, which is also the `iss` claim on
// every Access JWT - so this constant is not cosmetic: it is half of the
// signature check below, and a stale value rejects every valid token.
//
// It moved once already. Renaming the team in the Zero Trust dashboard
// changes the issuer immediately, with no deploy and no DNS involved, and
// nothing in this repository finds out. The symptom is `/status` answering
// 401 to a browser that Access itself has just authenticated, which reads as
// a bug in this route rather than as a value that went out of date.
//
// The live answer is one request away and needs no credentials, because
// Cloudflare redirects an unauthenticated visitor straight to it:
//
//   curl -sI https://dash.tone.rip | grep -i ^location
const ACCESS_TEAM_DOMAIN = "riptone.cloudflareaccess.com";

// The Access application's AUD tag, which survived that rename untouched -
// it identifies the application, not the team. Verified against the `kid`
// parameter on the same redirect.
const ACCESS_AUD =
  "28a3efd8f96a2e859f3bcd8158570e67538c297b25a8d7de9803b877e8a1881a";

// Self-hosted app up/down + Tailscale device presence is only meant for the
// dashboard, which is gated behind a Cloudflare Access policy - but that
// policy lives on a different hostname (dash.tone.rip), so it doesn't
// automatically cover this route. apps/dashboard's own api/status.ts proxies
// here server-to-server, forwarding the Cf-Access-Jwt-Assertion header
// Access already attached to ITS incoming request; verify that here instead.
statusRoute.use(
  "*",
  requireCloudflareAccess({ teamDomain: ACCESS_TEAM_DOMAIN, aud: ACCESS_AUD }),
);

statusRoute.get("/", async (c) => {
  // The same list the dashboard renders, from the same cached source - so a
  // tile can never appear with no status, or a status arrive for a tile that
  // is not on the board.
  const { apps: registry } = await fetchAccessApps({
    accountId: c.env.CF_ACCOUNT_ID,
    token: c.env.CF_ACCESS_TOKEN,
  });

  const [apps, tailnetDevice] = await Promise.all([
    probeAllApps(registry),
    getTailnetDevice(c.env),
  ]);

  return c.json(
    {
      generatedAt: new Date().toISOString(),
      apps,
      tailnet: { device: tailnetDevice },
    },
    200,
    { "Cache-Control": "no-store" },
  );
});
