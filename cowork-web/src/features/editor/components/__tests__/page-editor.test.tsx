import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// The provider a test drives by hand, and an editor that renders
// nothing of Tiptap's, which needs no socket to prove these.
const provider = vi.hoisted(() => ({
  last: null as null | {
    emit: (event: string, payload: object) => void;
    permissionDeniedHandler: () => void;
  },
}));

vi.mock("y-websocket", () => ({
  WebsocketProvider: class {
    handlers = new Map<string, (payload: object) => void>();
    permissionDeniedHandler = () => {};
    constructor() {
      provider.last = this as never;
    }
    on(event: string, handler: (payload: object) => void) {
      this.handlers.set(event, handler);
    }
    emit(event: string, payload: object) {
      this.handlers.get(event)?.(payload);
    }
    destroy() {}
  },
}));
vi.mock("@tiptap/react", () => ({
  EditorContent: () => null,
  useEditor: () => null,
}));
vi.mock("@tiptap/extension-collaboration", () => ({
  default: { configure: () => ({}) },
}));
vi.mock("@tiptap/extension-collaboration-caret", () => ({
  default: { configure: () => ({}) },
}));
vi.mock("@/plugins/manifest", () => ({
  registry: { extensions: () => [], blocks: () => [] },
}));
vi.mock("@/features/artifacts/components/artifact-embed", () => ({
  ArtifactEmbed: () => null,
}));

import { PageEditor } from "../page-editor";

const editor = (
  <PageEditor
    org="acme"
    page="p1"
    syncUrl="ws://cowork"
    user={{ name: "Ada", color: "#000000" }}
  />
);

describe("PageEditor", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("sends a visitor the server signed out to sign in", () => {
    const location = {
      pathname: "/acme/p1",
      search: "",
      assign: vi.fn(),
      reload: vi.fn(),
    };
    vi.stubGlobal("location", location);
    render(editor);

    act(() => provider.last?.emit("closed", { code: 4401 }));

    expect(location.assign).toHaveBeenCalledWith(
      "/auth/sign-in?next=%2Facme%2Fp1",
    );
  });

  it("stays read-only until its connection reconnects, whose upgrade decides again", () => {
    render(editor);
    act(() => provider.last?.emit("status", { status: "connected" }));
    expect(screen.getByTestId("sync-status")).toHaveTextContent("connected");

    act(() => provider.last?.permissionDeniedHandler());
    expect(screen.getByTestId("sync-status")).toHaveTextContent("read only");

    act(() => provider.last?.emit("status", { status: "disconnected" }));
    expect(screen.getByTestId("sync-status")).toHaveTextContent("read only");

    act(() => provider.last?.emit("status", { status: "connected" }));
    expect(screen.getByTestId("sync-status")).toHaveTextContent("connected");
  });
});
