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
      const run = await lighthouse(
        TARGET,
        { port: chrome.port, output: "json", logLevel: "error" },
        MOBILE ? undefined : desktopConfig,
      );
      const lhr = run?.lhr;
      if (!lhr) throw new Error("Lighthouse returned no result");

      const score = lhr.categories.performance?.score;
      console.log(`\n${TARGET}`);
      console.log(
        `Lighthouse ${lhr.lighthouseVersion} · ${MOBILE ? "mobile (throttled)" : "desktop"} preset\n`,
      );
      console.log(
        `  Performance  ${score === null || score === undefined ? "n/a" : Math.round(score * 100)}`,
      );
      for (const [id, label] of METRICS) {
        const audit = lhr.audits[id];
        console.log(`  ${label.padEnd(12)} ${audit?.displayValue ?? "n/a"}`);
      }

      const chainAudit = CHAIN_AUDIT_IDS.map((id) => lhr.audits[id]).find(
        Boolean,
      );
      const tree = chainAudit ? findTree(chainAudit.details) : null;
      if (!tree) {
        console.log("\n  (no critical-chain audit in this Lighthouse version)");
        return;
      }

      const lines = describeChain(tree.chains);
      const depth = lines.reduce(
        (deepest, line) => Math.max(deepest, line.depth),
        0,
      );
      console.log(`\n  Requests         ${lines.length}`);
      console.log(`  Depth            ${depth + 1} levels`);
      console.log(
        `  Longest chain    ${tree.longestChain?.duration ?? "n/a"} ms`,
      );
      for (const line of lines) console.log(`    ${line.text}`);
      console.log();
    } finally {
      chrome.kill();
    }
  } finally {
    server?.kill();
  }
}

await main();
