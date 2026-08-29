import { beforeEach, describe, expect, it, vi } from "vitest";
import { readStored, writeStored } from "../src/storage.js";

describe("storage", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("round-trips a value through localStorage", () => {
    writeStored("k", "v");
    expect(readStored("k")).toBe("v");
  });

  it("returns null for a key that was never set", () => {
    expect(readStored("missing")).toBeNull();
  });

  it("read returns null instead of throwing when localStorage.getItem throws", () => {
    vi.spyOn(window.localStorage, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(readStored("k")).toBeNull();
    vi.restoreAllMocks();
  });

  it("write silently no-ops instead of throwing when localStorage.setItem throws", () => {
    vi.spyOn(window.localStorage, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    expect(() => writeStored("k", "v")).not.toThrow();
    vi.restoreAllMocks();
  });
});
