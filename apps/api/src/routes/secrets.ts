import { requireCloudflareAccess } from "@repo/hono-middleware";
import { Hono } from "hono";
import { ACCESS_AUD, ACCESS_TEAM_DOMAIN } from "../access";
import type { AppEnv } from "../env";
import { fetchSecretNames } from "../services/bitwarden-secrets";

/* GET /secrets - which secrets Bitwarden Secrets Manager holds, by name,
 * project and last change. Never values: see services/bitwarden-secrets.ts.
 *
 * Gated behind Cloudflare Access like /apps, and for a stronger version of
 * the same reason: the names alone say which providers and keys exist. The
 * dashboard is the only caller and forwards the Access JWT server-side.
 */
export const secretsRoute = new Hono<AppEnv>();

secretsRoute.use(
  "*",
  requireCloudflareAccess({ teamDomain: ACCESS_TEAM_DOMAIN, aud: ACCESS_AUD }),
);

secretsRoute.get("/", async (c) => {
  const { secrets, state, vaultUrl } = await fetchSecretNames({
    accessToken: c.env.BWS_ACCESS_TOKEN,
    region: c.env.BWS_REGION,
  });
  if (state === "unconfigured")
    console.warn("[secrets] no BWS_ACCESS_TOKEN; serving no secrets");
  return c.json({ secrets, state, vaultUrl }, 200, {
    "Cache-Control": "no-store",
  });
});
