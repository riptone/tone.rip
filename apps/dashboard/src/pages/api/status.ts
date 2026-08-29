import type { APIRoute } from "astro";
import { ACCESS_JWT_HEADER, callApi } from "../../lib/api";

// Same-origin proxy for apps/api's /status: this dashboard is gated behind
// a Cloudflare Access policy, so every request reaching this Worker already
// carries a Cf-Access-Jwt-Assertion header that Access itself attached and
// verified. Forward it to apps/api server-to-server so that route can verify
// it too - a client-side browser fetch straight to api.tone.rip wouldn't
// carry it (different hostname, and a plain fetch() can't complete Access's
// interactive login redirect the way a full page navigation can).
//
// The hop itself now goes through the `API` service binding rather than out
// to api.tone.rip - see src/lib/api.ts. The JWT is still forwarded and still
// verified upstream; only the transport changed.

// The middle of a three-link chain, and the numbers only make sense as a
// set: the browser waits 5000ms (dashboard.ts), this waits 4000ms, and
// apps/api's route is bounded at roughly 3000ms by its own probe and
// Tailscale timeouts (app-health.ts). Each link gives up before the one
// above it, so a slow upstream produces a diagnosable answer here rather
// than an abort up there.
//
// It used to be 5000ms against an *unbounded* upstream while the browser
// waited only 2500ms - every link had a deadline shorter than the thing it
// was waiting for, which is how `/api/status` ended up intermittently
// answering with nothing at all.
const UPSTREAM_TIMEOUT_MS = 4000;

const NO_IDENTITY_BODY = JSON.stringify({ apps: [], tailnet: {} });

export const GET: APIRoute = async ({ request }) => {
  const jwt = request.headers.get(ACCESS_JWT_HEADER);

  // No Access identity to forward, which in practice means local `astro dev`
  // - Access is in front of the deployed Worker, not in front of this one.
  // Upstream would answer 401 every time, so the round trip is wasted and
  // the browser logs a failed request on every status sweep. Answer with the
  // truthful empty view instead: this proxy has no identity, therefore no
  // server-side data. The client already treats "no data" and "401" the
  // same way, and every tile falls back to its own browser probe.
  if (!jwt) {
    return new Response(NO_IDENTITY_BODY, {
      status: 200,
      headers: {
        "Content-Type": "application/json; charset=utf-8",
        "Cache-Control": "no-store",
        "X-Status-Source": "no-access-identity",
      },
    });
  }

  let upstream: Response;
  try {
    upstream = await callApi("/status", {
      jwt,
      timeoutMs: UPSTREAM_TIMEOUT_MS,
    });
  } catch {
    // A timeout or a network failure is not the dashboard being broken; it
    // is the same "no server-side view" the client already handles.
    return new Response(NO_IDENTITY_BODY, {
      status: 200,
      headers: {
        "Content-Type": "application/json; charset=utf-8",
        "Cache-Control": "no-store",
        "X-Status-Source": "upstream-unreachable",
      },
    });
  }

  return new Response(upstream.body, {
    status: upstream.status,
    headers: {
      "Content-Type": "application/json; charset=utf-8",
      "Cache-Control": "no-store",
      "X-Status-Source": "upstream",
    },
  });
};
