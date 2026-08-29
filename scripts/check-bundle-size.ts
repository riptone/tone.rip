/* Guards the Cloudflare Workers script-size limit (10 MiB gzip on the paid
   plan this account runs) by dry-run bundling every Worker exactly the way
   `wrangler deploy` would and reading the gzip size it reports.

   apps/web and apps/dashboard need `bun run build` first - their Worker
   entrypoint (`src/worker.ts`) pulls in Astro's server build from `dist/`,
   so a dry-run bundle without one either fails or measures stale output.
   `bun run build` (turbo) already runs ahead of this in CI.

   The budget here is deliberately not the real 10 MiB ceiling: it is early
   warning, set well above current usage so ordinary growth doesn't trip it,
   but far enough below the ceiling that a genuine regression (an
   accidentally-bundled dependency, a vendored asset) is caught long before
   a deploy would actually fail. */

import { rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

interface WorkerApp {
  dir: string;
  budgetKiB: number;
  /**
   * Whether this app's `deploy` script passes `--minify`, so the number here
   * is the one that actually ships.
   *
   * Only apps/api does, and the asymmetry is real rather than an oversight:
   * wrangler bundles that Worker straight from TypeScript, so minifying it
   * takes 153 KiB gzip to 115. apps/web and apps/dashboard hand wrangler a
   * build Astro has already minified - measured at 231.68 KiB with the flag
   * and 231.68 without, to the byte.
   *
   * This mattered because the flag was in apps/api's `deploy` script and the
   * CI workflow ran a hand-copied `wrangler deploy` without it, so the 25%
   * was written down and never shipped. CI runs the scripts now.
   */
  minify?: boolean;
}

const APPS: WorkerApp[] = [
  { dir: "apps/web", budgetKiB: 2048 },
  { dir: "apps/dashboard", budgetKiB: 2048 },
  { dir: "apps/api", budgetKiB: 2048, minify: true },
];

const TOTAL_UPLOAD_LINE =
  /Total Upload: [\d.]+ \wiB \/ gzip: ([\d.]+) (Ki|Mi)B/;

async function measureGzipKiB(app: WorkerApp): Promise<number> {
  const appDir = app.dir;
  const outdir = join(
    tmpdir(),
    `bundle-size-check-${appDir.replaceAll("/", "-")}`,
  );
  try {
    const proc = Bun.spawn(
      [
        "./node_modules/.bin/wrangler",
        "deploy",
        "--dry-run",
        "--outdir",
        outdir,
        ...(app.minify ? ["--minify"] : []),
      ],
      { cwd: appDir, stdout: "pipe", stderr: "pipe" },
    );
    const [stdout, stderr] = await Promise.all([
      new Response(proc.stdout).text(),
      new Response(proc.stderr).text(),
    ]);
    const exitCode = await proc.exited;
    if (exitCode !== 0) {
      throw new Error(
        `wrangler dry-run failed in ${appDir}:\n${stderr || stdout}`,
      );
    }

    const match = stdout.match(TOTAL_UPLOAD_LINE);
    if (!match) {
      throw new Error(
        `could not find a "Total Upload" line in wrangler's output for ${appDir}:\n${stdout}`,
      );
    }
    const [, size, unit] = match;
    return unit === "Mi" ? Number(size) * 1024 : Number(size);
  } finally {
    await rm(outdir, { recursive: true, force: true });
  }
}

let failed = false;
for (const app of APPS) {
  const gzipKiB = await measureGzipKiB(app);
  const ok = gzipKiB <= app.budgetKiB;
  if (!ok) failed = true;
  console.log(
    `${ok ? "✓" : "✗"} ${app.dir}: ${gzipKiB.toFixed(2)} KiB gzip (budget ${app.budgetKiB} KiB)`,
  );
}

if (failed) {
  console.error(
    "\nA Worker's bundled script grew past its size budget - see scripts/check-bundle-size.ts.",
  );
  process.exit(1);
}
