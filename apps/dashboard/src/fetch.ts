/* The dashboard's request pipeline, as a Hono app.
 *
 * Access control is delegated to Cloudflare Zero Trust in front of this
 * Worker (an Access policy on dash.tone.rip), not app code. So this pipeline
 * only adds the baseline nonce'd CSP + security-header set every app in this
 * monorepo shares, which is also a hard requirement for BaseHead.astro - it
 * reads Astro.locals.cspNonce to nonce its inline theme script.
 *
 * It is a Hono app rather than an Astro `src/middleware.ts` because Astro 7's
 * `astro/hono` lets a Worker-per-app project run the real Hono middleware
 * from @repo/hono-middleware. Before that this app called
 * `createAstroSecurityMiddleware`, a second implementation of
 * `securityHeaders()` written to Astro's middleware signature; there is now
 * one implementation, shared with apps/web and apps/api. See
 * apps/web/src/fetch.ts for the fuller note.
 */

import { type CspNonceEnv, securityHeaders } from "@repo/hono-middleware";
import { astro, getFetchState } from "astro/hono";
import { type Context, Hono, type MiddlewareHandler } from "hono";

export interface DashboardAppOptions {
  /** What answers once the middleware chain has had its say. */
  render?: MiddlewareHandler<CspNonceEnv>;
  /**
   * How the nonce reaches `Astro.locals`. Injectable because the default
   * needs a built Worker's ambient manifest; see apps/web/src/fetch.ts.
   */
  bindNonce?: (c: Context, nonce: string) => void;
}

export function createDashboardApp(
  options: DashboardAppOptions = {},
): Hono<CspNonceEnv> {
  const render = options.render ?? (astro() as MiddlewareHandler<CspNonceEnv>);
  const bindNonce =
    options.bindNonce ??
    ((c, nonce) => {
      getFetchState(c).locals.cspNonce = nonce;
    });

  const app = new Hono<CspNonceEnv>();

  app.use(
    securityHeaders({
      devHostnames: ["localhost", "127.0.0.1"],
      // No connectSrc, so connect-src is the shared default of 'self' alone.
      //
      // This used to be `https://*.tone.rip`: the board probed every tile's
      // own origin from the visitor's browser to colour it up/down/vpn, and
      // asked api.tone.rip for the server's view, so every one of those
      // origins had to be fetchable from the page. The status column is gone
      // and nothing on this page fetches anything any more - the tile list is
      // rendered on the server, through the API service binding, and the
      // filter and the field run on what is already in the document. Leaving
      // the wildcard would keep permission for requests nothing makes, which
      // is exactly the slack a policy is there to remove.
      //
      // Tile icons are whatever URL each Access application carries in its
      // logo_url, so their host is not knowable here. They are already
      // constrained to https at the API boundary (@repo/validation's
      // toSelfHostedApps drops anything else, because a non-https image on an
      // https page is blocked anyway). An icon is decoration; the strict half
      // of this policy is script-src and connect-src.
      imgSrc: ["https:"],
    }),
  );

  app.use(async (c, next) => {
    bindNonce(c, c.get("cspNonce"));
    await next();
  });

  app.all("*", render);

  return app;
}

export default createDashboardApp();
