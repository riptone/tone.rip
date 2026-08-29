/// <reference types="astro/client" />

type Runtime = import("@astrojs/cloudflare").Runtime<Env>;

// BaseHead.astro (packages/ui) reads Astro.locals.cspNonce, set by our own
// src/middleware.ts via @repo/hono-middleware/core's buildSecurityHeaders.
// TypeScript ambient declarations don't cross package boundaries
// automatically, so this app needs its own copy of the same augmentation -
// see packages/ui/src/env.d.ts for the canonical explanation.
declare namespace App {
  interface Locals extends Runtime {
    cspNonce: string;
  }
}

// window.tone's shape (installed by packages/ui's BaseHead.astro) is
// declared once, globally, in src/scripts/theme.ts - the one file that
// actually consumes it - instead of here, since a `declare global` needs to
// live in a module (a file with an import/export), and this file is a plain
// ambient script.
