/// <reference types="bun-types" />

/* Lighthouse against the real Worker, on demand.
 *
 * Deliberately not part of `bun run ci`. Every number below moves with the
 * machine it ran on, and a gate that fails for that reason is a gate people
 * learn to re-run rather than read - the same argument this repo makes for
 * pinning govulncheck rather than tracking @latest.
 *
 * What it is for is the case that produced it: a change was made to shorten
 * the critical request chain, measured by counting bytes in `dist`, and
 * shipped. The bytes were right and the chain got one level *longer*. This
 * is the thing that would have said so before the deploy.
 *
 * `wrangler dev` rather than `astro dev`, for the reason the e2e suites use
 * it: only the real Worker builds the production CSP, the real `_headers`,
 * and the real bundle graph. `astro dev` serves unbundled modules, so the
 * chain it produces is not the one visitors get.
 */

import { chromium } from "@playwright/test";
import { ensureServer } from "@repo/webview-config";
import { launch } from "chrome-launcher";
import lighthouse from "lighthouse";
import desktopConfig from "lighthouse/core/config/desktop-config.js";

/* A port of its own: the e2e suites own 8787, and running this while a suite
   is up should measure this build rather than silently attach to theirs. */
const PORT = 8790;
const TARGET = process.env.LIGHTHOUSE_URL ?? `http://localhost:${PORT}/`;
/** Set when the caller points at a URL themselves, in which case we boot nothing. */
const EXTERNAL = Boolean(process.env.LIGHTHOUSE_URL);
/* Desktop by default, because that is what the audits being chased were run
   on. `LIGHTHOUSE_PRESET=mobile` switches to Lighthouse's own default - a
   throttled mid-range phone - which is the only setting where the cost of an
   extra round trip is visible at all. On desktop every request in this site's
   chain lands inside 70ms and any two variants look identical. */
const MOBILE = process.env.LIGHTHOUSE_PRESET === "mobile";
/* One run of this is worth very little. An identical build measured three
   times spread 59-123ms on element render delay, so any A/B smaller than
   that spread needs a distribution rather than a number. `LIGHTHOUSE_RUNS=n`
   reuses one server and one browser across n runs and reports the median,
   which is what makes a small change decidable instead of arguable. */
const RUNS = Math.max(1, Number(process.env.LIGHTHOUSE_RUNS ?? 1));

/** Metrics worth printing, in the order Lighthouse's own summary uses. */
const METRICS = [
  ["first-contentful-paint", "FCP"],
  ["largest-contentful-paint", "LCP"],
  ["total-blocking-time", "TBT"],
  ["cumulative-layout-shift", "CLS"],
  ["speed-index", "SI"],
] as const;

/* The audit has been renamed more than once (`critical-request-chains`, then
   the `-insight` variants), so it is looked up by id with a title fallback
   rather than pinned to whichever name this version happens to use. */
const CHAIN_AUDIT_IDS = [
  "network-dependency-tree-insight",
  "critical-request-chains",
];

interface ChainNode {
  url?: string;
  transferSize?: number;
  navStartToEndTime?: number;
  isLongest?: boolean;
  children?: Record<string, ChainNode>;
}

interface NetworkTree {
  type: "network-tree";
  chains: Record<string, ChainNode>;
  longestChain?: { duration?: number };
}

/**
 * Dig the network tree out of the audit's nested `list-section` details.
 *
 * The audit renders several sections - the tree, preconnect origins,
 * preconnect candidates - so it is found by the `network-tree` marker on its
 * value rather than by position, which has already changed once.
 */
function findTree(details: unknown): NetworkTree | null {
  if (!details || typeof details !== "object") return null;
  const node = details as { type?: string; value?: unknown; items?: unknown[] };
  if (node.type === "network-tree") return node as unknown as NetworkTree;
  const fromValue = findTree(node.value);
  if (fromValue) return fromValue;
  for (const item of node.items ?? []) {
    const found = findTree(item);
    if (found) return found;
  }
  return null;
}

interface AuditRow {
  label?: string;
  subpart?: string;
  duration?: number;
  url?: string;
  totalBytes?: number;
}

/**
 * Pull the first table's rows out of an audit's details.
 *
 * Audits nest their tables to different depths - `lcp-breakdown-insight`
 * wraps one in a list, `render-blocking-insight` is a table already - so this
 * looks for the rows rather than for a shape.
 */
function findRows(details: unknown): AuditRow[] {
  if (!details || typeof details !== "object") return [];
  const node = details as { type?: string; items?: unknown[] };
  if (node.type === "table") return (node.items ?? []) as AuditRow[];
  for (const item of node.items ?? []) {
    const found = findRows(item);
    if (found.length > 0) return found;
  }
  return [];
}

interface ChainLine {
  depth: number;
  text: string;
}

/**
 * Flatten the tree to `depth: url` lines.
 *
 * Depth is the number worth watching and it is not what the Lighthouse UI
 * shows: the report prints the tree as an indented list, so two requests that
 * are *siblings* under the same parent - fetched in parallel - read as a
 * chain one level deeper than it is. Printing the depth explicitly is the
 * whole reason this exists.
 */
function describeChain(
  nodes: Record<string, ChainNode>,
  depth = 0,
  out: ChainLine[] = [],
): ChainLine[] {
  for (const node of Object.values(nodes)) {
    const path = (node.url ?? "").replace(/^https?:\/\/[^/]+/, "") || "/";
    const kib =
      typeof node.transferSize === "number"
        ? ` ${(node.transferSize / 1024).toFixed(2)} KiB`
        : "";
    const at =
      typeof node.navStartToEndTime === "number"
        ? ` @${node.navStartToEndTime}ms`
        : "";
    out.push({
      depth,
      text: `${"  ".repeat(depth)}${depth}: ${path}${kib}${at}${node.isLongest ? " *" : ""}`,
    });
    if (node.children) describeChain(node.children, depth + 1, out);
  }
  return out;
}

async function main(): Promise<void> {
  const server = EXTERNAL
    ? null
    : Bun.spawn(
        [
          "bunx",
          "wrangler",
          "dev",
          "--port",
          String(PORT),
          "--inspector-port",
          "9240",
        ],
        { stdout: "ignore", stderr: "ignore" },
      );

  try {
    await ensureServer(TARGET);

    /* Playwright's Chrome rather than whatever is installed: it is already a
       dependency, already cached in CI, and pinned by the lockfile - so the
       browser this measures in is the browser the e2e suites run in. */
    const chrome = await launch({
      chromePath: chromium.executablePath(),
      chromeFlags: ["--headless=new", "--no-sandbox", "--disable-gpu"],
    });

    try {
      /* Collected across every run, then reduced to medians below. Kept as
         raw samples rather than a running mean: a mean hides the one run in
         seven where the machine had a bad second, and that run is exactly
         what made the last comparison unreadable. */
      const samples: Record<string, number[]> = {};
      const record = (key: string, value: number | undefined): void => {
        if (typeof value !== "number" || !Number.isFinite(value)) return;
        const bucket = samples[key] ?? [];
        bucket.push(value);
        samples[key] = bucket;
      };

      type Result = NonNullable<Awaited<ReturnType<typeof lighthouse>>>;
      let lhr: Result["lhr"] | undefined;
      for (let i = 0; i < RUNS; i += 1) {
        const run = await lighthouse(
          TARGET,
          { port: chrome.port, output: "json", logLevel: "error" },
          MOBILE ? undefined : desktopConfig,
        );
        if (!run?.lhr) throw new Error("Lighthouse returned no result");
        const current = run.lhr;
        lhr = current;

        record("score", (current.categories.performance?.score ?? 0) * 100);
        for (const [id] of METRICS)
          record(id, current.audits[id]?.numericValue);
        for (const row of findRows(
          current.audits["lcp-breakdown-insight"]?.details,
        )) {
          record(`lcp:${row.label ?? row.subpart}`, row.duration);
        }
        const tree = findTree(
          CHAIN_AUDIT_IDS.map((id) => current.audits[id]).find(Boolean)
            ?.details,
        );
        record("chain", tree?.longestChain?.duration);
      }
      if (!lhr) throw new Error("Lighthouse returned no result");

      const median = (key: string): string => {
        const values = [...(samples[key] ?? [])].sort((a, b) => a - b);
        const mid = values[Math.floor(values.length / 2)];
        if (mid === undefined) return "n/a";
        const spread =
          values.length > 1
            ? ` (${values[0]?.toFixed(0)}-${values.at(-1)?.toFixed(0)})`
            : "";
        return `${mid.toFixed(mid < 10 ? 1 : 0)}${spread}`;
      };

      console.log(`\n${TARGET}`);
      console.log(
        `Lighthouse ${lhr.lighthouseVersion} · ${MOBILE ? "mobile (throttled)" : "desktop"} preset · ${RUNS} run${RUNS === 1 ? "" : "s"}`,
      );
      console.log(RUNS > 1 ? "median (min-max)\n" : "");

      console.log(`  Performance      ${median("score")}`);
      for (const [id, label] of METRICS) {
        console.log(`  ${label.padEnd(16)} ${median(id)} ms`);
      }

      const lcpKeys = Object.keys(samples).filter((k) => k.startsWith("lcp:"));
      if (lcpKeys.length > 0) {
        console.log("\n  LCP breakdown");
        for (const key of lcpKeys) {
          console.log(`    ${key.slice(4).padEnd(22)} ${median(key)} ms`);
        }
      }

      /* What the page actually costs a first-time visitor. The chain audit
         above says how the requests are ordered; this says how big they are,
         which is the other half and the one that stays true when the network
         is not the bottleneck. */
      const weight = lhr.audits["total-byte-weight"];
      if (weight?.numericValue) {
        console.log(
          `\n  Total transferred  ${(weight.numericValue / 1024).toFixed(1)} KiB`,
        );
        for (const row of findRows(weight.details).slice(0, 5)) {
          const kib =
            typeof row.totalBytes === "number"
              ? `${(row.totalBytes / 1024).toFixed(1)} KiB`
              : "?";
          const path = String(row.url ?? "").replace(/^https?:\/\/[^/]+/, "");
          console.log(`    ${kib.padStart(9)}  ${path || "/"}`);
        }
      }

      const blocking = findRows(lhr.audits["render-blocking-insight"]?.details);
      console.log(
        `\n  Render-blocking  ${blocking.length === 0 ? "none" : `${blocking.length} resource(s)`}`,
      );
      for (const row of blocking) console.log(`    ${row.url}`);

      const tree = findTree(
        CHAIN_AUDIT_IDS.map((id) => lhr.audits[id]).find(Boolean)?.details,
      );
      if (tree) {
        const lines = describeChain(tree.chains);
        const depth = lines.reduce((d, line) => Math.max(d, line.depth), 0);
        console.log(`\n  Requests         ${lines.length}`);
        console.log(`  Depth            ${depth + 1} levels`);
        console.log(`  Longest chain    ${median("chain")} ms`);
        for (const line of lines) console.log(`    ${line.text}`);
      }
      console.log();
    } finally {
      chrome.kill();
    }
  } finally {
    server?.kill();
  }
}

await main();
