export type AppTag =
  | "Self-Hosted"
  | "Personal"
  | "Network"
  | "Media"
  | "Security"
  | "Ops";

export interface SelfHostedApp {
  name: string;
  href: string;
  tags: AppTag[];
  iconUrl: string;
}

/** Ported from main-menu's src/apps.ts - the registry apps/dashboard renders and apps/api's /status route probes. */
export const SELF_HOSTED_APPS: SelfHostedApp[] = [
  {
    name: "Tailscale",
    href: "https://login.tailscale.com/admin/",
    tags: ["Network"],
    iconUrl:
      "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/tailscale-light.webp",
  },
  {
    name: "Nginx",
    href: "https://proxy.tone.rip",
    tags: ["Ops", "Network", "Self-Hosted"],
    iconUrl:
      "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/nginx-proxy-manager.webp",
  },
  {
    name: "Portainer",
    href: "https://ports.tone.rip",
    tags: ["Ops", "Self-Hosted"],
    iconUrl:
      "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/portainer-dark.webp",
  },
  {
    name: "Vaultwarden",
    href: "https://pass.tone.rip",
    tags: ["Security", "Self-Hosted"],
    iconUrl:
      "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/vaultwarden-light.webp",
  },
  {
    name: "Joplin",
    href: "https://notes.tone.rip",
    tags: ["Personal", "Self-Hosted"],
    iconUrl:
      "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/png/joplin.png",
  },
  {
    name: "Immich",
    href: "https://photos.tone.rip",
    tags: ["Media", "Self-Hosted"],
    iconUrl:
      "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/immich.webp",
  },
  {
    name: "OpenCloud",
    href: "https://drive.tone.rip",
    tags: ["Media", "Self-Hosted"],
    iconUrl:
      "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/webp/open-cloud-dark.webp",
  },
];

export const ALL_APP_TAGS: AppTag[] = Array.from(
  new Set(SELF_HOSTED_APPS.flatMap((app) => app.tags)),
).sort() as AppTag[];

/**
 * Vaultwarden (pass.tone.rip) serves an empty 200 at "/" behind auth walls
 * in some configs; probe its favicon instead. Shared by apps/api's
 * server-side health probe and apps/dashboard's client-side ping - they used
 * to each hardcode this hostname check separately.
 */
export function resolveProbePath(url: URL): string {
  return url.hostname === "pass.tone.rip" ? "/favicon.ico" : "/";
}
