import "@testing-library/jest-dom/vitest";

import { configure } from "@testing-library/react";
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

// A wait gives up with its test, at Vitest's 5 s per-test budget, which
// the worker count is sized against (#1528), rather than at Testing
// Library's own 1 s. Each wait is for a condition its test needs, and
// on a starved host the slowest took 0.9 to 1.8 s on one CPU (#1551):
// a second, shorter clock inside the test's own measured the host.
configure({ asyncUtilTimeout: 5_000 });
