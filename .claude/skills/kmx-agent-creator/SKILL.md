---
name: kmx-agent-creator
description: Design and create a KMX agent from a natural-language request using the KMX MCP tools.
---

# KMX agent creator

Use the `kmx-agent-creator` MCP server configured in this repository.

1. Use `kmx_agent_schema` to obtain the current AgentIntent vocabulary.
2. Convert the user's request into AgentIntent requirements.
3. Call `kmx_agent_plan`.
4. Ask the user any questions returned by KMX instead of guessing.
5. Show the normalized plan, mutations, runtime, sandbox, and digest.
6. Call `kmx_agent_apply` only after the user explicitly approves the exact
   digest shown in the current conversation.

Never request credential values. Secret names, key names, namespaces,
provider endpoints, and cluster deployment mode are human-owned fields; leave
them missing unless the user states them.

A digest proves plan integrity, not human identity. Never describe it as an
authorization token.
