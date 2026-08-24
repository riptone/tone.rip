import { fetchWithTimeout } from "@repo/content";
import { fetchProjects } from "./projects-cache";

const GITHUB_USER = "no-tone";
const GITHUB_API_ORIGIN = "https://api.github.com";
const CACHE_KEY_PREFIX = "https://readme-api.tonil.internal/v1/";
const EDGE_TTL_SECONDS = 3600;
const UPSTREAM_TIMEOUT_MS = 8000;
const CACHED_AT_HEADER = "x-tonil-cached-at";

type ReadmeCacheState =
  | "hit"
  | "miss"
  | "updated"
  | "revalidated"
  | "stale"
  | "absent";

export interface ReadmeResult {
  /** Rendered HTML fragment, or null when the repo has no README. */
  html: string | null;
  etag: string | null;
  cacheState: ReadmeCacheState;
}

// GitHub repo names allow alphanumerics, hyphen, underscore and period. The
// name is interpolated into an upstream URL, so validate it rather than trust
// it: a bare encodeURIComponent still lets "." / ".." through, and anything
// outside this set has no business reaching api.github.com.
const REPO_NAME_PATTERN = /^[A-Za-z0-9._-]{1,100}$/;

export function isValidRepoName(name: string): boolean {
  return REPO_NAME_PATTERN.test(name) && name !== "." && name !== "..";
}

interface FetchReadmeOptions {
  githubToken?: string;
  onUpstreamError?: (details: { error: string; hasStale: boolean }) => void;
}

function getEdgeCache(): Cache | undefined {
  return (globalThis as { caches?: { default: Cache } }).caches?.default;
}

function readCachedAtMs(res: Response): number {
  const parsed = Number(res.headers.get(CACHED_AT_HEADER));
  return Number.isFinite(parsed) ? parsed : 0;
}

function isFresh(cachedAtMs: number, nowMs: number): boolean {
  return cachedAtMs > 0 && nowMs - cachedAtMs < EDGE_TTL_SECONDS * 1000;
}

function toCacheEntry(html: string, etag: string | null): Response {
  const headers: Record<string, string> = {
    "Content-Type": "text/html; charset=utf-8",
    [CACHED_AT_HEADER]: String(Date.now()),
  };
  if (etag) headers.ETag = etag;
  return new Response(html, { status: 200, headers });
}

/**
 * Whether `name` is one of the repos /projects actually lists.
 *
 * Without this, /projects/:name/readme is a rate-limit amplifier: a miss isn't
 * cacheable (there's no content to cache), so every distinct made-up name -
 * /projects/aaa1/readme, /aaa2/… - becomes its own uncached GitHub call, and a
 * loop could drain the token's hourly budget and take /projects down with it.
 * The repo list is already cached by projects-cache, so this check is
 * effectively free and bounds us to real repos.
 */
async function isKnownRepo(
  name: string,
  githubToken?: string,
): Promise<boolean> {
  try {
    const { snapshot } = await fetchProjects({ githubToken });
    const repos = JSON.parse(snapshot.body) as Array<{ name?: unknown }>;
    const needle = name.toLowerCase();
    return repos.some(
      (repo) => String(repo?.name ?? "").toLowerCase() === needle,
    );
  } catch {
    return false;
  }
}

/**
 * Fetches a repo's README as a rendered HTML fragment, cached at the edge.
 *
 * apps/web used to fetch this straight from the visitor's browser, which meant
 * every visitor spent GitHub's 60-requests/hour unauthenticated per-IP budget,
 * nothing was cached, and any GitHub hiccup surfaced in the console as an
 * opaque CORS error (GitHub omits Access-Control-Allow-Origin on its own 5xx
 * responses). Proxying here instead reuses the GITHUB_TOKEN secret and the
 * same edge-cache approach as projects-cache.ts.
 */
export async function fetchReadmeHtml(
  name: string,
  options: FetchReadmeOptions = {},
): Promise<ReadmeResult> {
  const cache = getEdgeCache();
  const cacheKey = new Request(
    `${CACHE_KEY_PREFIX}${encodeURIComponent(name)}`,
  );
  const cached = cache
    ? ((await cache.match(cacheKey)) ?? undefined)
    : undefined;

  let staleHtml: string | null = null;
  let staleEtag: string | null = null;
  if (cached) {
    staleHtml = await cached.clone().text();
    staleEtag = cached.headers.get("ETag");
    if (isFresh(readCachedAtMs(cached), Date.now())) {
      return { html: staleHtml, etag: staleEtag, cacheState: "hit" };
    }
  }

  // Checked only when we're actually about to call GitHub - an already-cached
  // README keeps being served even if the repo list is momentarily unavailable.
  if (!(await isKnownRepo(name, options.githubToken))) {
    return staleHtml === null
      ? { html: null, etag: null, cacheState: "absent" }
      : { html: staleHtml, etag: staleEtag, cacheState: "stale" };
  }

  try {
    const upstream = await fetchWithTimeout(
      `${GITHUB_API_ORIGIN}/repos/${GITHUB_USER}/${encodeURIComponent(name)}/readme`,
      UPSTREAM_TIMEOUT_MS,
      {
        headers: {
          "User-Agent": "tonil-api",
          Accept: "application/vnd.github.html+json",
          ...(options.githubToken
            ? { Authorization: `Bearer ${options.githubToken}` }
            : {}),
          // GitHub doesn't count a 304 against the hourly rate limit, so
          // revalidating is cheaper than refetching.
          ...(staleEtag ? { "If-None-Match": staleEtag } : {}),
        },
      },
    );

    if (upstream.status === 304 && staleHtml !== null) {
      if (cache) await cache.put(cacheKey, toCacheEntry(staleHtml, staleEtag));
      return { html: staleHtml, etag: staleEtag, cacheState: "revalidated" };
    }

    // A repo with no README is a legitimate answer, not a failure - don't fall
    // back to a stale copy for it.
    if (upstream.status === 404) {
      return { html: null, etag: null, cacheState: "absent" };
    }
    if (!upstream.ok) throw new Error(`upstream-status-${upstream.status}`);

    const html = await upstream.text();
    const etag = upstream.headers.get("ETag");
    if (cache) await cache.put(cacheKey, toCacheEntry(html, etag));
    return {
      html,
      etag,
      cacheState: staleHtml === null ? "miss" : "updated",
    };
  } catch (error) {
    options.onUpstreamError?.({
      error: error instanceof Error ? error.message : "unknown-error",
      hasStale: staleHtml !== null,
    });
    // A GitHub outage shouldn't blank a README we already have.
    if (staleHtml !== null) {
      return { html: staleHtml, etag: staleEtag, cacheState: "stale" };
    }
    return { html: null, etag: null, cacheState: "absent" };
  }
}
