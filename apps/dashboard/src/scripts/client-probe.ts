/**
 * Client-side reachability probe for a single app tile.
 *
 * Mirrors apps/api's server-side
 * probe (src/services/app-health.ts's probeAppHealth) but runs in the
 * visitor's own browser via a no-cors fetch, since the browser - unlike the
 * Cloudflare Worker running apps/api - may itself be on the Tailscale network.
 */

import { fetchWithTimeout } from "@repo/net";

export async function pingUrl(
  href: string,
  timeoutMs: number,
  probePath = "/",
): Promise<boolean> {
  let url: URL;
  try {
    url = new URL(href);
  } catch {
    return false;
  }

  try {
    await fetchWithTimeout(new URL(probePath, url), timeoutMs, {
      mode: "no-cors",
      cache: "no-store",
    });
    return true;
  } catch {
    return false;
  }
}
