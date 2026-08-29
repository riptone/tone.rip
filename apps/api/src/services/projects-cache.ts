import {
  fetchWithTimeout,
  latestUpdateTimestamp,
  simplifyRepos,
} from "@repo/content";

const GITHUB_API_URL =
  "https://api.github.com/users/riptone/repos?per_page=100&sort=updated";
const CACHE_KEY_URL = "https://projects-api.tonil.internal/cache-v1";
const EDGE_TTL_SECONDS = 900;
// GitHub has been observed taking 10s+ and returning 504s; without a bound
// that latency passes straight through to the caller. On timeout we fall
// through to the stale/memory snapshot below, which is the better answer.
const UPSTREAM_TIMEOUT_MS = 8000;
const CACHED_AT_HEADER = "x-tonil-cached-at";
const LAST_UPDATED_HEADER = "x-tonil-last-updated";

export interface ProjectsSnapshot {
  body: string;
  etag: string | null;
  lastUpdated: string;
}

export type ProjectsCacheState =
  | "hit"
  | "revalidated"
  | "updated"
  | "miss"
  | "stale"
  | "memory-stale"
  | "unavailable";

interface ProjectsResult {
  snapshot: ProjectsSnapshot;
  cacheState: ProjectsCacheState;
}

// A module-level fallback so a cold GitHub outage still serves the last
// successful response even if the edge Cache API entry has been evicted.
let memorySnapshot: ProjectsSnapshot | null = null;

function readCachedAtMs(res: Response): number {
  const raw = res.headers.get(CACHED_AT_HEADER);
  if (!raw) return 0;
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : 0;
}

function isFresh(cachedAtMs: number, nowMs: number): boolean {
  return cachedAtMs > 0 && nowMs - cachedAtMs < EDGE_TTL_SECONDS * 1000;
}

async function snapshotFromResponse(res: Response): Promise<ProjectsSnapshot> {
  return {
    body: await res.clone().text(),
    etag: res.headers.get("ETag"),
    lastUpdated: res.headers.get(LAST_UPDATED_HEADER) ?? "",
  };
}

function toCacheEntry(snapshot: ProjectsSnapshot): Response {
  const headers: Record<string, string> = {
    "Content-Type": "application/json; charset=utf-8",
    [CACHED_AT_HEADER]: String(Date.now()),
    [LAST_UPDATED_HEADER]: snapshot.lastUpdated,
  };
  if (snapshot.etag) headers.ETag = snapshot.etag;
  return new Response(snapshot.body, { status: 200, headers });
}

function getEdgeCache(): Cache | undefined {
  return (globalThis as { caches?: { default: Cache } }).caches?.default;
}

interface FetchProjectsOptions {
  githubToken?: string;
  forceRevalidate?: boolean;
  onUpstreamError?: (details: {
    latencyMs: number;
    hasCachedSnapshot: boolean;
    error: string;
  }) => void;
}

/**
 * Ported from tone.rip's src/pages/api/projects.json.ts, split out of the
 * route handler so the caching/revalidation logic (the bulk of that file) can
 * be unit-tested independently of Hono/HTTP concerns.
 */
export async function fetchProjects(
  options: FetchProjectsOptions = {},
): Promise<ProjectsResult> {
  const nowMs = Date.now();
  const cache = getEdgeCache();
  const cacheKey = new Request(CACHE_KEY_URL);
  const cachedResponse = cache
    ? ((await cache.match(cacheKey)) ?? undefined)
    : undefined;

  let cachedSnapshot: ProjectsSnapshot | null = null;
  if (cachedResponse) {
    cachedSnapshot = await snapshotFromResponse(cachedResponse);
    if (
      !options.forceRevalidate &&
      isFresh(readCachedAtMs(cachedResponse), nowMs)
    ) {
      return { snapshot: cachedSnapshot, cacheState: "hit" };
    }
  }

  const upstreamStartedAt = Date.now();
  try {
    const upstream = await fetchWithTimeout(
      GITHUB_API_URL,
      UPSTREAM_TIMEOUT_MS,
      {
        headers: {
          "User-Agent": "tonil-api",
          Accept: "application/vnd.github.mercy-preview+json",
          ...(options.githubToken
            ? { Authorization: `Bearer ${options.githubToken}` }
            : {}),
          ...(cachedSnapshot?.etag
            ? { "If-None-Match": cachedSnapshot.etag }
            : {}),
        },
      },
    );

    if (upstream.status === 304 && cachedSnapshot) {
      if (cache)
        await cache.put(cacheKey, toCacheEntry(cachedSnapshot).clone());
      memorySnapshot = cachedSnapshot;
      return { snapshot: cachedSnapshot, cacheState: "revalidated" };
    }

    if (!upstream.ok) {
      throw new Error(`upstream-status-${upstream.status}`);
    }

    const raw = await upstream.json();
    const simplified = simplifyRepos(raw);
    const snapshot: ProjectsSnapshot = {
      body: JSON.stringify(simplified),
      etag: upstream.headers.get("ETag"),
      lastUpdated: latestUpdateTimestamp(simplified),
    };

    memorySnapshot = snapshot;
    if (cache) await cache.put(cacheKey, toCacheEntry(snapshot).clone());

    return { snapshot, cacheState: cachedSnapshot ? "updated" : "miss" };
  } catch (error) {
    options.onUpstreamError?.({
      latencyMs: Date.now() - upstreamStartedAt,
      hasCachedSnapshot: !!cachedSnapshot,
      error: error instanceof Error ? error.message : "unknown-error",
    });

    if (cachedSnapshot) {
      return { snapshot: cachedSnapshot, cacheState: "stale" };
    }
    if (memorySnapshot) {
      return { snapshot: memorySnapshot, cacheState: "memory-stale" };
    }
    return {
      snapshot: { body: "[]", etag: null, lastUpdated: "" },
      cacheState: "unavailable",
    };
  }
}

/** Test-only escape hatch to reset the module-level memory fallback between test cases. */
export function __resetProjectsMemorySnapshotForTests(): void {
  memorySnapshot = null;
}
