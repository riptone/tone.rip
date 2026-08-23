import { describe, expect, it } from "vitest";
import { summarizeCspReport } from "../src/csp-report-summary";

describe("summarizeCspReport", () => {
  it("sanitizes a real CSP report to origins/paths, dropping querystrings", () => {
    const body = JSON.stringify({
      "csp-report": {
        "document-uri": "https://tone.rip/?utm_source=x",
        "violated-directive": "script-src",
        "blocked-uri": "https://evil.example.com/payload.js?x=1",
        "source-file": "https://tone.rip/scripts/main.js",
      },
    });
    const summary = summarizeCspReport(body, "/api/csp-report");
    expect(summary.malformed).toBe(false);
    expect(summary.documentPath).toBe("/");
    expect(summary.blockedOrigin).toBe("https://evil.example.com");
    expect(summary.sourceFilePath).toBe("/scripts/main.js");
  });

  it("flags a payload missing the csp-report envelope as malformed", () => {
    const summary = summarizeCspReport(
      JSON.stringify({ foo: "bar" }),
      "/api/csp-report",
    );
    expect(summary.malformed).toBe(true);
  });

  it("flags invalid JSON as malformed instead of throwing", () => {
    const summary = summarizeCspReport("not json", "/api/csp-report");
    expect(summary.malformed).toBe(true);
    expect(summary.size).toBe(8);
  });

  // The sink used to run these through `isSelfInflictedTransitionReport`,
  // which dropped inline-style violations that named a source file - the
  // signature ClientRouter produced twice per navigation by parsing the next
  // page with DOMParser under this page's policy. Both are gone. This is the
  // shape that filter would have swallowed, and it must now summarize like
  // any other report.
  it("summarizes an inline-style violation that names a source file", () => {
    const summary = summarizeCspReport(
      JSON.stringify({
        "csp-report": {
          "effective-directive": "style-src-elem",
          "blocked-uri": "inline",
          "source-file": "https://tone.rip/_astro/injected.js",
          "document-uri": "https://tone.rip/work",
        },
      }),
      "/csp-report",
    );
    expect(summary.malformed).toBe(false);
    expect(summary.effectiveDirective).toBe("style-src-elem");
    expect(summary.blockedOrigin).toBe("inline");
    expect(summary.sourceFilePath).toBe("/_astro/injected.js");
  });
});
