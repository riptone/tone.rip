import { TONE_INFO } from "@repo/content";

// Composed from the shared TONE_INFO record (packages/content/src/site-info.ts)
// rather than restating the name and URL here, which is what apps/dashboard
// already did with DASHBOARD_INFO. Two apps solving the same problem two ways
// is how "tone" and "https://tone.rip" ended up written out in six files.
//
// There were two more constants here, `SITE_TITLE` and `SITE_DESCRIPTION`,
// and deleting /rss.xml left both with no readers - the feed was the last
// thing that wanted a site-wide title and blurb. `SITE_TITLE` was a second
// name for `SITE_NAME` (both were `TONE_INFO.name`), and every page already
// writes its own <title> and <meta description>, which is also where
// BaseHead's default WebSite schema takes them from. knip found them; that
// is the whole reason it runs.
export const SITE_NAME = TONE_INFO.name;
export const SITE_URL = TONE_INFO.url;
