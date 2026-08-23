import { DASHBOARD_INFO, type SiteInfo, TONE_INFO } from "@repo/content";
import { Hono } from "hono";
import { html } from "hono/html";
import type { AppEnv } from "../env";

const SITES: Record<string, SiteInfo> = {
  [TONE_INFO.slug]: TONE_INFO,
  [DASHBOARD_INFO.slug]: DASHBOARD_INFO,
};

function InfoPage({ site }: { site: SiteInfo }) {
  return (
    <html lang="en">
      <head>
        <meta charset="utf-8" />
        <meta name="viewport" content="width=device-width,initial-scale=1" />
        <title>{`${site.name} - info`}</title>
        <meta name="description" content={site.description} />
      </head>
      <body>
        <main>
          <h1>{site.name}</h1>
          <p>{site.tagline}</p>
          <p>{site.description}</p>
          <ul>
            {site.links.map((link) => (
              <li>
                <a href={link.href}>{link.label}</a>
              </li>
            ))}
          </ul>
        </main>
      </body>
    </html>
  );
}

export const infoRoute = new Hono<AppEnv>();

// Agents that ask for `Accept: text/markdown` get the machine-readable page;
// browsers (and anyone else) get the JSX-rendered HTML version - the same
// SiteInfo record backs both, so there's one source of truth per site.
infoRoute.get("/:slug", (c) => {
  const site = SITES[c.req.param("slug")];
  if (!site) return c.notFound();

  const accept = c.req.header("Accept") ?? "";
  if (accept.includes("text/markdown")) {
    return c.body(site.markdown, 200, {
      "Content-Type": "text/markdown; charset=utf-8",
      Vary: "Accept",
    });
  }

  return c.html(html`<!doctype html>${<InfoPage site={site} />}`);
});
