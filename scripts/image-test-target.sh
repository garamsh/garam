#!/bin/sh
# Admits the publisher role and the ECR repository that
# .github/workflows/test-image.yml names (ADR-0093), and refuses them
# before any job assumes the role: a value that is not an IAM role in
# account 486152169996 or an ECR repository name, the release role
# under its corrected name and its former one, which the test image
# never assumes, and every role but the publisher,
# garam-github-actions-dev.
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

printf '%s' "$role" | grep -Eq '^arn:aws:iam::486152169996:role/([A-Za-z0-9+=,.@_-]+/)*[A-Za-z0-9+=,.@_-]{1,64}$' \
	|| refuse "role '$role' is not an IAM role ARN in account 486152169996"
case $role in
*/garam-github-actions | */garamsh-garam-github-actions)
	refuse "role '$role' is the release role, which publishes release images alone (ADR-0039)"
	;;
arn:aws:iam::486152169996:role/garam-github-actions-dev) ;;
*)
	refuse "role '$role' is not the publisher role arn:aws:iam::486152169996:role/garam-github-actions-dev"
	;;
esac
printf '%s' "$repository" | grep -Eq '^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$' \
	|| refuse "repository '$repository' is not an ECR repository name"
echo "image-test-target: $role pushes to $repository"
