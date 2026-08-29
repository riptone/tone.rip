import { DASHBOARD_INFO } from "@repo/content";

// Composed from the shared DASHBOARD_INFO record
// (packages/content/src/site-info.ts) rather than restating the name and
// description here as a second copy of the same string.
export const SITE_NAME = DASHBOARD_INFO.name;
export const SITE_URL = DASHBOARD_INFO.url;
// "dash · tone", matching the site's own tab titles. The tagline used to
// be in here, which made a tab that read like a search result.
export const SITE_TITLE = "dash · tone";
export const SITE_DESCRIPTION = DASHBOARD_INFO.description;
