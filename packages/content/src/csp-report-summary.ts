export interface CspReportSummary {
  path: string;
  size: number;
  malformed: boolean;
  disposition: string | null;
  effectiveDirective: string | null;
  violatedDirective: string | null;
  blockedOrigin: string | null;
  documentPath: string | null;
  sourceFilePath: string | null;
}

function readString(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function sanitizeOrigin(value: unknown): string | null {
  const raw = readString(value);
  if (!raw || raw === "inline" || raw === "eval") return raw;
  try {
    return new URL(raw).origin;
  } catch {
    return raw;
  }
}

function sanitizePath(value: unknown): string | null {
  const raw = readString(value);
  if (!raw) return null;
  try {
    return new URL(raw).pathname;
  } catch {
    return raw.startsWith("/") ? raw : null;
  }
}

function malformedSummary(reportPath: string, size: number): CspReportSummary {
  return {
    path: reportPath,
    size,
    malformed: true,
    disposition: null,
    effectiveDirective: null,
    violatedDirective: null,
    blockedOrigin: null,
    documentPath: null,
    sourceFilePath: null,
  };
}

/**
 * Ported from tone.rip's src/utils/csp-report.ts. Takes an already-validated
 * (via @repo/validation's cspReportBodySchema) report body and reduces it to a
 * loggable summary - sanitizing origins/paths so we never log full querystrings
 * or credentials that might leak into a blocked-uri.
 */
/* There used to be an `isSelfInflictedTransitionReport` filter here, dropping
   the inline-style violations `<ClientRouter />` provoked on every navigation
   by parsing the next page with DOMParser under this page's policy.

   It went with ClientRouter. Keeping it would mean the sink silently swallows
   every inline-style violation that names a source file - which, now that
   nothing is generating them on purpose, is the shape a real one would take.
   A filter written for a cause that no longer exists stops being noise
   reduction and becomes a blind spot. */

export function summarizeCspReport(
  body: string,
  reportPath: string,
): CspReportSummary {
  try {
    const parsed = JSON.parse(body) as {
      "csp-report"?: Record<string, unknown>;
    };
    const report = parsed?.["csp-report"];
    if (!report || typeof report !== "object") {
      return malformedSummary(reportPath, body.length);
    }

    return {
      path: reportPath,
      size: body.length,
      malformed: false,
      disposition: readString(report.disposition),
      effectiveDirective: readString(report["effective-directive"]),
      violatedDirective: readString(report["violated-directive"]),
      blockedOrigin: sanitizeOrigin(report["blocked-uri"]),
      documentPath: sanitizePath(report["document-uri"]),
      sourceFilePath: sanitizePath(report["source-file"]),
    };
  } catch {
    return malformedSummary(reportPath, body.length);
  }
}
