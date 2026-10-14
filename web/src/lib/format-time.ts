/** A moment as the console shows it: relative, with the exact time beside. */
export type FormattedTime = {
  /** The machine-readable instant, for `<time dateTime>`. */
  iso: string;
  /** How long ago or ahead, in words: "3 minutes ago". */
  relative: string;
  /** The exact time with its zone: "Oct 14, 2026, 18:34:05 GMT+9". */
  absolute: string;
};

// Past these, a relative phrase stops being the useful one and the date
// is shown instead.
const SECOND = 1_000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const RELATIVE_LIMIT = 30 * DAY;

/**
 * Formats an instant from the API for display. `now` is passed in so a
 * test, or a server and a browser rendering the same tree, read one
 * clock; `timeZone` defaults to the runtime's own. An unparseable value
 * yields null rather than a string that only looks like a time.
 */
export function formatTime(
  value: string | Date,
  {
    now = new Date(),
    locale = "en",
    timeZone,
  }: { now?: Date; locale?: string; timeZone?: string } = {},
): FormattedTime | null {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return null;

  const absolute = new Intl.DateTimeFormat(locale, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
    timeZoneName: "short",
    timeZone,
  }).format(date);

  return {
    iso: date.toISOString(),
    relative: formatRelative(date, now, locale, timeZone),
    absolute,
  };
}

function formatRelative(
  date: Date,
  now: Date,
  locale: string,
  timeZone: string | undefined,
): string {
  const delta = date.getTime() - now.getTime();
  const distance = Math.abs(delta);
  if (distance >= RELATIVE_LIMIT) {
    return new Intl.DateTimeFormat(locale, {
      year: "numeric",
      month: "short",
      day: "numeric",
      timeZone,
    }).format(date);
  }
  const words = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  if (distance < MINUTE) return words.format(0, "second");
  if (distance < HOUR)
    return words.format(Math.round(delta / MINUTE), "minute");
  if (distance < DAY) return words.format(Math.round(delta / HOUR), "hour");
  return words.format(Math.round(delta / DAY), "day");
}
