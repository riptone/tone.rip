import type { MiddlewareHandler } from "hono";

export interface WwwRedirectOptions {
  /** The canonical apex host, e.g. "tone.rip". */
  apexHost: string;
  /** Defaults to `www.${apexHost}`. */
  wwwHost?: string;
}

/**
 * Redirects the www subdomain to the apex host with a 301.
 *
 * The scheme is upgraded to https rather than mirrored. Copying the request
 * URL and changing only the host sends `http://www.tone.rip/x` to
 * `http://tone.rip/x` - a second plaintext round trip, taken *before* HSTS is
 * ever set on the apex, so a first visit has no protection at all. There is
 * no case where this should redirect to http.
 */
export function wwwRedirect(options: WwwRedirectOptions): MiddlewareHandler {
  const wwwHost = options.wwwHost ?? `www.${options.apexHost}`;

  return async (c, next) => {
    const url = new URL(c.req.url);
    if (url.hostname === wwwHost) {
      url.hostname = options.apexHost;
      url.protocol = "https:";
      return c.redirect(url.toString(), 301);
    }
    await next();
  };
}
