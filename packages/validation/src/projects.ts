/* Shape of `GET api.tone.rip/projects`, validated at the point apps/web
   reads it.

   It is our own API and its response is already shaped by
   @repo/content's `simplifyRepos`, so this is not defending against a hostile
   payload - it is defending against the two failure modes that actually
   happen with an internal API: the shape changing on one side of a deploy,
   and a caching layer handing back something that is not the document you
   asked for. Both are silent without a check, and both look like "the list
   is empty" to a reader.

   Entries are parsed one at a time and the failures dropped: a page that
   shows one fewer project is a better outcome than a page that 500s because
   a single repository grew a null field. */

import { z } from "zod";

export const projectSchema = z.object({
  name: z.string().min(1),
  url: z.string().url(),
  homepage: z.string().default(""),
  language: z.string().default("Other"),
  description: z.string().default(""),
  topics: z.array(z.string()).default([]),
  isFork: z.boolean().default(false),
  isArchived: z.boolean().default(false),
  stars: z.number().default(0),
  updatedAt: z.string().default(""),
});

export type Project = z.infer<typeof projectSchema>;

/** Drops entries that do not parse rather than failing the whole response. */
export const projectsResponseSchema = z
  .array(z.unknown())
  .transform((entries) =>
    entries.flatMap((entry) => {
      const parsed = projectSchema.safeParse(entry);
      return parsed.success ? [parsed.data] : [];
    }),
  );
