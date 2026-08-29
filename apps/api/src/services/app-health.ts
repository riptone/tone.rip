import { fetchWithTimeout } from "@repo/net";
import type { SelfHostedApp } from "@repo/validation";
import type { Bindings } from "../env";

type HealthStatus = "up" | "down" | "unknown";

/* Everything in this module is on a clock, because /status is the last hop of
   a three-deep chain and the browser is the one holding the stopwatch:

     dashboard.ts       gives up after 5000ms
       api/status.ts    gives up after 4000ms
         this route     must therefore finish inside that

   The two halves of the route run in parallel, so its worst case is the
   slower of PROBE_TIMEOUT_MS and the Tailscale pair (token then devices,
   sequential) - about three seconds, comfortably inside the proxy's patience.

   Getting this wrong is what made /status "sometimes completely empty". The
   token exchange ran on *every* request: two unbounded round trips to
   api.tailscale.com in front of the device list, while the browser waited
   2500ms for the whole chain. When Tailscale was slow the proxy's own timeout
   fired and answered with its truthful-but-useless `{"apps":[],"tailnet":{}}`.
   Nothing was broken. Every deadline was just shorter than the thing it was
   waiting for. */

const PROBE_TIMEOUT_MS = 2000;
const TAILSCALE_TIMEOUT_MS = 1500;

export async function probeAppHealth(
  href: string,
  probePath = "/",
): Promise<HealthStatus> {
  let url: URL;
  try {
    url = new URL(href);
  } catch {
    return "unknown";
  }

  try {
    const response = await fetchWithTimeout(
      new URL(probePath, url),
      PROBE_TIMEOUT_MS,
      { cache: "no-store", redirect: "manual" },
    );
    return response.status < 500 ? "up" : "down";
  } catch {
    return "down";
  }
}

interface AppHealth {
  name: string;
  href: string;
  status: HealthStatus;
}

/**
 * Self-hosted apps live on the tailnet: their DNS records are grey-clouded
 * straight to a Tailscale CGNAT address (100.64.0.0/10), which Cloudflare's
 * network can't route to. A Worker subrequest to one doesn't reach the origin
 * at all - Cloudflare's edge refuses it with its own 403 (verified: `server:
 * cloudflare` + a cf-ray on the response), *whether the box is up or dead*.
 * Since 403 < 500, probeAppHealth read that as "up" and every tile showed
 * healthy even with the tailnet device offline for months.
 *
 * There's no probe that fixes this - the edge is simply not on the tailnet.
 * So report "unknown" for these and let the two signals that CAN see them
 * decide (see apps/dashboard's status-resolution.ts): the Tailscale device
 * status from getTailnetDevice below, and the visitor's own browser ping,
 * which works when the visitor is on the tailnet. Genuinely public entries
 * (e.g. login.tailscale.com) are still probed normally.
 */
function isTailnetOnly(app: SelfHostedApp): boolean {
  return app.tags.includes("Self-Hosted");
}

export async function probeAllApps(
  apps: SelfHostedApp[],
): Promise<AppHealth[]> {
  return Promise.all(
    apps.map(async (app) => ({
      name: app.name,
      href: app.href,
      status: isTailnetOnly(app)
        ? ("unknown" as HealthStatus)
        : await probeAppHealth(app.href, app.probePath),
    })),
  );
}

interface TailnetDevice {
  name: string;
  online: boolean;
  lastSeen: string | null;
}

interface RawTailnetDevice {
  name?: string;
  hostname?: string;
  addresses?: string[];
  online?: boolean;
  lastSeen?: string;
}

const TAILSCALE_TOKEN_URL = "https://api.tailscale.com/api/v2/oauth/token";

/**
 * Tailscale's client-credentials tokens are valid for an hour. Reusing one
 * for fifty minutes removes a round trip from all but the first request an
 * isolate serves, which was half the latency of this whole route.
 *
 * Module scope, so it lives as long as the Worker isolate - the same trick
 * services/projects-cache.ts uses. It is not shared between isolates and
 * does not need to be: the worst case is a few isolates each holding their
 * own token, which is exactly what happened on every request before.
 */
const TOKEN_TTL_MS = 50 * 60 * 1000;
let cachedToken: { value: string; at: number } | null = null;

function basicAuth(username: string, password: string): string {
  return `Basic ${btoa(`${username}:${password}`)}`;
}

async function getTailscaleToken(env: Bindings): Promise<string | null> {
  if (cachedToken && Date.now() - cachedToken.at < TOKEN_TTL_MS) {
    return cachedToken.value;
  }

  const clientId = env.TAILSCALE_OAUTH_CLIENT_ID?.trim();
  const clientSecret = env.TAILSCALE_OAUTH_CLIENT_SECRET?.trim();
  if (!clientId || !clientSecret) return null;

  const body = new URLSearchParams({
    grant_type: "client_credentials",
    scope: env.TAILSCALE_OAUTH_SCOPE?.trim() || "devices:core:read",
  });

  const response = await fetchWithTimeout(
    TAILSCALE_TOKEN_URL,
    TAILSCALE_TIMEOUT_MS,
    {
      method: "POST",
      headers: {
        authorization: basicAuth(clientId, clientSecret),
        "content-type": "application/x-www-form-urlencoded",
      },
      body,
    },
  );
  if (!response.ok) return null;

  const data = (await response.json()) as { access_token?: string };
  const token = data.access_token ?? null;
  // A failure is deliberately not cached: the next request should get a fresh
  // attempt rather than inherit fifty minutes of someone else's bad minute.
  if (token) cachedToken = { value: token, at: Date.now() };
  return token;
}

export function findTailnetDevice(
  devices: RawTailnetDevice[],
  target: string,
): RawTailnetDevice | null {
  const needle = target.trim().toLowerCase();
  if (!needle) return null;
  return (
    devices.find((device) => {
      const names = [
        device.hostname,
        device.name,
        ...(device.addresses ?? []),
      ].filter(Boolean);
      return names.some((name) => {
        const value = String(name).toLowerCase();
        return value === needle || value.startsWith(`${needle}.`);
      });
    }) ?? null
  );
}

/**
 * How long a device reading is reused.
 *
 * The dashboard sweeps on a timer while it is open, so a single visitor
 * produces a steady trickle of identical questions. Ten seconds is shorter
 * than any sweep interval, so nobody ever sees a staler answer than they
 * would have got anyway, and it collapses the bursts.
 */
const DEVICE_TTL_MS = 10_000;
let cachedDevice: { value: TailnetDevice | null; at: number } | null = null;

async function readTailnetDevice(env: Bindings): Promise<TailnetDevice | null> {
  const tailnet = env.TAILSCALE_TAILNET?.trim();
  const target = env.TAILSCALE_STATUS_DEVICE?.trim();
  if (!tailnet || !target) return null;

  const token = await getTailscaleToken(env);
  if (!token) return null;

  const response = await fetchWithTimeout(
    `https://api.tailscale.com/api/v2/tailnet/${encodeURIComponent(tailnet)}/devices`,
    TAILSCALE_TIMEOUT_MS,
    { headers: { authorization: `Bearer ${token}` }, cache: "no-store" },
  );
  if (!response.ok) {
    // A rejected token is the one failure worth reacting to rather than
    // waiting out: it stays rejected for the rest of the cached token's life
    // otherwise, which is up to fifty minutes of this route reporting no
    // device for a tailnet that is perfectly healthy.
    if (response.status === 401 || response.status === 403) cachedToken = null;
    return null;
  }

  const data = (await response.json()) as { devices?: RawTailnetDevice[] };
  const device = findTailnetDevice(data.devices ?? [], target);
  if (!device) return null;

  return {
    name: device.hostname ?? device.name ?? target,
    online: Boolean(device.online),
    lastSeen: device.lastSeen ?? null,
  };
}

/**
 * The tailnet device backing the dashboard's "is the box up" signal, or null.
 *
 * Null covers every way of not knowing - unconfigured, unauthorized, slow,
 * offline - and that is on purpose. This runs alongside `probeAllApps` in a
 * `Promise.all`, so a rejection here would take the app list down with it and
 * answer the dashboard with nothing at all. The device being unreachable is
 * information; losing the other half of the response because of it is not.
 */
export async function getTailnetDevice(
  env: Bindings,
): Promise<TailnetDevice | null> {
  if (cachedDevice && Date.now() - cachedDevice.at < DEVICE_TTL_MS) {
    return cachedDevice.value;
  }

  try {
    const value = await readTailnetDevice(env);
    cachedDevice = { value, at: Date.now() };
    return value;
  } catch {
    // Not cached: a timeout should not become ten seconds of certainty.
    return null;
  }
}
