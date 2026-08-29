export interface Bindings extends CloudflareBindings {
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
