#!/usr/bin/env bash
set -euo pipefail

context=${KUBE_CONTEXT:?KUBE_CONTEXT is required}
kubectl_bin=${KUBECTL_BIN:-kubectl}
endpoint=${OTLP_TRACES_ENDPOINT:-http://otel-collector.otel-demo.svc.cluster.local:4317}
trace_output=${TRACE_OUTPUT:-}
api_probe_port=${ORKA_API_PROBE_PORT:-19181}

deployment=orka-controller-manager
namespace=orka-system

wait_api() {
  local log_file pid deadline attempt_deadline
  deadline=$((SECONDS + 90))

  while ((SECONDS < deadline)); do
    log_file=$(mktemp)
    "$kubectl_bin" --context "$context" -n "$namespace" port-forward \
      service/orka-api "$api_probe_port:8080" >"$log_file" 2>&1 &
    pid=$!
    attempt_deadline=$((SECONDS + 10))

    while ((SECONDS < attempt_deadline)) && kill -0 "$pid" 2>/dev/null; do
      if curl --silent --output /dev/null "http://127.0.0.1:$api_probe_port/" \
        2>/dev/null; then
        kill "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
        rm -f "$log_file"
        return
      fi
      sleep 1
    done

    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    rm -f "$log_file"
    sleep 2
  done

  echo "Timed out waiting for the Orka API after controller rollout" >&2
  return 1
}

enable() {
  local args
  args=$("$kubectl_bin" --context "$context" -n "$namespace" get deployment "$deployment" \
    -o jsonpath='{.spec.template.spec.containers[?(@.name=="manager")].args}')

  if [[ "$args" != *"--enable-telemetry"* && "$args" != *"--enable-tracing"* ]]; then
    "$kubectl_bin" --context "$context" -n "$namespace" patch deployment "$deployment" \
      --type=json \
      -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--enable-telemetry"}]'
  fi

  "$kubectl_bin" --context "$context" -n "$namespace" set env deployment/"$deployment" \
    --containers=manager \
    OTEL_EXPORTER_OTLP_TRACES_ENDPOINT="$endpoint" \
    OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=grpc \
    OTEL_EXPORTER_OTLP_TRACES_INSECURE=true

  "$kubectl_bin" --context "$context" -n "$namespace" rollout status deployment/"$deployment" \
    --timeout=5m
  wait_api
}

collect() {
  if [[ -z "$trace_output" ]]; then
    echo "TRACE_OUTPUT is required for collect" >&2
    exit 2
  fi

  local deadline=$((SECONDS + 90))
  while ((SECONDS < deadline)); do
    "$kubectl_bin" --context "$context" -n otel-demo logs deployment/otel-collector \
      --since=15m >"$trace_output"
    if grep -q 'gen_ai.operation.name' "$trace_output" &&
      grep -q 'gen_ai.provider.name' "$trace_output" &&
      grep -q 'gen_ai.request.model' "$trace_output" &&
      grep -q 'gen_ai.usage.input_tokens' "$trace_output" &&
      grep -q 'gen_ai.usage.output_tokens' "$trace_output" &&
      grep -q 'service.name: Str(orka-ai-worker)' "$trace_output" &&
      grep -q 'orka.task.id' "$trace_output"; then
      return
    fi
    sleep 3
  done

  echo "Timed out waiting for GenAI spans; collector output: $trace_output" >&2
  return 1
}

verify() {
  collect

  for attribute in \
    gen_ai.operation.name \
    gen_ai.provider.name \
    gen_ai.request.model \
    gen_ai.usage.input_tokens \
    gen_ai.usage.output_tokens \
    'service.name: Str(orka-ai-worker)' \
    orka.task.id; do
    grep -q "$attribute" "$trace_output"
  done

  if grep -Eq 'gen_ai\.(input|output)\.messages|gen_ai\.system_instructions' "$trace_output"; then
    echo "Trace output unexpectedly contains captured GenAI content" >&2
    return 1
  fi

  local spans traces
  read -r spans traces < <(
    awk '
      /Trace ID[[:space:]]*:/ {
        current = $4
        spans_by_trace[current]++
      }
      /gen_ai.operation.name:/ { genai_trace[current] = 1 }
      END {
        for (id in genai_trace) {
          traces++
          spans += spans_by_trace[id]
        }
        print spans + 0, traces + 0
      }
    ' "$trace_output"
  )
  printf 'TRACE  verified %s spans across %s trace(s): OTLP GenAI attributes present; prompt and completion content absent\n' \
    "$spans" "$traces"
}

traces() {
  collect
  grep -E \
    'Trace ID|Span ID|Name:|service.name|gen_ai\.|orka\.(task|agent)\.' \
    "$trace_output" || true
}

case "${1:-}" in
  enable)
    enable
    ;;
  collect)
    collect
    ;;
  verify)
    verify
    ;;
  traces)
    traces
    ;;
  *)
    echo "usage: $0 enable|collect|verify|traces" >&2
    exit 2
    ;;
esac
