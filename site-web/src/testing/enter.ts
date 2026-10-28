import type { UserEvent } from "@testing-library/user-event";
import { expect } from "vitest";

/**
 * Enters text into one field the way a person pastes it. Typing a key
 * at a time does work for every character, and on a loaded host that
 * work is what outran a test's budget; no form test turns on
 * keystrokes. A paste lands where focus is, so the field must hold it
 * first: a step outliving its test meets its own unmounted field here
 * and stops, rather than pasting into the next test's form (#1200).
 *
 * The text is entered as written: unlike `user.type`, a paste reads no
 * `{` or `[` as a key.
 */
export async function enter(user: UserEvent, field: HTMLElement, text: string) {
  await user.click(field);
  expect(field).toHaveFocus();
  await user.paste(text);
}
