export {
  type ApiCatalogEntry,
  type ApiCatalogOptions,
  apiCatalog,
} from "./api-catalog";
export {
  type CloudflareAccessOptions,
  requireCloudflareAccess,
} from "./cloudflare-access";
// Everything here is framework-agnostic Hono middleware, imported by all
// three apps. There used to be an astro-security.ts kept out of this barrel
// because it was Astro-specific and apps/api has no Astro dependency; Astro 7
// made it unnecessary - `astro/hono` lets the Astro apps run these same
// middlewares, so `securityHeaders` below has one implementation instead of
// two. See apps/web/src/fetch.ts.
export {
  type ApiCatalogEntryInput,
  type BuildSecurityHeadersOptions,
  type BuiltSecurityHeaders,
  buildApiCatalogBody,
  buildSecurityHeaders,
  DEFAULT_PERMISSIONS_POLICY,
} from "./core";
export { type DevRobotsOptions, devRobots } from "./dev-robots";
export {
  type MarkdownNegotiationOptions,
  markdownNegotiation,
} from "./markdown-negotiation";
export { generateNonce } from "./nonce";
export {
  type ProblemDetails,
  problemDetails,
  problemJson,
  problemResponse,
} from "./problem-json";
export {
  type CspNonceEnv,
  type SecurityHeadersOptions,
  securityHeaders,
} from "./security-headers";
export { type WwwRedirectOptions, wwwRedirect } from "./www-redirect";
