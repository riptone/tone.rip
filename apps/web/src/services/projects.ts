/* Fetches the project list for /work.

   Server-side, at request time, rather than in the browser: the list is the
   page's content, so it should be in the HTML a crawler receives and should
   not arrive after first paint and reflow the page. apps/api already caches
   the GitHub response at the edge, so this is a cheap call to a neighbouring
   Worker rather than a round trip to GitHub.

   Note that `/projects` returns *already simplified* repositories - apps/api
   runs @repo/content's `simplifyRepos` before caching. Running it again here
   silently returns nothing, because the second pass looks for the raw GitHub
   field names (`html_url`, `fork`) that the first pass has already renamed.
   Parse against the response's own schema instead.

   A failure is not an error page. The rest of /work is still worth
   reading, so an outage degrades to a note and a link to GitHub. */

import { fetchWithTimeout } from "@repo/content";
import { type Project, projectsResponseSchema } from "@repo/validation";

const PROJECTS_URL = "https://api.tone.rip/projects";
/**
 * How long this page will wait for the list before rendering without it.
 *
 * Short, and it was 4000ms. The reason it has to be short is what a
 * cross-document view transition does while it waits: the browser keeps the
 * *old* page on screen, unchanged, until the new document arrives. There is
 * no spinner and no partial render - a slow answer here is indistinguishable
 * from a click that did nothing. Four seconds of that is not a slow page, it
 * is a broken one.
 *
 * This only bounds the case where there is nothing cached to fall back on;
 * everything else is answered from the memo below without waiting at all. A
 * Worker-to-Worker subrequest that has not answered in 1.5s is not going to.
 */
const TIMEOUT_MS = 1500;
/**
 * How long a fetched list is served before it is refreshed.
 *
 * A Workers isolate is kept alive between requests, so a module-level cache
 * survives them - the same trick apps/api's projects-cache uses for its own
 * upstream. It matters because every navigation to /work is a real request
 * for the page, so without this the API was called once per click.
 *
 * Matched to the API's own 15-minute edge TTL. Asking more often than the
 * data can change buys nothing.
 */
const MEMO_TTL_MS = 15 * 60 * 1000;

export interface ProjectsResult {
  repos: Project[];
  /** False when the API could not be reached - the page says so rather than showing nothing. */
  ok: boolean;
}

let memo: { at: number; result: ProjectsResult } | null = null;
/** The refresh currently in flight, so a burst of requests makes one call. */
let inflight: Promise<ProjectsResult | null> | null = null;

/** Drops the cached list. For tests; nothing in the app needs it. */
export function clearProjectsCache(): void {
  memo = null;
  inflight = null;
}

/**
 * Fetch the list, store it, and hand it back. Null if it could not be had.
 *
 * Never throws, and never stores a failure: the next visitor should get a
 * fresh attempt rather than inherit fifteen minutes of someone else's bad
 * minute. The previous good list, if there is one, is left where it is.
 *
 * It returns the result as well as caching it so that the blocking path
 * below has something to read. Reading `memo` there instead would be the
 * obvious shape and does not type: TypeScript cannot see that this function
 * assigns it, so after an early `if (memo)` return it narrows the variable
 * to `never`. Returning the value says the same thing without arguing.
 */
function refresh(): Promise<ProjectsResult | null> {
  if (inflight) return inflight;

  inflight = (async () => {
    try {
      const response = await fetchWithTimeout(PROJECTS_URL, TIMEOUT_MS, {
        headers: { Accept: "application/json" },
      });
      if (!response.ok) return null;

      const parsed = projectsResponseSchema.safeParse(await response.json());
      if (!parsed.success) return null;

      const repos = parsed.data
        .filter((repo) => !repo.isFork && !repo.isArchived)
        .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));

      const result: ProjectsResult = { repos, ok: true };
      memo = { at: Date.now(), result };
      return result;
    } catch {
      /* Unreachable API. Keep whatever we had; the next request tries again. */
      return null;
    } finally {
      inflight = null;
    }
  })();

  return inflight;
}

/**
 * Repositories to show, newest first.
 *
 * Forks and archived repos are dropped: they are not work, they are history,
 * and a list padded with them says less than a short honest one.
 *
 * Stale-while-revalidate, and the "while" is the point. Only the very first
 * request an isolate serves has to wait for the API; from then on the answer
 * is whatever is in hand, and the refresh happens behind the response. A
 * fifteen-minute-old list is a better page than a fifteen-minute-old list
 * plus a network round trip the reader can see.
 *
 * The background refresh is best-effort. Cloudflare may cut short work that
 * outlives its response, and there is no `ctx.waitUntil` reachable from a
 * page's frontmatter to say otherwise - so treat it as an optimisation, not a
 * guarantee. If it is cancelled the memo simply stays stale and the next
 * request starts another one.
 */
export async function loadProjects(): Promise<ProjectsResult> {
  if (memo) {
    // Past its TTL: answer now, refresh behind the response. Deliberately not
    // awaited - awaiting here is exactly the wait this exists to remove.
    if (Date.now() - memo.at >= MEMO_TTL_MS) void refresh();
    return memo.result;
  }

  // Nothing to serve, so this one has to wait.
  return (await refresh()) ?? { repos: [], ok: false };
}
