/* The slice of Bitwarden's client-side crypto that reading Secrets Manager
 * *names* needs, in WebCrypto, because the official SDK is a Rust crate
 * shipped as a native Node addon or a wasm bundle - neither of which belongs
 * in a Worker whose only job here is to list what exists.
 *
 * Secrets Manager is end-to-end encrypted: the server stores names, values and
 * the organization key as ciphertext, and a machine account's access token
 * carries the 16-byte secret that unwraps that key. Everything below is ported
 * from bitwarden/sdk-internal and pinned to its test vectors (see
 * test/bitwarden-crypto.test.ts):
 *
 * - access_token.rs: `0.<id>.<client secret>:<base64 16-byte key>`
 * - shareable_key.rs: HMAC-SHA256 keyed with "bitwarden-<name>", then
 *   HKDF-Expand to 64 bytes (the AES-256 key followed by the HMAC key)
 * - EncString type 2: `2.<iv>|<ciphertext>|<mac>`, AES-256-CBC with an
 *   HMAC-SHA256 over iv || ciphertext, checked before decrypting
 *
 * Nothing here ever sees a secret *value*: the list endpoint does not return
 * them, and this module only decrypts what it is handed.
 */

const ACCESS_TOKEN_VERSION = "0";
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const ENC_TYPE_AES_CBC_HMAC = "2";
const KEY_BYTES = 64;
const HALF = 32;

export interface AccessToken {
  id: string;
  clientSecret: string;
  /** The 16 bytes after the colon, before derivation. */
  keyMaterial: Uint8Array<ArrayBuffer>;
}

export class BitwardenCryptoError extends Error {}

export function base64ToBytes(b64: string): Uint8Array<ArrayBuffer> {
  const bin = atob(b64);
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

export function bytesToBase64(bytes: Uint8Array): string {
  return btoa(String.fromCharCode(...bytes));
}

const utf8 = new TextEncoder();

export function parseAccessToken(token: string): AccessToken {
  const [first, key] = token.split(":", 2);
  const parts = first?.split(".") ?? [];
  if (key === undefined) throw new BitwardenCryptoError("no decryption key");
  if (parts.length !== 3)
    throw new BitwardenCryptoError("wrong number of parts");
  const [version, id, clientSecret] = parts as [string, string, string];
  if (version !== ACCESS_TOKEN_VERSION)
    throw new BitwardenCryptoError("unsupported access token version");
  if (!UUID.test(id)) throw new BitwardenCryptoError("invalid access token id");
  const keyMaterial = base64ToBytes(key);
  if (keyMaterial.length !== 16)
    throw new BitwardenCryptoError("access token key is not 16 bytes");
  return { id, clientSecret, keyMaterial };
}

async function hmac(
  key: Uint8Array<ArrayBuffer>,
  data: Uint8Array<ArrayBuffer>,
): Promise<Uint8Array<ArrayBuffer>> {
  const k = await crypto.subtle.importKey(
    "raw",
    key,
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  return new Uint8Array(await crypto.subtle.sign("HMAC", k, data));
}

function concat(...parts: Uint8Array[]): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
}

/* HKDF-Expand only (RFC 5869 section 2.3): the SDK feeds it a PRK it built
   itself, so WebCrypto's HKDF - which always runs Extract first - would
   produce a different key. Two blocks of SHA-256 cover the 64 bytes. */
async function hkdfExpand(
  prk: Uint8Array<ArrayBuffer>,
  info: string,
  length: number,
): Promise<Uint8Array<ArrayBuffer>> {
  const infoBytes = new Uint8Array(utf8.encode(info));
  const blocks: Uint8Array[] = [];
  let previous = new Uint8Array(0);
  for (let i = 1; blocks.length * HALF < length; i++) {
    previous = await hmac(prk, concat(previous, infoBytes, Uint8Array.of(i)));
    blocks.push(previous);
  }
  return concat(...blocks).slice(0, length);
}

/** derive_shareable_key from the SDK: 64 bytes, encryption key then MAC key. */
export async function deriveShareableKey(
  secret: Uint8Array,
  name: string,
  info?: string,
): Promise<Uint8Array<ArrayBuffer>> {
  // Copies, so the bytes are backed by a plain ArrayBuffer as WebCrypto's types want.
  const prk = await hmac(
    new Uint8Array(utf8.encode(`bitwarden-${name}`)),
    new Uint8Array(secret),
  );
  return hkdfExpand(prk, info ?? "", KEY_BYTES);
}

/** The key that unwraps the organization key, from a machine account token. */
export function accessTokenKey(
  t: AccessToken,
): Promise<Uint8Array<ArrayBuffer>> {
  return deriveShareableKey(t.keyMaterial, "accesstoken", "sm-access-token");
}

/** Decrypts an EncString of type 2 with a 64-byte key; rejects a bad MAC. */
export async function decryptEncString(
  encString: string,
  key: Uint8Array<ArrayBuffer>,
): Promise<Uint8Array<ArrayBuffer>> {
  if (key.length !== KEY_BYTES)
    throw new BitwardenCryptoError("key is not 64 bytes");
  const dot = encString.indexOf(".");
  if (encString.slice(0, dot) !== ENC_TYPE_AES_CBC_HMAC)
    throw new BitwardenCryptoError("unsupported EncString type");
  const pieces = encString.slice(dot + 1).split("|");
  if (pieces.length !== 3)
    throw new BitwardenCryptoError("malformed EncString");
  const [iv, data, mac] = pieces.map(base64ToBytes) as [
    Uint8Array<ArrayBuffer>,
    Uint8Array<ArrayBuffer>,
    Uint8Array<ArrayBuffer>,
  ];

  // crypto.subtle.verify compares in constant time, which a byte loop would not.
  const macKey = await crypto.subtle.importKey(
    "raw",
    key.slice(HALF),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["verify"],
  );
  if (!(await crypto.subtle.verify("HMAC", macKey, mac, concat(iv, data))))
    throw new BitwardenCryptoError("EncString MAC mismatch");

  const encKey = await crypto.subtle.importKey(
    "raw",
    key.slice(0, HALF),
    { name: "AES-CBC" },
    false,
    ["decrypt"],
  );
  return new Uint8Array(
    await crypto.subtle.decrypt({ name: "AES-CBC", iv }, encKey, data),
  );
}

export async function decryptToString(
  encString: string,
  key: Uint8Array<ArrayBuffer>,
): Promise<string> {
  return new TextDecoder().decode(await decryptEncString(encString, key));
}
