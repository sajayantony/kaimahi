# Full-suite deployment contracts

Status: proposed internal contracts, not an implemented deployment workflow.

Sequencing: this is a **post-#339 integration proposal**. Land and reconcile
#339's builder contracts first, then adapt its output into this deployment
handoff. This branch is based on main, not stacked on #339, and does not include
or replace that PR's builder implementation. #339's schema changes are not
prerequisites for reviewing these interfaces; concrete integration must track
the final merged #339 shape.

## Decision

Use one suite lifecycle above runtime-specific deployment adapters. Both local
kind and remote AKS select a target and invoke the same adapter `Deploy` action.
Lift is the user-facing lifecycle verb; deployment is its runtime effect.

```text
AgentSuite directory / registry reference
  -> Resolver: complete validated suite + OCI/logical identities
  -> Builder: one exact member/platform implementation (#339)
  -> member image set + execution contracts
  -> target bindings + observed runtime installation
  -> Adapter.Plan: every member, shared resource and declared relationship
  -> evaluation gate + exact-plan review
  -> Adapter.Deploy
  -> aggregate receipt and per-member/resource outcomes
  -> Adapter.Observe / runtime executions
```

`internal/kmx/suitedeploy` is an experimental contract proposal. It supplies
types, interfaces, plan snapshots, topology/compatibility checks and tests. It
does not implement resolvers, builders, runtime adapters, orchestration, image
metadata publication, CLI commands or console actions. Its Go values are
in-process contracts, not a stable persistence format. No new CRD is required.

## Handoff to the image builder

This PR does not change `agentsuite.Suite`, `agentsuite.BuildProfile`, or their
JSON Schemas. The `Suite` and `Execution` types here are internal proposed
handoff values. A later coordinated schema change must define how a build
profile selects an execution contract and how a built image carries it.

#339's `SandboxPlan` and `SandboxBuilder` remain the starting implementation.
The proposed `BuildOutput` extends its archive result with:

- the actual runnable platform-image descriptor and a separate retrieval location;
- source-suite OCI descriptor, exact agent, platform, composition and build profile;
- execution metadata and its descriptor;
- the existing AgentSuite sandbox-binding descriptor;
- explicit experimental/conformant status and diagnostics.

The archive is a transport output; its checksum is not the image manifest digest.
`Suite.LogicalDigest` hashes canonical `agentsuite.json`; the OCI suite manifest
digest identifies the packaged artifact. Neither substitutes for the other.

The builder selects an execution contract through its build profile and emits
matching metadata. `Execution` separates configuration/harness ABI, supported
agent-serving modes, lifecycle, named runtime inputs and required capabilities.
The mode protocol is not the inference protocol. AgentKit's protocol selector
must be mapped by a qualified adapter, not passed as an arbitrary environment
override. Fixed model URLs remain fixed until the harness supports runtime binding.

The image metadata schema, media type and placement must be agreed with #339.
This proposal does not add a competing image label or modify the current
AgentSuite wire format. Experimental #339 images are not automatically accepted
as conformant; completing and qualifying its output remains an integration gate.
Image-to-suite verification, descriptor bytes and execution metadata must be
validated by the resolver/builder integration, not inferred from these Go structs.

## Suite and implementation selection

`Suite` retains all agents, verified instructions, ToolProviders, compositions,
build profiles and invocation edges. A resolver must verify every graph reference
and instruction digest before returning it. Native and image execution are
explicit alternatives per member. A native adapter must refuse image inputs
rather than discard them and render a different implementation.

`Request` selects every member exactly once. A missing image, incompatible member,
unconsumed binding or unsupported invocation bound fails whole-suite planning.
`Request.Validate` checks basic topology and descriptor shape only; it is not a
replacement for suite validation, image verification or adapter semantic checks.

The suite-to-image association is an independent build-output record. Its
discovery and publication contract remain open; adding image digests to the same
source suite already embedded in those images would create circular identity.

## Target and binding

`Target` separates context (access locator), cluster UID, namespace and selected
agent runtime. Inference is a binding, not a runtime selector. `Installation`
reports the observed runtime version, identity and capability revision plus
accepted execution contracts, binding formats and reconciliation support.

`TargetBinding` is a versioned adapter-owned document containing configuration
and Secret references, never credential values. Its digest must cover the exact
canonical format and payload. Binding decoding/digest verification belongs to
the adapter; generic orchestration must not interpret vendor-specific bytes.
Operation stores own persistence and rehydration. A receipt is not a replay recipe.

## Plan and deployment

The adapter plans the whole suite, not a loop that creates one agent before
validating the next. It owns shared-resource deduplication, naming, dependency
ordering and activation of invocation relationships. Invocation edges are not
necessarily an acyclic startup graph. Unsupported semantics must be refused.

`Plan` freezes ordered native documents, apply/review classification, member
selection identities, target and installation facts. `NewPlan` checks complete
selection and declared compatibility; only a qualified adapter can establish
that the native documents faithfully represent the full suite. The plan digest
covers apply/review disposition so a reviewed document cannot silently become a
mutation. Accessors return independent copies.

The application evaluates the promotion gate and obtains review for that exact
plan. `Authorization` records the plan match, not cluster authentication or a
trust decision about supplied evidence. Adapters recheck installation, cluster,
resource and binding identities immediately before effects. They never provision
or delete infrastructure. No automatic retry follows an ambiguous mutation.

Receipts contain per-member/resource outcomes, including unattempted members and
unknown effects. Partial success is not atomic suite installation. Readiness is
not proof of an answer or a passing evaluation. Observe must distinguish absence
from unreadable state and retain a retired state for session registrations.
Retirement/reclamation APIs and durable operation recovery are follow-ups.

## Existing implementation and integration order

1. Reuse AgentSuite validation and #339 resolution; expose a complete snapshot.
2. Agree build-output/execution metadata with #339 and verify real image bindings.
3. Implement shared Lift orchestration and typed whole-suite planning (#279).
4. Wrap existing runtime Render/Deploy machinery in suite adapters. Orka already
   reconciles single-agent resources. The second adapter's exact-version
   create-only path must not be advertised as reconciliation support (#294/#238).
5. Prove local kind and remote AKS against each qualified runtime/version pair.
   Registry credentials used by KMX and node image-pull credentials are separate.
6. Integrate existing evaluation policy and #341's session evidence without
   treating host-reported identity as a verified image descriptor.
7. Build the console around Suite / Images / Deployments / Runs using these shared
   operations, not printed-plan parsing or local-live-agent prerequisites.

This follows #326's internal-first maturity direction. There is no exact public
interface-count commitment, and existing `agent.yaml` workflows remain intact.
The original standalone HTTP-image experiment is separate from this proposal.

## Review decisions

- Should execution metadata be embedded alongside the sandbox binding or exposed
  by a defined OCI attachment, and which component verifies the association?
- What concrete harness/mode combinations will the first two adapters qualify?
- How will one suite publish/discover a complete, unambiguous image set?
- Which binding identities and observations must be rechecked before Deploy?
- How should native and image members participate in one suite's evaluation gate?

## Tests

`go test ./internal/kmx/suitedeploy` checks complete selection, incompatible
contracts and bindings, create-only refusal, immutable plan snapshots, disposition
identity, local/remote target separation and exact-plan gate authorization. These
tests do not claim cluster deployment or builder conformance.
