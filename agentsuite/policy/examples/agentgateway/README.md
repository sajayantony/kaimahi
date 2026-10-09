# Agentgateway policy samples

[All policy samples and enforcement layers](../README.md).

These are **gateway-only samples**, not whole-agent enforcement. The language
currently expresses exact network authorities and A2A capability/peer requests.
It does not yet cover all agentgateway policies or full A2A authorization.

| Sample | Purpose |
|---|---|
| [`../../testdata/allow-mcr.json`](../../testdata/allow-mcr.json) | Portable request: no file writes; permit only `https://mcr.microsoft.com:443`; no outbound peer grants. |
| [`allow-mcr.json`](allow-mcr.json) | Generated-equivalent agentgateway v1.6.0 CONNECT/SNI configuration for the **network-authority subset only**. |
| [`../../testdata/a2a-peer-request.json`](../../testdata/a2a-peer-request.json) | Express an advertised coordinator skill plus a requested call to peer `summarizer`, skill `summarize`. No approved grant or deployed peer is implied. |
| [`a2a-deny-all.json`](a2a-deny-all.json) | Native gateway route marked A2A, with mandatory HTTP authorization `require: false`. Discovery and messages both return 403. |

## Allow `curl` to Microsoft Container Registry

From the spike worktree in PowerShell 7, start the pinned gateway:

```powershell
$image = 'ghcr.io/agentgateway/agentgateway@sha256:482921556876a503ad3675b29223b1897b63a316983e46797ff99ebf83a2a6a2'
$config = Get-Content -Raw .\agentsuite\policy\examples\agentgateway\allow-mcr.json
podman run --rm --name kaimahi-mcr-gateway `
  --sysctl net.ipv4.ip_unprivileged_port_start=0 `
  -p 127.0.0.1:18080:3000 $image -c $config
```

The container-scoped sysctl lets the non-root image bind its internal TLS
listener on port 443. It does not change the Windows or Podman machine's host
port policy. Only the proxy port is published, and only on loopback.

In another terminal:

```powershell
# Allowed, with normal end-to-end certificate verification. No --insecure.
curl.exe --noproxy "" --proxy http://127.0.0.1:18080 https://mcr.microsoft.com/v2/

# Denied: SNI is not allowlisted (CONNECT can succeed, then TLS is rejected).
curl.exe --noproxy "" --proxy http://127.0.0.1:18080 https://example.com/

# Denied: wrong destination port (CONNECT 404).
curl.exe --noproxy "" --proxy http://127.0.0.1:18080 https://mcr.microsoft.com:444/

# Denied: cleartext HTTP (405). Bare "curl mcr.microsoft.com" uses HTTP.
curl.exe --noproxy "" --proxy http://127.0.0.1:18080 http://mcr.microsoft.com/
```

The example intentionally permits HTTPS only. Use `curl.exe` on Windows to
avoid PowerShell's historical `curl` alias. `--noproxy ""` ensures an inherited
NO_PROXY value cannot bypass the explicit test proxy. A curl command **without
the proxy** is not governed by this sample.

The gateway inspects TLS SNI without decrypting application traffic. Its allowed
route has a **fixed backend** `mcr.microsoft.com:443`, not a caller-controlled
dynamic upstream. An HTTP Host header cannot turn an unlisted SNI into an
allowed route. Paths such as `/` and `/v2/` are both allowed; this is an
authority allowlist, not a path-specific rule. Redirects to other hosts require
separate grants. This is not sufficient for a complete image pull if artifacts
redirect to other registries/CDNs.

## Generate rather than hand-maintain native policy

```powershell
go run .\cmd\policy-spike -gateway-egress `
  -policy .\agentsuite\policy\testdata\allow-mcr.json
```

Output includes `scope`, `policyDigest`, `config`, and explicit `unenforced`
obligations. Save **only the `config` object** as an agentgateway config file;
the wrapper is the translation report. A unit test ensures the checked-in
native MCR sample matches this projection.

This mode cannot be mixed with deployment flags. It does not enable network
allowlists in the existing deny-all sandbox backend. That backend still rejects
nonempty allowlists instead of silently weakening the full policy. The gateway
projector rejects nonempty A2A peer/skill grants: a TLS tunnel cannot implement
identity- and operation-aware authorization.

## A2A and agentgateway expressiveness

| Policy concern | Portable language now | Gateway support / remaining work |
|---|---|---|
| Exact HTTPS host and port | `network.allow` | Implemented scoped CONNECT/SNI projection; verified against MCR. |
| Full URL path and HTTP method | Not represented | Add typed HTTP match semantics and normalization rules; HTTPS requires termination/inspection, not this passthrough configuration. |
| Default-deny HTTP/A2A route | Native sample | `authorization.rules[].require: false`; do not use an empty allow list, which permits traffic. |
| A2A advertised skills | `capabilities.skills` | Advertisement input only; runtime must publish a versioned AgentCard. |
| A2A peer and skill requests | `invocations.allow` | Identity binding and protocol-aware enforcement are not implemented; fail closed. |
| Caller JWT/mTLS, claim checks | Not represented | Deployment-owned identity bindings plus typed authorization conditions; secrets/JWKS material remain outside the portable artifact. |
| MCP tool/method, model access | Not represented | Need separate protocol actions, not a generic destination allowlist. |
| Rate limits, budgets, guardrails, external authorization | Not represented | Versioned optional capabilities/required controls; do not copy the whole native gateway schema into the portable spec. |
| Filesystem, bypass prevention | Filesystem intent exists | Not enforced by a gateway. Compose sandbox/CNI/RBAC controls before claiming whole-policy activation. |

The A2A route sample uses native `a2a: {}` to identify traffic, but that setting
alone does not authorize a peer or skill. Its backend is deliberately absent;
the rejection must happen at authorization, not by relying on a backend outage.
The smoke sends a discovery GET and a JSON-RPC message-shaped POST. This checks
the denial boundary, not compliance with every A2A protocol revision.

## Reproduce the positive and negative cases

With the same tools directory used for the original spike:

```powershell
.\scripts\ci\gateway-policy-smoke.ps1 -Tools $Tools -OutputDirectory "$Evidence\gateway-run"
```

The script builds the projector, validates both native samples with the pinned
binary, runs real curl requests, checks gateway route-rejection logs, and saves
the projection, response/status evidence and receipt. Containers use temporary
loopback ports and are stopped/removed in `finally`. The MCR positive control
must pass before any deny result is accepted. It requires internet access to
MCR; an offline failure is not a successful policy test.

Reference: [agentgateway v1.6.0 egress proxy](https://github.com/agentgateway/agentgateway/tree/v1.6.0/examples/traffic-egress-proxy).
See the [component plan](../../../../docs/agentsuite-governance-spike.md) and
[sajayantony/kaimahi#2](https://github.com/sajayantony/kaimahi/issues/2).
