/* Language switching, done in the browser.

   Both languages ship in the HTML: every translatable node carries `data-en`
   and `data-pt`, and switching is a `textContent` swap. That is unusual
   enough to be worth defending.

   The alternative - a `/pt/` route tree, or negotiating on a cookie - means
   either double the pages to keep in step or a response that varies per
   visitor and so caches badly at the edge. Here the document is identical for
   everyone, the CDN caches one copy, and the swap costs no network at all.
   The page ships rendered in English, so a crawler that runs no JavaScript
   still reads a complete page rather than an empty shell.

   The cost is page weight: every string is present twice. For a CV that is a
   few kilobytes before compression, and gzip is very good at two copies of
   almost the same text. When this stops being a CV it should become routes.

   Pair with `applyLang` on every navigation - see mountLang. */

import { readStored, writeStored } from "../storage.js";

export type SiteLang = "en" | "pt";

const STORAGE_KEY = "lang";
const DEFAULT_LANG: SiteLang = "en";

/** Fired on `window` after the language changes, for anything that has to re-render. */
export const LANG_CHANGE_EVENT = "tone:langchange";

function isLang(value: string | null): value is SiteLang {
  return value === "en" || value === "pt";
}

/** The visitor's choice, or English. */
export function readLang(): SiteLang {
  const stored = readStored(STORAGE_KEY);
  return isLang(stored) ? stored : DEFAULT_LANG;
}

/**
 * Swap every `[data-en][data-pt]` node under `root` to `lang`.
 *
 * Also updates `<html lang>`, which is what a screen reader picks its voice
 * from - getting that wrong is a worse bug than any of the visible text.
 */
export function applyLang(lang: SiteLang, root: ParentNode = document): void {
  document.documentElement.lang = lang === "pt" ? "pt-PT" : "en";

  for (const el of root.querySelectorAll<HTMLElement>("[data-en]")) {
    const next = el.dataset[lang];
    if (next !== undefined) el.textContent = next;
  }

  for (const button of root.querySelectorAll<HTMLElement>("[data-lang-set]")) {
    const owns = button.dataset.langSet === lang;
    button.setAttribute("aria-pressed", String(owns));
  }
}

/** Persist a choice and apply it everywhere. */
export function setLang(lang: SiteLang): void {
  writeStored(STORAGE_KEY, lang);
  applyLang(lang);
  window.dispatchEvent(
    new CustomEvent<{ lang: SiteLang }>(LANG_CHANGE_EVENT, {
      detail: { lang },
    }),
  );
}

/**
 * Apply the stored language and bind the toggle.
 *
 * Idempotent, and safe after every view transition: the freshly rendered
 * document is in English until this runs, so it has to run on every
 * navigation, not just the first.
 */
export function mountLang(root: ParentNode = document): void {
  applyLang(readLang(), root);

  for (const button of root.querySelectorAll<HTMLElement>("[data-lang-set]")) {
    if (button.dataset.langBound) continue;
    button.dataset.langBound = "1";
    button.addEventListener("click", () => {
      const next = button.dataset.langSet;
      if (isLang(next ?? null)) setLang(next as SiteLang);
    });
  }
}
