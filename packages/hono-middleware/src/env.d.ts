/// <reference types="astro/client" />

// The Astro apps bind this package's CSP nonce onto Astro.locals.cspNonce in
// their own src/fetch.ts; consuming apps (apps/web, apps/dashboard) declare
// the same augmentation in their own env.d.ts - see packages/ui/src/env.d.ts
// for the fuller explanation of why this doesn't cross package boundaries
// automatically.
declare namespace App {
  interface Locals {
    cspNonce: string;
  }
}
