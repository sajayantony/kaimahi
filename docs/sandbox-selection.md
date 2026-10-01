# Sandbox selection POC

KMX treats the agent runtime and its code-execution sandbox as separate choices.
Orka or Kagent owns the agent lifecycle; the sandbox backend contains untrusted
agent-generated code or a tool implementation.

Request automatic placement while creating an agent:

```bash
kmx agent create report-writer \
  --runtime orka \
  --sandbox auto \
  --sandbox-language javascript \
  # existing provider, model, namespace, and Secret flags...
```

The POC resolves the smallest compatible backend:

| Requirements | Selection |
|---|---|
| JavaScript only; no shell, native packages, image, or device | `hyperlight-js` |
| Linux ABI, shell, native packages, Python, Node.js, or another ordinary Linux program | `unikraft` |
| OCI image or device access | `pod` |

The resolved backend and requirements are written into `agent.yaml`, so the
portable digest captures the decision. Orka and Kagent render the same
`sandbox.kaimahi.dev/backend` and `sandbox.kaimahi.dev/requirements`
annotations. A platform adapter can consume those annotations to provision
Hyperlight JS, Hyperlight with a Unikraft guest, or a full pod sandbox.

An explicit backend is still checked against the requirements. KMX refuses an
underspecified `auto` request and refuses choices such as Python on
`hyperlight-js` or device access on `unikraft`; it never silently escalates an
explicit choice.

This branch intentionally stops at a placement contract. It does not install a
VMM, container runtime, controller, or Kubernetes `RuntimeClass`, and it does
not claim the annotations are enforced until an adapter reports observed
evidence from the selected backend.
