/* Where to reach the person behind all of this, stated once.
 *
 * It was stated in six files - two layouts, three pages and site-info.ts -
 * which is the arrangement that made renaming the GitHub organisation a
 * find-and-replace across the repo, with a test asserting the outgoing URL as
 * the only thing standing between a typo and a broken API call.
 *
 * The organisation name is the fact; the rest are presentations of it, so
 * they are derived rather than written out again. A handle that appears as
 * `@riptone` in a footer, `github.com/riptone` as link text and
 * `https://github.com/riptone` as an href is one decision, not three.
 */

const GITHUB_ORG = "riptone";

export const CONTACT = {
  /** The one address on every surface. `mailto:` is added at the call site. */
  email: "m@tone.rip",
  /** Href form. */
  github: `https://github.com/${GITHUB_ORG}`,
  /** As a name - the footer, the right-click menu. */
  githubHandle: `@${GITHUB_ORG}`,
  /** As link text, where the URL itself is the label. */
  githubLabel: `github.com/${GITHUB_ORG}`,
} as const;
