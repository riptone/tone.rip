/* schema.org Person, built from the CV rather than written twice.
 *
 * The site already emits a `WebSite` node, which says a site exists and
 * nothing about whose it is. For a portfolio that is the wrong half: what a
 * search engine - and, increasingly, an assistant answering "who is X and
 * what do they work on" - needs is a `Person` with an occupation and a
 * subject area. An SEO audit put the site's "AI visibility" at 49/100 and
 * its credibility sub-score at 28%, and unstructured identity is a large
 * part of why.
 *
 * Everything below is derived from packages/content/src/cv.ts, so it cannot
 * describe a different person from the one the pages render. That is not
 * only tidiness: structured data that claims more than the page shows is a
 * spam signal, and the fastest way to claim more is to maintain it by hand.
 *
 * What is deliberately absent, and stays absent:
 *
 *   - `worksFor` / `alumniOf`. The pages print roles and degrees, and
 *     describe organisations by what they do rather than naming them - see
 *     the note on the CV page. `Experience.company` does now carry a name,
 *     because `apps/ssh-cv` prints it, but this node describes *this page*:
 *     structured data that claims more than the markup shows is a spam
 *     signal, so the name stays out of here for as long as it stays out of
 *     the page.
 *   - `email`. The address is available from llms.txt, the markdown homepage
 *     and the API's info route, and is deliberately not in the rendered
 *     markup. JSON-LD is rendered markup.
 */

import { BEST_AT, type CvLang, EXPERIENCE, SKILLS, SPOKEN } from "./cv";

export interface PersonSchemaOptions {
  /** The handle the site goes by. */
  name: string;
  /** Canonical site URL. */
  url: string;
  /** Profiles that are demonstrably the same person. */
  sameAs?: string[];
  /** Which language's CV to describe. Defaults to English, as the pages do. */
  lang?: CvLang;
}

/**
 * A `Person` node describing whoever the CV describes.
 *
 * `knowsAbout` is the skills list flattened, plus the headline capabilities -
 * both are visible on /cv, and between them they are the closest thing the
 * vocabulary has to "what this person actually does".
 */
export function buildPersonSchema(
  options: PersonSchemaOptions,
): Record<string, unknown> {
  const { name, url, sameAs = [], lang = "en" } = options;
  const current = EXPERIENCE[lang][0];

  const knowsAbout = [
    ...SKILLS[lang].flatMap((group) => group.items),
    ...BEST_AT[lang].map((row) => row.k),
  ];

  return {
    "@context": "https://schema.org",
    "@type": "Person",
    name,
    url,
    // The current role's title, which the CV prints as its first entry.
    ...(current ? { jobTitle: current.role } : {}),
    knowsAbout,
    // "Portuguese: native" -> "Portuguese". The proficiency is on the page;
    // the vocabulary wants the language.
    knowsLanguage: SPOKEN[lang].map((entry) => entry.split(":")[0]?.trim()),
    ...(sameAs.length > 0 ? { sameAs } : {}),
  };
}

/**
 * The `ProfilePage` wrapper for /cv.
 *
 * A CV page is not a generic WebPage: it is a page *about* one person, and
 * saying so is what lets a consumer attach the Person node to the document
 * rather than guess at the relationship.
 */
export function buildProfilePageSchema(
  person: Record<string, unknown>,
): Record<string, unknown> {
  return {
    "@context": "https://schema.org",
    "@type": "ProfilePage",
    mainEntity: person,
  };
}
