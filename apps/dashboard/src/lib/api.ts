import { env } from "cloudflare:workers";
import { withTimeout } from "@repo/net";

/* apps/api, reached the short way.
 *
 * This app makes two server-to-server calls to apps/api - the SSR tile fetch
 * in pages/index.astro and the /status proxy in pages/api/status.ts - and
 * both used to name `https://api.tone.rip` and go out through the public
 * internet to reach a Worker on the same account: DNS, TLS, a second pass
 * through Cloudflare's edge, a billed subrequest.
 *
 * wrangler.jsonc now binds that Worker as `API`, so the call is dispatched
 * in-process. Nothing about authorisation changes: apps/api verifies the
 * Access JWT this app forwards, on its own routes, exactly as before - see
 * the note in wrangler.jsonc.
 *
 * Both routes are `Cache-Control: no-store` upstream, so there was nothing in
 * Cloudflare's edge cache for the binding to skip past. The cache apps/api
 * keeps for the Access API is its own `caches.default`, inside the Worker,
 * and is reached either way.
 *
 * The binding comes from `cloudflare:workers` rather than
 * `Astro.locals.runtime.env`, which the adapter removed in Astro v6 - it now
 * throws with exactly that advice if you reach for it. */

/** Kept as the request URL because apps/api routes on the path, and because
 *  a binding still wants an absolute URL. It is no longer resolved by DNS. */
const API_ORIGIN = "https://api.tone.rip";

export const ACCESS_JWT_HEADER = "Cf-Access-Jwt-Assertion";

interface CallOptions {
  /** Forwarded so apps/api can verify the caller. Absent under `astro dev`. */
  jwt?: string | null;
  timeoutMs: number;
}

/**
 * Runs one request against apps/api.
 *
 * The fallback to the public hostname is for the case where the binding is
 * genuinely absent - `astro dev` outside wrangler, or a fork with no `api`
 * Worker. It is not the local-development path: `wrangler dev` reports the
 * binding as present but `[not connected]` when apps/api is not also running,
 * which is truthy here. Nothing local depends on that, because both callers
 * stop before this: index.astro uses fixtures on localhost, and the /status
 * proxy answers early when there is no Access identity to forward.
 */
export function callApi(
  path: string,
  { jwt, timeoutMs }: CallOptions,
): Promise<Response> {
  const url = `${API_ORIGIN}${path}`;
  const headers: Record<string, string> = { accept: "application/json" };
  if (jwt) headers[ACCESS_JWT_HEADER] = jwt;

  // Read inside the call, not at module scope: `env` is a request-scoped
  // proxy, and touching it while the module is still evaluating is exactly
  // the case workerd refuses.
  const binding: Fetcher | undefined = env.API;

  return withTimeout(timeoutMs, (signal) =>
    binding
      ? binding.fetch(url, { headers, signal })
      : fetch(url, { headers, signal }),
  );
}
