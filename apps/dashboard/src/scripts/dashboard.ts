/**
 * Wires up the launcher page: collects the rendered app tiles, and connects
 * the pure logic in this directory (status-resolution.ts, tailnet.ts,
 * client-probe.ts, status-client.ts) plus @repo/ui's shared filter to the
 * actual DOM.
 * This module is intentionally "thin glue" - branching logic that's worth
 * unit testing lives in the modules it imports, not here.
 */
import { mountFilter } from "@repo/ui/site";
import { pingUrl } from "./client-probe";
import { fetchServerStatuses } from "./status-client";
import {
  needsPing,
  resolveTileStatus,
  type TileStatus,
} from "./status-resolution";
import { detectTailnetPresence } from "./tailnet";

const STATUS_REFRESH_MS = 90_000;
/**
 * Floor between two status sweeps, however they were triggered.
 *
 * Returning to the tab used to re-run the whole check immediately, so
 * flicking away and back fired a fresh round of probes against nine hosts
 * for no new information. Statuses do not change on the timescale of an
 * alt-tab.
 */
const STATUS_MIN_GAP_MS = 30_000;
/**
 * How long to wait for the server's view of everything.
 *
 * Longer than any single probe because it is not one: /api/status proxies to
 * apps/api, which probes the public hosts and asks Tailscale about the box,
 * so this number has to sit above that whole chain rather than above one
 * request. At 2500ms it sat *below* it, and the result was not an error -
 * it was `apps: []`, indistinguishable from "the server knows nothing",
 * which is why the tiles still looked right (each falls back to its own
 * browser probe) while the endpoint appeared to return an empty object.
 *
 * The chain is documented from the other end in apps/api's app-health.ts.
 * The three numbers only work as a set; change one and read the other two.
 */
const STATUS_TIMEOUT_MS = 5000;
/** A single probe from this browser, which is one hop and should be quick. */
const CLIENT_TIMEOUT_MS = 1200;
/**
 * The same, for a host out on the public internet rather than on the tailnet.
 *
 * Its own constant rather than borrowing STATUS_TIMEOUT_MS, which it used to
 * do back when the two happened to be equal. They are not the same kind of
 * measurement - one bounds a browser's single request, the other bounds a
 * three-hop server chain - and letting one number stand for both meant
 * raising the chain's budget would have quietly doubled how long a tile sits
 * on "checking".
 */
const PUBLIC_PING_TIMEOUT_MS = 2500;
const TAILNET_IP_TIMEOUT_MS = 700;
const TAILNET_IP_CACHE_MS = 30_000;

const STATUS_LABEL: Record<TileStatus, string> = {
  up: "up",
  down: "down",
  vpn: "vpn",
  checking: "...",
  unknown: "?",
};

interface Tile {
  el: HTMLAnchorElement;
  href: string;
  name: string;
  tags: string;
  isSelfHosted: boolean;
  probePath: string;
  statusEl: HTMLElement;
  statusTextEl: HTMLElement;
}

function collectTiles(): Tile[] {
  return Array.from(
    document.querySelectorAll<HTMLAnchorElement>("a.tile[data-name]"),
  ).map((el) => {
    const statusEl = el.querySelector<HTMLElement>(".status");
    const statusTextEl = el.querySelector<HTMLElement>(".status .text");
    if (!statusEl || !statusTextEl) {
      throw new Error("tile missing .status or .status .text");
    }
    const tags = el.dataset.tags ?? "";
    return {
      el,
      href: el.dataset.href ?? el.href,
      name: el.dataset.name ?? "",
      tags,
      isSelfHosted: tags.includes("Self-Hosted"),
      probePath: el.dataset.probePath || "/",
      statusEl,
      statusTextEl,
    };
  });
}

function setTileStatus(tile: Tile, status: TileStatus): void {
  tile.statusEl.dataset.status = status;
  tile.statusEl.setAttribute("aria-label", `Status: ${status}`);
  tile.statusTextEl.textContent = STATUS_LABEL[status];
}

let tailnetPromise: Promise<boolean> | null = null;
let tailnetPromiseAt = 0;

/** Caches the (slow-ish) WebRTC-based tailnet detection for a short window so every status refresh doesn't re-run it. */
function cachedTailnetPresence(): Promise<boolean> {
  const now = Date.now();
  if (tailnetPromise && now - tailnetPromiseAt < TAILNET_IP_CACHE_MS) {
    return tailnetPromise;
  }
  tailnetPromiseAt = now;
  tailnetPromise = detectTailnetPresence({ timeoutMs: TAILNET_IP_TIMEOUT_MS });
  return tailnetPromise;
}

export function initDashboard(): void {
  // Search, tag and count are @repo/ui's now - the same control the work
  // list uses. All this app supplies is the markup and which selectors mean
  // what, which is exactly the amount of coupling that should exist.
  mountFilter();

  const refreshBtn = document.getElementById("refresh");
  const tiles = collectTiles();

  const checkStatuses = async () => {
    const visible = tiles.filter((tile) => !tile.el.hidden);
    if (visible.length === 0) return;
    for (const tile of visible) setTileStatus(tile, "checking");

    const [server, onTailnet] = await Promise.all([
      fetchServerStatuses(STATUS_TIMEOUT_MS),
      cachedTailnetPresence(),
    ]);

    await Promise.all(
      visible.map(async (tile) => {
        const serverStatus = server.apps.get(tile.href);
        const tailnetDeviceOnline = server.tailnetDeviceOnline;
        // Shared with resolveTileStatus so the two cannot disagree about
        // when a ping would tell us anything.
        const pingRequired = needsPing({
          isSelfHosted: tile.isSelfHosted,
          serverStatus,
          tailnetDeviceOnline,
          onTailnet,
        });
        const pingOk = pingRequired
          ? await pingUrl(
              tile.href,
              tile.isSelfHosted ? CLIENT_TIMEOUT_MS : PUBLIC_PING_TIMEOUT_MS,
              tile.probePath,
            )
          : null;

        setTileStatus(
          tile,
          resolveTileStatus({
            isSelfHosted: tile.isSelfHosted,
            serverStatus,
            tailnetDeviceOnline,
            pingOk,
            onTailnet,
          }),
        );
      }),
    );
  };

  let statusRunning = false;
  let lastCheckAt = 0;
  const runStatusCheck = (force = false) => {
    if (statusRunning) return;
    if (!force && Date.now() - lastCheckAt < STATUS_MIN_GAP_MS) return;
    statusRunning = true;
    lastCheckAt = Date.now();
    checkStatuses().finally(() => {
      statusRunning = false;
    });
  };

  if (refreshBtn) {
    refreshBtn.addEventListener("click", () => {
      refreshBtn.classList.remove("spinning");
      void refreshBtn.offsetWidth;
      refreshBtn.classList.add("spinning");
      // Explicitly asked for, so it skips the floor - that is what the
      // button is for.
      runStatusCheck(true);
    });
    refreshBtn.addEventListener("animationend", () =>
      refreshBtn.classList.remove("spinning"),
    );
  }

  let intervalId: ReturnType<typeof setInterval> | null = null;
  const startInterval = () => {
    if (intervalId !== null) return;
    intervalId = setInterval(runStatusCheck, STATUS_REFRESH_MS);
  };
  const stopInterval = () => {
    if (intervalId === null) return;
    clearInterval(intervalId);
    intervalId = null;
  };

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      stopInterval();
    } else if (intervalId === null) {
      // Not forced: coming back to the tab is not new information, and
      // STATUS_MIN_GAP_MS decides whether enough time has passed.
      runStatusCheck();
      startInterval();
    }
  });

  runStatusCheck(true);
  startInterval();
}
