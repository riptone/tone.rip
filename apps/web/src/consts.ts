// Site metadata for apps/web. Not shared via @repo/content: that package's
// TONE_INFO carries the cross-app machine-readable summary (used by the
// markdown-negotiation branch in middleware.ts), while these drive this app's
// own <title>/meta description and RSS feed.
export const SITE_NAME = "tone";
export const SITE_URL = "https://tone.rip";
export const SITE_TITLE = "tone";
// Feeds and the default WebSite schema, not a page's <meta description> -
// each page writes its own, sized for a search result (see index/work/cv).
export const SITE_DESCRIPTION =
  "Software engineer building web applications end to end - the front-end, the API behind it, and the infrastructure both run on. Work, CV and contact.";
