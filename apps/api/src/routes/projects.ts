import { type Context, Hono } from "hono";
import { HTTPException } from "hono/http-exception";
import type { AppEnv } from "../env";
import {
  fetchProjects,
  type ProjectsCacheState,
  type ProjectsSnapshot,
} from "../services/projects-cache";
import { fetchReadmeHtml, isValidRepoName } from "../services/readme-cache";

const BROWSER_TTL_SECONDS = 300;
const EDGE_TTL_SECONDS = 900;
const LAST_UPDATED_HEADER = "x-tone-last-updated";

// CORS (Access-Control-Allow-Origin, Vary) and Cross-Origin-Resource-Policy
// are both handled once for the whole API in src/index.ts - this route used
// to also reject any cross-origin request outright (origin !== same-origin
// → 403), which made sense when tone.rip served this route itself, but
// now that apps/web and apps/api are deliberately different origins, that
// check was rejecting the site's own legitimate requests. Trust the
// app-level CORS allowlist instead of duplicating a second, stricter one
// here.
function buildHeaders(extra?: Record<string, string>): Record<string, string> {
  return {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": `public, max-age=${BROWSER_TTL_SECONDS}, s-maxage=${EDGE_TTL_SECONDS}`,
    "X-Content-Type-Options": "nosniff",
    "Referrer-Policy": "no-referrer",
    ...extra,
  };
}

export const projectsRoute = new Hono<AppEnv>();

projectsRoute.get("/", async (c) => {
  const { snapshot, cacheState } = await fetchProjects({
    githubToken: c.env.GITHUB_TOKEN,
    forceRevalidate: c.req.header("x-tone-revalidate") === "1",
    onUpstreamError: (details) =>
      console.warn("[projects-api] upstream_failed", details),
  });

  return respond(c, snapshot, cacheState);
});

// Rendered README for a single repo, consumed by apps/web's projects panel.
// Returns { html: null } rather than a 404 when a repo simply has no README -
// "we looked, there isn't one" is a successful answer here, same as /projects
// returning [], and it keeps the visitor's console clean.
projectsRoute.get("/:name/readme", async (c) => {
  const name = c.req.param("name");
  if (!isValidRepoName(name)) {
    throw new HTTPException(400, { message: "Invalid repository name" });
  }

  const { html, etag, cacheState } = await fetchReadmeHtml(name, {
    githubToken: c.env.GITHUB_TOKEN,
    onUpstreamError: (details) =>
      console.warn("[readme-api] upstream_failed", { name, ...details }),
  });

  const headers: Record<string, string> = {
    "Cache-Control":
      html === null
        ? "no-store"
        : `public, max-age=${BROWSER_TTL_SECONDS}, s-maxage=${EDGE_TTL_SECONDS}`,
    "X-Content-Type-Options": "nosniff",
    "Referrer-Policy": "no-referrer",
    "X-Tone-Cache": cacheState,
  };
  if (etag) headers.ETag = etag;

  if (etag && c.req.header("If-None-Match") === etag) {
    return c.body(null, 304, headers);
  }
  return c.json({ html }, 200, headers);
});

function respond(
  c: Context<AppEnv>,
  snapshot: ProjectsSnapshot,
  cacheState: ProjectsCacheState,
) {
  const extra: Record<string, string> = {
    [LAST_UPDATED_HEADER]: snapshot.lastUpdated,
    "X-Tone-Cache": cacheState,
  };
  if (cacheState === "stale" || cacheState === "memory-stale") {
    extra.Warning = '110 - "Response is stale"';
  }
  if (cacheState === "unavailable") {
    return c.body(snapshot.body, 200, {
      ...buildHeaders({
        "Cache-Control": "no-store",
        "X-Tone-Cache": cacheState,
      }),
    });
  }
  const headers = buildHeaders(extra);
  if (snapshot.etag) headers.ETag = snapshot.etag;
  // The ETag was previously only used to revalidate against GitHub, never
  // offered back to callers - so a client that already had the payload
  // re-downloaded it on every poll.
  if (snapshot.etag && c.req.header("If-None-Match") === snapshot.etag) {
    return c.body(null, 304, headers);
  }
  return c.body(snapshot.body, 200, headers);
}
