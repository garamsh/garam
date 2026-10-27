import { act, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Message } from "@/lib/cowork";

// An embed reads its artifact through a server action, which a test
// renders no server for.
vi.mock("@/features/artifacts/actions", () => ({
  embedAction: () => new Promise(() => {}),
}));

import { LiveMessages } from "../live-messages";
import { MessageItem } from "../message-item";

const message = (over: Partial<Message>): Message => ({
  id: "m1",
  channel: "c1",
  thread: "",
  author: "grn:root:default:user:ada",
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
  "grn:root:default:user:ada": "Ada",
  "grn:root:default:user:bob": "Bob",
};

describe("MessageItem", () => {
  it("names its author and mentions, says which agent was not reached, and links its pages", () => {
    render(
      <ul>
        <MessageItem
          org="acme"
          names={{ ...names, "grn:acme:default:agent:helper": "Helper" }}
          pages={{ p1: "Spec" }}
          message={message({
            mentions: [
              "grn:root:default:user:bob",
              "grn:acme:default:agent:helper",
            ],
            undeliveredMentions: ["grn:acme:default:agent:helper"],
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
  onclose?: () => void;
  onmessage?: (event: { data: string }) => void;
  constructor(public url: string) {
    FakeSocket.last = this;
  }
  close() {}
  deliver(m: Message) {
    act(() => this.onmessage?.({ data: JSON.stringify(m) }));
  }
}

describe("LiveMessages", () => {
  afterEach(() => vi.unstubAllGlobals());

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
