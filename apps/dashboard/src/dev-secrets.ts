/* Stand-in secret names, for localhost only - the counterpart to dev-apps.ts,
 * for the same two reasons: `bun run dev` would otherwise render an empty
 * section, and the end-to-end tests would assert on a real vault.
 *
 * **Invented, and names only.** The real list comes from Bitwarden Secrets
 * Manager through apps/api's GET /secrets, which never returns values; these
 * keep that shape - a couple of projects, one secret in none - and nothing
 * else.
 */

export interface DevSecret {
  name: string;
  projects: string[];
  revisedAt: string;
}

export const DEV_SECRETS: DevSecret[] = [
  {
    name: "EXAMPLE_API_KEY",
    projects: ["example-app"],
    revisedAt: "2026-01-12T09:30:00Z",
  },
  {
    name: "EXAMPLE_DATABASE_URL",
    projects: ["example-app"],
    revisedAt: "2026-02-03T18:05:00Z",
  },
  {
    name: "EXAMPLE_WEBHOOK_SECRET",
    projects: ["example-site"],
    revisedAt: "2026-03-21T11:00:00Z",
  },
  { name: "LOOSE_TOKEN", projects: [], revisedAt: "2025-12-01T00:00:00Z" },
];
