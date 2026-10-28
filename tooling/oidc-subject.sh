#!/bin/sh
# oidc-subject prints the `sub` claim of the OIDC token the GitHub
# Actions runner it runs on would present, and nothing else about that
# token: not the token, not the rest of the payload, and not jq's parse
# error, which quotes its input (docs/convention/ci.md §Security
# baseline). Why a release run prints it, and where the step sits, is in
# .github/workflows/release.yml — its only caller.
#
# curl's status is read on its own line rather than out of a pipeline's,
# so a fetch that failed is reported as that rather than reaching jq and
# arriving as an unreadable payload.
#
# ACTIONS_ID_TOKEN_REQUEST_URL and ACTIONS_ID_TOKEN_REQUEST_TOKEN arrive
# in the environment. A runner sets both on a job it granted
# `id-token: write`, and nothing off a runner holds them, so this is the
# one script here a contributor cannot usefully run. Without the URL it
# says so and stops, rather than dialling whatever an empty one makes.

test -n "$ACTIONS_ID_TOKEN_REQUEST_URL" \
	|| { echo 'oidc-subject: ACTIONS_ID_TOKEN_REQUEST_URL is unset — only a GitHub Actions runner granted id-token: write holds it, so there is nothing here to read and nothing was read' >&2; exit 1; }

response="$(curl -sSf -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
	"$ACTIONS_ID_TOKEN_REQUEST_URL&audience=sts.amazonaws.com")" \
	|| { echo 'oidc-subject: the runner OIDC token endpoint did not answer; nothing further about it is printed' >&2; exit 1; }

printf '%s' "$response" \
	| jq -er '.value | split(".")[1] | gsub("-"; "+") | gsub("_"; "/") | @base64d | fromjson | .sub' 2>/dev/null \
	|| { echo 'oidc-subject: no sub claim read from the runner OIDC token; nothing further about it is printed' >&2; exit 1; }
