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

// A wait gives up with its test, at Vitest's 5 s per-test budget,
// rather than at Testing Library's own 1 s, as console's setup has it.
configure({ asyncUtilTimeout: 5_000 });
