param(
    [Parameter(Mandatory)][string]$Tools,
    [Parameter(Mandatory)][string]$OutputDirectory,
    [string]$Namespace = ('cpu-agents-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path (Split-Path $PSScriptRoot)
$Tools = (Resolve-Path $Tools).Path
$kubectl = Join-Path $Tools 'kubectl.exe'
$kube = @('--kubeconfig', (Join-Path $Tools 'cpu-agents-kubeconfig'), '--context', 'kind-kaimahi-cpu-agents', '--request-timeout=180s')
$node = 'kaimahi-cpu-agents-control-plane'
if ($Namespace -notmatch '^cpu-agents-[a-z0-9-]{1,40}$') { throw 'Use a dedicated cpu-agents-* namespace' }
if (Test-Path $OutputDirectory) { throw 'Output directory must be new' }
New-Item -ItemType Directory $OutputDirectory | Out-Null
$OutputDirectory = (Resolve-Path $OutputDirectory).Path
$objects = [Collections.Generic.List[object]]::new()
$pythonImage = 'docker.io/library/python@sha256:cfe2e24a75302a15934d37c2d86412893c0aa934dc3a97cbb439d04c01890ca9'
$gatewayImage = 'ghcr.io/agentgateway/agentgateway@sha256:482921556876a503ad3675b29223b1897b63a316983e46797ff99ebf83a2a6a2'
$modelImage = 'docker.io/ollama/ollama@sha256:b86366bb528bbf7f1424435d165028497a5b69bf6ddb4fa5a87102e2b79f44fb'
function Run([string]$Exe, [string[]]$ArgList, [string]$InputText = '') {
    $output = if ($InputText) { $InputText | & $Exe @ArgList 2>&1 } else { & $Exe @ArgList 2>&1 }
    if ($LASTEXITCODE -ne 0) { throw "$Exe failed: $($output -join "`n")" }
    return ($output -join "`n")
}
function Save([string]$Name, $Value) {
    $Value | ConvertTo-Json -Depth 100 | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory $Name)
}
function Create($Object, [switch]$Transient) {
    if (-not $Transient) { $objects.Add($Object) }
    Run $kubectl ($kube + @('create', '-f', '-')) ($Object | ConvertTo-Json -Depth 100) | Out-Null
}
function Selector([string]$App) { return @{podSelector = @{matchLabels = @{app = $App}}} }
function NetworkPolicy([string]$App, [array]$Ingress, [array]$Egress) {
    Create @{apiVersion = 'networking.k8s.io/v1'; kind = 'NetworkPolicy'; metadata = @{name = $App; namespace = $Namespace}
        spec = @{podSelector = @{matchLabels = @{app = $App}}; policyTypes = @('Ingress', 'Egress'); ingress = $Ingress; egress = $Egress}}
}
function Service([string]$App, [int[]]$Ports) {
    Create @{apiVersion = 'v1'; kind = 'Service'; metadata = @{name = $App; namespace = $Namespace}
        spec = @{selector = @{app = $App}; ports = @($Ports | ForEach-Object { @{name = "p$_"; port = $_; targetPort = $_} })}}
}
function IP([string]$Kind, [string]$Name) {
    $path = if ($Kind -eq 'service') { '{.spec.clusterIP}' } else { '{.status.podIP}' }
    return Run $kubectl ($kube + @('-n', $Namespace, 'get', $Kind, $Name, '-o', "jsonpath=$path"))
}
function Wait-Pods([string[]]$Names) {
    Run $kubectl ($kube + @('-n', $Namespace, 'wait', '--for=condition=Ready') + @($Names | ForEach-Object { "pod/$_" }) + @('--timeout=180s')) | Out-Null
}
$uid = Run $kubectl ($kube + @('get', 'namespace', 'kube-system', '-o', 'jsonpath={.metadata.uid}'))
Run $kubectl ($kube + @('-n', 'kube-system', 'exec', 'ds/cilium', '--', 'cilium-dbg', 'status', '--brief')) | Write-Host
foreach ($image in @($pythonImage, $gatewayImage, $modelImage)) {
    Run podman @('exec', $node, 'crictl', 'pull', $image) | Out-Null
}
$compiler = Join-Path $OutputDirectory 'policy-spike.exe'
Push-Location $repo
try { Run (Join-Path $Tools 'go\bin\go.exe') @('build', '-o', $compiler, '.\cmd\policy-spike') | Out-Null }
finally { Pop-Location }
$example = Join-Path $repo 'agentsuite\policy\examples\kind-cpu-agents'
$policies = @{}; $projections = @{}
foreach ($agent in @('coordinator', 'registry-reader')) {
    $file = Join-Path $example "$agent.json"
    $policies[$agent] = Get-Content $file -Raw | ConvertFrom-Json -AsHashtable
    $projections[$agent] = Run $compiler @('-gateway-sandbox', '-policy', $file) | ConvertFrom-Json -AsHashtable
    Save "$agent-sandbox.json" $projections[$agent]
}
# This binding is intentionally finite. Unknown policy requests cannot become
# arbitrary native gateway routes or silently ignored controls.
foreach ($agent in $policies.Keys) {
    $policy = $policies[$agent]
    if ($policy.agent -ne $agent -or $policy.capabilities.skills.Count -ne 1) { throw 'Unexpected example identity/skills' }
    $expected = if ($agent -eq 'coordinator') { @('http://cpu-model:11434', 'http://registry-reader:8080') } else { @('http://cpu-model:11434', 'https://mcr.microsoft.com:443') }
    $actual = @($policy.network.allow | ForEach-Object { "$($_.scheme)://$($_.host):$($_.port)" })
    if (Compare-Object $expected $actual) { throw "Unsupported network request for $agent" }
    $skill = if ($agent -eq 'coordinator') { 'coordinate' } else { 'inspect-registry' }
    if ($policy.capabilities.skills[0].id -ne $skill) { throw 'Unsupported advertised skill' }
}
$grants = $policies.coordinator.invocations.allow
if ($grants.Count -ne 1 -or $grants[0].agent -ne 'registry-reader' -or
    $grants[0].skills.Count -ne 1 -or $grants[0].skills[0] -ne 'inspect-registry' -or
    $policies['registry-reader'].invocations.allow.Count -ne 0) { throw 'Unsupported invocation binding' }

Create @{apiVersion = 'v1'; kind = 'Namespace'; metadata = @{name = $Namespace}}
Create @{apiVersion = 'networking.k8s.io/v1'; kind = 'NetworkPolicy'; metadata = @{name = 'default-deny'; namespace = $Namespace}
    spec = @{podSelector = @{}; policyTypes = @('Ingress', 'Egress'); ingress = @(); egress = @()}}
$dns = @{to = @(@{namespaceSelector = @{matchLabels = @{'kubernetes.io/metadata.name' = 'kube-system'}}
        podSelector = @{matchLabels = @{'k8s-app' = 'kube-dns'}}}); ports = @(@{protocol = 'UDP'; port = 53}, @{protocol = 'TCP'; port = 53})}
NetworkPolicy 'validator' @() @(@{})
NetworkPolicy 'cpu-model' @(@{from = @((Selector 'gateway-coordinator'), (Selector 'gateway-registry-reader'), (Selector 'validator')); ports = @(@{port = 11434})}) @()
Create @{apiVersion = 'networking.k8s.io/v1'; kind = 'NetworkPolicy'; metadata = @{name = 'model-bootstrap'; namespace = $Namespace}
    spec = @{podSelector = @{matchLabels = @{app = 'cpu-model'}}; policyTypes = @('Egress')
        egress = @($dns, @{to = @(@{ipBlock = @{cidr = '0.0.0.0/0'}}); ports = @(@{port = 443})})}} -Transient
Create @{apiVersion = 'v1'; kind = 'Pod'; metadata = @{name = 'cpu-model'; namespace = $Namespace; labels = @{app = 'cpu-model'}}
    spec = @{automountServiceAccountToken = $false; containers = @(@{name = 'model'; image = $modelImage
        env = @(@{name = 'OLLAMA_HOST'; value = '0.0.0.0:11434'}, @{name = 'OLLAMA_NUM_PARALLEL'; value = '1'},
            @{name = 'OLLAMA_MAX_LOADED_MODELS'; value = '1'}, @{name = 'OLLAMA_CONTEXT_LENGTH'; value = '2048'},
            @{name = 'CUDA_VISIBLE_DEVICES'; value = '-1'}, @{name = 'ROCR_VISIBLE_DEVICES'; value = '-1'})
        resources = @{requests = @{cpu = '100m'; memory = '512Mi'}; limits = @{cpu = '2'; memory = '3Gi'}}
        securityContext = @{allowPrivilegeEscalation = $false; capabilities = @{drop = @('ALL')}; seccompProfile = @{type = 'RuntimeDefault'}}
        readinessProbe = @{tcpSocket = @{port = 11434}; periodSeconds = 2}})}}
Service 'cpu-model' @(11434)
Create @{apiVersion = 'v1'; kind = 'Pod'; metadata = @{name = 'validator'; namespace = $Namespace; labels = @{app = 'validator'}}
    spec = @{automountServiceAccountToken = $false; containers = @(@{name = 'client'; image = $pythonImage
        command = @('python', '-B', '-c', 'import time; time.sleep(86400)')
        resources = @{requests = @{cpu = '10m'; memory = '32Mi'}; limits = @{cpu = '500m'; memory = '128Mi'}}})}}
Wait-Pods @('cpu-model', 'validator')
Write-Host 'Downloading qwen3:0.6b into the CPU model pod...'
try {
    Run $kubectl ($kube + @('-n', $Namespace, 'exec', 'cpu-model', '--', 'ollama', 'pull', 'qwen3:0.6b')) | Out-Null
} finally {
    Run $kubectl ($kube + @('-n', $Namespace, 'delete', 'networkpolicy', 'model-bootstrap', '--wait=true')) | Out-Null
}
$modelIP = IP 'service' 'cpu-model'
$mcrIP = Run $kubectl ($kube + @('-n', $Namespace, 'exec', 'validator', '--', 'python', '-c',
    'import socket,ipaddress; ip=socket.gethostbyname("mcr.microsoft.com"); assert ipaddress.ip_address(ip).is_global; print(ip)'))
$dnsIP = Run $kubectl ($kube + @('-n', 'kube-system', 'get', 'service', 'kube-dns', '-o', 'jsonpath={.spec.clusterIP}'))
foreach ($agent in @('coordinator', 'registry-reader')) { Service $agent @(8080); Service "gateway-$agent" @(3000, 3001) }
foreach ($agent in @('coordinator', 'registry-reader')) {
    $gateway = @{
        config = @{adminAddr = 'off'; statsAddr = 'off'; workerThreads = '2'; enableIpv6 = $false}
        gateways = @{http = @{port = 3001}}
        routes = @(@{name = 'model-inference'; gateways = 'http'; matches = @(@{path = @{exact = '/api/chat'}; method = 'POST'})
            policies = @{authorization = @{rules = @(@{require = 'true'})}}
            backends = @(@{host = "cpu-model.${Namespace}.svc.cluster.local:11434"})})
    }
    $gatewayEgress = @($dns, @{to = @((Selector 'cpu-model')); ports = @(@{port = 11434})})
    if ($agent -eq 'coordinator') {
        $gateway.routes += @{name = 'inspect-registry'; gateways = 'http'; matches = @(@{path = @{exact = '/skills/inspect-registry/message:send'}; method = 'POST'})
            policies = @{authorization = @{rules = @(@{require = 'true'})}}
            backends = @(@{host = "registry-reader.${Namespace}.svc.cluster.local:8080"})}
        $gatewayEgress += @{to = @((Selector 'registry-reader')); ports = @(@{port = 8080})}
    } else {
        $mcr = Run $compiler @('-gateway-egress', '-policy', (Join-Path $repo 'agentsuite\policy\testdata\allow-mcr.json')) | ConvertFrom-Json -AsHashtable
        $gateway.binds = $mcr.config.binds
        $gateway.gateways['tls-443'] = $mcr.config.gateways['tls-443']
        $gateway.tcpRoutes = $mcr.config.tcpRoutes
        $gatewayEgress += @{to = @(@{ipBlock = @{cidr = '0.0.0.0/0'; except = @('10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', '127.0.0.0/8', '169.254.0.0/16')}}); ports = @(@{port = 443})}
    }
    $gateway.routes += @{name = 'deny-other-http'; gateways = 'http'
        policies = @{authorization = @{rules = @(@{require = 'false'})}}; backends = @(@{host = '127.0.0.1:9'})}
    Save "$agent-gateway.json" $gateway
    $native = $gateway | ConvertTo-Json -Depth 100 -Compress
    Run podman @('run', '--rm', $gatewayImage, '--validate-only', '-c', $native) | Out-Null
    NetworkPolicy "gateway-$agent" @(@{from = @((Selector $agent), (Selector 'validator')); ports = @(@{port = 3000}, @{port = 3001})}) $gatewayEgress
    Create @{apiVersion = 'v1'; kind = 'Pod'; metadata = @{name = "gateway-$agent"; namespace = $Namespace; labels = @{app = "gateway-$agent"}}
        spec = @{automountServiceAccountToken = $false
            securityContext = @{runAsUser = 65532; runAsGroup = 65532; runAsNonRoot = $true
                sysctls = @(@{name = 'net.ipv4.ip_unprivileged_port_start'; value = '0'})}
            containers = @(@{name = 'gateway'; image = $gatewayImage; args = @('-c', $native)
                securityContext = @{readOnlyRootFilesystem = $true; allowPrivilegeEscalation = $false; capabilities = @{drop = @('ALL')}; seccompProfile = @{type = 'RuntimeDefault'}}
                resources = @{requests = @{cpu = '25m'; memory = '64Mi'}; limits = @{cpu = '500m'; memory = '256Mi'}}
                readinessProbe = @{tcpSocket = @{port = 3001}; periodSeconds = 2}})}}
}
foreach ($agent in @('coordinator', 'registry-reader')) {
    $from = @((Selector 'validator'))
    if ($agent -eq 'registry-reader') { $from += Selector 'gateway-coordinator' }
    NetworkPolicy $agent @(@{from = $from; ports = @(@{port = 8080})}) @(@{to = @((Selector "gateway-$agent")); ports = @(@{port = 3000}, @{port = 3001})})
    $projection = $projections[$agent]
    $profileFile = Join-Path $OutputDirectory "$agent-seccomp.json"
    $projection.seccomp | ConvertTo-Json -Depth 100 | Set-Content -Encoding utf8NoBOM $profileFile
    $target = '/var/lib/kubelet/seccomp/' + $projection.profilePath
    Run podman @('exec', $node, 'mkdir', '-p', '/var/lib/kubelet/seccomp/agentsuite') | Out-Null
    Run podman @('cp', $profileFile, "${node}:$target") | Out-Null
    $hash = ((Run podman @('exec', $node, 'sha256sum', $target)) -split '\s+')[0]
    if ($hash -ne (Get-FileHash $profileFile -Algorithm SHA256).Hash.ToLowerInvariant()) { throw 'Installed seccomp bytes differ' }
    $peer = if ($agent -eq 'coordinator') { 'registry-reader' } else { 'coordinator' }
    $skill = $policies[$agent].capabilities.skills[0].id
    $config = @{gatewayIP = (IP 'service' "gateway-$agent"); otherGatewayIP = (IP 'service' "gateway-$peer")
        modelIP = $modelIP; peerIP = (IP 'service' $peer); mcrIP = $mcrIP; dnsIP = $dnsIP
        skillPath = "/skills/$skill/message:send"; publicURL = "http://${agent}:8080/skills/$skill"}
    Create @{apiVersion = 'v1'; kind = 'ConfigMap'; metadata = @{name = $agent; namespace = $Namespace}
        data = @{'agent.py' = (Get-Content (Join-Path $PSScriptRoot 'policy-cpu-agent.py') -Raw)
            'launcher.py' = $projection.launcher; 'policy.json' = ($policies[$agent] | ConvertTo-Json -Depth 100)}}
    Create @{apiVersion = 'v1'; kind = 'Pod'; metadata = @{name = $agent; namespace = $Namespace; labels = @{app = $agent}
            annotations = @{'agentsuite.dev/policy-digest' = $projection.policyDigest; 'agentsuite.dev/profile-digest' = $projection.profileDigest}}
        spec = @{nodeName = $node; automountServiceAccountToken = $false; hostNetwork = $false; hostPID = $false; hostIPC = $false
            securityContext = @{runAsUser = 65532; runAsGroup = 65532; runAsNonRoot = $true
                seccompProfile = @{type = 'Localhost'; localhostProfile = $projection.profilePath}}
            volumes = @(@{name = 'example'; configMap = @{name = $agent}})
            containers = @(@{name = 'agent'; image = $pythonImage
                command = @('python', '-B', '/example/launcher.py', 'python', '-B', '/example/agent.py')
                env = @(@{name = 'EXAMPLE_CONFIG'; value = ($config | ConvertTo-Json -Compress)})
                volumeMounts = @(@{name = 'example'; mountPath = '/example'; readOnly = $true})
                securityContext = @{readOnlyRootFilesystem = $true; allowPrivilegeEscalation = $false; capabilities = @{drop = @('ALL')}}
                resources = @{requests = @{cpu = '25m'; memory = '32Mi'}; limits = @{cpu = '500m'; memory = '128Mi'}}
                readinessProbe = @{httpGet = @{path = '/healthz'; port = 8080}; periodSeconds = 2}})}}
}
Save 'objects.json' @{apiVersion = 'v1'; kind = 'List'; items = @($objects)}
Wait-Pods @('gateway-coordinator', 'gateway-registry-reader', 'coordinator', 'registry-reader')
Write-Host "Agent pods ready in $Namespace. Running model-backed workflow and policy probes..."
$validation = @'
import http.client,json,socket,sys,time
ns,model,mcr=sys.argv[1:]
def call(host,path,payload=None,port=8080):
    connection=http.client.HTTPConnection(host,port,timeout=180)
    try:
        connection.request("GET" if payload is None else "POST",path,None if payload is None else json.dumps(payload),{"Content-Type":"application/json"})
        response=connection.getresponse()
        data=response.read()
        assert response.status==200,(host,path,response.status,data.decode())
        return json.loads(data)
    finally: connection.close()
# Same live destinations must be reachable from the ungoverned positive control.
for host,port in [(model,11434),("registry-reader",8080),("coordinator",8080),(mcr,443),
                  ("gateway-coordinator",3001),("gateway-registry-reader",3001)]:
    with socket.create_connection((host,port),timeout=15): pass
cards={agent:call(agent,"/.well-known/agent-card.json") for agent in ["coordinator","registry-reader"]}
response=call("coordinator","/skills/coordinate/message:send",{"message":{"messageId":"cpu-policy-proof","role":"ROLE_USER","parts":[{"text":"Check whether the MCR registry API is reachable and summarize the result."}]}})
evidence=response["message"]["metadata"]["policyExample"]
assert evidence["peer"]["registry"]["status"]==200,evidence
assert evidence["model"]["eval_count"]>0 and evidence["peer"]["model"]["eval_count"]>0,evidence
cpu=call(model,"/api/ps",port=11434)
assert len(cpu["models"])==1 and cpu["models"][0]["size_vram"]==0,cpu
results=[]
for agent in ["coordinator","registry-reader"]:
    for case in ["filesystem","direct-model","direct-peer","other-gateway","direct-mcr","child-bypass",
                 "dns-datagram","model-admin","forbidden-skill","forbidden-peer","wrong-method"]:
        result=call(agent,"/probe",{"case":case})
        result["agent"]=agent
        results.append(result)
results.append(call("registry-reader","/probe",{"case":"unlisted-sni"}))
# Readiness must survive the denied operations.
for agent in cards: assert call(agent,"/healthz")["ready"]
print(json.dumps({"workflow":response,"cpu":cpu,"cards":cards,"probes":results,"positiveDestinations":True}))
'@
$before = Run $kubectl ($kube + @('-n', 'kube-system', 'exec', 'ds/cilium', '--', 'cilium-dbg', 'bpf', 'metrics', 'list', '-o', 'json'))
$result = Run $kubectl ($kube + @('-n', $Namespace, 'exec', 'validator', '--', 'python', '-B', '-c', $validation, $Namespace, $modelIP, $mcrIP)) | ConvertFrom-Json -AsHashtable
$after = Run $kubectl ($kube + @('-n', 'kube-system', 'exec', 'ds/cilium', '--', 'cilium-dbg', 'bpf', 'metrics', 'list', '-o', 'json'))
$before | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory 'cilium-before.json')
$after | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory 'cilium-after.json')
$beforeDrops = ($before | ConvertFrom-Json | Where-Object { $_.reason -eq 'Policy denied' -and $_.direction -eq 'egress' } | Measure-Object -Property packets -Sum).Sum
$afterDrops = ($after | ConvertFrom-Json | Where-Object { $_.reason -eq 'Policy denied' -and $_.direction -eq 'egress' } | Measure-Object -Property packets -Sum).Sum
if ($afterDrops -le $beforeDrops) { throw 'No Cilium egress policy-drop evidence was observed' }
$logs = Run $kubectl ($kube + @('-n', $Namespace, 'logs', 'gateway-registry-reader'))
$logs | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory 'reader-gateway.log')
if ($logs -notmatch 'tls.sni=example.com[^\r\n]*error="route not found"') { throw 'Missing explicit gateway SNI rejection evidence' }
$pods = Run $kubectl ($kube + @('-n', $Namespace, 'get', 'pods', '-o', 'json')) | ConvertFrom-Json -AsHashtable
$receipt = @{context = 'kind-kaimahi-cpu-agents'; clusterUID = $uid; namespace = $Namespace
    policies = @($projections.Values | ForEach-Object { $_.policyDigest }); result = $result
    ciliumEgressPolicyDropDelta = $afterDrops - $beforeDrops
    pods = @($pods.items | ForEach-Object { @{name = $_.metadata.name; uid = $_.metadata.uid; images = $_.status.containerStatuses.imageID} })
    objectsSHA256 = (Get-FileHash (Join-Path $OutputDirectory 'objects.json') -Algorithm SHA256).Hash.ToLowerInvariant()
    scope = 'Bounded CPU-backed workflow with fixed skill endpoint bindings; not general A2A conformance, native kmx lift, or adversarial sandbox certification'}
Save 'receipt.json' $receipt
$result.workflow | ConvertTo-Json -Depth 100
Write-Host "CPU-only inference and $($result.probes.Count) policy probe groups passed. Evidence: $OutputDirectory"
Write-Host "Namespace $Namespace retained for inspection. The model cache is disposable with its container."
