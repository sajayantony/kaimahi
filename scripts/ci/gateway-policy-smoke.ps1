param(
    [Parameter(Mandatory)][string]$Tools,
    [Parameter(Mandatory)][string]$OutputDirectory
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path (Split-Path $PSScriptRoot)
$Tools = (Resolve-Path $Tools).Path
if (Test-Path $OutputDirectory) { throw 'Output directory must be new' }
New-Item -ItemType Directory $OutputDirectory | Out-Null
$OutputDirectory = (Resolve-Path $OutputDirectory).Path
$image = 'ghcr.io/agentgateway/agentgateway@sha256:482921556876a503ad3675b29223b1897b63a316983e46797ff99ebf83a2a6a2'
$containers = [Collections.Generic.List[string]]::new()

function Invoke-Checked([string]$Exe, [string[]]$ArgList) {
    $result = & $Exe @ArgList 2>&1
    if ($LASTEXITCODE -ne 0) { throw "$Exe failed: $($result -join "`n")" }
    return ($result -join "`n")
}
function Start-Gateway([string]$Config) {
    Invoke-Checked podman @('run', '--rm', $image, '--validate-only', '-c', $Config) | Out-Null
    $name = 'kaimahi-gateway-test-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
    # The image is non-root; this sysctl applies only to its network namespace.
    Invoke-Checked podman @('run', '-d', '--name', $name,
        '--sysctl', 'net.ipv4.ip_unprivileged_port_start=0', '-p', '127.0.0.1::3000', $image, '-c', $Config) | Out-Null
    $containers.Add($name)
    $mapping = Invoke-Checked podman @('port', $name, '3000/tcp')
    if ($mapping -notmatch '^127\.0\.0\.1:(\d+)$') { throw "Unexpected proxy port mapping: $mapping" }
    $portNumber = [int]$Matches[1]
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        if ((Invoke-Checked podman @('inspect', $name, '--format', '{{.State.Running}}')) -ne 'true') {
            throw (Invoke-Checked podman @('logs', $name))
        }
        $client = [Net.Sockets.TcpClient]::new()
        try {
            $task = $client.ConnectAsync('127.0.0.1', $portNumber)
            if ($task.Wait(500) -and $client.Connected) {
                return @{name = $name; address = "http://127.0.0.1:$portNumber"}
            }
        } catch [AggregateException] {
            if ($attempt -eq 29) { throw }
        } finally { $client.Dispose() }
        Start-Sleep -Milliseconds 200
    }
    throw 'Gateway did not become responsive'
}
function Request([string]$Name, [string]$Address, [string]$URL, [string[]]$Extra = @(), [switch]$Direct) {
    $routing = if ($Direct) { @('--noproxy', '*') } else { @('--noproxy', '', '--proxy', $Address) }
    $result = & curl.exe @routing --silent --show-error --connect-timeout 5 --max-time 30 `
        --output (Join-Path $OutputDirectory "$Name.body") --write-out '%{http_code}|%{http_connect}' @Extra $URL 2>&1
    $exitCode = $LASTEXITCODE
    $text = $result -join "`n"
    $text | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory "$Name.curl.txt")
    if ($text -notmatch '(\d{3})\|(\d{3})$') { throw "Missing curl status for $Name`: $text" }
    return @{case = $Name; exitCode = $exitCode; status = [int]$Matches[1]; connectStatus = [int]$Matches[2]}
}

try {
    $compiler = Join-Path $OutputDirectory 'policy-spike.exe'
    Push-Location $repo
    try { Invoke-Checked (Join-Path $Tools 'go\bin\go.exe') @('build', '-o', $compiler, '.\cmd\policy-spike') | Out-Null }
    finally { Pop-Location }
    $projection = Invoke-Checked $compiler @('-gateway-egress', '-policy', (Join-Path $repo 'agentsuite\policy\testdata\allow-mcr.json'))
    $projection | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory 'projection.json')
    $projectionObject = $projection | ConvertFrom-Json
    $native = $projectionObject.config | ConvertTo-Json -Depth 100 -Compress
    $native | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory 'agentgateway.json')
    $gateway = Start-Gateway $native
    $allowed = Request 'allow-mcr' $gateway.address 'https://mcr.microsoft.com/v2/'
    if ($allowed.exitCode -ne 0 -or $allowed.status -ne 200 -or $allowed.connectStatus -ne 200) {
        throw 'MCR positive control failed; no deny evidence is accepted without it'
    }
    $blocked = Request 'deny-unlisted-host' $gateway.address 'https://example.com/'
    $spoofed = Request 'deny-forged-host-header' $gateway.address 'https://example.com/' @('--header', 'Host: mcr.microsoft.com')
    foreach ($denial in @($blocked, $spoofed)) {
        if ($denial.exitCode -ne 35 -or $denial.status -ne 0 -or $denial.connectStatus -ne 200) {
            throw "Expected SNI rejection after CONNECT for $($denial.case)"
        }
    }
    $port = Request 'deny-port-444' $gateway.address 'https://mcr.microsoft.com:444/'
    if ($port.exitCode -eq 0 -or $port.connectStatus -ne 404) { throw 'Unlisted port was not rejected at CONNECT' }
    $cleartext = Request 'deny-http' $gateway.address 'http://mcr.microsoft.com/'
    if ($cleartext.exitCode -ne 0 -or $cleartext.status -ne 405) { throw 'Cleartext HTTP was not rejected' }
    $logs = Invoke-Checked podman @('logs', $gateway.name)
    $logs | Set-Content -Encoding utf8NoBOM (Join-Path $OutputDirectory 'gateway.log')
    if ($logs -notmatch 'tls.sni=example.com[^\r\n]*error="route not found"' -or
        $logs -notmatch 'endpoint=mcr.microsoft.com:443[^\r\n]*tls.sni=mcr.microsoft.com') {
        throw 'Gateway logs did not establish the permitted backend and SNI route rejection'
    }
    $a2aConfig = Get-Content -Raw (Join-Path $repo 'agentsuite\policy\examples\agentgateway\a2a-deny-all.json')
    $a2a = Start-Gateway $a2aConfig
    $a2aGet = Request 'a2a-card-denied' '' ($a2a.address + '/.well-known/agent-card.json') -Direct
    $message = '{"jsonrpc":"2.0","id":"probe","method":"message/send","params":{"message":{"role":"user","messageId":"probe","parts":[{"kind":"text","text":"hello"}]}}}'
    $a2aPost = Request 'a2a-message-denied' '' $a2a.address @('--header', 'Content-Type: application/json', '--data-raw', $message) -Direct
    if ($a2aGet.status -ne 403 -or $a2aPost.status -ne 403 -or $a2aGet.exitCode -ne 0 -or $a2aPost.exitCode -ne 0) {
        throw 'A2A route did not reject both discovery and message requests'
    }
    $receipt = @{
        image = $image; scope = $projectionObject.scope; policyDigest = $projectionObject.policyDigest
        unenforced = $projectionObject.unenforced
        results = @($allowed, $blocked, $spoofed, $port, $cleartext, $a2aGet, $a2aPost)
    }
    $receipt | ConvertTo-Json -Depth 100 | Tee-Object -FilePath (Join-Path $OutputDirectory 'receipt.json')
} finally {
    foreach ($name in $containers) {
        Invoke-Checked podman @('stop', '--time', '5', $name) | Out-Null
        Invoke-Checked podman @('rm', $name) | Out-Null
    }
}
