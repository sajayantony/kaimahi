# Suite policy -> Kubernetes application

**Portable intent and deployment metadata are separate.** This bounded spike
runs coordinator -> registry-reader -> MCR, with real local CPU inference.

| Input | Purpose |
|---|---|
| `bundled/` | Complete AgentSuite; manifest pins `policy/suite-policy.json` by canonical digest. |
| `external/` + `suite-policy.json` | Same agents, policy supplied separately. |
| `suite-policy.json` | Deny defaults, logical model/HTTPS resources, per-agent requests and A2A skills. No Cilium/Kubernetes fields. |
| `binding-*.json` | Operator metadata: suite/policy digests, namespace, workload names, images, model and adapter selection. Not an approval. |

```text
suite + portable policy + deployment binding
  -> validate identities and required invocation edges
  -> separate Cilium / agentgateway / A2A / filesystem adapters
  -> Kubernetes application + cards + profiles + plan digest
```

`cilium.go` emits native CiliumNetworkPolicy resources; `application_runtime.go`
emits gateways and workloads; `a2a.go` emits cards. All live under
`internal/kmx/governance`. Cards advertise; gateway routes authorize the sample's
fixed skill endpoints. Landlock/seccomp deny writes; Cilium prevents bypass.

## Convert or run

From the repository root on Linux (full suite validation requires POSIX modes):

```sh
go run ./cmd/policy-spike -suite agentsuite/policy/examples/suite-application/bundled -binding agentsuite/policy/examples/suite-application/binding-bundled.json
go run ./cmd/policy-spike -suite agentsuite/policy/examples/suite-application/external -suite-policy agentsuite/policy/examples/suite-application/suite-policy.json -binding agentsuite/policy/examples/suite-application/binding-external.json
```

On Windows, the smoke stages validation through Linux without weakening checks.
After [Podman/kind/Cilium setup](../../../../docs/agentsuite-governance-spike.md#cpu-backed-agent-example):

```powershell
.\scripts\ci\policy-suite-smoke.ps1 -Tools $Tools -OutputDirectory .\evidence-suite-bundled -Mode bundled
.\scripts\ci\policy-suite-smoke.ps1 -Tools $Tools -OutputDirectory .\evidence-suite-external -Mode external
```

The runner creates a fresh namespace, applies `network` and `services`, installs
verified `profiles`, starts `model` with temporary `bootstrap` download access,
removes that access, then applies `workloads`. It saves split manifests, cards,
gateway configs and a receipt; checks real inference/MCR success and 23 denial
probe groups; leaves resources for inspection. The bundle is **not** one blindly
applicable Kubernetes manifest.

**Bounds:** two sample roles, one skill each, one logical model, one reader HTTPS
authority on port 443, invocation depth/concurrency one. Unsupported inputs fail.
Bundled/external policy conflicts fail; neither overrides the other. Existing
build/lift paths reject bundled policy they cannot enforce. No general A2A
authorization, production approval, image build or installed lift integration.
Operators, node labels, model server and the test validator remain trusted.

Regenerate fixtures and digest references after edits:
`python3 scripts/ci/policy-suite-sample.py agentsuite/policy/examples/suite-application --update`.
