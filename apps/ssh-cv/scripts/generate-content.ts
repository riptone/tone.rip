/* Generates apps/ssh-cv/internal/cv/cv.json from @repo/content.

   The Go binary embeds the result with go:embed, so the SSH CV and the
   website are rendering the same words by construction rather than by
   somebody remembering to update both. Run it via `bun run build` in this
   app; CI runs it too, and the generated file is committed so a plain
   `go build` in a checkout without Bun still works.

   This is a projection, not a second source: every string below comes out of
   @repo/content. The SSH CV is simply the longer projection - it prints the
   company names and the per-role `detail` that the website deliberately
   leaves out (see packages/content/src/cv.ts). */

import { mkdir, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  BEST_AT,
  CERTIFICATIONS,
  CV_LABELS,
  type CvLang,
  EDUCATION,
  EXPERIENCE,
  INTERESTS,
  NO_TONE_INFO,
  SKILLS,
  SPOKEN,
} from "@repo/content";

const LANGS: CvLang[] = ["en", "pt"];

/* The one fact the content module has no reason to know: the hostname this
   server answers on. SSH has no SNI, so the binary cannot learn it from the
   connection either - see apps/ssh-cv/README.md. */
const SSH_HOST = "cv.no-tone.com";

/** `https://no-tone.com/` -> `no-tone.com`. A terminal wants the name. */
function bare(url: string): string {
  return url.replace(/^https?:\/\//, "").replace(/\/$/, "");
}

/**
 * The site's own link list is the source for these, so the contact block
 * cannot drift from the footer and the markdown homepage. A missing link is
 * a build failure rather than an empty row: the CV's last page would
 * silently lose a line otherwise.
 */
function linkStartingWith(prefix: string): string {
  const found = NO_TONE_INFO.links.find((entry) =>
    entry.href.startsWith(prefix),
  );
  if (!found) {
    throw new Error(`NO_TONE_INFO has no link starting with "${prefix}"`);
  }
  return found.href;
}

const payload = {
  // A note for anyone who opens the generated file wondering where to edit.
  $comment:
    "Generated from packages/content/src/cv.ts - do not edit. Run `bun run build` in apps/ssh-cv.",
  langs: LANGS,
  contact: {
    web: bare(NO_TONE_INFO.url),
    email: linkStartingWith("mailto:").slice("mailto:".length),
    github: bare(linkStartingWith("https://github.com/")),
    ssh: `ssh ${SSH_HOST}`,
  },
  byLang: Object.fromEntries(
    LANGS.map((lang) => [
      lang,
      {
        labels: CV_LABELS[lang],
        bestAt: BEST_AT[lang],
        experience: EXPERIENCE[lang],
        education: EDUCATION[lang],
        certifications: CERTIFICATIONS[lang],
        skills: SKILLS[lang],
        spoken: SPOKEN[lang],
        interests: INTERESTS[lang],
      },
    ]),
  ),
};

const outPath = join(
  dirname(dirname(fileURLToPath(import.meta.url))),
  "internal",
  "cv",
  "cv.json",
);
await mkdir(dirname(outPath), { recursive: true });
await writeFile(outPath, `${JSON.stringify(payload, null, 2)}\n`, "utf8");
console.log(`wrote ${outPath}`);
