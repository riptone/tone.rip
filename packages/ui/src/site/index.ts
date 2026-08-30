/* Shared site chrome - the parts of a page that are the same on tone.rip
   and on the dashboard.

   Components live next to their behaviour and are imported by path
   (`@repo/ui/site/Footer.astro`); this barrel is only for the scripts, so an
   app has one import for everything it needs to boot the chrome. */

export { mountContact } from "./contact.js";
export { mountContextMenu } from "./context-menu.js";
export { syncField } from "./field.js";
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
