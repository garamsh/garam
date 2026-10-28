/**
 * The reference tokens of a JSON Pointer (RFC 6901), unescaped: `""`
 * is the whole document and `/Statement/1/Effect` is
 * `["Statement", "1", "Effect"]`. Null for text that is not a pointer.
 */
export function pointerTokens(pointer: string): string[] | null {
  if (pointer === "") return [];
  if (!pointer.startsWith("/")) return null;
  return pointer
    .slice(1)
    .split("/")
    .map((token) => token.replaceAll("~1", "/").replaceAll("~0", "~"));
}
