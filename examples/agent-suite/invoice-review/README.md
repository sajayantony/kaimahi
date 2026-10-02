# Invoice review Agent Suite

This sample follows the incubation experiment in
`kaimahi-agents/kaimahi#273`: one coordinator, two immutable specialist
members, deterministic suite identity, local OCI Image Layout packaging, and
a read-only Substrate lift plan.

```sh
kmx suite inspect ./examples/agent-suite/invoice-review

kmx suite package ./examples/agent-suite/invoice-review \
  --output /tmp/invoice-review-oci

kmx suite inspect /tmp/invoice-review-oci

kmx suite run ./examples/agent-suite/invoice-review \
  --server 127.0.0.1:18080 \
  --project agentsuite-poc \
  --prompt "Should invoice 1042 be approved?"
```

`suite run` creates independent durable AgentSessions executions for the
purchasing and receiving specialists. It then supplies both recorded outputs
to the coordinator for explicit aggregation. Each session is annotated with
the same suite digest and run UID, plus its immutable member digest. Completed
specialist sessions are suspended after their outputs are committed, freeing
Substrate workers before the next member runs.

The declared `allowedAgents` graph is release metadata. The three actual
session executions are observed runtime evidence; the POC does not conflate
the two.
