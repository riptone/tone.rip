/* What this Worker reads from its environment.
 *
 * Deliberately not `extends CloudflareBindings` (wrangler's generated
 * interface), for two reasons that turned out to be the same reason.
 *
 * Nothing here reads `env.ASSETS`, which was the only real binding that
 * interface contributed - the assets are served by the runtime, not by this
 * code - so the inheritance bought nothing.
 *
 * And what it cost was reproducibility. `wrangler types` infers bindings
 * from `.dev.vars` as well as from wrangler.jsonc, and `.dev.vars` is
 * gitignored: on a machine that has one, the six secrets below are generated
 * as required `string`s; on CI, or a fresh clone, they are absent entirely.
 * Extending that made this file typecheck differently depending on whether
 * the person running it happened to have local credentials - and it failed
 * outright once the generated file caught up, because these are optional
 * here on purpose. They are Worker secrets: the code branches on their
 * absence (see /apps answering "unconfigured"), and a type saying they are
 * always present would delete those branches from the type system while
 * leaving them in the code.
 *
 * So this interface states what the code expects, and the generated file is
 * left to describe the runtime. */
export interface Bindings {
  GITHUB_TOKEN?: string;
  TAILSCALE_OAUTH_CLIENT_ID?: string;
  TAILSCALE_OAUTH_CLIENT_SECRET?: string;
  TAILSCALE_OAUTH_SCOPE?: string;
  TAILSCALE_TAILNET?: string;
  TAILSCALE_STATUS_DEVICE?: string;
  /** Bearer token apps/ssh-cv presents to POST /ssh/authorize. */
  SSH_GATEWAY_TOKEN?: string;
  /** Newline-separated allowlist; see services/ssh-allowlist.ts for the format. */
  SSH_AUTHORIZED_KEYS?: string;
  /**
   * Cloudflare API token with Access: Apps Read, and the account it belongs
   * to. Both optional: without them /apps answers "unconfigured" and the
   * dashboard renders no tiles, which is the correct behaviour for a fork or
   * a local `wrangler dev` rather than an error.
   */
  CF_ACCESS_TOKEN?: string;
  CF_ACCOUNT_ID?: string;
  /**
   * Hosts needing a probe path other than "/", as `host=path` pairs separated
   * by commas. A secret, not a constant: this repository is public and the
   * hostnames are the sensitive part. See services/access-apps.ts.
   */
  PROBE_PATHS?: string;
}

export interface AppEnv {
  Bindings: Bindings;
  Variables: {
    cspNonce: string;
  };
}
