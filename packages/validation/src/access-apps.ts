import { z } from "zod";

/* Cloudflare Zero Trust Access applications, as this repo consumes them.
 *
 * The dashboard's tile list used to be a hand-written array in
 * @repo/content. It is now whatever the Access account says it is, which
 * means a new application appears on the launcher by existing rather than by
 * somebody remembering to add it twice.
 *
 * Validated rather than trusted, for the usual reason plus one specific to
 * this shape: `domain` becomes an `https://` origin that ends up in the
 * dashboard's `connect-src`, and `logo_url` becomes an `<img src>`. Both are
 * strings an account administrator types into a form, and neither has any
 * business reaching a CSP header or the DOM unvalidated.
 */

/** Applications that are not a link to a self-hosted thing. */
const RENDERABLE_TYPES = new Set(["self_hosted", "saas", "bookmark", "public"]);

/**
 * The tag an application must carry to appear on the launcher.
 *
 * An Access account holds more than a launcher's worth of applications -
 * infrastructure targets, things gated for one person, whatever is mid-setup.
 * Rendering all of them would make the dashboard a mirror of the account
 * rather than a list of services, so inclusion is opt-in: tag it in Zero
 * Trust and it shows up.
 *
 * Compared case-insensitively because it is typed by hand into a form, where
 * "self-hosted" and "Self-Hosted" are the same intention and a silent
 * mismatch would read as the API being broken.
 */
export const LAUNCHER_TAG = "Self-Hosted";

/* Both sides normalised, not just the incoming one. Comparing a lowercased
   tag against this constant directly is a bug that hides itself: it silently
   matches nothing, so the launcher goes empty and the API looks broken. */
const launcherTag = LAUNCHER_TAG.trim().toLowerCase();

function hasLauncherTag(tags: string[]): boolean {
  return tags.some((tag) => tag.trim().toLowerCase() === launcherTag);
}

/**
 * One application, as the API returns it.
 *
 * Everything except `id` is optional in practice: the API omits fields that
 * were never set, and an account with no custom logo on any application would
 * otherwise fail validation wholesale. The narrowing to "renderable" happens
 * in `toSelfHostedApps`, not here, so a rejected application is a filtered
 * one rather than a thrown error.
 */
export const accessApplicationSchema = z.object({
  id: z.string(),
  name: z.string().optional(),
  domain: z.string().optional(),
  type: z.string().optional(),
  logo_url: z.string().optional(),
  tags: z.array(z.string()).optional(),
});

export type AccessApplication = z.infer<typeof accessApplicationSchema>;

/**
 * The envelope every Cloudflare v4 endpoint answers with.
 *
 * `success` is checked rather than assumed: Cloudflare returns HTTP 200 with
 * `success: false` and a populated `errors` array for several failures,
 * including an API token that is valid but lacks the scope. Reading `result`
 * without looking at `success` turns that into "the account has no
 * applications", which is indistinguishable from the truth and wipes the
 * dashboard.
 */
export const accessApplicationsResponseSchema = z.object({
  success: z.boolean(),
  errors: z
    .array(z.object({ code: z.number().optional(), message: z.string() }))
    .optional(),
  result: z.array(accessApplicationSchema).nullable().optional(),
});

export interface SelfHostedApp {
  name: string;
  href: string;
  tags: string[];
  iconUrl: string | null;
}

/**
 * Applications the dashboard can actually render, newest API shape mapped to
 * the shape the tiles already expect.
 *
 * Three filters, each for a concrete failure:
 *
 *   - **no domain** - an application can exist with none (a draft, or an
 *     infrastructure target); a tile linking to `https://undefined` is worse
 *     than no tile.
 *   - **not renderable** - Access covers things that are not web pages at all
 *     (`ssh`, `vnc`, `rdp`, warp policies). They belong to the account, not to
 *     a launcher.
 *   - **unparseable domain** - `new URL()` is the same parse the CSP
 *     derivation does later, so anything that would throw there is dropped
 *     here instead, where it is one missing tile rather than a 500.
 *
 * `logo_url` is required to be https for the same reason the CSP is: an
 * http image on an https page is blocked anyway, so admitting it would only
 * produce a broken tile with a console error attached.
 */
export function toSelfHostedApps(
  applications: AccessApplication[],
): SelfHostedApp[] {
  const apps: SelfHostedApp[] = [];

  for (const application of applications) {
    if (!application.domain || !application.name) continue;
    if (application.type && !RENDERABLE_TYPES.has(application.type)) continue;

    const tags = application.tags ?? [];
    if (!hasLauncherTag(tags)) continue;

    // A domain arrives bare ("notes.tone.rip") or with a path
    // ("tone.rip/admin"), never with a scheme.
    let href: string;
    try {
      href = new URL(
        `https://${application.domain.replace(/^https?:\/\//, "")}`,
      ).href;
    } catch {
      continue;
    }

    const logo = application.logo_url?.trim();
    apps.push({
      name: application.name,
      href,
      // The launcher tag is kept, not stripped.
      //
      // It is tempting to drop it here - it is on every entry by definition,
      // so as a *label* it distinguishes nothing. But it is also load-bearing
      // data: apps/api's `isTailnetOnly` reads it to decide whether the edge
      // may probe a host at all, and hosts that resolve to Tailscale CGNAT
      // addresses are unreachable from Cloudflare, so probing them would
      // report a confident "down" for everything that is actually fine.
      //
      // Stripping it here broke exactly that, silently. Hiding a label is a
      // presentation concern and is done where the labels are drawn.
      tags,
      iconUrl: logo?.startsWith("https://") ? logo : null,
    });
  }

  // Stable order, so an unchanged account produces an unchanged response and
  // the edge cache is not invalidated by Cloudflare's own result ordering.
  return apps.sort((a, b) => a.name.localeCompare(b.name));
}
