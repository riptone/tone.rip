# Shared static assets

The font, the favicons, the icon set, and the `_headers` rules that give them
a cache lifetime. One copy, here, because `@repo/ui` is what *references*
them: `src/fonts.ts` names `/fonts/hanken-grotesk-400-500.woff2`, `BaseHead.astro`
emits the `<link rel="icon">` tags and the font preload, and `_headers` exists
for exactly those paths. The package that declares the contract now also holds
the files.

They used to sit in `apps/web/public/` and `apps/dashboard/public/` as
byte-identical copies — 57KB duplicated, and nothing to notice if one of them
changed. Each app's `public/` now holds a relative symlink here instead.
Astro dereferences symlinks when it copies `public/` (verified: both a
symlinked file and a symlinked directory arrive in `dist/client/` as real
files), so the build output is unchanged.

Git stores the symlinks natively. On Windows they need `core.symlinks=true`,
which is the default everywhere except a Windows checkout without developer
mode.

`robots.txt` deliberately stays per-app: `apps/api` and `apps/dashboard` have
identical ones today, but "this surface is not indexable" is a decision each
app makes for itself, and `apps/web` answers it from a route instead.
