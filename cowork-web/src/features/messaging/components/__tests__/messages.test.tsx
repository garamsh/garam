import {
  act,
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Message } from "@/lib/cowork";
import { startFreshPage } from "@/testing/clock";
import { server } from "@/testing/setup";

// An embed reads its artifact through a server action, which a test
// renders no server for.
vi.mock("@/features/artifacts/actions", () => ({
  embedAction: () => new Promise(() => {}),
}));

// What reading the channel again answers once the feed opens, which a
// test sets.
const read = vi.hoisted(() => ({ messages: [] as Message[] }));
vi.mock("@/features/messaging/actions", () => ({
  readMessagesAction: async () => read.messages,
}));

import { LiveMessages } from "../live-messages";
import { MessageItem } from "../message-item";

const message = (over: Partial<Message>): Message => ({
  id: "m1",
  channel: "c1",
  thread: "",
  author: "grn:root/iam/user/ada",
  body: "hello",
  mentions: [],
  undeliveredMentions: [],
  pages: [],
  artifacts: [],
  createdAt: "2026-10-27T10:00:00Z",
  replies: 0,
  ...over,
});

const names = {
  "grn:root/iam/user/ada": "Ada",
  "grn:root/iam/user/bob": "Bob",
};

describe("MessageItem", () => {
  it("names its author and mentions, says which agent was not reached, and links its pages", () => {
    render(
      <ul>
        <MessageItem
          org="acme"
          names={{ ...names, "grn:acme/iam/agent/helper": "Helper" }}
          pages={{ p1: "Spec" }}
          message={message({
            mentions: ["grn:root/iam/user/bob", "grn:acme/iam/agent/helper"],
            undeliveredMentions: ["grn:acme/iam/agent/helper"],
            pages: ["p1"],
          })}
        />
      </ul>,
    );
    expect(screen.getByText("Ada")).toBeInTheDocument();
    expect(screen.getByText("@Bob")).toBeInTheDocument();
    expect(screen.getByText("@Helper (not delivered)")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Spec" })).toHaveAttribute(
      "href",
      "/acme/p1",
    );
  });
});

class FakeSocket {
  static last: FakeSocket;
  onopen?: () => void;
  onclose?: (event: { code: number }) => void;
  onmessage?: (event: { data: string }) => void;
  constructor(public url: string) {
    FakeSocket.last = this;
  }
  close() {}
  async open() {
    await act(async () => this.onopen?.());
  }
  deliver(m: Message) {
    act(() => this.onmessage?.({ data: JSON.stringify(m) }));
  }
}

describe("LiveMessages", () => {
  beforeEach(startFreshPage);
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("sends a visitor the server signed out to sign in", () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    const location = {
      origin: "http://cowork.test",
      pathname: "/acme/channels/c1",
      search: "",
      assign: vi.fn(),
      reload: vi.fn(),
    };
    vi.stubGlobal("location", location);
    render(
      <LiveMessages
        org="acme"
        channel="c1"
        initial={[]}
        syncUrl="ws://cowork"
        names={names}
        pages={{}}
      />,
    );

    act(() => FakeSocket.last.onclose?.({ code: 4401 }));

    expect(location.assign).toHaveBeenCalledWith(
      "/auth/sign-in?next=%2Facme%2Fchannels%2Fc1",
    );
  });

  describe("after the feed closes without a refusal it can read", () => {
    function render_(location: { assign: () => void }) {
      vi.stubGlobal("WebSocket", FakeSocket);
      vi.stubGlobal("location", {
        origin: "http://cowork.test",
        pathname: "/acme/channels/c1",
        search: "",
        assign: location.assign,
        reload: vi.fn(),
      });
      render(
        <LiveMessages
          org="acme"
          channel="c1"
          initial={[]}
          syncUrl="ws://cowork"
          names={names}
          pages={{}}
        />,
      );
    }

    it("sends a visitor whose session the platform refuses to sign in", async () => {
      const location = { assign: vi.fn() };
      server.use(
        http.get(
          "http://cowork.test/auth/session",
          () => new HttpResponse(null, { status: 401 }),
        ),
      );
      render_(location);

      act(() => FakeSocket.last.onclose?.({ code: 1006 }));

      await waitFor(() =>
        expect(location.assign).toHaveBeenCalledWith(
          "/auth/sign-in?next=%2Facme%2Fchannels%2Fc1",
        ),
      );
    });

    it("asks nothing of a feed the page closed itself", async () => {
      const location = { assign: vi.fn() };
      const asked = vi.fn();
      server.use(
        http.get("http://cowork.test/auth/session", () => {
          asked();
          return new HttpResponse(null, { status: 401 });
        }),
      );
      render_(location);
      const closedByPage = FakeSocket.last;
      cleanup();
      render_(location);

      // The page's own close first, then a lost connection: once the
      // second has been answered, the first has had its chance to ask.
      act(() => closedByPage.onclose?.({ code: 1005 }));
      act(() => FakeSocket.last.onclose?.({ code: 1006 }));

      await waitFor(() => expect(location.assign).toHaveBeenCalled());
      expect(asked).toHaveBeenCalledOnce();
    });

    it("keeps a visitor on the page when the network fails", async () => {
      const location = { assign: vi.fn() };
      const asked = vi.fn();
      server.use(
        http.get("http://cowork.test/auth/session", () => {
          asked();
          return HttpResponse.error();
        }),
      );
      render_(location);

      act(() => FakeSocket.last.onclose?.({ code: 1006 }));

      await waitFor(() => expect(asked).toHaveBeenCalledOnce());
      expect(location.assign).not.toHaveBeenCalled();
    });
  });

  it("appends what arrives, once, and counts a reply on its thread rather than listing it", () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    render(
      <LiveMessages
        org="acme"
        channel="c1"
        initial={[message({})]}
        syncUrl="ws://cowork"
        names={names}
        pages={{}}
      />,
    );
    expect(FakeSocket.last.url).toBe("ws://cowork/orgs/acme/channels/c1/live");

    FakeSocket.last.deliver(message({ id: "m2", body: "second" }));
    FakeSocket.last.deliver(message({ id: "m2", body: "second" }));
    FakeSocket.last.deliver(
      message({ id: "r1", body: "a reply", thread: "m1" }),
    );

    const list = screen.getByRole("list", { name: "Messages" });
    expect(within(list).getAllByTestId("message")).toHaveLength(2);
    expect(screen.queryByText("a reply")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "1 reply" })).toHaveAttribute(
      "href",
      "/acme/channels/c1?thread=m1",
    );
  });

  it("reads the channel again once live, keeping what the feed already brought", async () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    render(
      <LiveMessages
        org="acme"
        channel="c1"
        initial={[message({})]}
        syncUrl="ws://cowork"
        names={names}
        pages={{}}
      />,
    );
    // m2 was posted after the page's read and before the feed opened;
    // m3 arrives on the feed before the read answers.
    read.messages = [message({}), message({ id: "m2", body: "missed" })];
    FakeSocket.last.deliver(message({ id: "m3", body: "on the feed" }));
    await FakeSocket.last.open();

    const list = screen.getByRole("list", { name: "Messages" });
    expect(
      within(list)
        .getAllByTestId("message")
        .map((li) => li.dataset.messageId),
    ).toEqual(["m1", "m2", "m3"]);
    read.messages = [];
  });

  it("in a thread, appends that thread's replies alone", () => {
    vi.stubGlobal("WebSocket", FakeSocket);
    render(
      <LiveMessages
        org="acme"
        channel="c1"
        thread="m1"
        initial={[message({})]}
        syncUrl="ws://cowork"
        names={names}
        pages={{}}
      />,
    );
    FakeSocket.last.deliver(
      message({ id: "r1", body: "in this thread", thread: "m1" }),
    );
    FakeSocket.last.deliver(
      message({ id: "r2", body: "in another", thread: "m9" }),
    );
    FakeSocket.last.deliver(message({ id: "m3", body: "top level" }));

    const list = screen.getByRole("list", { name: "Thread" });
    expect(
      within(list)
        .getAllByTestId("message")
        .map((li) => li.dataset.messageId),
    ).toEqual(["m1", "r1"]);
  });
});
