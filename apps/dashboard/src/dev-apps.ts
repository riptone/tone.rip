/* Stand-in tiles, for localhost only.
 *
 * The launcher's real list comes from Cloudflare Access through apps/api, and
 * that call needs an API token the deployed Worker holds and nobody else
 * does. Without something here, two things break in ways that look like bugs:
 *
 *   - `bun run dev` renders an empty board, so the search, the filter, the
 *     status colours and the tile layout cannot be worked on at all without
 *     deploying;
 *   - the end-to-end tests reach out to the real API from inside
 *     `wrangler dev` and assert on whatever the account happens to contain.
 *     They passed locally and failed in CI, which is the signature of a test
 *     coupled to production rather than a flaky one.
 *
 * **These are invented.** This repository is public, and the set of services
 * somebody self-hosts - and the hostnames they answer on - is not something a
 * public repository should enumerate. It is a map of the attack surface, and
 * it is free to give away by accident and impossible to take back.
 *
 * So the names and hosts below are fiction, on a domain reserved by RFC 2606
 * for exactly this. What they preserve is the *shape*: a mixture of tag
 * counts, one entry with no icon, and enough rows to make the filter worth
 * exercising. Nothing here needs to match the real account, because nothing
 * here is ever compared against it.
 */

export interface DevTile {
  name: string;
  href: string;
  tags: string[];
  /** Path the browser probes for reachability. See apps/api's PROBE_PATHS. */
  probePath: string;
  iconUrl: string | null;
}

export const DEV_APPS: DevTile[] = [
  {
    name: "Gallery",
    href: "https://gallery.example.com/",
    tags: ["Media", "Self-Hosted"],
    probePath: "/",
    iconUrl: null,
  },
  {
    name: "Notebook",
    href: "https://notebook.example.com/",
    tags: ["Personal", "Self-Hosted"],
    probePath: "/",
    iconUrl: null,
  },
  {
    name: "Files",
    href: "https://files.example.com/",
    tags: ["Media", "Self-Hosted"],
    probePath: "/",
    iconUrl: null,
  },
  {
    name: "Containers",
    href: "https://containers.example.com/",
    tags: ["Ops", "Self-Hosted"],
    probePath: "/",
    iconUrl: null,
  },
  {
    name: "Gateway",
    href: "https://gateway.example.com/",
    tags: ["Ops", "Network", "Self-Hosted"],
    probePath: "/",
    iconUrl: null,
  },
  {
    // The one with a non-default probe path, so that branch is exercised in
    // dev and in the end-to-end run rather than only in production.
    name: "Secrets",
    href: "https://secrets.example.com/",
    tags: ["Security", "Self-Hosted"],
    probePath: "/favicon.ico",
    iconUrl: null,
  },
];
