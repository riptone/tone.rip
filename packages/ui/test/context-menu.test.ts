import { beforeEach, describe, expect, it, vi } from "vitest";

/* The behaviour under test binds document-level listeners once at module
   scope - that is deliberate (a page has one context menu, and rebinding it
   on every view transition would stack handlers), but it means each test
   needs its own copy of the module. */
async function fresh() {
  vi.resetModules();
  return await import("../src/site/context-menu.js");
}

/**
 * The markup ContextMenu.astro renders, by hand.
 *
 * Kept deliberately literal rather than generated: this is the contract
 * between the component and the script, and a test that built it from the
 * same helper the component uses would not notice the two drifting apart.
 */
function markup(): HTMLElement {
  document.body.innerHTML = `
    <button id="opener" type="button">opener</button>
    <input id="field" />
    <div class="ctxmenu" role="menu" hidden data-context-menu>
      <a class="ctxmenu__item" role="menuitem" tabindex="-1" href="/somewhere">
        <span class="ctxmenu__label" data-ctx-label>source</span>
      </a>
      <div class="ctxmenu__sep" role="separator"></div>
      <button class="ctxmenu__item" role="menuitem" type="button" tabindex="-1"
              data-ctx-copy="m@tone.rip">
        <span class="ctxmenu__label" data-ctx-label>copy email</span>
      </button>
      <button class="ctxmenu__item" role="menuitem" type="button" tabindex="-1"
              data-ctx-copy="ssh cv.tone.rip">
        <span class="ctxmenu__label" data-ctx-label>ssh</span>
      </button>
    </div>`;
  const menu = document.querySelector<HTMLElement>("[data-context-menu]");
  if (!menu) throw new Error("fixture is missing the menu");
  return menu;
}

function rightClick(
  target: EventTarget,
  init: MouseEventInit = {},
): MouseEvent {
  const event = new MouseEvent("contextmenu", {
    bubbles: true,
    cancelable: true,
    clientX: 120,
    clientY: 140,
    ...init,
  });
  target.dispatchEvent(event);
  return event;
}

function labels(): string[] {
  return Array.from(
    document.querySelectorAll<HTMLElement>("[role='menuitem']"),
  ).map((item) => item.textContent?.trim() ?? "");
}

describe("mountContextMenu", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
  });

  it("does nothing when the page has no menu", async () => {
    const { mountContextMenu } = await fresh();
    document.body.innerHTML = "<p>no menu here</p>";
    expect(() => mountContextMenu()).not.toThrow();
    expect(rightClick(document.body).defaultPrevented).toBe(false);
  });

  it("opens at the pointer and takes over the native menu", async () => {
    const { mountContextMenu } = await fresh();
    const menu = markup();
    mountContextMenu();

    const event = rightClick(document.body, { clientX: 120, clientY: 140 });

    expect(event.defaultPrevented).toBe(true);
    expect(menu.hidden).toBe(false);
    expect(menu.style.left).toBe("120px");
    expect(menu.style.top).toBe("140px");
  });

  it("keeps the menu inside the viewport", async () => {
    const { mountContextMenu } = await fresh();
    const menu = markup();
    mountContextMenu();

    // Far outside on both axes. jsdom reports a zero-sized box, so the clamp
    // reduces to "one margin in from the far edge" - which is the part worth
    // pinning down anyway: the menu must never be positioned off-screen.
    rightClick(document.body, { clientX: 99_999, clientY: 99_999 });

    expect(Number.parseFloat(menu.style.left)).toBeLessThanOrEqual(
      window.innerWidth,
    );
    expect(Number.parseFloat(menu.style.top)).toBeLessThanOrEqual(
      window.innerHeight,
    );
  });

  it("yields to the native menu on shift, and inside text fields", async () => {
    const { mountContextMenu } = await fresh();
    const menu = markup();
    mountContextMenu();

    // The escape hatch. Every site that replaces this control needs one.
    expect(rightClick(document.body, { shiftKey: true }).defaultPrevented).toBe(
      false,
    );
    expect(menu.hidden).toBe(true);

    // And the place the native menu genuinely earns its keep.
    const field = document.querySelector("#field");
    if (!field) throw new Error("fixture is missing the input");
    expect(rightClick(field).defaultPrevented).toBe(false);
    expect(menu.hidden).toBe(true);
  });

  it("survives being raised on the document itself", async () => {
    // A contextmenu event raised on the document has the Document as its
    // target, which has no `closest`. Casting instead of type-testing threw
    // here and silently killed the handler.
    const { mountContextMenu } = await fresh();
    const menu = markup();
    mountContextMenu();

    rightClick(document);

    expect(menu.hidden).toBe(false);
  });

  it("focuses the first item, wraps with the arrows, and closes on Escape", async () => {
    const { mountContextMenu } = await fresh();
    const menu = markup();
    mountContextMenu();

    const opener = document.querySelector<HTMLElement>("#opener");
    opener?.focus();
    rightClick(document.body);

    const items = Array.from(
      document.querySelectorAll<HTMLElement>("[role='menuitem']"),
    );
    expect(document.activeElement).toBe(items[0]);

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown" }));
    expect(document.activeElement).toBe(items[1]);

    // Up from the top is the bottom: a menu is a ring, not a list you can
    // fall off the front of.
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowUp" }));
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowUp" }));
    expect(document.activeElement).toBe(items[items.length - 1]);

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(menu.hidden).toBe(true);
    expect(document.activeElement).toBe(opener);
  });

  it("closes when the pointer goes down outside it", async () => {
    const { mountContextMenu } = await fresh();
    const menu = markup();
    mountContextMenu();
    rightClick(document.body);

    menu.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    expect(menu.hidden).toBe(false);

    document.body.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    expect(menu.hidden).toBe(true);
  });

  it("copies an item's value instead of navigating, and confirms in place", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    const { mountContextMenu } = await fresh();
    markup();
    mountContextMenu();
    rightClick(document.body);

    const item = document.querySelector<HTMLElement>("[data-ctx-copy]");
    if (!item) throw new Error("fixture is missing a copy item");
    const click = new MouseEvent("click", { bubbles: true, cancelable: true });
    item.dispatchEvent(click);

    expect(click.defaultPrevented).toBe(true);
    expect(writeText).toHaveBeenCalledWith("m@tone.rip");

    // The confirmation is the item changing its own word - see the CSS note
    // on why there is no separate toast.
    await vi.waitFor(() => expect(labels()).toContain("copied"));
    expect(item.hasAttribute("data-copied")).toBe(true);
  });

  it("lets a link item navigate", async () => {
    const { mountContextMenu } = await fresh();
    const menu = markup();
    mountContextMenu();
    rightClick(document.body);

    const link = document.querySelector<HTMLElement>("a[role='menuitem']");
    const click = new MouseEvent("click", { bubbles: true, cancelable: true });
    link?.dispatchEvent(click);

    expect(click.defaultPrevented).toBe(false);
    expect(menu.hidden).toBe(true);
  });
});
