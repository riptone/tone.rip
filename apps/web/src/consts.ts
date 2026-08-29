import { TONE_INFO } from "@repo/content";

// Composed from the shared TONE_INFO record (packages/content/src/site-info.ts)
// rather than restating the name and URL here, which is what apps/dashboard
// already did with DASHBOARD_INFO. Two apps solving the same problem two ways
// is how "tone" and "https://tone.rip" ended up written out in six files.
export const SITE_NAME = TONE_INFO.name;
export const SITE_URL = TONE_INFO.url;
export const SITE_TITLE = TONE_INFO.name;

// The one field that is deliberately *not* TONE_INFO's. That record's
// `description` is the cross-app machine-readable summary, served to agents by
// the markdown-negotiation branch in middleware.ts. This one is written for a
// search result, and drives feeds and the default WebSite schema - each page
// writes its own <meta description> on top (see index/work/cv).
export const SITE_DESCRIPTION =
  "Software engineer building web applications end to end - the front-end, the API behind it, and the infrastructure both run on. Work, CV and contact.";
