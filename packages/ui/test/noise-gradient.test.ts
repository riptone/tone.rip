import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HostToWorker, WorkerToHost } from "../src/gradient/field.js";
import { mountNoiseGradient } from "../src/gradient/noise-gradient.js";

/* The field is decorative. The things that run after it are not.
 *
 * apps/web's SiteLayout entry calls `syncField()` and then mounts the
 * contact reveal, the context menu, the /work filter and the language
 * switch - and those five calls minify into a single comma expression, so
 * anything thrown out of the first one silently removes the other four. A
 * visitor loses the language switch because the background could not paint.
 *
 * That shipped: an embedded runtime returned a `Worker` stub whose
 * `postMessage` was missing, `new Worker()` succeeded, and the first frame
 * threw `TypeError: o.postMessage is not a function` out of the mount.
 *
 * These assert the contract the fix establishes - the mount does not throw,
 * and it does not touch the host on the way out - rather than reproducing
 * that trace. jsdom has no matchMedia and no observers, so an unguarded
 * mount here dies at whichever of those it reaches first; the browser is
 * where it dies at `postMessage`. What pins the difference between the guard
 * and a bare `try` around the constructor is the error each case reports:
 * only the shape check can fail a stub that constructs perfectly well. */

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.body.replaceChildren();
});

/** A host with server-rendered content in it, to prove that content survives. */
function makeHost(): { host: HTMLElement; child: HTMLElement } {
  const host = document.createElement("div");
  const child = document.createElement("span");
  child.textContent = "server-rendered";
  host.append(child);
  document.body.append(host);
  return { host, child };
}

interface BrokenWorker {
  Stub: new (...args: never[]) => unknown;
  /** What the guard should have caught, and therefore what it should report. */
  reports: (error: unknown) => void;
}

const BROKEN_WORKERS: Record<string, BrokenWorker> = {
  /* The case that actually shipped: constructs, then has no method to call.
     A `try` around `new Worker` alone catches nothing here. */
  "a stub with no postMessage": {
    Stub: class {
      terminate(): void {}
    },
    reports: (error) => {
      expect(error).toBeInstanceOf(TypeError);
      expect(String(error)).toContain("postMessage");
    },
  },
  /* Constructing throws outright - a CSP that refuses the worker URL, say. */
  "a constructor that throws": {
    Stub: class {
      constructor() {
        throw new DOMException("blocked", "SecurityError");
      }
    },
    reports: (error) => {
      expect(error).toBeInstanceOf(DOMException);
    },
  },
};

describe("mountNoiseGradient without a usable Worker", () => {
  for (const [name, { Stub, reports }] of Object.entries(BROKEN_WORKERS)) {
    describe(name, () => {
      it("returns an inert handle instead of throwing", () => {
        vi.stubGlobal("Worker", Stub);
        vi.spyOn(console, "warn").mockImplementation(() => {});
        const { host } = makeHost();

        const handle = mountNoiseGradient(host);

        // Both are part of the contract, and `destroy()` is the one that
        // would reach `terminate()` on a stub that never had it.
        expect(() => handle.update({ progress: 0.5 })).not.toThrow();
        expect(() => handle.destroy()).not.toThrow();
      });

      it("leaves the host exactly as it was rendered", () => {
        vi.stubGlobal("Worker", Stub);
        vi.spyOn(console, "warn").mockImplementation(() => {});
        const { host, child } = makeHost();

        mountNoiseGradient(host);

        // `replaceChildren()` and the canvas come after the guard, so a
        // failed mount has to be invisible rather than a blank hole where
        // the server's markup used to be.
        expect([...host.children]).toEqual([child]);
        expect(host.classList.contains("ntg")).toBe(false);
        expect(host.querySelector("canvas")).toBeNull();
      });

      it("warns with the reason, and does not log an error", () => {
        vi.stubGlobal("Worker", Stub);
        const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
        const error = vi.spyOn(console, "error").mockImplementation(() => {});
        const { host } = makeHost();

        mountNoiseGradient(host);

        // `warn`, not `error`: both e2e suites fail the run on a console
        // error. A browser that cannot host a worker is not a regression in
        // this code, and making it one trains everybody to ignore that
        // assertion - which is the assertion that catches the real ones.
        expect(warn).toHaveBeenCalledOnce();
        expect(error).not.toHaveBeenCalled();
        reports(warn.mock.calls[0]?.[1]);
      });
    });
  }
});

/* The rest of the mount, with the browser it needs stood up around it.
 *
 * jsdom has no observers, no matchMedia and no 2D context, which is why this
 * module went untested and therefore uncounted - and why adding the guard
 * tests above, on their own, *lowered* @repo/ui's coverage: they pulled a
 * 350-statement module into the denominator and covered one branch of it.
 *
 * The stubs below are small because the module asks for little: a worker it
 * posts state to, two observers it re-renders on, and a media query per
 * concern. What they buy is the ability to assert the things a browser would
 * otherwise be the only witness to - that a frame arriving with nowhere to
 * draw is still closed rather than leaked, and that `destroy()` really does
 * let go of all four subscriptions. */

class FakeWorker {
  static last: FakeWorker | null = null;
  readonly posted: HostToWorker[] = [];
  terminated = 0;
  onmessage: ((event: { data: WorkerToHost }) => void) | null = null;

  constructor() {
    FakeWorker.last = this;
  }

  postMessage(message: HostToWorker): void {
    this.posted.push(message);
  }

  terminate(): void {
    this.terminated += 1;
  }

  /** The last state the host asked for, which is the only one on screen. */
  get lastState() {
    const last = this.posted.at(-1);
    if (last?.type !== "state") throw new Error("no state posted");
    return last.state;
  }
}

class FakeObserver {
  static readonly instances: FakeObserver[] = [];
  readonly observed: Element[] = [];
  disconnected = 0;

  constructor(readonly callback: (entries: unknown[]) => void) {
    FakeObserver.instances.push(this);
  }

  observe(target: Element): void {
    this.observed.push(target);
  }

  unobserve(): void {}
  disconnect(): void {
    this.disconnected += 1;
  }
  takeRecords(): unknown[] {
    return [];
  }
}

/** `[resize, intersection]`, in the order mountNoiseGradient constructs them. */
function observers(): [FakeObserver, FakeObserver] {
  const [resize, intersection] = FakeObserver.instances;
  if (!resize || !intersection) throw new Error("observers not constructed");
  return [resize, intersection];
}

const HOST_SIZE = { width: 320, height: 200 };

function mountedHost(): HTMLElement {
  const host = document.createElement("div");
  // jsdom lays nothing out, so every element measures 0x0 - and `send()`
  // returns early on a zero-sized host, which would make every assertion
  // below pass without a single frame ever having been asked for.
  Object.defineProperty(host, "clientWidth", {
    value: HOST_SIZE.width,
    configurable: true,
  });
  Object.defineProperty(host, "clientHeight", {
    value: HOST_SIZE.height,
    configurable: true,
  });
  document.body.append(host);
  return host;
}

describe("mountNoiseGradient with a working Worker", () => {
  beforeEach(() => {
    FakeWorker.last = null;
    FakeObserver.instances.length = 0;
    vi.useFakeTimers();
    vi.stubGlobal("Worker", FakeWorker);
    vi.stubGlobal("ResizeObserver", FakeObserver);
    vi.stubGlobal("IntersectionObserver", FakeObserver);
    vi.stubGlobal("matchMedia", (media: string) => ({
      media,
      matches: false,
      addEventListener() {},
      removeEventListener() {},
    }));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  /** Let `schedule()`'s coalescing window elapse. */
  function settle(): void {
    vi.advanceTimersByTime(100);
  }

  function worker(): FakeWorker {
    if (!FakeWorker.last) throw new Error("no worker constructed");
    return FakeWorker.last;
  }

  it("takes the host over and posts the opening frame", () => {
    const host = mountedHost();
    const stale = document.createElement("span");
    host.append(stale);

    mountNoiseGradient(host, { ramp: "glacier" });

    expect(host.classList.contains("ntg")).toBe(true);
    expect(host.getAttribute("aria-hidden")).toBe("true");
    expect(host.contains(stale)).toBe(false);
    expect(host.querySelector("canvas.ntg__canvas")).not.toBeNull();
    expect(host.querySelector("div.ntg__grain")).not.toBeNull();

    // Synchronous, not scheduled: the field has to be asked for before the
    // first paint, or the element is an empty box until something else moves.
    expect(worker().posted).toHaveLength(1);
    expect(worker().lastState).toMatchObject(HOST_SIZE);
  });

  it("re-renders when the host resizes or leaves the viewport", () => {
    const host = mountedHost();
    mountNoiseGradient(host);
    const [resize, intersection] = observers();
    expect(resize.observed).toEqual([host]);
    expect(intersection.observed).toEqual([host]);

    resize.callback([]);
    settle();
    expect(worker().posted).toHaveLength(2);

    // Scrolling the field off screen has to reach the worker: that is what
    // stops it rendering 30 frames a second for nobody.
    intersection.callback([{ isIntersecting: false }]);
    settle();
    expect(worker().lastState.visible).toBe(false);
  });

  it("carries an update through to the next frame", () => {
    const host = mountedHost();
    const handle = mountNoiseGradient(host, { progress: 0 });

    handle.update({ progress: 0.5 });
    settle();

    expect(worker().lastState.progress).toBe(0.5);
  });

  it("coalesces a burst of updates into one frame", () => {
    const host = mountedHost();
    const handle = mountNoiseGradient(host);
    const opening = worker().posted.length;

    // A scroll produces one of these per frame; the point of the coalescing
    // window is that the worker sees one state, not thirty.
    for (let i = 0; i < 30; i += 1) handle.update({ progress: i / 30 });
    settle();

    expect(worker().posted).toHaveLength(opening + 1);
  });

  it("closes a frame it has nowhere to draw", () => {
    // jsdom has no 2D context, which is the same position as a browser that
    // refuses one. The bitmap still has to be released - it is off-heap, and
    // dropping the reference does not free it.
    const host = mountedHost();
    mountNoiseGradient(host);
    const close = vi.fn();

    worker().onmessage?.({
      data: {
        type: "frame",
        bitmap: { width: 32, height: 20, close } as unknown as ImageBitmap,
        width: HOST_SIZE.width,
        height: HOST_SIZE.height,
        dpr: 1,
        progress: 0,
        profile: new Int16Array(0),
      } as WorkerToHost,
    });

    expect(close).toHaveBeenCalledOnce();
  });

  it("reports a worker error instead of throwing on it", () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const host = mountedHost();
    mountNoiseGradient(host);

    expect(() =>
      worker().onmessage?.({
        data: { type: "error", message: "OffscreenCanvas unavailable" },
      }),
    ).not.toThrow();
    expect(error).toHaveBeenCalledWith(
      "[noise-gradient] worker:",
      "OffscreenCanvas unavailable",
    );
  });

  it("lets go of everything on destroy", () => {
    const host = mountedHost();
    const handle = mountNoiseGradient(host);
    const [resize, intersection] = observers();

    handle.destroy();

    expect(worker().terminated).toBe(1);
    expect(resize.disconnected).toBe(1);
    expect(intersection.disconnected).toBe(1);
    expect(host.children).toHaveLength(0);
    expect(host.classList.contains("ntg")).toBe(false);

    // Idempotent, and silent afterwards: a second destroy is what a
    // re-entrant teardown looks like, and a post after it would be a leak.
    const posted = worker().posted.length;
    handle.destroy();
    handle.update({ progress: 1 });
    settle();
    expect(worker().terminated).toBe(1);
    expect(worker().posted).toHaveLength(posted);
  });
});
