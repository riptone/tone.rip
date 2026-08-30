import type { Env, MiddlewareHandler } from "hono";
import { type BuildSecurityHeadersOptions, buildSecurityHeaders } from "./core";
import { generateNonce } from "./nonce";

export type CspNonceEnv = Env & { Variables: { cspNonce: string } };

export type SecurityHeadersOptions = Omit<
  BuildSecurityHeadersOptions,
  "url" | "nonce"
>;

/**
 * Sets a CSP nonce on the context (readable via `c.get("cspNonce")`) and,
 * after the handler runs, applies the full baseline security-header set + a
 * nonce'd CSP built by ./core's buildSecurityHeaders.
 *
 * This is the only implementation. The Astro apps used to carry a second one
 * written against Astro's middleware signature, because a Worker-per-app
 * project could not run Hono middleware; Astro 7's `astro/hono` ended that,
 * and apps/web, apps/dashboard and apps/api all run this now. The Astro apps
 * additionally copy the nonce onto `Astro.locals.cspNonce`, which is where
 * BaseHead.astro reads it - see apps/web/src/fetch.ts.
 */
export function securityHeaders(
  options: SecurityHeadersOptions = {},
): MiddlewareHandler<CspNonceEnv> {
  return async (c, next) => {
    const nonce = generateNonce();
    c.set("cspNonce", nonce);

    await next();

    const url = new URL(c.req.url);
    const { headers, csp } = buildSecurityHeaders({ ...options, url, nonce });

    c.res.headers.delete("Content-Security-Policy-Report-Only");

    const contentType = c.res.headers.get("Content-Type") || "";
    if (contentType.startsWith("text/html")) {
      c.res.headers.set("Content-Type", "text/html; charset=utf-8");
      c.res.headers.set("Cache-Control", "private, max-age=0, must-revalidate");
    }

    for (const [name, value] of Object.entries(headers)) {
      c.res.headers.set(name, value);
    }
    c.res.headers.set("Content-Security-Policy", csp);
  };
}
