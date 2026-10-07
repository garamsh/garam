#!/bin/sh
# Admits the publisher role and the ECR repository that
# .github/workflows/test-image.yml names (ADR-0093), and refuses them
# before any job assumes the role: a placeholder that garamsh/infra has
# not yet replaced, a value that is not an IAM role in account
# 486152169996 or an ECR repository name, and the release role, which
# the test image never assumes.
#
#   scripts/image-test-target.sh <role-arn> <repository>
set -eu

[ $# -eq 2 ] || {
	echo 'usage: scripts/image-test-target.sh <role-arn> <repository>' >&2
	exit 2
}
role=$1 repository=$2

refuse() {
	echo "image-test-target: $1; no role assumed and nothing pushed" >&2
	exit 1
}

case $role$repository in
*PLACEHOLDER*)
	refuse "the publisher role or the repository is still the placeholder .github/workflows/test-image.yml carries until garamsh/infra supplies both (#1227)"
	;;
esac
printf '%s' "$role" | grep -Eq '^arn:aws:iam::486152169996:role/([A-Za-z0-9+=,.@_-]+/)*[A-Za-z0-9+=,.@_-]{1,64}$' \
	|| refuse "role '$role' is not an IAM role ARN in account 486152169996"
case $role in
*/garamsh-garam-github-actions)
	refuse "role '$role' is the release role, which publishes release images alone (ADR-0039)"
	;;
esac
printf '%s' "$repository" | grep -Eq '^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$' \
	|| refuse "repository '$repository' is not an ECR repository name"
echo "image-test-target: $role pushes to $repository"
