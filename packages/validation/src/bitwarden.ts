import { z } from "zod";

/* The two Bitwarden Secrets Manager payloads apps/api reads, validated at the
 * boundary like every other upstream. Field names are the ones in
 * bitwarden/sdk-internal's generated API models. Every `key`/`name` here is
 * an EncString - ciphertext - until apps/api decrypts it. */

/** POST /connect/token with grant_type=client_credentials, scope=api.secrets. */
export const bitwardenTokenResponseSchema = z.object({
  access_token: z.string().min(1),
  expires_in: z.number().int().positive(),
  /** The organization key, encrypted with the access token's derived key. */
  encrypted_payload: z.string().min(1),
});

const encString = z.string().regex(/^2\.[^|]+\|[^|]+\|[^|]+$/);

const projectRefSchema = z.object({
  id: z.uuid(),
  name: encString.nullish(),
});

/** GET /organizations/{organizationId}/secrets - names only, never values. */
export const bitwardenSecretsListSchema = z.object({
  secrets: z
    .array(
      z.object({
        id: z.uuid(),
        key: encString,
        creationDate: z.string(),
        revisionDate: z.string(),
        projects: z.array(projectRefSchema).nullish(),
      }),
    )
    .nullish(),
  projects: z.array(projectRefSchema).nullish(),
});

export type BitwardenSecretsList = z.infer<typeof bitwardenSecretsListSchema>;
