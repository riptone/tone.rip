import type { APIContext } from "astro";

const DEV_HOSTNAME = "dev.tone.rip";
// @astrojs/sitemap emits an index plus the sheets it points at; the static
// public/sitemap.xml that used to sit here listed exactly one URL and
// shadowed it, so the three real routes were never advertised.
const PROD_SITEMAP = "https://tone.rip/sitemap-index.xml";

const buildHeaders = (cacheControl: string, robotsTag?: string) => {
  const headers: Record<string, string> = {
    "Content-Type": "text/plain; charset=utf-8",
    "Cache-Control": cacheControl,
  };
  if (robotsTag) {
    headers["X-Robots-Tag"] = robotsTag;
  }
  return headers;
};

export function GET({ url }: APIContext): Response {
  if (url.hostname === DEV_HOSTNAME) {
    // The Content-Signal is here as well as on the production branch below,
    // and it is not redundant: `Disallow` binds a crawler that asks, while
    // the signal states what may be done with content obtained any other
    // way. A staging host is the likelier of the two to leak.
    const devBody = [
      "User-agent: *",
      "Content-Signal: search=no, ai-input=no, ai-train=no",
      "Disallow: /",
      "",
    ].join("\n");
    return new Response(devBody, {
      status: 200,
      headers: buildHeaders(
        "no-store",
        "noindex, nofollow, noarchive, nosnippet",
      ),
    });
  }

  // Content Signals (contentsignals.org): findable in search and readable by
  // agents answering questions, but not used to train models.
  const body = [
    "User-agent: *",
    "Content-Signal: search=yes, ai-input=yes, ai-train=no",
    "Allow: /",
    "",
    `Sitemap: ${PROD_SITEMAP}`,
    "",
  ].join("\n");

  return new Response(body, {
    status: 200,
    headers: buildHeaders("public, max-age=3600, s-maxage=3600"),
  });
}
