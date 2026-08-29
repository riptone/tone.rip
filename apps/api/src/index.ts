import {
  apiCatalog,
  problemDetails,
  problemJson,
  problemResponse,
  securityHeaders,
} from "@repo/hono-middleware";
import { Hono } from "hono";
import { cors } from "hono/cors";
import type { AppEnv } from "./env";
import { appsRoute } from "./routes/apps";
import { cspReportRoute } from "./routes/csp-report";
import { infoRoute } from "./routes/info";
import { projectsRoute } from "./routes/projects";
import { sshRoute } from "./routes/ssh";
import { statusRoute } from "./routes/status";

const API_ORIGIN = "https://api.tone.rip";

// /projects is fetched client-side from tone.rip's browser scripts, so it
// needs real CORS. /status and /csp-report aren't browser cross-origin
// fetches - status is proxied server-to-server by apps/dashboard (see its
// api/status.ts), and csp-report is posted by the browser's own CSP
// reporting mechanism, not application JS subject to CORS.
const ALLOWED_ORIGINS = new Set(["https://tone.rip", "https://www.tone.rip"]);
const LOCAL_DEV_ORIGIN = /^https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/;
/** Hostnames this Worker is allowed to *be* when it honours a localhost origin. */
const DEV_HOSTNAMES = new Set(["localhost", "127.0.0.1"]);

const app = new Hono<AppEnv>();

app.onError(problemJson());

// onError only fires for *thrown* errors - an unmatched route goes to Hono's
// default handler, which returns plain text. Without this, a 404 was the one
// response that broke the API's own RFC 7807 contract.
app.notFound((c) =>
  problemResponse(
    c,
    problemDetails(404, "Not Found", {
      instance: new URL(c.req.url).pathname,
    }),
  ),
);

app.use(
  "*",
  cors({
    origin: (origin, c) => {
      if (ALLOWED_ORIGINS.has(origin)) return origin;
      // The localhost allowance is for running apps/web against this API
      // locally, and it is gated on *this Worker* also being local. It used
      // to be unconditional, which meant production api.tone.rip replied
      // `Access-Control-Allow-Origin: http://localhost:5173` to anything
      // served from a visitor's own machine - a dev server, an Electron app,
      // anything squatting a local port. Nothing here is credentialed, so the
      // reach was small, but a production API has no reason to name localhost
      // as trusted at all.
      const isLocal = DEV_HOSTNAMES.has(new URL(c.req.url).hostname);
      if (isLocal && LOCAL_DEV_ORIGIN.test(origin)) return origin;
      return null;
    },
    // Preflight otherwise advertises PUT/DELETE/PATCH, none of which exist
    // here - POST is only for /csp-report.
    allowMethods: ["GET", "HEAD", "POST", "OPTIONS"],
  }),
);

app.use(
  "*",
  securityHeaders({
    devHostnames: ["localhost", "127.0.0.1"],
    connectSrc: ["https://api.github.com", "https://api.tailscale.com"],
    // This API is deliberately consumed cross-origin by every frontend in
    // the monorepo (unlike apps/web/apps/dashboard, which default to
    // same-origin) - otherwise browsers block the response even with CORS
    // headers present, independent of the CORS check above.
    crossOriginResourcePolicy: "cross-origin",
  }),
);

const ENDPOINTS = [
  {
    href: `${API_ORIGIN}/apps`,
    description: "Self-hosted applications, from Cloudflare Access.",
  },
  { href: `${API_ORIGIN}/projects`, description: "Public GitHub repo list." },
  {
    href: `${API_ORIGIN}/projects/{repo}/readme`,
    description: "Rendered README for one repo.",
  },
  {
    href: `${API_ORIGIN}/status`,
    description: "Self-hosted app health. Requires a Cloudflare Access JWT.",
  },
  { href: `${API_ORIGIN}/csp-report`, description: "CSP violation sink." },
  {
    href: `${API_ORIGIN}/info/{slug}`,
    description: "Site info as HTML, or markdown via Accept: text/markdown.",
  },
];

// Deliberately absent from ENDPOINTS: /ssh/authorize is a control-plane
// endpoint for exactly one client (apps/ssh-cv), not a public API. Listing it
// in the RFC 9727 catalog and on GET / would advertise its existence to
// everyone, which is free reconnaissance for no benefit - the one caller is
// configured with the URL.

app.use("*", apiCatalog({ entries: ENDPOINTS.map(({ href }) => ({ href })) }));

// A bare GET / used to 404. Cheaper to point a human (or agent) at what's
// here than to make them guess or find the RFC 9727 catalog first.
app.get("/", (c) =>
  c.json(
    {
      name: "tonil api",
      catalog: "/.well-known/api-catalog",
      endpoints: ENDPOINTS,
    },
    200,
    { "Cache-Control": "public, max-age=3600" },
  ),
);

app.route("/apps", appsRoute);
app.route("/projects", projectsRoute);
app.route("/status", statusRoute);
app.route("/csp-report", cspReportRoute);
app.route("/info", infoRoute);
app.route("/ssh", sshRoute);

export default app;
