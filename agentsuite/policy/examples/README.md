# AgentSuite policy spike

**Portable policy -> runtime controls -> enforcement evidence.**
Capabilities advertise behavior; they do not grant permission.

| Sample | Implementation | Proves |
|---|---|---|
| [Deny all](../testdata/deny-all.json) | Landlock + seccomp + read-only filesystem; agentgateway `require: false` | File writes and new outbound calls fail; gateway requests return 403. |
| [Allow MCR](../testdata/allow-mcr.json) / [native gateway](agentgateway/allow-mcr.json) | CONNECT proxy, exact TLS SNI, fixed `mcr.microsoft.com:443` backend | HTTPS MCR succeeds; other hosts, ports and cleartext HTTP fail. Gateway-only: direct bypass remains possible. |
| [A2A request](../testdata/a2a-peer-request.json) / [native deny](agentgateway/a2a-deny-all.json) | Separate advertised skills and requested peer grants; native route denies every request | Discovery and message requests return 403. Does **not** implement general A2A authorization. |
| [CPU coordinator](kind-cpu-agents/coordinator.json) + [registry reader](kind-cpu-agents/registry-reader.json) | Two Python agents, Ollama `qwen3:0.6b`, dedicated agentgateways, Cilium NetworkPolicy, filesystem sandbox | Real CPU inference and delegated MCR access succeed; file writes, unauthorized routes and direct-network bypass fail. |

## Enforcement layers

```text
coordinator -> its gateway -> registry-reader -> its gateway -> MCR HTTPS
                    \                            \
                     +------> local Ollama <------+
```

- **Agentgateway:** fixed backends; only inference and explicitly bound skill routes pass.
- **Cilium:** each agent can contact only its own gateway. Agents have no DNS exception.
- **Landlock/seccomp:** deny filesystem mutation; containers are non-root, read-only and capability-free.
- **Ollama:** CPU-only, two CPUs / 3 GiB. Download access is removed before agents start.

Agentgateway is **not based on Cilium or Istio**. Cilium prevents network bypass;
agentgateway filters application traffic. This example does not use Istio.

## Run

Use PowerShell 7 and `$Tools` containing Go, kind and kubectl.
[Cluster setup and interactive request](../../../docs/agentsuite-governance-spike.md#cpu-backed-agent-example).

```powershell
.\scripts\ci\policy-smoke.ps1 -Tools $Tools -OutputDirectory .\evidence-deny
.\scripts\ci\gateway-policy-smoke.ps1 -Tools $Tools -OutputDirectory .\evidence-gateway
.\scripts\ci\policy-cpu-smoke.ps1 -Tools $Tools -OutputDirectory .\evidence-cpu
```

The CPU runner validates policies, generates controls, deploys agents and saves
objects plus a receipt. Real inference, zero VRAM use, denial probes and Cilium/gateway
evidence determine success—not model prose. Cluster resources remain inspectable.

**Limits:** bounded workflow; A2A REST-shaped messages with fixed, single-skill
endpoint bindings—not general A2A authorization or conformance. Identity uses
operator-controlled pod labels, not JWT/mTLS. Projection modes remain partial;
no production governance, native `kmx lift`, or privileged-admin isolation is claimed.

[Manual MCR curl commands](agentgateway/README.md) ·
[Design and remaining work](../../../docs/agentsuite-governance-spike.md) ·
[HTML object map](../../../docs/assets/agentsuite-governance.html)
