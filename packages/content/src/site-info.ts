import { CONTACT } from "./contact";

export interface SiteInfo {
  slug: string;
  name: string;
  tagline: string;
  description: string;
  url: string;
  links: { label: string; href: string }[];
  /** Machine-readable homepage served to agents that send `Accept: text/markdown`. */
  markdown: string;
}

export const TONE_INFO: SiteInfo = {
  slug: "tone",
  name: "tone",
  tagline: "Software engineer",
  description:
    "Personal site of a software engineer: work, CV and contact. Web applications end to end, from the front-end to the API and the infrastructure under it.",
  url: "https://tone.rip",
  links: [
    { label: "Work", href: "https://tone.rip/work" },
    { label: "CV", href: "https://tone.rip/cv" },
    { label: "GitHub", href: CONTACT.github },
    { label: "Contact", href: `mailto:${CONTACT.email}` },
  ],
  markdown: [
    "# tone",
    "",
    "Personal site of a software engineer. Web applications end to end: the",
    "front-end, the API behind it, and the infrastructure it runs on.",
    "",
    "## Pages",
    "- **/** - intro",
    "- **/work** - public repositories, newest first",
    "- **/cv** - experience, education, skills",
    "",
    "## Elsewhere",
    `- GitHub: ${CONTACT.github}`,
    `- Contact: ${CONTACT.email}`,
    "- CV over SSH: `ssh cv.tone.rip`",
    "",
    "## API",
    "- `GET https://api.tone.rip/projects` - public repositories as JSON",
    "",
    "## More",
    "- Sitemap: https://tone.rip/sitemap-index.xml",
    "- API catalog: https://api.tone.rip/.well-known/api-catalog",
    "",
  ].join("\n"),
};

export const DASHBOARD_INFO: SiteInfo = {
  slug: "dashboard",
  name: "dash",
  tagline: "Self-hosted services launcher",
  description: "A minimal launcher for tone's self-hosted services.",
  url: "https://dash.tone.rip",
  links: [{ label: "Apps API", href: "https://api.tone.rip/apps" }],
  markdown: [
    "# dash",
    "",
    "A minimal launcher for tone's self-hosted services: the Cloudflare Access applications tagged Self-Hosted, with search and a tag filter.",
    "",
    "## API",
    "- `GET /apps` - the applications the launcher lists. Requires a Cloudflare Access JWT.",
    "",
  ].join("\n"),
};
