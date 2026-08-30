/* Shared site chrome - the parts of a page that are the same on tone.rip
   and on the dashboard.

   Components live next to their behaviour and are imported by path
   (`@repo/ui/site/Footer.astro`); this barrel is only for the scripts, so an
   app has one import for everything it needs to boot the chrome. */

export { mountContact } from "./contact.js";
export { mountContextMenu } from "./context-menu.js";
/* `syncField` is deliberately not here, and it is the only member of the
   chrome that is missing. Re-exporting it puts field.js in the static graph
   of everything that imports this barrel, and the bundler then inlines the
   `import("@repo/ui/site/field")` in both apps back into their entry chunks -
   which is the whole thing those dynamic imports exist to avoid. Import it
   from "@repo/ui/site/field". */
export {
  type FilterableItem,
  matchesFilter,
  mountFilter,
} from "./filter.js";
export {
  applyLang,
  LANG_CHANGE_EVENT,
  mountLang,
  readLang,
  type SiteLang,
  setLang,
} from "./lang.js";
