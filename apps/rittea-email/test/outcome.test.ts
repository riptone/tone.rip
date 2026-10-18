import { describe, expect, it } from "vitest";
import { outcome } from "../src/outcome";

describe("outcome", () => {
  it("is delivered on 200", () => {
    expect(outcome(200)).toBe("delivered");
  });

  it("is ignored on 202", () => {
    expect(outcome(202)).toBe("ignored");
  });

  it.each([201, 204, 400, 401, 404, 500, 503])("fails on %i", (status) => {
    expect(outcome(status)).toBe("failed");
  });
});
