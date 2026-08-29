/// <reference types="astro/client" />

/* The adapter's own locals type. Not `Runtime<Env>`, which is what stood here
   and was a silent error: `Runtime` has not been generic since the adapter
   dropped `locals.runtime.env` in Astro v6, so `Runtime<Env>` was
   `error TS2315: Type 'Runtime' is not generic` - invisible only because
   `skipLibCheck: true` skips .d.ts files. The augmentation below therefore
   extended nothing, which is why `Astro.locals.runtime` never had a type.

   Bindings do not come from locals any more. They come from
   `import { env } from "cloudflare:workers"`; the adapter throws with exactly
   that advice if you reach for the old path. */
type Runtime = import("@astrojs/cloudflare").Runtime;

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
