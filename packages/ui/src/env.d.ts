/// <reference types="astro/client" />

// BaseHead.astro reads Astro.locals.cspNonce, set by each app's own
// src/fetch.ts (via @repo/hono-middleware's securityHeaders).
// Consuming apps (apps/web, apps/dashboard) need the same augmentation in
// their own env.d.ts - TypeScript ambient declarations don't cross package
// boundaries on their own.
declare namespace App {
  interface Locals {
    cspNonce: string;
  }
}
