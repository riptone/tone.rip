export type TileStatus = "up" | "down" | "vpn" | "checking" | "unknown";
export type ServerAppStatus = "up" | "down" | "unknown";

interface ResolveTileStatusInput {
  isSelfHosted: boolean;
  serverStatus: ServerAppStatus | undefined;
  tailnetDeviceOnline: boolean | null;
  /** null when no browser ping was performed (short-circuited by an earlier signal). */
  pingOk: boolean | null;
  onTailnet: boolean;
}

/**
 * Combines apps/api's server-side probe, the (optional) Tailscale device
 * status, and the visitor's own browser ping into a single tile status.
 *
 * A pure function rather than a branch inside the status sweep, so the table
 * below can be tested directly - six rules with three inputs each is the kind
 * of thing that is only ever wrong in the case nobody clicked through. This
 * comment is the table; there is no other copy of it.
 *
 * - Worker says `up` -> tile is `up`.
 * - Self-hosted + OAuth device offline -> tile is `down`.
 * - Browser ping succeeds -> tile is `up`.
 * - Public + ping fails -> tile is `down`.
 * - Self-hosted + ping fails + visitor on tailnet -> tile is `down`.
 * - Self-hosted + ping fails + visitor not on tailnet -> tile is `vpn`.
 */
export function resolveTileStatus(input: ResolveTileStatusInput): TileStatus {
  if (input.serverStatus === "up") return "up";
  if (input.isSelfHosted && input.tailnetDeviceOnline === false) return "down";
  if (!input.isSelfHosted) return input.pingOk ? "up" : "down";
  if (input.pingOk) return "up";
  return input.onTailnet ? "down" : "vpn";
}

/**
 * Whether a browser ping is needed at all, given the signals already known.
 *
 * The `onTailnet` case is worth spelling out. A self-hosted app is only
 * reachable from inside the tailnet, so when the visitor's own browser is not
 * on it, the ping cannot succeed - and `resolveTileStatus` returns `vpn` for
 * that combination whether `pingOk` is `false` or `null`. The request is
 * therefore provably redundant, and skipping it is not a heuristic.
 *
 * It is also the difference between a clean console and eight
 * `ERR_TUNNEL_CONNECTION_FAILED` lines per refresh: a failed connection is
 * logged by the browser's network stack, and no amount of `try`/`catch`
 * around `fetch` suppresses it. The only way not to see the error is not to
 * make the request.
 */
export function needsPing(
  input: Pick<
    ResolveTileStatusInput,
    "isSelfHosted" | "serverStatus" | "tailnetDeviceOnline" | "onTailnet"
  >,
): boolean {
  if (input.serverStatus === "up") return false;
  if (input.isSelfHosted && input.tailnetDeviceOnline === false) return false;
  if (input.isSelfHosted && !input.onTailnet) return false;
  return true;
}
