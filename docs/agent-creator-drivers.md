# Copilot and Claude agent-creator drivers

KMX keeps agent creation deterministic while letting Copilot CLI or Claude
Code translate a natural-language request into a typed `AgentIntent`.

There are two entry points over one contract:

1. **MCP:** the coding agent owns the conversation and calls KMX tools.
2. **Headless draft:** KMX invokes an installed Copilot or Claude CLI once to
   draft `AgentIntent` JSON, then KMX validates and plans it normally.

Copilot and Claude are drafters, not deployment authorities. KMX owns
validation, missing-field questions, runtime and sandbox selection, rendering,
and application.

## Concept recording

Play the deterministic concept recording:

```bash
asciinema play docs/assets/agent-creator-copilot-demo.cast
```

The recording is intentionally simulated and performs no mutation. It shows
Copilot asking KMX for available contexts, creating the agent on a local kind
context, and later interpreting "lift" as a new context-specific plan that the
user must approve.

Regenerate it with:

```bash
python3 scripts/generate-agent-creator-cast.py
```

## Build the POC

```bash
make
```

This writes `bin/kmx`, which the committed Copilot agent profile and Claude
MCP configuration invoke.

## MCP experience

The server command is:

```bash
bin/kmx agent creator mcp
```

It exposes:

| Tool | Contract |
|---|---|
| `kmx_agent_schema` | Return the current AgentIntent JSON Schema |
| `kmx_agent_plan` | Validate and normalize intent, return focused questions or a review plan |
| `kmx_agent_apply` | Apply only when `approvedDigest` exactly matches the unchanged reviewed plan |

The repository includes:

- `.github/agents/kmx-agent-creator.agent.md` for Copilot CLI;
- `.mcp.json` and `.claude/skills/kmx-agent-creator/SKILL.md` for Claude Code.

Example:

```bash
copilot --agent kmx-agent-creator --prompt \
  "Create a read-only pull request reviewer that may run JavaScript but has no shell."
```

In Claude Code, ask for the `kmx-agent-creator` skill or describe the agent and
request that Claude use the KMX agent-creator MCP tools.

The coding agent should call plan repeatedly until KMX returns
`review_required`, show the exact digest and mutations, then ask for explicit
approval before apply.

## Headless driver experience

Copilot:

```bash
bin/kmx agent creator draft \
  --driver copilot \
  --description "Create a read-only pull request reviewer that may run JavaScript but has no shell." \
  --out intent.json
```

Claude:

```bash
bin/kmx agent creator draft \
  --driver claude \
  --description-file request.txt \
  --out intent.json
```

Both drivers run in an empty temporary directory, disable custom instructions,
interactive questions, and native tool use, bound output size and execution
time, and require one schema-compatible JSON object. KMX treats that object as
untrusted input.

Create the review plan:

```bash
bin/kmx agent creator plan --intent intent.json --out plan.json
```

If the result is `needs_input`, fill the returned human-owned fields and plan
again. KMX does not let the coding agent silently invent an explicit namespace,
Secret reference, model, or provider.

Review `plan.json`, including its mutations and sandbox decision. Then apply
the exact nested plan:

```bash
digest="$(jq -r .plan.digest plan.json)"
bin/kmx agent creator apply --plan plan.json --approve "$digest"
```

Offline mode writes the portable bundle and review artifact. Cluster mode
passes the normalized intent into the existing guarded `kmx agent create`
path.

## Approval boundary

The digest protects integrity: if any field changes after planning, apply
refuses it. It does **not** prove that a human supplied the digest. Interactive
Copilot and Claude profiles are instructed to request explicit approval, but
the trusted mutation boundary remains KMX's existing target confirmation and
deployment guards.

For stronger automation approval, a future integration should bind plans to a
Git review, signed policy decision, or external workflow identity rather than
presenting the digest itself as authorization.
