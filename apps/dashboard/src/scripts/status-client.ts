/**
 * Fetches apps/api's /status endpoint (server-side app health + Tailscale
 * device status - see apps/api/src/routes/status.ts) via this app's own
 * same-origin api/status.ts proxy, not api.tone.rip directly - that
 * route requires a Cloudflare Access JWT this page's own Access session can
 * supply, but only via a same-origin request (see api/status.ts's comment).
 *
 * NOTE: in local `astro dev`, api/status.ts's own fetch still hits the real
 * production API with no Access JWT to forward, so it'll 401. Known,
 * accepted limitation - there's no dev proxy for this yet.
 */

import { fetchWithTimeout } from "@repo/net";
import type { ServerAppStatus } from "./status-resolution";

interface ServerStatus {
  apps: Map<string, ServerAppStatus>;
  tailnetDeviceOnline: boolean | null;
}

const STATUS_URL = "/api/status";

interface StatusResponseBody {
  apps?: { href: string; status: ServerAppStatus }[];
  tailnet?: { device?: { online?: boolean } };
}

export async function fetchServerStatuses(
  timeoutMs: number,
): Promise<ServerStatus> {
  try {
    const response = await fetchWithTimeout(STATUS_URL, timeoutMs, {
      cache: "no-store",
    });
    if (!response.ok) return { apps: new Map(), tailnetDeviceOnline: null };

    const data = (await response.json()) as StatusResponseBody;
    return {
      apps: new Map((data.apps ?? []).map((app) => [app.href, app.status])),
      tailnetDeviceOnline:
        typeof data.tailnet?.device?.online === "boolean"
          ? data.tailnet.device.online
          : null,
    };
  } catch {
    return { apps: new Map(), tailnetDeviceOnline: null };
  }
}
