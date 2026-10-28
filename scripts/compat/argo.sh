#!/usr/bin/env bash
set +x
set -euo pipefail
umask 077

# Every Kubernetes operation names the disposable kubeconfig and context.
readonly argo_version=v4.1.2
readonly manifest_sha=adac8370b9c0470acd3a71cf4139d40d7b8e995a774c1a639864b3db0e601a83
readonly node_image='kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5'
repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
readonly repo_root

if [[ ${1:-} == --help ]]; then
  echo 'Usage: scripts/compat/argo.sh [--check]'
  echo 'Creates a disposable Docker/kind cluster, verifies Argo HTTPS/auth and managed forwarding, then deletes it.'
  exit 0
fi
if [[ $# -gt 1 || ( $# -eq 1 && $1 != --check ) ]]; then
  echo 'Expected --check, --help, or no arguments' >&2
  exit 2
fi
missing=0
for program in docker kind kubectl go curl openssl python3; do
  if ! command -v "$program" >/dev/null; then echo "Missing prerequisite: $program" >&2; missing=1; fi
done
if [[ $missing == 1 ]]; then exit 1; fi
docker info >/dev/null
if [[ ${1:-} == --check ]]; then echo 'Disposable compatibility prerequisites available'; exit 0; fi

compat_state=$(mktemp -d "${TMPDIR:-/tmp}/micko-compat.XXXXXXXX")
compat_cluster="micko-compat-$(basename "$compat_state" | tr '[:upper:].' '[:lower:]-')"
compat_owned=0
cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [[ $compat_owned == 1 ]]; then
    if ! kind delete cluster --name "$compat_cluster" --kubeconfig "$compat_state/kubeconfig"; then
      echo "Cleanup failed: run kind delete cluster --name $compat_cluster" >&2
      status=1
    fi
  fi
  rm -rf "$compat_state"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if kind get clusters 2>/dev/null | python3 -c 'import sys; name=sys.argv[1]; sys.exit(0 if name in sys.stdin.read().splitlines() else 1)' "$compat_cluster"; then
  echo 'Refusing to reuse an existing cluster' >&2; exit 1
fi
compat_port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
cat > "$compat_state/kind.yaml" <<YAML
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 32746
    hostPort: $compat_port
    listenAddress: "127.0.0.1"
    protocol: TCP
YAML
compat_owned=1
kind create cluster --name "$compat_cluster" --image "$node_image" --kubeconfig "$compat_state/kubeconfig" --config "$compat_state/kind.yaml" --wait 180s
export KUBECONFIG="$compat_state/kubeconfig"
k() { kubectl --kubeconfig "$compat_state/kubeconfig" --context "kind-$compat_cluster" "$@"; }

curl --fail --silent --show-error --location --retry 3 \
  "https://github.com/argoproj/argo-workflows/releases/download/$argo_version/install.yaml" -o "$compat_state/install.yaml"
python3 - "$compat_state/install.yaml" "$manifest_sha" <<'PY'
import hashlib, pathlib, sys
if hashlib.sha256(pathlib.Path(sys.argv[1]).read_bytes()).hexdigest() != sys.argv[2]:
    raise SystemExit('Pinned Argo manifest checksum differs; refusing to install')
PY
k create namespace argo
k label namespace argo app.kubernetes.io/part-of=micko-compat
k apply --server-side -n argo -f "$compat_state/install.yaml" >/dev/null
k wait --for=condition=Established crd/workflows.argoproj.io crd/workflowtemplates.argoproj.io crd/clusterworkflowtemplates.argoproj.io crd/cronworkflows.argoproj.io --timeout=120s

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=micko-compat-ca \
  -keyout "$compat_state/ca.key" -out "$compat_state/ca.crt" >"$compat_state/openssl.log" 2>&1
openssl req -new -newkey rsa:2048 -nodes -subj /CN=argo-server \
  -keyout "$compat_state/tls.key" -out "$compat_state/tls.csr" >>"$compat_state/openssl.log" 2>&1
cat > "$compat_state/tls.ext" <<'CONF'
subjectAltName=IP:127.0.0.1,DNS:localhost,DNS:argo-server,DNS:argo-server.argo.svc
extendedKeyUsage=serverAuth
basicConstraints=CA:FALSE
CONF
openssl x509 -req -days 1 -in "$compat_state/tls.csr" -CA "$compat_state/ca.crt" -CAkey "$compat_state/ca.key" \
  -CAcreateserial -extfile "$compat_state/tls.ext" -out "$compat_state/tls.crt" >>"$compat_state/openssl.log" 2>&1
k -n argo create secret tls micko-compat-tls --cert="$compat_state/tls.crt" --key="$compat_state/tls.key" >/dev/null
k -n argo patch deployment argo-server --type=strategic \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"argo-server","args":["server","--auth-mode=client","--secure=true","--tls-certificate-secret-name=micko-compat-tls"]}]}}}}' >/dev/null
k -n argo patch service argo-server --type=strategic \
  -p '{"spec":{"type":"NodePort","ports":[{"port":2746,"targetPort":2746,"nodePort":32746}]}}' >/dev/null
k apply -f "$repo_root/scripts/compat/resources.yaml" >/dev/null
k -n argo rollout status deployment/argo-server --timeout=240s
k -n argo rollout status deployment/workflow-controller --timeout=240s
k -n micko-compat create token micko-runner --duration=30m > "$compat_state/runner.token"
k -n micko-compat create token micko-noaccess --duration=30m > "$compat_state/forbidden.token"
export MICKO_COMPAT_CONFIG="$compat_state/config.yaml"
cat > "$MICKO_COMPAT_CONFIG" <<YAML
currentProfile: direct
profiles:
  direct:
    server: https://127.0.0.1:$compat_port
    namespace: micko-compat
    tokenFile: $compat_state/runner.token
    caFile: $compat_state/ca.crt
  managed:
    server: https://127.0.0.1:2746
    namespace: micko-compat
    tokenFile: $compat_state/runner.token
    caFile: $compat_state/ca.crt
    kubeContext: kind-$compat_cluster
    service: argo-server
    serviceNamespace: argo
    remotePort: 2746
YAML
export MICKO_COMPAT_SERVER="https://127.0.0.1:$compat_port"
export MICKO_COMPAT_CA="$compat_state/ca.crt"
export MICKO_COMPAT_TOKEN="$compat_state/runner.token"
export MICKO_COMPAT_FORBIDDEN_TOKEN="$compat_state/forbidden.token"
export MICKO_COMPAT_CLUSTER="$compat_cluster"
export XDG_CONFIG_HOME="$compat_state/config"
export XDG_STATE_HOME="$compat_state/state"
cd "$repo_root"
MICKO_COMPAT=1 go test -tags=compat ./tests/compat/... -count=1 -v -timeout=12m
