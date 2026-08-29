import { handle } from "@astrojs/cloudflare/handler";

// The projects API (GitHub repos, caching/ETag revalidation) now lives on
// apps/api's own Worker at api.tone.rip, not on this Worker. Warming its
// cache on our cron tick still makes sense operationally (keeps the edge
// cache hot ahead of real traffic), so we just hit that Worker's public
// endpoint over the network instead of calling `handle()` against our own
// (now nonexistent) local route.
const PROJECTS_API_URL = "https://api.tone.rip/projects";

const warmProjectsCache = async (): Promise<void> => {
  try {
    const response = await fetch(PROJECTS_API_URL, {
      method: "GET",
      headers: {
        accept: "application/json",
        "x-tone-revalidate": "1",
        "user-agent": "tone-web-cron-warmup",
      },
    });

    if (!response.ok) {
      console.warn("[projects-warmup] non-ok response", {
        status: response.status,
      });
    }
  } catch (error) {
    console.warn("[projects-warmup] failed", {
      error: error instanceof Error ? error.message : "unknown-error",
    });
  }
};

export default {
  async fetch(request, env, ctx) {
    return handle(request, env, ctx);
  },
  async scheduled(_controller, _env, ctx) {
    ctx.waitUntil(warmProjectsCache());
  },
} satisfies ExportedHandler<Env>;
