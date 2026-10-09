param(
    [Parameter(Mandatory)][string]$Tools,
    [Parameter(Mandatory)][string]$OutputDirectory,
    [string]$ClusterName = 'kaimahi-policy-spike',
    [string]$Namespace = ('policy-spike-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path (Split-Path $PSScriptRoot)
$Tools = (Resolve-Path $Tools).Path
$kubeconfig = Join-Path $Tools 'policy-kubeconfig'
$kubectl = Join-Path $Tools 'kubectl.exe'
$go = Join-Path $Tools 'go\bin\go.exe'
$env:KIND_EXPERIMENTAL_PROVIDER = 'podman'
if ($ClusterName -notmatch '^[a-z][a-z0-9-]+$' -or $Namespace -notmatch '^[a-z][a-z0-9-]+$') {
    throw 'Invalid cluster or namespace name'
}
if (Test-Path $OutputDirectory) { throw 'Output directory must be new' }
New-Item -ItemType Directory $OutputDirectory | Out-Null
$OutputDirectory = (Resolve-Path $OutputDirectory).Path
$kube = @('--kubeconfig', $kubeconfig, '--context', "kind-$ClusterName", '--request-timeout=30s')

function Invoke-Checked([string]$Exe, [string[]]$ArgList, [string]$InputText = '') {
    if ($InputText) { $result = $InputText | & $Exe @ArgList 2>&1 }
    else { $result = & $Exe @ArgList 2>&1 }
    if ($LASTEXITCODE -ne 0) { throw "$Exe failed: $($result -join "`n")" }
    return ($result -join "`n")
}
function Write-Json([string]$Name, $Value) {
    $path = Join-Path $OutputDirectory $Name
    $Value | ConvertTo-Json -Depth 100 | Set-Content -Encoding utf8NoBOM $path
    return $path
}
function Create-Object($Value) {
    Invoke-Checked $kubectl ($kube + @('create', '-f', '-')) ($Value | ConvertTo-Json -Depth 100) | Out-Null
}

$uid = Invoke-Checked $kubectl ($kube + @('get', 'namespace', 'kube-system', '-o', 'jsonpath={.metadata.uid}'))
$pythonImage = 'docker.io/library/python@sha256:cfe2e24a75302a15934d37c2d86412893c0aa934dc3a97cbb439d04c01890ca9'
$gatewayImage = 'ghcr.io/agentgateway/agentgateway@sha256:482921556876a503ad3675b29223b1897b63a316983e46797ff99ebf83a2a6a2'
$digest = 'sha256:' + ('a' * 64)
$environment = @{
    apiVersion = 'kaimahi.dev/lift/v1alpha1'; name = 'policy-probe'; context = "kind-$ClusterName"
    clusterUID = $uid; namespace = $Namespace; adapter = 'kubernetes-http-v1'
    platform = @{os = 'linux'; architecture = 'amd64'}; inputs = @{}
}
# Synthetic source identity: this tests the PR's renderer, not registry provenance.
$record = @{
    schemaVersion = '1.0.0-draft'; mediaType = 'application/vnd.agentsuite.image.deployment.v1+json'
    suiteReference = "registry.example/suite@$digest"; suiteDigest = $digest
    agent = 'policy-probe'; platform = $environment.platform; compositionDigest = $digest
    buildProfile = 'default'
    execution = @{kind = 'kubernetes-http-v1'; protocol = 'openai-chat-v1'; port = 8080; healthPath = '/healthz'; inputs = @()}
}
$environmentFile = Write-Json 'environment.json' $environment
$recordFile = Write-Json 'record.json' $record
$backend = Get-Content -Raw (Join-Path $PSScriptRoot 'policy-backend.py')
$probe = Get-Content -Raw (Join-Path $PSScriptRoot 'policy-probe.py')
$commandJSON = @('python', '-B', '-c', $backend, $probe) | ConvertTo-Json -Compress
$compiler = Join-Path $OutputDirectory 'policy-spike.exe'
Push-Location $repo
try { Invoke-Checked $go @('build', '-o', $compiler, '.\cmd\policy-spike') | Out-Null }
finally { Pop-Location }
$bundleText = Invoke-Checked $compiler @('-policy', (Join-Path $repo 'agentsuite\policy\testdata\deny-all.json'),
    '-record', $recordFile, '-environment', $environmentFile, '-image', $pythonImage, '-command', $commandJSON)
$bundle = $bundleText | ConvertFrom-Json -AsHashtable
$bundleText | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory 'plan.json')
$profileFile = Write-Json 'seccomp.json' $bundle.enforcement.seccomp

# The new namespace is the ownership boundary; never reuse somebody else's.
Create-Object @{apiVersion = 'v1'; kind = 'Namespace'; metadata = @{name = $Namespace}}
$node = "$ClusterName-control-plane"
$profilePath = '/var/lib/kubelet/seccomp/' + $bundle.enforcement.profilePath
Invoke-Checked podman @('exec', $node, 'mkdir', '-p', '/var/lib/kubelet/seccomp/agentsuite') | Out-Null
Invoke-Checked podman @('cp', $profileFile, "${node}:$profilePath") | Out-Null
$installedHash = ((Invoke-Checked podman @('exec', $node, 'sha256sum', $profilePath)) -split '\s+')[0]
if ($installedHash -ne (Get-FileHash $profileFile -Algorithm SHA256).Hash.ToLowerInvariant()) {
    throw 'Installed profile bytes differ from compiled profile'
}
Invoke-Checked $kubectl ($kube + @('label', 'node', $node, ($bundle.enforcement.nodeLabel + '=installed'), '--overwrite')) | Out-Null
foreach ($image in @($pythonImage, $gatewayImage)) {
    Invoke-Checked podman @('exec', $node, 'crictl', 'pull', $image) | Out-Null
}
$denyConfig = $bundle.enforcement.gateway | ConvertTo-Json -Depth 100 -Compress
$allow = $denyConfig | ConvertFrom-Json -AsHashtable
$allow.gateways.default.port = 3001
$allow.routes[0].policies.authorization.rules[0].require = 'true'
$allowConfig = $allow | ConvertTo-Json -Depth 100 -Compress
$control = @{
    apiVersion = 'v1'; kind = 'Pod'; metadata = @{name = 'control'; namespace = $Namespace; labels = @{app = 'control'}}
    spec = @{
        automountServiceAccountToken = $false
        containers = @(
            @{name = 'backend'; image = $pythonImage; command = @('python', '-B', '-c', $backend)},
            @{name = 'deny'; image = $gatewayImage; args = @('-c', $denyConfig)},
            @{name = 'allow'; image = $gatewayImage; args = @('-c', $allowConfig); env = @(
                @{name = 'ADMIN_ADDR'; value = '127.0.0.1:15100'},
                @{name = 'STATS_ADDR'; value = '127.0.0.1:15120'},
                @{name = 'READINESS_ADDR'; value = '127.0.0.1:15121'}
            )}
        )
    }
}
Create-Object $control
Create-Object @{
    apiVersion = 'v1'; kind = 'Service'; metadata = @{name = 'control'; namespace = $Namespace}
    spec = @{selector = @{app = 'control'}; ports = @(
        @{name = 'backend'; port = 8080}, @{name = 'deny'; port = 3000}, @{name = 'allow'; port = 3001}
    )}
}
# Execute the rendered PodSpec unchanged. Probe subprocesses must descend from
# the Landlock launcher; kubectl exec would create a separate unconstrained task.
$podSpec = $bundle.deployment.objects[0].spec.template.spec
$pod = @{apiVersion = 'v1'; kind = 'Pod'; metadata = @{name = 'policy-probe'; namespace = $Namespace}; spec = $podSpec}
$executedPodFile = Write-Json 'executed-pod.json' $pod
Create-Object $pod
Invoke-Checked $kubectl ($kube + @('-n', $Namespace, 'wait', '--for=condition=Ready', 'pod/control', 'pod/policy-probe', '--timeout=180s')) | Out-Null
$serviceIP = Invoke-Checked $kubectl ($kube + @('-n', $Namespace, 'get', 'service', 'control', '-o', 'jsonpath={.spec.clusterIP}'))
$gatewayProbe = @'
import json,socket,sys,time,urllib.request,urllib.error
host=sys.argv[1]
socket.getaddrinfo("kubernetes.default.svc.cluster.local",443)
with open("/tmp/policy-positive","w") as output: output.write("positive")
def get(port,path):
    return urllib.request.urlopen(f"http://{host}:{port}{path}",timeout=5)
for attempt in range(30):
    try:
        with get(3001,"/positive") as response:
            assert response.status==200
        break
    except urllib.error.URLError:
        if attempt==29: raise
        time.sleep(1)
with get(8080,"/hits") as response: before=json.load(response)["hits"]
results=[]
for path in ["/v1/chat/completions","/mcp","/a2a","/.well-known/agent-card.json"]:
    try:
        get(3000,path)
    except urllib.error.HTTPError as error:
        assert error.code==403,(path,error.code)
        results.append({"case":path,"status":error.code})
    else: raise AssertionError(f"gateway allowed {path}")
with get(8080,"/hits") as response: after=json.load(response)["hits"]
assert before==after,(before,after)
print(json.dumps({"positiveGatewayStatus":200,"dnsPositive":True,"denials":results,"backendHitDelta":after-before}))
'@
$gatewayResult = Invoke-Checked $kubectl ($kube + @('-n', $Namespace, 'exec', 'control', '-c', 'backend', '--', 'python', '-B', '-c', $gatewayProbe, $serviceIP))
$probeIP = Invoke-Checked $kubectl ($kube + @('-n', $Namespace, 'get', 'pod', 'policy-probe', '-o', 'jsonpath={.status.podIP}'))
$invokeProbe = @'
import sys,urllib.request,urllib.error
try:
    print(urllib.request.urlopen(f"http://{sys.argv[1]}:8080/probe/{sys.argv[2]}",timeout=40).read().decode())
except urllib.error.HTTPError as error:
    raise RuntimeError(error.read().decode()) from error
'@
$kernelResult = Invoke-Checked $kubectl ($kube + @('-n', $Namespace, 'exec', 'control', '-c', 'backend', '--', 'python', '-B', '-c', $invokeProbe, $probeIP, $serviceIP))
$missing = $pod | ConvertTo-Json -Depth 100 | ConvertFrom-Json -AsHashtable
$missing.metadata.name = 'missing-profile'
$missing.spec.securityContext.seccompProfile.localhostProfile += '.absent'
Create-Object $missing
$missingProfileRejected = $false
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    $state = Invoke-Checked $kubectl ($kube + @('-n', $Namespace, 'get', 'pod', 'missing-profile', '-o', 'json')) | ConvertFrom-Json
    $waiting = if ($state.status.containerStatuses) { $state.status.containerStatuses[0].state.waiting } else { $null }
    if ($waiting.reason -eq 'CreateContainerError' -and $waiting.message -match 'seccomp|\.absent') {
        $missingProfileRejected = $true
        break
    }
    if ($state.status.phase -eq 'Running') { throw 'Missing-profile workload unexpectedly started' }
    Start-Sleep -Seconds 1
}
if (-not $missingProfileRejected) { throw 'Did not establish explicit missing-profile rejection' }
Invoke-Checked $kubectl ($kube + @('-n', $Namespace, 'delete', 'pod', 'missing-profile', '--wait=true')) | Out-Null
$receipt = @{
    context = "kind-$ClusterName"; clusterUID = $uid; namespace = $Namespace
    policyDigest = $bundle.enforcement.policyDigest; profileDigest = $bundle.enforcement.profileDigest
    planDigest = $bundle.deployment.digest; executedPodSHA256 = (Get-FileHash $executedPodFile -Algorithm SHA256).Hash.ToLowerInvariant()
    pythonImage = $pythonImage; gatewayImage = $gatewayImage
    gateway = ($gatewayResult | ConvertFrom-Json); kernel = ($kernelResult | ConvertFrom-Json)
    missingProfileRejected = $missingProfileRejected
    scope = 'Synthetic workload using lift-rendered security context; not registry lift, A2A protocol conformance, or adversarial sandbox certification'
}
Write-Json 'receipt.json' $receipt | Out-Null
$receipt | ConvertTo-Json -Depth 100
Write-Host "Evidence: $OutputDirectory"
Write-Host "Resources retained in dedicated namespace $Namespace; cluster profile retained for reruns."
