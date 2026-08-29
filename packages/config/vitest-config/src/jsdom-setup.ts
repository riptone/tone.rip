/* jsdom (as wired up by this Node/Bun toolchain) doesn't provide a working
   `window.localStorage` out of the box - Node's own experimental global
   `localStorage` shadows jsdom's and requires CLI flags we don't want to
   force on every contributor. Install a tiny in-memory Storage-compatible
   polyfill instead, scoped to the test environment only. */

class MemoryStorage implements Storage {
  private store = new Map<string, string>();

  get length(): number {
    return this.store.size;
  }

  clear(): void {
    this.store.clear();
  }

  getItem(key: string): string | null {
    return this.store.has(key) ? (this.store.get(key) as string) : null;
  }

  key(index: number): string | null {
    return Array.from(this.store.keys())[index] ?? null;
  }

  removeItem(key: string): void {
    this.store.delete(key);
  }

  setItem(key: string, value: string): void {
    this.store.set(key, String(value));
  }
}

const hasWorkingLocalStorage = (): boolean => {
  try {
    return typeof window.localStorage?.getItem === "function";
  } catch {
    return false;
  }
};

if (typeof window !== "undefined" && !hasWorkingLocalStorage()) {
  const storage = new MemoryStorage();
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: storage,
  });
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: storage,
  });
}
