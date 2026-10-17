/* The Cloudflare Access application this Worker trusts, for
 * requireCloudflareAccess() on the routes the dashboard calls.
 *
 * Its own module because it is configuration for the gate, not part of any
 * one route: it lived in routes/status.ts, and /apps imported it from there,
 * so deleting /status would have taken the half of /apps' signature check
 * that names the issuer with it. */

// The Cloudflare Zero Trust team domain, which is also the `iss` claim on
// every Access JWT - so this constant is not cosmetic: it is half of the
// signature check, and a stale value rejects every valid token.
//
// It moved once already. Renaming the team in the Zero Trust dashboard
// changes the issuer immediately, with no deploy and no DNS involved, and
// nothing in this repository finds out. The symptom is a gated route
// answering 401 to a browser that Access itself has just authenticated -
// on the dashboard, an empty board - which reads as a bug in the route
// rather than as a value that went out of date.
//
// The live answer is one request away and needs no credentials, because
// Cloudflare redirects an unauthenticated visitor straight to it:
//
//   curl -sI https://dash.tone.rip | grep -i ^location
export const ACCESS_TEAM_DOMAIN = "riptone.cloudflareaccess.com";

// The Access application's AUD tag, which survived that rename untouched -
// it identifies the application, not the team. Verified against the `kid`
// parameter on the same redirect.
export const ACCESS_AUD =
  "28a3efd8f96a2e859f3bcd8158570e67538c297b25a8d7de9803b877e8a1881a";
