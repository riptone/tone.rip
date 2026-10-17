import { fetchWithTimeout } from "@repo/net";
import {
  bitwardenSecretsListSchema,
  bitwardenTokenResponseSchema,
} from "@repo/validation";
import {
  accessTokenKey,
  base64ToBytes,
  decryptToString,
  parseAccessToken,
} from "./bitwarden-crypto";

/* Which secrets exist in Bitwarden Secrets Manager, for the dashboard - by
 * name, project and last change, and never by value.
 *
 * The point is knowing what is backed up (is STRIPE_WEBHOOK_SECRET there, and
 * when did it last change?) without turning the dashboard into the one place
 * that, compromised, hands over every credential at once. So the values are
 * not merely hidden in the UI: this module never requests them. The list
 * endpoint returns ciphertext names and dates only; the value of a secret
 * comes from GET /secrets/{id}, which nothing here calls.
 *
 * The machine account behind BWS_ACCESS_TOKEN should be read-only and scoped
 * to the projects worth listing - that is the real boundary; this code just
 * stays inside it.
 */

const UPSTREAM_TIMEOUT_MS = 6000;
/* A minute, in this isolate only: the names change when somebody edits the
   vault, and the dashboard's own refresh is the only caller. No edge cache -
   a list of credential names is not something to leave in a shared cache. */
const MEMORY_TTL_MS = 60_000;
/* Renew a little before the server says the session ends, so a request that
   starts just under the wire does not carry an expired bearer. */
const TOKEN_MARGIN_MS = 60_000;

const REGIONS = {
  eu: {
    identity: "https://identity.bitwarden.eu",
    api: "https://api.bitwarden.eu",
  },
  us: {
    identity: "https://identity.bitwarden.com",
    api: "https://api.bitwarden.com",
  },
} as const;
type BitwardenRegion = keyof typeof REGIONS;

interface SecretName {
  name: string;
  projects: string[];
  revisedAt: string;
}

export type SecretNamesResult =
  | { state: "ok"; secrets: SecretName[] }
  | { state: "unconfigured" | "unavailable"; secrets: [] };

interface Session {
  bearer: string;
  organizationId: string;
  organizationKey: Uint8Array<ArrayBuffer>;
  expiresAt: number;
}

let session: { token: string; value: Session } | null = null;
let memory: { at: number; secrets: SecretName[] } | null = null;

export function __resetBitwardenForTests(): void {
  session = null;
  memory = null;
}

/** The `organization` claim of the bearer: the token is the SDK's source for it too. */
function organizationOf(bearer: string): string {
  const payload = bearer.split(".")[1] ?? "";
  const json = atob(payload.replaceAll("-", "+").replaceAll("_", "/"));
  const claims = JSON.parse(json) as { organization?: unknown };
  if (typeof claims.organization !== "string")
    throw new Error("bearer has no organization claim");
  return claims.organization;
}

async function login(
  token: string,
  region: BitwardenRegion,
  now: number,
): Promise<Session> {
  if (session?.token === token && session.value.expiresAt > now)
    return session.value;
  const parsed = parseAccessToken(token);
  const res = await fetchWithTimeout(
    `${REGIONS[region].identity}/connect/token`,
    UPSTREAM_TIMEOUT_MS,
    {
      method: "POST",
      headers: { "content-type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        scope: "api.secrets",
        client_id: parsed.id,
        client_secret: parsed.clientSecret,
        grant_type: "client_credentials",
      }),
    },
  );
  if (!res.ok) throw new Error(`identity responded ${res.status}`);
  const body = bitwardenTokenResponseSchema.parse(await res.json());
  const payload = JSON.parse(
    await decryptToString(body.encrypted_payload, await accessTokenKey(parsed)),
  ) as { encryptionKey?: unknown };
  if (typeof payload.encryptionKey !== "string")
    throw new Error("payload has no encryptionKey");
  const value: Session = {
    bearer: body.access_token,
    organizationId: organizationOf(body.access_token),
    organizationKey: base64ToBytes(payload.encryptionKey),
    expiresAt: now + body.expires_in * 1000 - TOKEN_MARGIN_MS,
  };
  session = { token, value };
  return value;
}

async function list(
  s: Session,
  region: BitwardenRegion,
): Promise<SecretName[]> {
  const res = await fetchWithTimeout(
    `${REGIONS[region].api}/organizations/${s.organizationId}/secrets`,
    UPSTREAM_TIMEOUT_MS,
    { headers: { authorization: `Bearer ${s.bearer}` } },
  );
  if (!res.ok) throw new Error(`api responded ${res.status}`);
  const body = bitwardenSecretsListSchema.parse(await res.json());

  const projectNames = new Map<string, string>();
  for (const p of body.projects ?? [])
    if (p.name)
      projectNames.set(p.id, await decryptToString(p.name, s.organizationKey));

  const secrets = await Promise.all(
    (body.secrets ?? []).map(async (secret) => ({
      name: await decryptToString(secret.key, s.organizationKey),
      projects: (secret.projects ?? []).map(
        (p) => projectNames.get(p.id) ?? p.id,
      ),
      revisedAt: secret.revisionDate,
    })),
  );
  return secrets.sort((a, b) => a.name.localeCompare(b.name));
}

export async function fetchSecretNames(opts: {
  accessToken: string | undefined;
  region: string | undefined;
  now?: number;
}): Promise<SecretNamesResult> {
  const region: BitwardenRegion = opts.region === "us" ? "us" : "eu";
  if (!opts.accessToken) return { state: "unconfigured", secrets: [] };
  const now = opts.now ?? Date.now();
  if (memory && now - memory.at < MEMORY_TTL_MS)
    return { state: "ok", secrets: memory.secrets };
  try {
    const secrets = await list(
      await login(opts.accessToken, region, now),
      region,
    );
    memory = { at: now, secrets };
    return { state: "ok", secrets };
  } catch (e) {
    // The message, never the token: what failed is enough to act on.
    console.warn(
      "[secrets] Bitwarden unavailable:",
      e instanceof Error ? e.message : String(e),
    );
    session = null;
    return { state: "unavailable", secrets: [] };
  }
}
