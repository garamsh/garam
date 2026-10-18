import "@testing-library/jest-dom/vitest";

import { vi } from "vitest";

// jsdom ships no `matchMedia`; the sidebar's breakpoint hook needs one.
vi.stubGlobal("matchMedia", (query: string) => ({
  matches: false,
  media: query,
  addEventListener: () => {},
  removeEventListener: () => {},
}));

// jsdom implements neither pointer capture nor `scrollIntoView`; the
// Radix menu and select primitives call both while opening.
Element.prototype.hasPointerCapture = () => false;
Element.prototype.setPointerCapture = () => {};
Element.prototype.releasePointerCapture = () => {};
Element.prototype.scrollIntoView = () => {};

// jsdom lays nothing out, so a Range has no rectangles; CodeMirror
// measures the cursor with them after every edit.
// An element's own list is jsdom's empty one.
Range.prototype.getClientRects = () =>
  document.createElement("span").getClientRects();
Range.prototype.getBoundingClientRect = () => new DOMRect();
