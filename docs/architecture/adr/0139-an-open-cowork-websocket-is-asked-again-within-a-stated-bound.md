# ADR 0139: An open cowork WebSocket is asked again within a stated bound

> Status: accepted
> Date: 2026-10-31

Drafted for #1721; the PM reserved this number for it. It supersedes
[ADR-0129](./0129-cowork-acts-in-each-organization-as-a-principal-the-platform-issues-with-it-proving-one-deployment-key.md)
§7's last sentence, that an open WebSocket stops at its next reconnect
as a policy change does. The rest of ADR-0129 stands. garam is read at
b957e78197a96a2c6bb15ebe6308331c4979370e.

## Context

- **A socket makes no next request.** cowork's sync and live endpoints
  asked the platform once, at the upgrade, so a revoked session, a
  member removed or a write denied kept an open connection as it was.
  [ADR-0131](./0131-the-session-cookie-is-host-only-and-the-site-hands-a-sign-in-to-another-app-by-a-one-time-code.md)
  §Signs the decision was wrong names that case.
- **The platform tells cowork nothing.** Nothing is pushed when a
  session ends, so cowork has to ask, and each asking costs the
  platform.
- **What a re-check costs, measured.** The platform's `serve api` and
  `serve authz` ran beside one Postgres 17.11 in Docker, with the load
  generator, on one 12-core host. `checkPermissions` with a page's two
  pairs answered:

  | Concurrent clients | Requests a second | p50 | p99 |
  |---|---|---|---|
  | 1 | 557 | 1.6 ms | 4.4 ms |
  | 8 | 1,961 | 3.9 ms | 7.3 ms |
  | 32 | 2,456 | 12.6 ms | 20.7 ms |

  Each request costs four Postgres transactions (200 requests moved
  `pg_stat_database.xact_commit` by 805) and one engine `Check` per
  pair, read from the engine's policy cache. This host's figures
  compare intervals and predict no deployment.
- **What an interval costs.** 10,000 open sockets is a reference
  figure rather than a forecast.

  | Interval | Requests a second | Of the 2,456 measured | Bound |
  |---|---|---|---|
  | 5 s | 2,000 | 81% | 15 s |
  | 10 s | 1,000 | 41% | 20 s |
  | 30 s | 333 | 14% | 40 s |
  | 60 s | 167 | 7% | 70 s |

## Decision

1. **Every open sync and live socket asks again every 30 seconds.** A
   socket closes within 40 seconds of the platform refusing its caller:
   the interval, and the 10 seconds a request to the platform may take.
   30 seconds is the shortest interval of the table above that leaves
   the platform's reference load under a sixth of what the host
   measured; 60 nearly doubles the bound on a stolen session to save 7
   points of that.
2. **One request asks everything a socket holds.** A sync socket's
   `checkPermissions` carries `cowork:page-read` and `cowork:page-write`
   on the page; a live socket asks what `getChannel` asks. Either
   presents the caller's own credential, so the same answer covers the
   session, the membership and the permission.
3. **The close says why.** `4401` is a session the platform refuses and
   `4403` a member removed or a read denied, each the status the
   upgrade would now answer. y-websocket 3.1.0 does not reconnect after
   a code from 4400 to 4499. cowork-web sends `4401` to sign in by the
   handoff, and reloads on `4403`, so the page says what it says to
   anyone who may not see it.
4. **A changed write answer closes the connection with `1013`.** The
   client reconnects and the upgrade decides, so a connection's
   `Writable` stays fixed for its life and nothing in `internal/collab`
   changes.
5. **An unanswered re-check closes nothing.** cowork decides nothing
   alone when the platform cannot be reached (ADR-0127), and a
   connection already holds a decision the platform made, unlike a
   request, which holds none and is answered 503. Closing would also
   turn a platform outage into every editor disconnected with no
   upgrade able to bring it back. The failure is logged and the next
   re-check asks again, and only a refusal closes a socket.

## Consequences

- A sign-out, a revocation, a removal, a denied read and a principal
  revoked under ADR-0129 §7 each reach an open socket within 40 seconds
  while the platform answers.
- While the platform does not answer, the window is as long as the
  outage, and a session revoked in it stays connected until the
  platform answers again. No ceiling closes a socket during an outage.
- The platform takes one request per open socket per 30 seconds, which
  grows with the sockets open. A person with an editor and a channel
  open costs 4 requests a minute.
- A session that expires closes its sockets as a revocation does.
- A browser cannot read an upgrade's status. A socket that died before
  a refusal reconnects into a 401 it sees as a failed connection, and
  y-websocket goes on retrying; such a visitor learns on their next
  request.

## Signs the decision was wrong

- Re-checks are a visible share of the platform's requests or of its
  Postgres load, which would call for a longer interval or one re-check
  shared by a session's sockets.
- All sockets reconnecting together keep re-checking together, and the
  platform sees the load as a spike every 30 seconds.
- A stolen session is reported as used through an open socket during a
  platform outage, which would call for a ceiling on decision 5.
- A person reports losing an edit to the `1013` close.
