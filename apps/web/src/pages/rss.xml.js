import rss from "@astrojs/rss";
import { SITE_DESCRIPTION, SITE_TITLE } from "../consts";

// One canonical entry. There is nothing to syndicate yet - no posts, no
// changelog - so this is a discovery stub rather than a feed, and it should
// either grow real items or be deleted rather than sit here forever.
export async function GET(context) {
  return rss({
    title: SITE_TITLE,
    description: SITE_DESCRIPTION,
    site: context.site,
    items: [
      {
        title: "tone",
        description: "Software engineer. Work, CV and contact.",
        link: "/",
        pubDate: new Date("2026-07-12T00:00:00Z"),
      },
    ],
  });
}
