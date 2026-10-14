import { vi } from "vitest";

/**
 * Stands an in-memory `localStorage` in for the browser's. Node 22 and
 * later define a `localStorage` global of their own, which reads as
 * undefined without `--localstorage-file` and shadows the one jsdom
 * provides, so a test reaching storage installs this instead.
 */
export function stubLocalStorage(): Storage {
  const entries = new Map<string, string>();
  const storage: Storage = {
    get length() {
      return entries.size;
    },
    clear: () => entries.clear(),
    getItem: (key) => entries.get(key) ?? null,
    key: (index) => [...entries.keys()][index] ?? null,
    removeItem: (key) => {
      entries.delete(key);
    },
    setItem: (key, value) => {
      entries.set(key, String(value));
    },
  };
  vi.stubGlobal("localStorage", storage);
  return storage;
}
