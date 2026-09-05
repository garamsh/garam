import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import {
  createGroup,
  createProject,
} from "@/features/orgs/api/create-resource";
import { API_URL, engineering, frontend } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `revalidatePath` only works inside a Next.js request store.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/**
 * A submission of a create form: the org it was rendered for, and a
 * name.
 */
function form(name: string): FormData {
  const data = new FormData();
  data.set("org", "acme");
  data.set("name", name);
  return data;
}

/**
 * The two resources one action creates, and what garam answers for
 * each.
 */
const RESOURCES = [
  {
    action: createGroup,
    kind: "group",
    path: `${API_URL}/orgs/acme/groups`,
    typed: "Engineering",
    answer: engineering,
    identifier: "eng",
  },
  {
    action: createProject,
    kind: "project",
    path: `${API_URL}/orgs/acme/projects`,
    typed: "Frontend",
    answer: frontend,
    identifier: "frontend",
  },
] as const;

describe.each(RESOURCES)(
  "create $kind",
  ({ action, kind, path, typed, answer, identifier }) => {
    /** Records every body posted to this resource's path. */
    function recordCreates() {
      const posted: unknown[] = [];
      server.use(
        http.post(path, async ({ request }) => {
          posted.push(await request.json());
          return HttpResponse.json(answer, { status: 201 });
        }),
      );
      return posted;
    }

    it("posts the name alone and answers with the identifier garam minted", async () => {
      const posted = recordCreates();

      const state = await action({ status: "idle" }, form(typed));

      expect(state).toEqual({
        status: "created",
        name: typed,
        identifier,
      });
      expect(posted).toEqual([{ name: typed }]);
    });

    it("posts the name garam is given, trimmed of the spaces around it", async () => {
      const posted = recordCreates();

      await action({ status: "idle" }, form(`  ${typed}  `));

      expect(posted).toEqual([{ name: typed }]);
    });

    it(`refuses a nameless submission and names the ${kind}`, async () => {
      const posted = recordCreates();

      await expect(action({ status: "idle" }, form("   "))).resolves.toEqual({
        status: "error",
        message: `Give the ${kind} a name.`,
      });
      expect(posted).toEqual([]);

      // The control: the same form carrying a name is created, so the
      // refusal above is the empty name and not a form the action reads
      // nothing from.
      await expect(
        action({ status: "idle" }, form(typed)),
      ).resolves.toMatchObject({ status: "created" });
    });

    it("renders the sentence the API refused with rather than throwing", async () => {
      server.use(
        http.post(path, () =>
          HttpResponse.json(
            {
              kind: "invalid_argument",
              message: `this organization already holds a ${kind} named ${typed}`,
            },
            { status: 400 },
          ),
        ),
      );

      await expect(action({ status: "idle" }, form(typed))).resolves.toEqual({
        status: "error",
        message: `this organization already holds a ${kind} named ${typed}`,
      });
    });

    it("renders an outage rather than throwing it at the boundary", async () => {
      server.use(http.post(path, () => HttpResponse.error()));

      await expect(action({ status: "idle" }, form(typed))).resolves.toEqual({
        status: "error",
        message: "the garam API could not be reached",
      });
    });
  },
);
