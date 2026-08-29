import { describe, expect, it } from "vitest";
import { needsPing, resolveTileStatus } from "../src/scripts/status-resolution";

// The resolution table from src/scripts/status-resolution.ts, one test per
// row. Kept in the same order as the comment there so a new rule added in one
// place is visibly missing from the other:
// - Worker says `up` -> tile is `up`.
// - Self-hosted + OAuth device offline -> tile is `down`.
// - Browser ping succeeds -> tile is `up`.
// - Public + ping fails -> tile is `down`.
// - Self-hosted + ping fails + visitor on tailnet -> tile is `down`.
// - Self-hosted + ping fails + visitor not on tailnet -> tile is `vpn`.
describe("resolveTileStatus", () => {
  it("is up when the server probe says up, regardless of anything else", () => {
    expect(
      resolveTileStatus({
        isSelfHosted: true,
        serverStatus: "up",
        tailnetDeviceOnline: false,
        pingOk: false,
        onTailnet: false,
      }),
    ).toBe("up");
  });

  it("is down for a self-hosted app when the Tailscale device is confirmed offline", () => {
    expect(
      resolveTileStatus({
        isSelfHosted: true,
        serverStatus: "down",
        tailnetDeviceOnline: false,
        pingOk: null,
        onTailnet: false,
      }),
    ).toBe("down");
  });

  it("is up when the browser ping succeeds, self-hosted or not", () => {
    expect(
      resolveTileStatus({
        isSelfHosted: true,
        serverStatus: "down",
        tailnetDeviceOnline: null,
        pingOk: true,
        onTailnet: false,
      }),
    ).toBe("up");
    expect(
      resolveTileStatus({
        isSelfHosted: false,
        serverStatus: "down",
        tailnetDeviceOnline: null,
        pingOk: true,
        onTailnet: false,
      }),
    ).toBe("up");
  });

  it("is down for a public app when the ping fails", () => {
    expect(
      resolveTileStatus({
        isSelfHosted: false,
        serverStatus: "down",
        tailnetDeviceOnline: null,
        pingOk: false,
        onTailnet: false,
      }),
    ).toBe("down");
  });

  it("is down for a self-hosted app when the ping fails and the visitor is on the tailnet", () => {
    expect(
      resolveTileStatus({
        isSelfHosted: true,
        serverStatus: "down",
        tailnetDeviceOnline: null,
        pingOk: false,
        onTailnet: true,
      }),
    ).toBe("down");
  });

  it("is vpn for a self-hosted app when the ping fails and the visitor is not on the tailnet", () => {
    expect(
      resolveTileStatus({
        isSelfHosted: true,
        serverStatus: "down",
        tailnetDeviceOnline: null,
        pingOk: false,
        onTailnet: false,
      }),
    ).toBe("vpn");
  });
});

describe("needsPing", () => {
  it("is false once the server already says up", () => {
    expect(
      needsPing({
        isSelfHosted: false,
        serverStatus: "up",
        tailnetDeviceOnline: null,
        onTailnet: true,
      }),
    ).toBe(false);
  });

  it("is false for a self-hosted app once the tailnet device is confirmed offline", () => {
    expect(
      needsPing({
        isSelfHosted: true,
        serverStatus: "down",
        tailnetDeviceOnline: false,
        onTailnet: true,
      }),
    ).toBe(false);
  });

  it("is false for a self-hosted app when the visitor is not on the tailnet", () => {
    // The ping cannot reach a tailnet-only host from outside it, and
    // resolveTileStatus answers `vpn` either way - so the request would only
    // buy an ERR_TUNNEL_CONNECTION_FAILED in the console.
    const input = {
      isSelfHosted: true,
      serverStatus: "down",
      tailnetDeviceOnline: null,
      onTailnet: false,
    } as const;
    expect(needsPing(input)).toBe(false);
    expect(resolveTileStatus({ ...input, pingOk: null })).toBe("vpn");
    expect(resolveTileStatus({ ...input, pingOk: false })).toBe("vpn");
  });

  it("is true otherwise", () => {
    expect(
      needsPing({
        isSelfHosted: true,
        serverStatus: "down",
        tailnetDeviceOnline: null,
        onTailnet: true,
      }),
    ).toBe(true);
    // A public app is reachable from anywhere, so the visitor's own tailnet
    // membership says nothing about it.
    expect(
      needsPing({
        isSelfHosted: false,
        serverStatus: "down",
        tailnetDeviceOnline: null,
        onTailnet: false,
      }),
    ).toBe(true);
  });
});
