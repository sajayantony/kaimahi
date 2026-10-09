param(
    [Parameter(Mandatory)][string]$Tools,
    [Parameter(Mandatory)][string]$OutputDirectory,
    [ValidateSet('bundled', 'external')][string]$Mode = 'bundled'
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path (Split-Path $PSScriptRoot)
$Tools = (Resolve-Path $Tools).Path
$kubectl = Join-Path $Tools 'kubectl.exe'
$kube = @('--kubeconfig', (Join-Path $Tools 'cpu-agents-kubeconfig'), '--context', 'kind-kaimahi-cpu-agents', '--request-timeout=180s')
$node = 'kaimahi-cpu-agents-control-plane'
$python = 'docker.io/library/python@sha256:cfe2e24a75302a15934d37c2d86412893c0aa934dc3a97cbb439d04c01890ca9'
if (Test-Path $OutputDirectory) { throw 'Output directory must be new' }
New-Item -ItemType Directory $OutputDirectory | Out-Null
$OutputDirectory = (Resolve-Path $OutputDirectory).Path
function Run([string]$Exe, [string[]]$Arguments, [string]$InputText = '') {
    $result = if ($InputText) { $InputText | & $Exe @Arguments 2>&1 } else { & $Exe @Arguments 2>&1 }
    if ($LASTEXITCODE -ne 0) { throw "$Exe failed: $($result -join "`n")" }
    return ($result -join "`n")
}
function Save([string]$Name, $Object) {
    $Object | ConvertTo-Json -Depth 100 | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory $Name)
}
function Apply($Object) { Run $kubectl ($kube + @('apply', '-f', '-')) ($Object | ConvertTo-Json -Depth 100) | Out-Null }
function List-Object($Items) { return @{apiVersion = 'v1'; kind = 'List'; items = @($Items)} }
function Wait-Deployment([string]$Name) {
    Run $kubectl ($kube + @('-n', $namespace, 'rollout', 'status', "deployment/$Name", '--timeout=180s')) | Out-Null
}
$namespace = 'suite-policy-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
$example = Join-Path $repo 'agentsuite\policy\examples\suite-application'
$binding = Get-Content (Join-Path $example "binding-$Mode.json") -Raw | ConvertFrom-Json -AsHashtable
$binding.namespace = $namespace
Save 'binding.json' $binding
$compiler = Join-Path $OutputDirectory 'policy-spike'
$oldOS, $oldArch, $oldCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
Push-Location $repo
try {
    $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    Run (Join-Path $Tools 'go\bin\go.exe') @('build', '-o', $compiler, '.\cmd\policy-spike') | Out-Null
} finally {
    $env:GOOS = $oldOS; $env:GOARCH = $oldArch; $env:CGO_ENABLED = $oldCGO
    Pop-Location
}
# Preserve POSIX validation: stage only the sample in a Linux filesystem rather
# than weakening file-mode checks for the Windows checkout.
$external = if ($Mode -eq 'external') { '-suite-policy /source/suite-policy.json' } else { '' }
$command = "cp -R /source/$Mode /tmp/suite && chmod -R go-w /tmp/suite && cp /runner/policy-spike /tmp/policy-spike && chmod +x /tmp/policy-spike && /tmp/policy-spike -suite /tmp/suite -binding /runner/binding.json $external"
$text = Run podman @('run', '--rm', '--network', 'none', '-v', "${example}:/source:ro",
    '-v', "${OutputDirectory}:/runner:ro", $python, 'sh', '-c', $command)
$bundle = $text | ConvertFrom-Json -AsHashtable
$text | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory 'bundle.json')
foreach ($part in @('network', 'services', 'model', 'workloads')) { Save "$part.json" (List-Object $bundle[$part]) }
Save 'agent-cards.json' $bundle.cards
Save 'gateway-configs.json' $bundle.gateways
Save 'bootstrap.json' $bundle.bootstrap
Run $kubectl ($kube + @('-n', 'kube-system', 'exec', 'ds/cilium', '--', 'cilium-dbg', 'status', '--brief')) | Write-Host
foreach ($gateway in $bundle.gateways.Values) {
    Run podman @('run', '--rm', $binding.gatewayImage, '--validate-only', '-c', ($gateway | ConvertTo-Json -Depth 100 -Compress)) | Out-Null
}
$uid = Run $kubectl ($kube + @('get', 'namespace', 'kube-system', '-o', 'jsonpath={.metadata.uid}'))
# Never apply into an existing namespace.
Run $kubectl ($kube + @('create', 'namespace', $namespace)) | Out-Null
Apply (List-Object $bundle.network)
Apply (List-Object $bundle.services)
foreach ($profile in $bundle.profiles.Values) {
    Save 'seccomp.json' $profile.seccomp
    $file = Join-Path $OutputDirectory 'seccomp.json'
    $target = '/var/lib/kubelet/seccomp/' + $profile.profilePath
    Run podman @('exec', $node, 'mkdir', '-p', '/var/lib/kubelet/seccomp/agentsuite') | Out-Null
    Run podman @('cp', $file, "${node}:$target") | Out-Null
    $hash = ((Run podman @('exec', $node, 'sha256sum', $target)) -split '\s+')[0]
    if ($hash -ne (Get-FileHash $file -Algorithm SHA256).Hash.ToLowerInvariant()) { throw 'Profile installation mismatch' }
    $label = 'agentsuite.dev/profile-' + $profile.profileDigest.Substring(7, 32) + '=installed'
    Run $kubectl ($kube + @('label', 'node', $node, $label, '--overwrite')) | Out-Null
}
Apply $bundle.bootstrap
try {
    Apply (List-Object $bundle.model)
    Wait-Deployment $binding.model.workload
    Run $kubectl ($kube + @('-n', $namespace, 'exec', "deployment/$($binding.model.workload)", '--', 'ollama', 'pull', $binding.model.model)) | Out-Null
} finally {
    Run $kubectl ($kube + @('-n', $namespace, 'delete', 'ciliumnetworkpolicy', 'model-bootstrap', '--wait=true')) | Out-Null
}
Apply (List-Object $bundle.workloads)
foreach ($agent in $binding.agents.Values) { Wait-Deployment $agent.workload; Wait-Deployment "gateway-$($agent.workload)" }
Apply @{apiVersion = 'v1'; kind = 'Pod'; metadata = @{name = 'validator'; namespace = $namespace; labels = @{app = 'validator'}}
    spec = @{automountServiceAccountToken = $false; containers = @(@{name = 'client'; image = $python
        command = @('python', '-B', '-c', 'import time; time.sleep(86400)')
        resources = @{requests = @{cpu = '10m'; memory = '32Mi'}; limits = @{cpu = '500m'; memory = '128Mi'}}})}}
Run $kubectl ($kube + @('-n', $namespace, 'wait', '--for=condition=Ready', 'pod/validator', '--timeout=120s')) | Out-Null
$before = Run $kubectl ($kube + @('-n', 'kube-system', 'exec', 'ds/cilium', '--', 'cilium-dbg', 'bpf', 'metrics', 'list', '-o', 'json'))
$check = @'
import http.client,json,socket,sys
def call(host,path,data=None,port=8080):
    c=http.client.HTTPConnection(host,port,timeout=180)
    try:
        c.request("GET" if data is None else "POST",path,None if data is None else json.dumps(data),{"Content-Type":"application/json"})
        r=c.getresponse(); raw=r.read()
        assert r.status==200,(host,path,r.status,raw.decode())
        return json.loads(raw)
    finally: c.close()
ip=socket.gethostbyname("mcr.microsoft.com")
for host,port in [("cpu-model",11434),("coordinator",8080),("registry-reader",8080),(ip,443),
                  ("gateway-coordinator",3001),("gateway-registry-reader",3001)]:
    with socket.create_connection((host,port),timeout=10): pass
result=call("coordinator","/skills/coordinate/message:send",{"message":{"messageId":"suite-proof","role":"ROLE_USER","parts":[{"text":"Check MCR."}]}})
e=result["message"]["metadata"]["policyExample"]
assert e["peer"]["registry"]["status"]==200 and e["peer"]["model"]["eval_count"]>0 and e["model"]["eval_count"]>0,result
cpu=call("cpu-model","/api/ps",port=11434)
assert len(cpu["models"])==1 and cpu["models"][0]["size_vram"]==0,cpu
cards={name:call(name,"/.well-known/agent-card.json") for name in ["coordinator","registry-reader"]}
probes=[]
for name in cards:
    for case in ["filesystem","direct-model","direct-peer","other-gateway","direct-mcr","child-bypass","dns-datagram","model-admin","forbidden-skill","forbidden-peer","wrong-method"]:
        p=call(name,"/probe",{"case":case,"mcrIP":ip}); p["agent"]=name; probes.append(p)
probes.append(call("registry-reader","/probe",{"case":"unlisted-sni","mcrIP":ip}))
print(json.dumps({"workflow":result,"cpu":cpu,"cards":cards,"probes":probes}))
'@
$result = Run $kubectl ($kube + @('-n', $namespace, 'exec', 'validator', '--', 'python', '-B', '-c', $check)) | ConvertFrom-Json -AsHashtable
foreach ($name in $bundle.cards.Keys) {
    $expected = $bundle.cards[$name] | ConvertTo-Json -Depth 100 -Compress
    $served = $result.cards[$name] | ConvertTo-Json -Depth 100 -Compress
    if ($served -cne $expected) { throw "Served AgentCard differs from generated card: $name" }
}
$after = Run $kubectl ($kube + @('-n', 'kube-system', 'exec', 'ds/cilium', '--', 'cilium-dbg', 'bpf', 'metrics', 'list', '-o', 'json'))
$countBefore = ($before | ConvertFrom-Json | Where-Object { $_.reason -eq 'Policy denied' -and $_.direction -eq 'egress' } | Measure-Object packets -Sum).Sum
$countAfter = ($after | ConvertFrom-Json | Where-Object { $_.reason -eq 'Policy denied' -and $_.direction -eq 'egress' } | Measure-Object packets -Sum).Sum
if ($countAfter -le $countBefore) { throw 'No Cilium policy-drop evidence' }
$logs = Run $kubectl ($kube + @('-n', $namespace, 'logs', 'deployment/gateway-registry-reader'))
if ($logs -notmatch 'tls.sni=example.com[^\r\n]*error="route not found"') { throw 'Missing gateway rejection evidence' }
$before | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory 'cilium-before.json')
$after | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory 'cilium-after.json')
$logs | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory 'gateway.log')
Save 'receipt.json' @{mode = $Mode; namespace = $namespace; clusterUID = $uid; planDigest = $bundle.planDigest
    suiteDigest = $bundle.suiteDigest; policyDigest = $bundle.policyDigest; bindingDigest = $bundle.bindingDigest
    policyDropDelta = $countAfter - $countBefore; result = $result}
$result.workflow | ConvertTo-Json -Depth 100
Write-Host "$Mode suite conversion: $($result.probes.Count) probe groups passed. Namespace: $namespace. Evidence: $OutputDirectory"
