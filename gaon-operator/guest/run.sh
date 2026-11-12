#!/bin/sh
# run.sh runs the composed guest image as one container, the way a
# MicroVM would run it: one image, its one entrypoint, user 65532, the
# credentials as files owned by that user, mode 0400. It shows the three
# programs starting in order, the readiness port answering, and a
# SIGTERM drain that ends with the adapter, and fails where one differs.
#
# The credentials are placeholders: a self-signed root and fixed token
# text, so the adapter runs but no call it makes reaches garam. What it
# does with an admitted agent is not shown here.
#
# usage: run.sh <image>
set -eu

image="${1:?usage: run.sh <image>}"
name="${GUEST_DEMO_NAME:-gaon-guest-demo-$$}"
grn=grn:demo/iam/agent/0000000000000001
scratch="$(mktemp -d)"

remove_container() { docker rm -f -v "$name" >/dev/null 2>&1 || true; }
cleanup() {
	remove_container
	rm -rf "$scratch"
}
trap cleanup EXIT
remove_container

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=demo \
	-keyout "$scratch/key.pem" -out "$scratch/root.pem" 2>/dev/null
mkdir "$scratch/files"
for f in garam-root.pem control-root.pem; do
	cp "$scratch/root.pem" "$scratch/files/$f"
done
for f in agent-token placement-token control-token; do
	echo demo-token >"$scratch/files/$f"
done

echo "== image labels"
docker image inspect "$image" --format \
	'{{range $k, $v := .Config.Labels}}{{$k}}={{$v}}{{"\n"}}{{end}}' | sort

docker create --name "$name" --user 65532:65532 \
	-e GUEST_AGENT_GRN="$grn" \
	-e GUEST_ASSIGNMENT_EPOCH=1 \
	-e GUEST_MACHINE_URL=https://127.0.0.1:9 \
	-e GUEST_CONTROL_URL=https://127.0.0.1:9 \
	"$image" >/dev/null
tar -C "$scratch/files" --owner=65532 --group=65532 --mode=0400 -cf - . |
	docker cp - "$name":/run/garam
docker start "$name" >/dev/null

echo "== readiness: waiting for port 8088 to answer"
tries=0
until docker exec "$name" bash -c 'exec 3<>/dev/tcp/127.0.0.1/8088' 2>/dev/null; do
	tries=$((tries + 1))
	[ "$tries" -lt 60 ] || { echo "port 8088 never answered"; docker logs "$name"; exit 1; }
	sleep 0.5
done
echo "port 8088 answers"

echo "== start order (supervisor log)"
docker logs "$name" 2>&1 | grep -E 'msg=(started|ready)' | cut -c1-120
started="$(docker logs "$name" 2>&1 | sed -n 's/.* msg=started program=\([a-z]*\) .*/\1/p' | tr '\n' ' ')"

echo "== SIGTERM: docker stop sends it to the supervisor, which is PID 1"
docker stop -t 60 "$name" >/dev/null
docker logs "$name" 2>&1 | grep -E 'msg=(draining|terminating|exited)' | cut -c1-120
drained="$(docker logs "$name" 2>&1 | sed -n 's/.* msg=terminating program=\([a-z]*\)$/\1/p' | tr '\n' ' ')"
code="$(docker inspect "$name" --format '{{.State.ExitCode}}')"
echo "supervisor exit status: $code"

echo "== the one user: $(docker inspect "$name" --format '{{.Config.User}}')"

[ "$started" = "runtime workspace adapter " ] || { echo "start order was: $started"; exit 1; }
[ "$drained" = "runtime workspace adapter " ] || { echo "drain order was: $drained"; exit 1; }
[ "$code" = 0 ] || { echo "supervisor exited $code"; exit 1; }
echo "== ok: started and drained runtime, workspace, adapter"
