import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { signInWithPassword } from "@/features/auth/api/sign-in-with-password";
import { PASSWORD_REFUSED } from "@/lib/password-credentials";
import { API_URL, alice } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

// `redirect()` throws a control-flow error the router catches; the
// test only needs to observe where it was sent.
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// The cookie jar the action writes the session through. The shared
// setup stubs `next/headers` for every file; this one also reads back
// what was written, which is how the planted session is observed.
const { planted } = vi.hoisted(() => ({
  planted: [] as Record<string, unknown>[],
}));
vi.mock("next/headers", () => ({
  cookies: () =>
    Promise.resolve({
      get: (name: string) => ({ name, value: "test-session-token" }),
      set: (options: Record<string, unknown>) => {
        planted.push(options);
      },
      delete: () => {},
    }),
}));

/** The password these tests sign in with; it is a fixture, not a value. */
const PASSWORD = "correct-horse-battery-staple";

/** What garam answers a sign-in with: the user, and the session cookie. */
const SET_COOKIE =
  "garam_session=session-value; Path=/; Max-Age=3600; HttpOnly; SameSite=Lax";

function form(entries: Record<string, string>): FormData {
  const data = new FormData();
  for (const [key, value] of Object.entries(entries)) data.set(key, value);
  return data;
}

/** Answers as garam does when a password verifies. */
function accepts(setCookie = SET_COOKIE) {
  server.use(
    http.post(`${API_URL}/auth/password/login`, () =>
      HttpResponse.json(alice, { headers: { "set-cookie": setCookie } }),
    ),
  );
}

/** Answers as garam does when a password does not verify. */
function refuses(message = "username or password is incorrect") {
  server.use(
    http.post(`${API_URL}/auth/password/login`, () =>
      HttpResponse.json({ kind: "unauthenticated", message }, { status: 401 }),
    ),
  );
}

describe("signInWithPassword", () => {
  it("posts the credential and plants the session garam issued", async () => {
    planted.length = 0;
    let sent: unknown;
    server.use(
      http.post(`${API_URL}/auth/password/login`, async ({ request }) => {
        sent = await request.json();
        return HttpResponse.json(alice, {
          headers: { "set-cookie": SET_COOKIE },
        });
      }),
    );

    await expect(
      signInWithPassword(
        { status: "idle" },
        form({ username: "alice", password: PASSWORD }),
      ),
    ).rejects.toThrow("NEXT_REDIRECT:/console");

    expect(sent).toEqual({ username: "alice", password: PASSWORD });
    // The attributes are garam's own, read off the header rather than
    // decided here: a browser holding a cookie the backend marked
    // differently would be a second policy.
    expect(planted).toEqual([
      {
        name: "garam_session",
        value: "session-value",
        path: "/",
        httpOnly: true,
        secure: false,
        sameSite: "lax",
        maxAge: 3600,
      },
    ]);
  });

  it("lands where the visitor was going, and only inside the console", async () => {
    accepts();

    await expect(
      signInWithPassword(
        { status: "idle" },
        form({
          username: "alice",
          password: PASSWORD,
          next: "/console/invitations/secret-token",
        }),
      ),
    ).rejects.toThrow(/^NEXT_REDIRECT:\/console\/invitations\/secret-token$/);

    // The control: a return path off the console is dropped rather
    // than followed, so the landing above is the path and not a
    // redirect that goes wherever it is told.
    await expect(
      signInWithPassword(
        { status: "idle" },
        form({
          username: "alice",
          password: PASSWORD,
          next: "https://evil.example/console",
        }),
      ),
    ).rejects.toThrow(/^NEXT_REDIRECT:\/console$/);
  });

  it("carries the Secure a backend on HTTPS marks the session with", async () => {
    planted.length = 0;
    accepts(`${SET_COOKIE}; Secure`);

    await expect(
      signInWithPassword(
        { status: "idle" },
        form({ username: "alice", password: PASSWORD }),
      ),
    ).rejects.toThrow("NEXT_REDIRECT:/console");

    expect(planted[0]).toMatchObject({ secure: true });
  });

  it("sends the password in the body of a POST and puts it in no URL", async () => {
    const seen: { method: string; url: string }[] = [];
    server.use(
      http.post(`${API_URL}/auth/password/login`, ({ request }) => {
        seen.push({ method: request.method, url: request.url });
        return HttpResponse.json(
          { kind: "unauthenticated", message: "no" },
          { status: 401 },
        );
      }),
    );

    const state = await signInWithPassword(
      { status: "idle" },
      form({ username: "alice", password: PASSWORD }),
    );

    expect(seen).toEqual([
      { method: "POST", url: `${API_URL}/auth/password/login` },
    ]);
    // The control for the search: the refusal reached the caller, so
    // what carries no password is a real answer and not an empty one.
    expect(state).toEqual({
      status: "error",
      message: "That username and password do not match an account.",
    });
    expect(JSON.stringify(state)).not.toContain(PASSWORD);
  });

  it("answers one refusal for a wrong password and for a username no account holds", async () => {
    // garam refuses the two identically (ADR-0041). These two answers
    // differ on purpose: the sentence the console renders is its own,
    // so what the interface says rests on nothing it cannot check.
    refuses("no account holds that username");
    const unknownUsername = await signInWithPassword(
      { status: "idle" },
      form({ username: "mallory", password: PASSWORD }),
    );

    refuses("that password is wrong");
    const wrongPassword = await signInWithPassword(
      { status: "idle" },
      form({ username: "alice", password: "not-the-password-at-all" }),
    );

    expect(unknownUsername).toEqual({
      status: "error",
      message: PASSWORD_REFUSED,
    });
    expect(unknownUsername).toEqual(wrongPassword);

    // The control: a status that refuses no credential keeps garam's
    // own message, so the sentence above is one the console chooses
    // rather than every message it drops.
    server.use(
      http.post(`${API_URL}/auth/password/login`, () =>
        HttpResponse.json(
          {
            kind: "resource_exhausted",
            message: "too many password verifications are live",
          },
          { status: 429 },
        ),
      ),
    );

    await expect(
      signInWithPassword(
        { status: "idle" },
        form({ username: "alice", password: PASSWORD }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: REFUSAL_COPY.tooManyRequests,
    });
  });

  it("answers an unreachable API with a state rather than a throw", async () => {
    server.use(
      http.post(`${API_URL}/auth/password/login`, () => HttpResponse.error()),
    );

    await expect(
      signInWithPassword(
        { status: "idle" },
        form({ username: "alice", password: PASSWORD }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: REFUSAL_COPY.unreachable,
    });
  });

  it("refuses a submission that arrived without the fields", async () => {
    await expect(
      signInWithPassword({ status: "idle" }, form({ username: "alice" })),
    ).resolves.toEqual({
      status: "error",
      message: "The form did not arrive complete.",
    });
  });
});
