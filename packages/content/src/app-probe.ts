/* What is left of the self-hosted app registry.
 *
 * The registry itself is gone: the list of applications, their names, tags and
 * icons now comes from Cloudflare Access (see apps/api's /apps route), because
 * the account already had to know all of it in order to render the Access App
 * Launcher, and keeping a second copy here meant adding every new service
 * twice and watching the two drift.
 *
 * This one function could not go with it. It encodes a fact about a specific
 * service that no API reports, and it is needed by both probes - the
 * server-side one in apps/api and the client-side one in apps/dashboard -
 * which is exactly why it lives in a shared package rather than being
 * hardcoded twice.
 */

/**
 * The path to probe for reachability on a given host.
 *
 * Vaultwarden (pass.tone.rip) serves an empty 200 at "/" behind auth walls in
 * some configurations, so a probe of the root cannot tell "up" from "up but
 * useless". Its favicon is a real file and answers honestly.
 *
 * Everything else is probed at "/". A hostname check rather than a
 * configuration field because it is one exception, and a per-app "probePath"
 * option would be a knob that exists for a single value.
 */
export function resolveProbePath(url: URL): string {
  return url.hostname === "pass.tone.rip" ? "/favicon.ico" : "/";
}
