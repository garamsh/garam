import userEvent from "@testing-library/user-event";

/**
 * A user-event session that dispatches each input as soon as the one
 * before it is handled.
 *
 * user-event's default `delay: 0` is still a timer per keystroke, and
 * nothing a test asserts waits on it: React handles an input event
 * before the next one is dispatched. Each timer is a wake-up a loaded
 * host grants late, so typing spent a test's budget on time no
 * condition needed, and typing a timed-out test left running went on
 * into the next test's fields (#1200).
 */
export function setupUser() {
  return userEvent.setup({ delay: null });
}
