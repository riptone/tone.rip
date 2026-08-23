import type { MiddlewareHandler } from "hono";

export interface WwwRedirectOptions {
  /** The canonical apex host, e.g. "tone.rip". */
  apexHost: string;
  /** Defaults to `www.${apexHost}`. */
  wwwHost?: string;
}

/** Redirects the www subdomain to the apex host with a 301. */
export function wwwRedirect(options: WwwRedirectOptions): MiddlewareHandler {
  const wwwHost = options.wwwHost ?? `www.${options.apexHost}`;

  return async (c, next) => {
    const url = new URL(c.req.url);
    if (url.hostname === wwwHost) {
      url.hostname = options.apexHost;
      return c.redirect(url.toString(), 301);
    }
    await next();
  };
}
