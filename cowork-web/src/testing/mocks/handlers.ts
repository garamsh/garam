import { HttpResponse, http } from "msw";

/** The platform's browser API, where the app's server-side calls go. */
export const API_URL = "http://localhost:8080";

/**
 * What the platform answers when a test says nothing else: the
 * caller's session stands. A route not listed is left unhandled, so a
 * test reaching one fails by name.
 */
export const handlers = [
  http.get(`${API_URL}/me`, () =>
    HttpResponse.json({ grn: "grn:root:default:user:ada", nickname: "Ada" }),
  ),
];
