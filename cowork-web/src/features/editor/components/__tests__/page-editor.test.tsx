import { act, render, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { startFreshPage } from "@/testing/clock";
import { server } from "@/testing/setup";

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
  beforeEach(startFreshPage);
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("sends a visitor the server signed out to sign in", () => {
    const location = {
      origin: "http://cowork.test",
      pathname: "/acme/p1",
      search: "",
      assign: vi.fn(),
      reload: vi.fn(),
    };
    vi.stubGlobal("location", location);
    render(editor);

    act(() => provider.last?.emit("connection-close", { code: 4401 }));

    expect(location.assign).toHaveBeenCalledWith(
      "/auth/sign-in?next=%2Facme%2Fp1",
    );
  });

  describe("after a connection fails to a refusal a browser cannot read", () => {
    const location = () => {
      const stub = {
        origin: "http://cowork.test",
        pathname: "/acme/p1",
        search: "",
        assign: vi.fn(),
        reload: vi.fn(),
      };
      vi.stubGlobal("location", stub);
      return stub;
    };

    it("sends a visitor whose session the platform refuses to sign in", async () => {
      const stub = location();
      server.use(
        http.get(
          "http://cowork.test/auth/session",
          () => new HttpResponse(null, { status: 401 }),
        ),
      );
      render(editor);

      act(() => provider.last?.emit("connection-close", { code: 1006 }));

      await waitFor(() =>
        expect(stub.assign).toHaveBeenCalledWith(
          "/auth/sign-in?next=%2Facme%2Fp1",
        ),
      );
    });

    it("keeps a visitor on the page when the network fails", async () => {
      const stub = location();
      const asked = vi.fn();
      server.use(
        http.get("http://cowork.test/auth/session", () => {
          asked();
          return HttpResponse.error();
        }),
      );
      render(editor);

      act(() => provider.last?.emit("connection-close", { code: 1006 }));

      await waitFor(() => expect(asked).toHaveBeenCalledOnce());
      expect(stub.assign).not.toHaveBeenCalled();
    });

    it("asks nothing of a close the provider made itself", async () => {
      const stub = location();
      const asked = vi.fn();
      server.use(
        http.get("http://cowork.test/auth/session", () => {
          asked();
          return new HttpResponse(null, { status: 401 });
        }),
      );
      render(editor);

      // The provider's own close first, then the server's: once the
      // second has been answered, the first has had its chance to ask.
      act(() => provider.last?.emit("connection-close", null as never));
      act(() => provider.last?.emit("connection-close", { code: 1006 }));

      await waitFor(() => expect(stub.assign).toHaveBeenCalled());
      expect(asked).toHaveBeenCalledOnce();
    });
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
