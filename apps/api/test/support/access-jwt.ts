import { sign } from "hono/jwt";

/* A real RS256 Access JWT and the JWKS that verifies it, for the routes gated
   by requireCloudflareAccess(). Shared by every gated route's tests, so the
   team domain and audience are stated once - they must match src/access.ts. */

const ACCESS_TEAM_DOMAIN = "riptone.cloudflareaccess.com";
export const ACCESS_ISSUER = `https://${ACCESS_TEAM_DOMAIN}`;
export const ACCESS_AUD =
  "28a3efd8f96a2e859f3bcd8158570e67538c297b25a8d7de9803b877e8a1881a";
export const ACCESS_JWKS_URL = `${ACCESS_ISSUER}/cdn-cgi/access/certs`;
export const ACCESS_HEADER = "Cf-Access-Jwt-Assertion";
const KID = "test-kid";

export async function generateSignedAccessToken(
  payload: Record<string, unknown>,
) {
  const keyPair = (await crypto.subtle.generateKey(
    {
      name: "RSASSA-PKCS1-v1_5",
      hash: "SHA-256",
      modulusLength: 2048,
      publicExponent: new Uint8Array([1, 0, 1]),
    },
    true,
    ["sign", "verify"],
  )) as CryptoKeyPair;
  const privateJwk = (await crypto.subtle.exportKey(
    "jwk",
    keyPair.privateKey,
  )) as JsonWebKey;
  const publicJwk = (await crypto.subtle.exportKey(
    "jwk",
    keyPair.publicKey,
  )) as JsonWebKey;
  const token = await sign(
    payload,
    { ...privateJwk, alg: "RS256", kid: KID },
    "RS256",
  );
  const jwks = { keys: [{ ...publicJwk, alg: "RS256", kid: KID, use: "sig" }] };
  return { token, jwks };
}
