# KMX agent OpenTelemetry POC

> Proven locally on Podman on September 28, 2026. Experimental evidence only.

## Experience first

Run one command from the Kaimahi worktree:

```bash
cd poc/kmx-agent-otel
./run.sh
```

After preparing the local runtime and Collector, the script runs the equivalent
of:

```bash
kmx --context kind-kaimahi-otel-poc agent create otel-hello-... \
  --namespace orka-system \
  --provider-type openai \
  --model qwen2.5:3b \
  --secret local-provider-key \
  --base-url http://ollama.ollama.svc.cluster.local:11434/v1 \
  --result-service-account orka-result-reader \
  --task 'Reply with exactly: hello from an OpenTelemetry traced agent' \
  --tail
```

The rebuilt KMX CLI follows the selected runtime's execution log stream on
stderr while keeping the final answer on stdout. This is an excerpt from the
real September 28 run:

```text
Created Task/otel-hello-... (UID 93899893-...); waiting for execution and a retrievable answer.
TAIL  execution otel-hello-... logs
[pod/otel-hello-.../worker] Worker ai started task=orka-system/otel-hello-...
[pod/otel-hello-.../worker] Executing tool: recall_memory
[pod/otel-hello-.../worker] Executing tool: remember
[pod/otel-hello-.../worker] Task orka-system/otel-hello-... completed successfully
The response "hello from an OpenTelemetry traced agent" has been successfully proposed as a durable memory for review.
TRACE  verified 12 spans across 1 trace(s): OTLP GenAI attributes present; prompt and completion content absent
TRACE  detailed Collector output: .../kaimahi-otel-poc/collector-traces.log
```

To inspect the detailed Collector output afterward:

```bash
./run.sh traces
```

## Runtime-neutral `--tail`

`--tail` is not defined as an Orka Task or Kubernetes Pod operation. KMX passes
an opaque execution reference to an optional runtime observation interface and
prints the neutral log entries it receives. The runtime adapter owns how that
execution is located:

| Layer | Responsibility |
|---|---|
| KMX command | Request log following and render entries to stderr |
| Neutral runtime contract | `ExecutionRef`, `LogOptions`, `LogEntry`, and `ExecutionObserver.Logs` |
| Orka adapter | Map the execution to `orka.ai/task=<name>` and follow its worker Pod |
| Future kagent adapter | Map the same execution reference to kagent's native run/session resources |

The current KMX checkout registers Orka only and has retired its earlier kagent
implementation. This POC therefore demonstrates Orka live and validates the
same neutral observation contract with a kagent runtime identity in tests; it
does **not** claim that kagent is currently runnable from this branch.

## Telemetry separation of concerns

The layout follows
[kaimahi-agents/kaimahi#194](https://github.com/kaimahi-agents/kaimahi/issues/194)
and
[kaimahi-agents/kaimahi#224](https://github.com/kaimahi-agents/kaimahi/issues/224):

| Layer | File | Responsibility |
|---|---|---|
| Developer flow | `run.sh` | Compose setup, one execution, live logs, and semantic verification |
| Telemetry backend | `collector.yaml` | Independently deploy an OTLP receiver and debug exporter |
| Runtime adapter | `adapters/orka.sh` | Contain Orka telemetry activation and Kubernetes patch mechanics |
| Workload intent | KMX agent inputs | Instructions, model reference, and task; no Collector endpoint or telemetry ownership |

The POC does not put Orka, kagent, Kubernetes, OTLP endpoints, or Collector
ownership into the portable workload contract. Orka remains the telemetry
producer; the Collector remains independently operated; KMX composes the
developer experience and consumes neutral execution observations.

## Why the trace export works

KMX currently pins Orka `v0.1.3`. That release already:

- gates telemetry behind `--enable-telemetry` (`--enable-tracing` is an alias);
- reads standard `OTEL_EXPORTER_OTLP_*` environment variables;
- propagates worker-reachable, non-secret OTLP settings into AI worker Jobs;
- injects W3C trace context into workers; and
- wraps model and tool calls with GenAI spans.

The adapter enables that existing producer and sets:

```text
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://otel-collector.otel-demo.svc.cluster.local:4317
OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=grpc
OTEL_EXPORTER_OTLP_TRACES_INSECURE=true
```

The `http://` scheme is required by the Go OTLP gRPC exporter in this setup.

## Verification contract

The POC waits for and asserts:

- `service.name=orka-ai-worker`
- `gen_ai.operation.name`
- `gen_ai.provider.name`
- `gen_ai.request.model`
- `gen_ai.usage.input_tokens`
- `gen_ai.usage.output_tokens`
- `orka.task.id`

It fails if the output contains `gen_ai.input.messages`,
`gen_ai.output.messages`, or `gen_ai.system_instructions`. `verify` prints one
summary line; `traces` prints the detailed span and attribute subset.

The authoritative conventions are the standalone
`open-telemetry/semantic-conventions-genai` repository. Its client and agent
span documents are Development status. Orka `v0.1.3` identifies its emitted
subset with schema URL `gen-ai-dev/1.42.0-dev`, so this POC verifies the
intersection Orka emits rather than claiming compatibility with every field on
the repository's moving `main` branch.

## Defaults and commands

- cluster: `kaimahi-otel-poc`
- context: `kind-kaimahi-otel-poc`
- KMX state: `${TMPDIR}/kaimahi-otel-poc`
- Collector: `otel/opentelemetry-collector-contrib:0.161.0`
- trace transport: OTLP/gRPC
- Orka API probe port: `19181`

```bash
TASK='Say hello and identify your model.' ./run.sh
TRACE_OUTPUT=/tmp/agent-traces.log ./run.sh verify
./run.sh traces
./run.sh down
```

## Primary sources

- OpenTelemetry GenAI conventions:
  <https://github.com/open-telemetry/semantic-conventions-genai/tree/main/docs/gen-ai>
- GenAI client spans:
  <https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-spans.md>
- GenAI agent and framework spans:
  <https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-agent-spans.md>
- Orka `v0.1.3` observability guide:
  <https://github.com/orka-agents/orka/blob/v0.1.3/website/docs/guides/observability.md>
- Orka `v0.1.3` telemetry initialization:
  <https://github.com/orka-agents/orka/blob/v0.1.3/internal/tracing/tracing.go>
- Orka `v0.1.3` GenAI provider wrapper:
  <https://github.com/orka-agents/orka/blob/v0.1.3/internal/llm/tracing.go>
