import type { MiddlewareHandler } from "hono";
import { HTTPException } from "hono/http-exception";
import { verifyWithJwks } from "hono/jwt";

export interface CloudflareAccessOptions {
  /** e.g. "riptone.cloudflareaccess.com" */
  teamDomain: string;
  /** The protected Access application's AUD tag (Access → Applications → that app → Overview). */
  aud: string;
  /** @default "Cf-Access-Jwt-Assertion" */
  headerName?: string;
}

/**
 * Verifies a Cloudflare Access JWT against the team's JWKS (hono/jwt's
 * verifyWithJwks - Hono's already a dependency everywhere, no need for a
 * separate JWT library). Reads the JWT from a header rather than Access's
 * own CF_Authorization cookie: this is for routes reached via a
 * server-to-server forward (see apps/dashboard's api/status.ts) from an app
 * that's already behind its own Access policy on a different hostname, not
 * directly behind Access itself. Throws a 401 if the header is missing or
 * the token doesn't verify.
 */
export function requireCloudflareAccess(
  options: CloudflareAccessOptions,
): MiddlewareHandler {
  const headerName = options.headerName ?? "Cf-Access-Jwt-Assertion";
  const issuer = `https://${options.teamDomain}`;
  const jwksUri = `${issuer}/cdn-cgi/access/certs`;

  return async (c, next) => {
    const token = c.req.header(headerName);
    if (!token) {
      throw new HTTPException(401, { message: "Unauthorized" });
    }
    try {
      await verifyWithJwks(token, {
        jwks_uri: jwksUri,
        verification: { iss: issuer, aud: options.aud },
        allowedAlgorithms: ["RS256"],
      });
    } catch (error) {
      // The client still gets a bare 401 - a rejected token learns nothing
      // about why. The *log* says why, which is the difference between
      // "Access is broken" and a five-minute diagnosis.
      //
      // Earned the hard way: renaming the Zero Trust team changed the `iss`
      // claim on every newly issued token, while sessions minted before the
      // rename kept the old issuer and were rejected until their holder
      // logged out. From the outside that is an unexplained 401 on a page
      // Access had visibly just authenticated.
      //
      // The token's own claims are read without verifying - which is safe
      // precisely because it has already been rejected, and is the only way
      // to say what it *claimed* to be.
      let claimed = "unreadable";
      try {
        const [, body] = token.split(".");
        if (body) {
          const payload = JSON.parse(
            atob(body.replace(/-/g, "+").replace(/_/g, "/")),
          ) as {
            iss?: string;
            aud?: string | string[];
          };
          claimed = `iss=${payload.iss ?? "?"} aud=${
            Array.isArray(payload.aud)
              ? payload.aud.join(",")
              : (payload.aud ?? "?")
          }`;
        }
      } catch {
        // Keep "unreadable" - a malformed token is its own answer.
      }
      console.warn(
        `[access] rejected a token. expected iss=${issuer} aud=${options.aud}; token had ${claimed}. ` +
          `reason: ${error instanceof Error ? error.message : "unknown"}`,
      );
      throw new HTTPException(401, { message: "Unauthorized" });
    }
    await next();
  };
}
