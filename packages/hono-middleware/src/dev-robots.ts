import type { MiddlewareHandler } from "hono";

export interface DevRobotsOptions {
  /** Hostnames that should be excluded from indexing, e.g. "dev.tone.rip". */
  devHostnames: string[];
  body?: string;
  xRobotsTag?: string;
}

/**
 * Serves a blanket-disallow robots.txt on dev/staging hostnames and tags every
 * other response on those hosts with X-Robots-Tag, so preview deploys never
 * get indexed.
 */
export function devRobots(options: DevRobotsOptions): MiddlewareHandler {
  const devHostnames = new Set(options.devHostnames);
  const body = options.body ?? "User-agent: *\nDisallow: /\n";
  const tag = options.xRobotsTag ?? "noindex, nofollow, noarchive, nosnippet";

  return async (c, next) => {
    const url = new URL(c.req.url);
    const isDevHost = devHostnames.has(url.hostname);

    if (isDevHost && url.pathname === "/robots.txt") {
      return c.body(body, 200, {
        "Content-Type": "text/plain; charset=utf-8",
        "Cache-Control": "no-store",
        "X-Robots-Tag": tag,
      });
    }

    await next();

    if (isDevHost) {
      c.res.headers.set("X-Robots-Tag", tag);
    }
  };
}
