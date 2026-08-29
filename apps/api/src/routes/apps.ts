import { requireCloudflareAccess } from "@repo/hono-middleware";
import { Hono } from "hono";
import type { AppEnv } from "../env";
import { fetchAccessApps } from "../services/access-apps";
import { ACCESS_AUD, ACCESS_TEAM_DOMAIN } from "./status";

/* GET /apps - the self-hosted applications, from Cloudflare Access.
 *
 * Gated behind Cloudflare Access, like /status. The list is the set of
 * services somebody self-hosts — hostnames, names, tags, icons — which is a
 * map of the attack surface. Serving it publicly would be enumeration: to
 * resolve DNS you already need the names, this endpoint hands you the list.
 * The dashboard is the only consumer and it forwards the Access JWT
 * server-side, so the cheap public cache is not worth the leak.
 */
export const appsRoute = new Hono<AppEnv>();

appsRoute.use(
  "*",
  requireCloudflareAccess({ teamDomain: ACCESS_TEAM_DOMAIN, aud: ACCESS_AUD }),
);

appsRoute.get("/", async (c) => {
  const { apps, state } = await fetchAccessApps({
    accountId: c.env.CF_ACCOUNT_ID,
    token: c.env.CF_ACCESS_TOKEN,
    probePaths: c.env.PROBE_PATHS,
  });

  if (state === "unconfigured") {
    console.warn("[apps] no CF_ACCESS_TOKEN/CF_ACCOUNT_ID; serving no apps");
  } else if (state === "unavailable") {
    console.warn("[apps] upstream unavailable and nothing cached");
  }

  return c.json({ apps, state }, 200, {
    "Cache-Control": "no-store",
  });
});
