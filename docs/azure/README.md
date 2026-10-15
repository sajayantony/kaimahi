# Guidance for Azure

An agent-followable runbook for a small, disposable AKS development cluster:
**Create -> Prove -> Lift**. Start with one CPU node; inference is optional
test infrastructure, not a requirement for developing Kaimahi.

**Boundary:** this is a proposed cloud recipe, not continuously qualified Azure
automation. The pinned model has bounded kind CI coverage, not an AKS performance
guarantee. Use a current checkout build, not an older tagged CLI. This guide uses
Azure CLI for minimal infrastructure and KMX for runtime/evaluation/Lift; it does
not introduce a cloud command or a KMX-compatible infrastructure receipt.
See [AKS compatibility provisioning](../aks.md),
[Azure discovery history](../azure-discovery-performance.md) and
[cloud implementation extraction](https://github.com/kaimahi-agents/kaimahi/issues/223).
Other providers can have sibling guides; no provider framework is required.

## 1. Inputs, consent and preflight

**Agent contract:** execute one stage at a time, check every exit status and
retain its evidence before continuing. Stop on unreadable state, occupied names,
partial creation or a failed assertion. Never adopt, overwrite, retry an
ambiguous write, open a model publicly, or delete resources to make a step pass.

Obtain operator approval for the exact subscription, region, names, API access,
billable resources and inference test; disclose eventual data-loss implications. Do not infer
permission from a cluster's friendly name. Destructive cleanup needs separate
approval; setup approval is not teardown approval.

| Input | Starter choice / requirement |
|---|---|
| Subscription | Explicit operator-selected name or ID; resolve and retain its immutable ID |
| Region | `westus3`; use `westus2` only after checking it and obtaining approval |
| Cluster / resource groups | New dedicated names, including the AKS-managed node group |
| Compute | Free control plane, one `Standard_D4as_v5` amd64 node: 4 vCPU / 16 GiB |
| Storage / network | 64-GiB managed OS disk, Azure CNI Overlay/Cilium, Standard load balancer and outbound IP |
| Access | Entra ID/Azure RBAC, local accounts disabled, API restricted to an approved public IPv4 CIDR |
| Optional inference | Pinned Qwen 3.5 2B Q4 CPU model, one test at a time; no GPU or model pool |
| Local state | New private directory outside checkout/workspace/cache; never commit receipts, kubeconfig or live identifiers |

The model's 4-GiB memory limit leaves room for runtime/system pods on a 16-GiB
VM, but Kubernetes allocatable is smaller than VM capacity. Limit inference to
three CPUs and three threads to leave CPU headroom. This is a sizing estimate:
check actual scheduling, memory pressure and a completed answer. No concurrency,
latency or high-availability guarantee; one node cannot survive its own restart.

As of 2026-10-15, public Linux `Standard_D4as_v5` compute in West US 3 was
approximately **$0.172/hour (~$126 per 730-hour month)**, excluding disk, load
balancer, IP, egress and tax. Recheck the
[Azure pricing calculator](https://azure.microsoft.com/pricing/calculator/).
Do not create ACR, paid monitoring or a dedicated model pool by default.

Commands below use **Bash on Linux, macOS or WSL**, with `az`, a compatible
`kubectl`, `kubelogin`, Go, Python 3, curl and Git in that same environment.
Authenticate Azure there; do not assume Windows credentials/tools are available
inside WSL. KMX can obtain its pinned Helm toolchain. Use the Linux build inside
WSL on Windows: the current checkout's Unix filesystem guards do not compile as
a native Windows CLI. Do not paste Bash into PowerShell.

Set reviewed inputs in one shell; placeholders are not defaults:

```bash
set -euo pipefail
umask 077
unset KAIMAHI_CONFIRM
SUBSCRIPTION='<subscription-name-or-id>'
LOCATION=westus3
RG='<new-dedicated-resource-group>'
CLUSTER='<new-cluster-name>'
NODE_RG='<new-node-resource-group>'
API_CIDR='<approved-public-ipv4>/32'
K8S_VERSION='<reviewed-supported-patch>'
STATE='<absolute-private-state-directory>'
REPO="$(pwd)"
mkdir "$STATE"                         # must not already exist
az account show --subscription "$SUBSCRIPTION" -o json > "$STATE/account.json"
SUB_ID="$(python3 -c 'import json,sys; a=json.load(open(sys.argv[1])); assert a["state"]=="Enabled"; print(a["id"])' "$STATE/account.json")"
az vm list-usage --subscription "$SUB_ID" --location "$LOCATION" -o table
az vm list-skus --subscription "$SUB_ID" --location "$LOCATION" \
  --resource-type virtualMachines --size Standard_D4as_v5 --all -o json
az aks get-versions --subscription "$SUB_ID" --location "$LOCATION" -o table
go build -o "$STATE/kmx" ./cmd/kmx
KMX="$STATE/kmx"
"$KMX" version
"$KMX" lift --help
```

**Gate:** verify remaining regional and Dasv5-family quota (including upgrade
surge), no applicable SKU restriction, a supported non-preview Kubernetes patch
(`K8S_VERSION`), registered Azure providers and permission to create resources
and scoped role assignments. Review the account identity privately, never paste
it into issue evidence. Stop if any prerequisite is unknown.

## 2. Create the cluster and prove access

Require `az group exists` to return exactly `false` for both group names, and
verify that cluster-scoped Azure policies do not require extra billed features.
Then obtain the scoped creation approval from stage 1.

```bash
test "$(az group exists --subscription "$SUB_ID" --name "$RG" -o tsv)" = false
test "$(az group exists --subscription "$SUB_ID" --name "$NODE_RG" -o tsv)" = false
az group create --subscription "$SUB_ID" --name "$RG" --location "$LOCATION" \
  --tags purpose=kaimahi-development -o json > "$STATE/resource-group.json"
az aks create --subscription "$SUB_ID" --resource-group "$RG" --name "$CLUSTER" \
  --location "$LOCATION" --kubernetes-version "$K8S_VERSION" --tier free \
  --node-count 1 --node-vm-size Standard_D4as_v5 --nodepool-name system \
  --node-osdisk-type Managed --node-osdisk-size 64 --os-sku Ubuntu \
  --node-resource-group "$NODE_RG" --enable-managed-identity --no-ssh-key \
  --network-plugin azure --network-plugin-mode overlay \
  --network-dataplane cilium --network-policy cilium \
  --load-balancer-sku standard --outbound-type loadBalancer \
  --load-balancer-managed-outbound-ip-count 1 \
  --enable-aad --enable-azure-rbac --disable-local-accounts \
  --api-server-authorized-ip-ranges "$API_CIDR" \
  --auto-upgrade-channel patch --tags purpose=kaimahi-development \
  -o json > "$STATE/cluster.json"
```

Retain acknowledged resource IDs immediately, including the managed node group
and its resources. If creation fails, retain the group record and inspect the
exact target read-only; a timeout is not absence and does not authorize a retry.
Do not manufacture an ownership record from names found after an ambiguous write.

Resolve `CLUSTER_ID` from the acknowledged `cluster.json`. Resolve the approved
operator's object ID into `OPERATOR_ID` (for a human login,
`az ad signed-in-user show --query id -o tsv`; automation must name its own
approved principal instead). Assign only cluster-scoped Kubernetes administration:

```bash
CLUSTER_ID="$(python3 -c 'import json,sys; a=json.load(open(sys.argv[1])); assert a["provisioningState"]=="Succeeded"; print(a["id"])' "$STATE/cluster.json")"
OPERATOR_ID="$(az ad signed-in-user show --query id -o tsv)"
az role assignment create --subscription "$SUB_ID" --scope "$CLUSTER_ID" \
  --role 'Azure Kubernetes Service RBAC Cluster Admin' \
  --assignee-object-id "$OPERATOR_ID" --assignee-principal-type User \
  -o json > "$STATE/cluster-role.json"
export KUBECONFIG="$STATE/kubeconfig"
test ! -e "$KUBECONFIG"
az aks get-credentials --subscription "$SUB_ID" --resource-group "$RG" \
  --name "$CLUSTER" --file "$KUBECONFIG" --context "$CLUSTER"
kubelogin convert-kubeconfig -l azurecli
CTX="$CLUSTER"
kubectl --context "$CTX" wait --for=condition=Ready nodes --all --timeout=5m
kubectl --context "$CTX" get nodes -o wide
kubectl --context "$CTX" describe nodes
```

Use the correct principal type for automation, not `User`. Wait for role
propagation with bounded read-only checks; never bypass failure with admin
credentials or an unrestricted API. Keep this isolated kubeconfig and explicit
contexts for all later commands; do not alter the user's shared current-context.
The approved CIDR must match traffic to the Kubernetes API, which can differ
from browser/proxy egress. On connection resets/timeouts, diagnose that route and
obtain approval for any exact CIDR correction; never broaden to all addresses.
Azure management-plane readiness checks do not establish workstation API access.

**Gate:** Azure reports `Succeeded`/`Running`; exactly one Ready amd64 node has
the reviewed VM size, usable allocatable resources and no pressure conditions.
Record the kube-system namespace UID as cluster identity. Check no paid add-ons
were enabled. Infrastructure Ready is not runtime or inference proof.

## 3. Install Orka, then optionally prepare CPU inference

Review [Orka installation and backup limits](../orka.md). Inspect existing CRDs,
Helm releases and controller Deployments before installing. An occupied/partial
installation requires investigation, not `helm upgrade --force`.

```bash
"$KMX" --context "$CTX" orka install --provider - --no-apply
KAIMAHI_CONFIRM="$CTX" "$KMX" --context "$CTX" orka install --provider -
"$KMX" --context "$CTX" orka status
kubectl --context "$CTX" -n orka-system get deployments,pvc
kubectl --context "$CTX" -n orka-system get serviceaccount orka-result-reader
```

**Gate:** pinned Orka v0.2.0 harness-v2 controller Ready, PVC Bound and namespaced
Task-get result access installed. `--provider -` deliberately creates no inference
Provider. Back up the chart's snapshot key with its data, never print Secret
values. If no execution test is requested, stop here and report inference/Lift
as not attempted.

For the test, use the existing reviewed model inputs, not the CI qualification
runner. Create a **private copy** of
[`native-suite-model.yaml`](../../scripts/ci/native-suite-model.yaml), retaining
its exact image digest, ConfigMap mount and ClusterIP-only Service. Change only
its CPU limit to `"3"`. Copy
[`eval-loop-model.yaml`](../../scripts/ci/eval-loop-model.yaml) privately and
change only `threads: 4` to `threads: 3`; retain the exact model, Q4 weights,
4096-token context and CPU backend. These copies are operator-owned test
manifests, not a supported model installer or a command to apply CI indiscriminately.

```bash
sed 's/cpu: "4"/cpu: "3"/' "$REPO/scripts/ci/native-suite-model.yaml" > "$STATE/model.yaml"
sed 's/threads: 4/threads: 3/' "$REPO/scripts/ci/eval-loop-model.yaml" > "$STATE/model-config.yaml"
```

Review the copied manifests, ensure the three `native-suite-aikit` names are
absent in `orka-system` with successful reads, and obtain test-workload consent.
Then create them, without `apply`/replacement:

```bash
kubectl --context "$CTX" -n orka-system create configmap native-suite-aikit \
  --from-file=config.yaml="$STATE/model-config.yaml" -o json > "$STATE/model-configmap.json"
kubectl --context "$CTX" create -f "$STATE/model.yaml" -o json > "$STATE/model-resources.json"
kubectl --context "$CTX" -n orka-system rollout status deployment/native-suite-aikit --timeout=8m
kubectl --context "$CTX" -n orka-system get pods
```

Inspect scheduling/allocatable memory if Pending; do not weaken requests or
increase the cluster bill without approval. In a second shell with the same
isolated `KUBECONFIG` and `CTX`, forward the Service to a free literal-loopback
port in a tracked helper process, not a public LoadBalancer:

```bash
kubectl --context "$CTX" -n orka-system port-forward --address 127.0.0.1 \
  service/native-suite-aikit 18091:8080
```

Back in the original shell, send one bounded Chat Completions request using
[`eval-loop-warmup.json`](../../scripts/ci/eval-loop-warmup.json):

```bash
HTTP_CODE="$(curl --silent --show-error --max-time 180 \
  -H 'Content-Type: application/json' \
  --data-binary "@$REPO/scripts/ci/eval-loop-warmup.json" \
  --output "$STATE/model-response.json" --write-out '%{http_code}' \
  http://127.0.0.1:18091/v1/chat/completions)"
test "$HTTP_CODE" = 200
python3 - "$STATE/model-response.json" <<'PY'
import json, sys
response = json.load(open(sys.argv[1]))
assert response["model"] == "qwen-3.5-2b"
message = response["choices"][0]["message"]
assert message["role"] == "assistant" and message["content"].strip()
print("Direct CPU inference: passed")
PY
rm "$STATE/model-response.json"
```

Retain only payload-free test status privately. `/readyz` alone is insufficient.
Stop the specific forward process afterwards, including on failure; retain a
failed response privately only for diagnosis. No retry or silent model switch.

Check the `cpu-test-key` Secret and `cpu-test` Provider absent with successful
reads, then create an owner-reviewed
[keyless Provider](../orka.md#the-provider-connects-the-local-model-without-a-key).
The required Secret contains an explicit **noncredential** placeholder; record
only its UID, never dump Secret values:

```bash
printf %s not-used-by-this-endpoint | kubectl --context "$CTX" -n orka-system \
  create secret generic cpu-test-key --from-file=api-key=/dev/stdin \
  -o jsonpath='{.metadata.uid}' > "$STATE/model-secret-uid"
kubectl --context "$CTX" create -f - -o json > "$STATE/inference-provider.json" <<'YAML'
apiVersion: core.orka.ai/v1alpha1
kind: Provider
metadata:
  name: cpu-test
  namespace: orka-system
spec:
  type: openai
  defaultModel: qwen-3.5-2b
  baseURL: http://native-suite-aikit.orka-system.svc.cluster.local:8080/v1
  secretRef: {name: cpu-test-key, key: api-key}
YAML
```

Never substitute a real key into argv, logs or a ConfigMap.

**Gate:** model answers and Provider Ready for its current generation, with the
exact declared model and referenced key present. This is direct inference proof,
not full workspace evaluation.

## 4. Create, prove and Lift against the remote cluster

The smallest path needs **no registry**: publish to a local OCI layout and Lift
that exact digest to the remote context. The client reads the layout; it is not
uploaded to a node. Eval first places/evaluates on this same remote target, so
this Lift demonstrates **exact evaluated reuse**, not cross-cluster promotion.

Choose new workspace/layout paths and an existing private evidence directory
outside the workspace/cache. Write a short, LF-only `instructions.md`; use the
authored arithmetic case only as a smoke test and review every ordered case.

```bash
WORKSPACE="$STATE/workspace"
EVIDENCE="$STATE/passing.json"
LAYOUT="$STATE/suite-layout"
printf '%s\n' 'Answer briefly and in plain text.' > "$STATE/instructions.md"
"$KMX" create cpu-smoke --workspace "$WORKSPACE" \
  --model qwen-3.5-2b --instructions "$STATE/instructions.md"
KAIMAHI_CONFIRM="$CTX" "$KMX" eval --workspace "$WORKSPACE" \
  --to-context "$CTX" --to-namespace orka-system \
  --inference provider:cpu-test --out "$EVIDENCE"
"$KMX" push --workspace "$WORKSPACE" --evaluation "$EVIDENCE" \
  --to-layout "$LAYOUT" cpu-smoke:v1
```

**Gate:** every case passes and one new private passing receipt exists.
Capture `DIGEST` as the exact `sha256:...` **OCI manifest digest printed by
push**, not its graph digest, a tag resolution later, or a guessed hash.
Do not edit the workspace after evaluation. Receipts are unsigned observations,
not authenticated deployment approval.

```bash
"$KMX" lift "cpu-smoke@$DIGEST" --from-layout "$LAYOUT" \
  --to-context "$CTX" --to-namespace orka-system \
  --inference provider:cpu-test --evidence "$EVIDENCE" --plan -o json
KAIMAHI_CONFIRM="$CTX" "$KMX" lift "cpu-smoke@$DIGEST" --from-layout "$LAYOUT" \
  --to-context "$CTX" --to-namespace orka-system \
  --inference provider:cpu-test --evidence "$EVIDENCE" -o json
```

Review the GET-only plan before separately approving Apply. Require successful
receipt-bound `reused`/Ready identities matching the evaluation. Lift makes no
Task/model call; do not follow it with legacy `run --agent` on a suite-owned
Agent. Read an existing evaluation Task with `task result` if needed, without
resending. See [workspace evaluation](../suite-workspace.md#evaluate-the-whole-workspace)
and [digest Lift](../suite-workspace.md#digest-lift) for failure contracts.

For actual **local-to-remote promotion**, evaluate on a separate qualified source,
publish with matching evidence to an explicitly authorized registry (or use the
local layout), and prepare an absent destination pair with a Ready same-model
Provider and matching runtime. Use `--promote --plan`, review source-only evidence,
then separately consent to `--promote` Apply. Source evidence is not destination
evaluation. Occupied targets refuse; do not delete or relabel them to proceed.

## 5. Report, stop and clean up

Report each stage separately: infrastructure, runtime, direct inference, ordered
evaluation, exact publication, Lift planning and Lift Apply. Include attempted /
passed / failed / not attempted, package digest and identity-only outcomes, plus
remaining billable resources and private ownership-record location. Do not post
subscription IDs, endpoints, resource names, credentials, prompts or answers.
Do not close the guidance issue merely because a cluster became Ready.

For idle periods, obtain approval and run explicitly scoped `az aks stop`;
verify Stopped. Restart with `az aks start`, verify Running/Ready and recheck
identity, inference and evidence before reuse. Disks/IPs and other retained
resources may still bill while stopped.

For teardown, obtain separate data-loss approval, back up required runtime data
and snapshot key, and compare the live subscription, immutable resource IDs,
ownership records and group contents. Delete only the recorded owned group by
its exact ID; wait and verify absence, including the recorded node group and
resources. Retain the record on partial/unknown cleanup. This manually created
cluster has **no KMX lift ownership receipt**: `kmx aks down` is not its cleanup
path. Never infer ownership from a name/tag or recursively delete shared groups.
