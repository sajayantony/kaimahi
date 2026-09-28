#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)

kmx_bin=${KMX_BIN:-"$repo/bin/kmx"}
kubectl_bin=${KUBECTL_BIN:-kubectl}
kind_cluster=${KIND_CLUSTER:-kaimahi-otel-poc}
context=${KUBE_CONTEXT:-kind-$kind_cluster}
state_dir=${KMX_OTEL_STATE_DIR:-"${TMPDIR:-/tmp}/kaimahi-otel-poc"}
task=${TASK:-"Reply with exactly: hello from an OpenTelemetry traced agent"}
agent_name=${AGENT_NAME:-"otel-hello-$(date -u +%Y%m%d%H%M%S)"}
trace_output=${TRACE_OUTPUT:-"$state_dir/collector-traces.log"}

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

require podman
require curl
require "$kubectl_bin"
[[ -x "$kmx_bin" ]] || {
  echo "kmx binary not found at $kmx_bin; run make in the worktree" >&2
  exit 1
}

mkdir -p "$state_dir"

export CONTAINER_ENGINE=podman
export KIND_CLUSTER="$kind_cluster"
export KMX_HOME="$state_dir/kmx-home"
export KUBE_CONTEXT="$context"
export KUBECTL_BIN="$kubectl_bin"
export TRACE_OUTPUT="$trace_output"

case "${1:-run}" in
  run)
    "$kmx_bin" --container-engine podman --context "$context" up

    "$kubectl_bin" --context "$context" apply -f "$here/collector.yaml"
    "$kubectl_bin" --context "$context" -n otel-demo rollout status deployment/otel-collector \
      --timeout=5m

    "$here/adapters/orka.sh" enable

    "$kmx_bin" --container-engine podman --context "$context" agent create "$agent_name" \
      --namespace orka-system \
      --provider-type openai \
      --model qwen2.5:3b \
      --secret local-provider-key \
      --base-url http://ollama.ollama.svc.cluster.local:11434/v1 \
      --result-service-account orka-result-reader \
      --bundle-path "$state_dir/agents/$agent_name" \
      --task "$task" \
      --tail

    "$here/adapters/orka.sh" verify
    printf 'TRACE  detailed Collector output: %s\n' "$trace_output"
    ;;
  verify)
    "$here/adapters/orka.sh" verify
    ;;
  traces)
    "$here/adapters/orka.sh" traces
    ;;
  down)
    "$kmx_bin" --container-engine podman --context "$context" down
    ;;
  *)
    echo "usage: $0 run|verify|traces|down" >&2
    exit 2
    ;;
esac
