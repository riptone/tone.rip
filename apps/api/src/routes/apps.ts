import { Hono } from "hono";
import type { AppEnv } from "../env";
import { fetchAccessApps } from "../services/access-apps";

/* GET /apps - the self-hosted applications, from Cloudflare Access.
 *
 * Public, unlike /status. What this returns is a list of names, hostnames,
 * tags and icon URLs for services that are *already* published at those
 * hostnames and gated by Access on arrival - so it discloses nothing that
 * resolving the DNS would not. `/status` stays behind Access because up/down
 * over time is a different thing to publish than a list of names.
 *
 * The dashboard is the only consumer today, and it needs this before it can
 * render, so the cheap and cacheable version matters more than the private
 * one.
 */
export const appsRoute = new Hono<AppEnv>();

appsRoute.get("/", async (c) => {
  const { apps, state } = await fetchAccessApps({
    accountId: c.env.CF_ACCOUNT_ID,
    token: c.env.CF_ACCESS_TOKEN,
  });

  if (state === "unconfigured") {
    console.warn("[apps] no CF_ACCESS_TOKEN/CF_ACCOUNT_ID; serving no apps");
  } else if (state === "unavailable") {
    console.warn("[apps] upstream unavailable and nothing cached");
  }

  return c.json({ apps, state }, 200, {
    // Shorter than the service's own 15-minute edge TTL on purpose: this
    // header governs the *browser*, and a visitor who has just added a
    // service should not have to hard-refresh for five minutes to see it.
    // The expensive half - the credentialed upstream call - is already
    // absorbed by the edge cache behind this.
    "Cache-Control": "public, max-age=60, s-maxage=60",
  });
});
