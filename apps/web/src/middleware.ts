import { TONE_INFO } from "@repo/content";
import { createAstroSecurityMiddleware } from "@repo/hono-middleware/astro-security";
import { buildApiCatalogBody } from "@repo/hono-middleware/core";
import type { MiddlewareHandler } from "astro";

// Native Astro middleware: this app is its own Cloudflare Worker (separate
// from apps/api's Hono app), so it can't use the Hono middlewares in
// @repo/hono-middleware directly. The nonce-generation + security-header
// application shared with apps/dashboard lives in createAstroSecurityMiddleware
// (@repo/hono-middleware/astro-security); everything below is specific to
// this app - www-redirect, dev robots.txt, the RFC 9727 catalog, and
// markdown content-negotiation on the homepage.

const DEV_HOSTNAME = "dev.tone.rip";
const DEV_ROBOTS_TXT = "User-agent: *\nDisallow: /\n";
const DEV_ROBOTS_TAG = "noindex, nofollow, noarchive, nosnippet";
const API_ORIGIN = "https://api.tone.rip";
// Centralized in apps/api now - this app no longer serves its own CSP-report
// endpoint.
const CSP_REPORT_URL = `${API_ORIGIN}/csp-report`;
const API_CATALOG_BODY = buildApiCatalogBody([
  { href: `${API_ORIGIN}/projects` },
]);

const LINKS = [
  '</.well-known/api-catalog>; rel="api-catalog"',
  '</llms.txt>; rel="describedby"; type="text/markdown"',
];

const security = createAstroSecurityMiddleware({
  devHostnames: ["localhost", "127.0.0.1"],
  // api.tone.rip only. The project list is fetched *server-side* now (see
  // services/projects.ts), so no page actually needs this at the moment -
  // but the API is the one origin this site is ever expected to talk to, and
  // naming it is cheaper than rediscovering why a fetch is being blocked.
  connectSrc: [API_ORIGIN],
  reportPath: CSP_REPORT_URL,
  links: LINKS,
});

/**
 * Stamp the request nonce onto inline <style>/<script> tags that lack one.
 *
 * A backstop, and deliberately kept as one after the thing it was written for
 * went away. It arrived because `<ClientRouter />` injected an unnonced
 * stylesheet for its transition animations; the site now uses browser-native
 * cross-document transitions, which inject nothing, and every tag the
 * templates emit already carries `Astro.locals.cspNonce` from BaseHead.
 *
 * What it still covers is everything Astro may decide to inline on its own -
 * `build.inlineStylesheets` puts small component stylesheets in the document
 * by default, so adding one `<style>` block to any .astro file three months
 * from now would silently produce a page whose CSS the browser refuses. In
 * production only: `astro dev` serves \'unsafe-inline\', so no local test can
 * catch it. A pass over the response is a small price for a failure mode
 * that is invisible until it is live.
 *
 * (Astro\'s own `security.csp` is the other way to solve this and is now
 * *technically* available, since the documented reason it could not be used -
 * ClientRouter - is gone. It stays unused: it emits a second policy in a
 * <meta> tag, and two enforced policies intersect, so the effective rules
 * would live in two places that have to be reasoned about together. One
 * source of truth is worth more than one fewer HTMLRewriter pass.)
 *
 * HTMLRewriter is a Workers primitive and does not exist under `astro dev`,
 * which is fine for the same reason: dev serves \'unsafe-inline\'.
 */
function nonceInlineTags(response: Response, nonce?: string): Response {
  if (!nonce || typeof HTMLRewriter === "undefined") return response;
  if (!(response.headers.get("Content-Type") ?? "").includes("text/html")) {
    return response;
  }

  // `:not([nonce])` would be the obvious selector, but HTMLRewriter supports
  // only a subset of CSS and `:not()` is not in it - hence the check inside.
  const stamp = {
    element(element: {
      getAttribute(n: string): string | null;
      setAttribute(n: string, v: string): void;
    }) {
      if (element.getAttribute("nonce") === null) {
        element.setAttribute("nonce", nonce);
      }
    },
  };

  return (
    new HTMLRewriter()
      .on("style", stamp)
      // External scripts are already covered by \'self\'; this is for anything
      // inline. Stamping a src\'d script too would be harmless but untrue.
      .on("script", stamp)
      .transform(response)
  );
}

export const onRequest: MiddlewareHandler = async (context, next) => {
  const requestUrl = new URL(context.request.url);

  if (requestUrl.hostname === "www.tone.rip") {
    const target = new URL(requestUrl);
    target.hostname = "tone.rip";
    // Upgrade the scheme as well as the host. Copying the URL preserved
    // whatever the request arrived on, so `http://www.tone.rip/x`
    // redirected to `http://tone.rip/x` - a second plaintext round trip,
    // and one that happens *before* HSTS is ever set on the apex, so a first
    // visit had no protection at all. There is no reason to ever redirect to
    // http from here.
    target.protocol = "https:";
    return Response.redirect(target.toString(), 301);
  }

  // One canonical URL per page: /cv/ and /cv both used to answer 200 and each
  // named itself canonical, which is textbook duplicate content.
  //
  // In production this rarely runs - Workers Assets normalises the trailing
  // slash before the Worker is invoked at all (verified against a local
  // `wrangler dev` build: a request to /nope/ came back 308 without reaching
  // the www branch above it). It stays because `astro dev` has no assets
  // layer, and a dev server that 200s a URL production redirects is how the
  // difference goes unnoticed until it is live.
  //
  // Not `trailingSlash: "never"` in astro.config: that makes Astro refuse
  // /cv/ outright, turning duplicate content into a 404 for anyone following
  // an older link - including the ones the previous sitemap published.
  if (requestUrl.pathname.length > 1 && requestUrl.pathname.endsWith("/")) {
    const target = new URL(requestUrl);
    target.pathname = requestUrl.pathname.replace(/\/+$/, "");
    return Response.redirect(target.toString(), 308);
  }

  const isDevHost = requestUrl.hostname === DEV_HOSTNAME;
  if (isDevHost && requestUrl.pathname === "/robots.txt") {
    return new Response(DEV_ROBOTS_TXT, {
      status: 200,
      headers: {
        "Content-Type": "text/plain; charset=utf-8",
        "Cache-Control": "no-store",
        "X-Robots-Tag": DEV_ROBOTS_TAG,
      },
    });
  }

  // RFC 9727 API catalog (agent discovery for the public API on api.tone.rip).
  if (requestUrl.pathname === "/.well-known/api-catalog") {
    return new Response(API_CATALOG_BODY, {
      status: 200,
      headers: {
        "Content-Type": "application/linkset+json; charset=utf-8",
        "Cache-Control": "public, max-age=3600, s-maxage=3600",
        "X-Content-Type-Options": "nosniff",
      },
    });
  }

  // Markdown content negotiation: agents that ask for text/markdown get a
  // machine-readable homepage; browsers (which don't) still get the app.
  const accept = context.request.headers.get("Accept") || "";
  if (requestUrl.pathname === "/" && accept.includes("text/markdown")) {
    return new Response(TONE_INFO.markdown, {
      status: 200,
      headers: {
        "Content-Type": "text/markdown; charset=utf-8",
        "Cache-Control": "public, max-age=3600, s-maxage=3600",
        "X-Content-Type-Options": "nosniff",
        Vary: "Accept",
      },
    });
  }

  const response = await security(context, next);

  if (isDevHost) {
    response.headers.set("X-Robots-Tag", DEV_ROBOTS_TAG);
  }
  return nonceInlineTags(response, context.locals.cspNonce);
};
