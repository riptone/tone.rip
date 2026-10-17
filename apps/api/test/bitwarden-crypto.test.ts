import { describe, expect, it } from "vitest";
import {
  BitwardenCryptoError,
  bytesToBase64,
  decryptEncString,
  decryptToString,
  deriveShareableKey,
  parseAccessToken,
} from "../src/services/bitwarden-crypto";

const utf8 = new TextEncoder();

/* Encrypts the way Bitwarden does, so the decrypt path is tested against a
   ciphertext it did not produce itself. */
async function encString(
  plaintext: string,
  key: Uint8Array<ArrayBuffer>,
): Promise<string> {
  const iv = crypto.getRandomValues(new Uint8Array(16));
  const enc = await crypto.subtle.importKey(
    "raw",
    key.slice(0, 32),
    { name: "AES-CBC" },
    false,
    ["encrypt"],
  );
  const data = new Uint8Array(
    await crypto.subtle.encrypt(
      { name: "AES-CBC", iv },
      enc,
      utf8.encode(plaintext),
    ),
  );
  const macKey = await crypto.subtle.importKey(
    "raw",
    key.slice(32),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const mac = new Uint8Array(
    await crypto.subtle.sign("HMAC", macKey, new Uint8Array([...iv, ...data])),
  );
  return `2.${bytesToBase64(iv)}|${bytesToBase64(data)}|${bytesToBase64(mac)}`;
}

describe("deriveShareableKey", () => {
  // The vectors from bitwarden/sdk-internal crates/bitwarden-crypto/src/keys/shareable_key.rs.
  it("matches the SDK without info", async () => {
    const key = await deriveShareableKey(
      utf8.encode("&/$%F1a895g67HlX"),
      "test_key",
    );
    expect(bytesToBase64(key)).toBe(
      "4PV6+PcmF2w7YHRatvyMcVQtI7zvCyssv/wFWmzjiH6Iv9altjmDkuBD1aagLVaLezbthbSe+ktR+U6qswxNnQ==",
    );
  });

  it("matches the SDK with info", async () => {
    const key = await deriveShareableKey(
      utf8.encode("67t9b5g67$%Dh89n"),
      "test_key",
      "test",
    );
    expect(bytesToBase64(key)).toBe(
      "F9jVQmrACGx9VUPjuzfMYDjr726JtL300Y3Yg+VYUnVQtQ1s8oImJ5xtp1KALC9h2nav04++1LDW4iFD+infng==",
    );
  });
});

describe("parseAccessToken", () => {
  const id = "ec2c1d46-6a4b-4751-a310-af9601317f2d";
  const key = bytesToBase64(new Uint8Array(16).fill(7));

  it("splits version, id, client secret and the 16-byte key", () => {
    const t = parseAccessToken(`0.${id}.C2IgxjjLF7qSshsbwe8JGcbM075YXw:${key}`);
    expect(t.id).toBe(id);
    expect(t.clientSecret).toBe("C2IgxjjLF7qSshsbwe8JGcbM075YXw");
    expect(t.keyMaterial).toHaveLength(16);
  });

  it.each([
    ["no key", `0.${id}.secret`],
    ["wrong parts", `0.${id}:${key}`],
    ["wrong version", `1.${id}.secret:${key}`],
    ["bad id", `0.not-a-uuid.secret:${key}`],
    ["short key", `0.${id}.secret:${bytesToBase64(new Uint8Array(8))}`],
  ])("rejects %s", (_, token) => {
    expect(() => parseAccessToken(token)).toThrow(BitwardenCryptoError);
  });
});

describe("decryptEncString", () => {
  const key = crypto.getRandomValues(new Uint8Array(64));

  it("round-trips a type 2 EncString", async () => {
    expect(
      await decryptToString(await encString("GITHUB_TOKEN", key), key),
    ).toBe("GITHUB_TOKEN");
  });

  it("refuses a tampered ciphertext before decrypting it", async () => {
    const [head, data, mac] = (await encString("x", key)).split("|");
    const flipped = `${data?.[0] === "A" ? "B" : "A"}${data?.slice(1)}`;
    await expect(
      decryptEncString(`${head}|${flipped}|${mac}`, key),
    ).rejects.toThrow("MAC mismatch");
  });

  it("refuses other EncString types and short keys", async () => {
    await expect(decryptEncString("0.a|b", key)).rejects.toThrow(
      BitwardenCryptoError,
    );
    await expect(
      decryptEncString(await encString("x", key), key.slice(0, 32)),
    ).rejects.toThrow("64 bytes");
  });
});
