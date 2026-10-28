import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { registerWithPassword } from "@/features/auth/api/register-with-password";
import {
  PASSWORD_MIN_LENGTH,
  USERNAME_MAX_LENGTH,
} from "@garam/platform-client/password-credentials";
import {
  API_URL,
  acmeInvitation,
  aliceMembership,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@garam/platform-client/refusal-copy";

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

/** The password these tests register with; it is a fixture, not a value. */
const PASSWORD = "correct-horse-battery-staple";

const TOKEN = "invitation-token-nobody-guesses";

function form(entries: Record<string, string>): FormData {
  const data = new FormData();
  for (const [key, value] of Object.entries(entries)) data.set(key, value);
  return data;
}

/** Records what the action posted, and answers as garam does. */
function recordRegistrations() {
  const posted: unknown[] = [];
  server.use(
    http.post(`${API_URL}/auth/password/register`, async ({ request }) => {
      posted.push(await request.json());
      return HttpResponse.json(aliceMembership, {
        status: 201,
        headers: {
          "set-cookie":
            "garam_session=session-value; Path=/; Max-Age=3600; HttpOnly; SameSite=Lax",
        },
      });
    }),
    // Read back once the account exists, to land in the organization
    // the invitation joined it to: spent by now, and still naming it.
    http.get(`${API_URL}/invitations/${TOKEN}`, () =>
      HttpResponse.json({ ...acmeInvitation, state: "accepted" }),
    ),
  );
  return posted;
}

describe("registerWithPassword", () => {
  it("posts the invitation, the username and the password, plants the session, and lands in the organization joined", async () => {
    planted.length = 0;
    const posted = recordRegistrations();

    await expect(
      registerWithPassword(
        { status: "idle" },
        form({
          invitationToken: TOKEN,
          username: "alice",
          password: PASSWORD,
        }),
      ),
    ).rejects.toThrow(/^NEXT_REDIRECT:\/console\/orgs\/acme$/);

    expect(posted).toEqual([
      { invitationToken: TOKEN, username: "alice", password: PASSWORD },
    ]);
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

  it("sends the password in the body of a POST and puts it in no URL", async () => {
    const seen: { method: string; url: string }[] = [];
    server.use(
      http.post(`${API_URL}/auth/password/register`, ({ request }) => {
        seen.push({ method: request.method, url: request.url });
        return HttpResponse.json(
          { kind: "not_found", message: "invitation not found" },
          { status: 404 },
        );
      }),
    );

    const state = await registerWithPassword(
      { status: "idle" },
      form({ invitationToken: TOKEN, username: "alice", password: PASSWORD }),
    );

    expect(seen).toEqual([
      { method: "POST", url: `${API_URL}/auth/password/register` },
    ]);
    expect(state).toEqual({
      status: "error",
      message: REFUSAL_COPY.invitationNotFound,
      field: "invitationToken",
    });
    expect(JSON.stringify(state)).not.toContain(PASSWORD);
  });

  it("refuses a password under the floor before anything is sent", async () => {
    const posted = recordRegistrations();
    const short = "a".repeat(PASSWORD_MIN_LENGTH - 1);

    await expect(
      registerWithPassword(
        { status: "idle" },
        form({ invitationToken: TOKEN, username: "alice", password: short }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: `A password holds at least ${PASSWORD_MIN_LENGTH} characters.`,
      field: "password",
    });
    expect(posted).toEqual([]);

    // The control: the same password one character longer is posted, so
    // the floor is what refused it.
    await expect(
      registerWithPassword(
        { status: "idle" },
        form({
          invitationToken: TOKEN,
          username: "alice",
          password: `${short}a`,
        }),
      ),
    ).rejects.toThrow("NEXT_REDIRECT:/console");
    expect(posted).toHaveLength(1);
  });

  it("refuses a username the character rule does not allow before anything is sent", async () => {
    const posted = recordRegistrations();

    await expect(
      registerWithPassword(
        { status: "idle" },
        form({
          invitationToken: TOKEN,
          username: "Alice",
          password: PASSWORD,
        }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: "A username holds only lowercase letters, digits and hyphens.",
      field: "username",
    });
    expect(posted).toEqual([]);

    // The control: the same username cased down is posted, so the
    // character rule is what refused it.
    await expect(
      registerWithPassword(
        { status: "idle" },
        form({
          invitationToken: TOKEN,
          username: "alice",
          password: PASSWORD,
        }),
      ),
    ).rejects.toThrow("NEXT_REDIRECT:/console");
    expect(posted).toHaveLength(1);
  });

  it("refuses a username past the length the reserved prefix leaves", async () => {
    const posted = recordRegistrations();
    const tooLong = "a".repeat(USERNAME_MAX_LENGTH + 1);

    await expect(
      registerWithPassword(
        { status: "idle" },
        form({
          invitationToken: TOKEN,
          username: tooLong,
          password: PASSWORD,
        }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: `A username holds at most ${USERNAME_MAX_LENGTH} characters; this one holds ${tooLong.length}.`,
      field: "username",
    });
    expect(posted).toEqual([]);
  });

  it("asks for the invitation when the field arrived blank", async () => {
    const posted = recordRegistrations();

    await expect(
      registerWithPassword(
        { status: "idle" },
        form({
          invitationToken: "   ",
          username: "alice",
          password: PASSWORD,
        }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: "Paste the invitation token you were sent.",
      field: "invitationToken",
    });
    expect(posted).toEqual([]);
  });

  it("shows what garam refused the invitation or the username with", async () => {
    server.use(
      http.post(`${API_URL}/auth/password/register`, () =>
        HttpResponse.json(
          { kind: "already_exists", message: "user: already exists" },
          { status: 409 },
        ),
      ),
    );

    await expect(
      registerWithPassword(
        { status: "idle" },
        form({
          invitationToken: TOKEN,
          username: "alice",
          password: PASSWORD,
        }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: "That username is taken. Choose another one.",
      field: "username",
    });
  });

  it("answers an unreachable API with a state rather than a throw", async () => {
    server.use(
      http.post(`${API_URL}/auth/password/register`, () =>
        HttpResponse.error(),
      ),
    );

    await expect(
      registerWithPassword(
        { status: "idle" },
        form({
          invitationToken: TOKEN,
          username: "alice",
          password: PASSWORD,
        }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: REFUSAL_COPY.unreachable,
    });
  });

  it("refuses a submission that arrived without the fields", async () => {
    await expect(
      registerWithPassword({ status: "idle" }, form({ username: "alice" })),
    ).resolves.toEqual({
      status: "error",
      message: "The form did not arrive complete.",
    });
  });
});
