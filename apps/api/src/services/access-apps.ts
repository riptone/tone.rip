import { fetchWithTimeout } from "@repo/net";
import {
  accessApplicationsResponseSchema,
  type SelfHostedApp,
  toSelfHostedApps,
} from "@repo/validation";
import { getEdgeCache } from "./edge-cache";

/* The dashboard's tile list, read from Cloudflare Access.
 *
 * This replaces a hand-written array in @repo/content. The account already
 * knows which applications exist, what they are called, what icon they use
 * and how they are tagged - it has to, because that is what the Access App
 * Launcher renders - so keeping a second copy in this repository meant every
 * new service was added in two places and drifted from one.
 *
 * Cached at the edge for the same reason projects-cache is: this is a
 * credentialed upstream call on the path of a page render, the answer changes
 * about as often as somebody adds a service, and the token's rate limit is
 * not a thing to spend per visitor.
 */

const EDGE_TTL_SECONDS = 900;
const UPSTREAM_TIMEOUT_MS = 6000;
/* Versioned, and the version is part of the contract - bump it whenever the
   *shape* of what is stored changes, not just when the fetching changes.
   Earned: v1 held tiles whose `tags` had the launcher tag stripped out. When
   that was reverted (apps/api's probe reads that tag to decide whether the
   edge may reach a host), every cached entry kept serving the old shape for
   the full TTL, and the probe silently read the wrong thing. A new key makes
   a format change take effect on deploy instead of fifteen minutes later. */
const CACHE_KEY_URL = "https://access-apps.tone-rip.internal/cache-v2";
const CACHED_AT_HEADER = "x-tone-cached-at";

type AccessAppsState =
  | "hit"
  | "updated"
  | "stale"
  | "unconfigured"
  | "unavailable";

export interface AccessAppsResult {
  apps: SelfHostedApp[];
  state: AccessAppsState;
}

/* Same shape as projects-cache's: a last-known-good copy in module scope, so
   a cold isolate whose edge entry has been evicted still answers with
   something rather than with an empty launcher. */
let memoryApps: SelfHostedApp[] | null = null;

function isFresh(response: Response, nowMs: number): boolean {
  const cachedAt = Number(response.headers.get(CACHED_AT_HEADER));
  return (
    Number.isFinite(cachedAt) &&
    cachedAt > 0 &&
    nowMs - cachedAt < EDGE_TTL_SECONDS * 1000
  );
}

export interface AccessAppsOptions {
  accountId?: string;
  token?: string;
  /**
   * Hosts that must be probed somewhere other than "/", as
   * `host=path` pairs separated by commas:
   *
   *     secrets.example.com=/favicon.ico,other.example.com=/health
   *
   * A secret rather than a constant, because the hostnames are the point. A
   * public repository listing the services somebody self-hosts hands over a
   * map of their attack surface, and the fact that one of them answers 200 at
   * "/" while signed out is exactly the kind of detail worth not publishing.
   *
   * Absent means every app is probed at "/", which is correct for most and
   * merely imprecise for the rest.
   */
  probePaths?: string;
}

/**
 * Parse the PROBE_PATHS secret into a host -> path map.
 *
 * Malformed pairs are skipped rather than thrown on: this runs on the render
 * path of the launcher, and one typo in a secret should cost one app a
 * accurate probe, not the whole board.
 */
function parseProbePaths(raw: string | undefined): Map<string, string> {
  const paths = new Map<string, string>();
  if (!raw) return paths;

  for (const entry of raw.split(",")) {
    const [host, path] = entry.split("=");
    const cleanHost = host?.trim().toLowerCase();
    const cleanPath = path?.trim();
    if (!cleanHost || !cleanPath?.startsWith("/")) continue;
    paths.set(cleanHost, cleanPath);
  }
  return paths;
}

/** Apply the configured overrides to a freshly mapped list. */
export function withProbePaths(
  apps: SelfHostedApp[],
  raw: string | undefined,
): SelfHostedApp[] {
  const paths = parseProbePaths(raw);
  if (paths.size === 0) return apps;

  return apps.map((app) => {
    let hostname: string;
    try {
      hostname = new URL(app.href).hostname.toLowerCase();
    } catch {
      return app;
    }
    const override = paths.get(hostname);
    return override ? { ...app, probePath: override } : app;
  });
}

/**
 * Every renderable Access application, newest first cached, then upstream.
 *
 * Never throws. The dashboard is a launcher: a page that renders no tiles
 * because a token expired is a worse outcome than a page rendering the tiles
 * it saw fifteen minutes ago, so every failure path degrades to the best copy
 * available and says which one it used in `state`.
 *
 * `unconfigured` is deliberately distinct from `unavailable`. The first means
 * no token was ever set - the expected state of a fork, or of a local
 * `wrangler dev` - and should not read as an outage in a log.
 */
export async function fetchAccessApps(
  options: AccessAppsOptions,
): Promise<AccessAppsResult> {
  const { accountId, token, probePaths } = options;

  const cache = getEdgeCache();
  const cacheKey = new Request(CACHE_KEY_URL);
  const cached = cache
    ? ((await cache.match(cacheKey)) ?? undefined)
    : undefined;

  let stale: SelfHostedApp[] | null = memoryApps;
  if (cached) {
    try {
      stale = (await cached.clone().json()) as SelfHostedApp[];
      if (isFresh(cached, Date.now())) {
        memoryApps = stale;
        return { apps: stale, state: "hit" };
      }
    } catch {
      // A corrupt entry is worth no more than a missing one.
    }
  }

  if (!accountId || !token) {
    return stale
      ? { apps: stale, state: "stale" }
      : { apps: [], state: "unconfigured" };
  }

  try {
    const response = await fetchWithTimeout(
      // per_page is the maximum: this list is small, and paginating a list
      // that will never reach fifty entries is code with no way to be tested.
      `https://api.cloudflare.com/client/v4/accounts/${accountId}/access/apps?per_page=50`,
      UPSTREAM_TIMEOUT_MS,
      {
        headers: {
          Authorization: `Bearer ${token}`,
          Accept: "application/json",
        },
      },
    );

    if (!response.ok) throw new Error(`upstream-status-${response.status}`);

    const parsed = accessApplicationsResponseSchema.safeParse(
      await response.json(),
    );
    if (!parsed.success) throw new Error("upstream-shape");

    // Cloudflare answers 200 with success:false for a token that is valid but
    // unscoped. Treating that as "no applications" would wipe the launcher.
    if (!parsed.data.success) {
      throw new Error(
        parsed.data.errors?.[0]?.message ?? "upstream-unsuccessful",
      );
    }

    const apps = withProbePaths(
      toSelfHostedApps(parsed.data.result ?? []),
      probePaths,
    );

    // An empty result from a *successful* call is not cached: an account
    // genuinely has applications, so zero of them almost certainly means a
    // scope or account-id mistake, and caching it would hold the launcher
    // empty for the full TTL.
    if (apps.length === 0) {
      return stale
        ? { apps: stale, state: "stale" }
        : { apps: [], state: "unavailable" };
    }

    memoryApps = apps;
    if (cache) {
      await cache.put(
        cacheKey,
        new Response(JSON.stringify(apps), {
          headers: {
            "Content-Type": "application/json",
            [CACHED_AT_HEADER]: String(Date.now()),
          },
        }),
      );
    }
    return { apps, state: "updated" };
  } catch {
    return stale
      ? { apps: stale, state: "stale" }
      : { apps: [], state: "unavailable" };
  }
}
