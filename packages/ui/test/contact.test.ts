import { beforeEach, describe, expect, it, vi } from "vitest";
import { mountContact } from "../src/site/contact.js";

/* Long enough for the address to finish typing: 15 characters at the 72ms
   cadence is comfortably past vi.waitFor's one-second default. The pace is
   the effect, so the test waits rather than the code hurrying. */
const TYPED_MS = 4000;

/* Reveal.astro's markup, class names included.

   The classes are inert as far as mountContact is concerned - it binds on
   [data-copy] and finds its parts by [data-typed] and [data-copied-badge] -
   but the fixture had drifted to a set (`sitefoot__contact`, `sitefoot__mail`)
   that no component has rendered since the control moved into Reveal.astro,
   while still claiming in a comment to be what the footer renders. A fixture
   that describes markup nobody emits tests the code against a page that does
   not exist. */
function control(label: string, value: string): string {
  return `
    <button class="reveal" type="button" data-copy="${value}"
            aria-label="Copy ${value}">
      <span class="reveal__label" aria-hidden="true">${label}</span>
      <span class="reveal__value" aria-hidden="true"><span data-typed></span><span class="reveal__caret"></span></span>
      <span class="reveal__copied" role="status" aria-live="polite" data-copied-badge></span>
    </button>`;
}

function markup(email = "m@tone.rip"): HTMLButtonElement {
  document.body.innerHTML = control("contact", email);
  const el = document.querySelector<HTMLButtonElement>("[data-copy]");
  if (!el) throw new Error("fixture is missing the control");
  return el;
}

function typed(): string {
  return document.querySelector("[data-typed]")?.textContent ?? "";
}

function badge(): string {
  return document.querySelector("[data-copied-badge]")?.textContent ?? "";
}

describe("mountContact", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    // Typing character-by-character is the default; the reduced-motion path
    // gets its own test.
    vi.stubGlobal(
      "matchMedia",
      vi.fn().mockReturnValue({ matches: false, addEventListener: vi.fn() }),
    );
  });

  it("reveals on the first click without copying", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    const el = markup();
    mountContact();
    el.click();

    expect(el.dataset.revealed).toBe("1");
    expect(writeText).not.toHaveBeenCalled();
    // A first click that copied would put the address on the clipboard of
    // anyone who merely wanted to see it.
    await vi.waitFor(() => expect(typed()).toBe("m@tone.rip"), TYPED_MS);
  });

  it("copies on the second click and confirms", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    const el = markup();
    mountContact();
    el.click();
    el.click();

    expect(writeText).toHaveBeenCalledWith("m@tone.rip");
    await vi.waitFor(() => {
      expect(badge()).toBe("Copied");
      expect(el.dataset.copied).toBe("1");
    });
  });

  it("skips the typing when reduced motion is asked for", () => {
    vi.stubGlobal(
      "matchMedia",
      vi.fn().mockReturnValue({ matches: true, addEventListener: vi.fn() }),
    );
    const el = markup();
    mountContact();
    el.click();

    expect(typed()).toBe("m@tone.rip");
  });

  it("does not throw when the clipboard is unavailable", async () => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: undefined,
    });

    const el = markup();
    mountContact();
    el.click();
    expect(() => el.click()).not.toThrow();
    // The address is on screen either way, so a refused copy costs nothing
    // but a manual selection.
    await vi.waitFor(() => expect(typed()).toBe("m@tone.rip"), TYPED_MS);
    expect(badge()).toBe("");
  });

  it("binds each control once, however often it is mounted", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    const el = markup();
    // Safe to call more than once per document by contract, so that a page
    // which grows a second region of controls needs no bookkeeping.
    mountContact();
    mountContact();
    mountContact();

    el.click();
    el.click();
    expect(writeText).toHaveBeenCalledTimes(1);
  });

  /* The footer puts two of these on one line - `cv` and `contact` - and both
     expand to something several times the width of their label. The row
     absorbs one of them by letting its hairlines retract (see
     site-footer.css), and below 620px the hairlines are hidden and there is
     nothing to give at all. Two open at once ran off the screen.

     So this is not a nicety, it is what makes that layout viable: the row
     only ever has to fit one address. */
  it("collapses any other revealed control when one is opened", async () => {
    document.body.innerHTML =
      control("cv", "ssh cv.tone.rip") + control("contact", "m@tone.rip");
    const [cv, contact] = Array.from(
      document.querySelectorAll<HTMLButtonElement>("[data-copy]"),
    );
    // A missing control would otherwise surface as "cannot read properties of
    // undefined (reading 'click')" several lines below, which reads as a bug
    // in mountContact rather than in this fixture.
    if (!cv || !contact) throw new Error("expected two [data-copy] controls");
    mountContact();

    cv.click();
    await vi.waitFor(() => expect(typed()).toBe("ssh cv.tone.rip"), TYPED_MS);

    contact.click();
    expect(cv.dataset.revealed).toBeUndefined();
    expect(contact.dataset.revealed).toBe("1");
    // Not just collapsed - emptied. A hidden element still holding the
    // address keeps it in the accessibility tree and in the page's text.
    expect(cv.querySelector("[data-typed]")?.textContent).toBe("");
  });
});
