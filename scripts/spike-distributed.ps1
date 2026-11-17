param(
    [string]$Bacalhau = 'C:\Bacalhau\bin\bacalhau.exe',
    [string]$Go = 'C:\Program Files\Go\bin\go.exe',
    [string]$Results,
    [switch]$NativeNodes,
    [switch]$PlacementOnly,
    [switch]$Recovery,
    [switch]$ProbeFailover,
    [string]$PhysicalHost,
    [switch]$ScaledOnly,
    [switch]$Short
)
$ErrorActionPreference = 'Stop'
if ($PhysicalHost -and (!$Recovery -or $NativeNodes -or $PhysicalHost -cnotmatch '^[A-Za-z0-9_][A-Za-z0-9_.@-]*$')) {
    throw '-PhysicalHost requires -Recovery and an SSH host alias or user@hostname'
}
if ($ProbeFailover -and (!$Recovery -or (!$NativeNodes -and !$PhysicalHost))) {
    throw '-ProbeFailover requires -Recovery and a staging URL common to both nodes (-NativeNodes or -PhysicalHost)'
}
Set-Location (Split-Path $PSScriptRoot)
if (!$Results) { $Results = ".poc-runtime/distributed-$([guid]::NewGuid().ToString('N'))" }
if (Test-Path -LiteralPath $Results) { throw "Use a new results directory: $Results" }
$Results = (New-Item -ItemType Directory $Results).FullName
$revision = '3a6a73aa6529755ac2c9d96a771f0566ff3ee5da'
$image = 'ghcr.io/bacalhau-project/bacalhau@sha256:903c4052a4522720adf43d428bdbb267cb24dec5039a759e27dffbcbbd68d878'
$container = "viii-issue9-$([guid]::NewGuid().ToString('N'))"
$env:GOCACHE = Join-Path $env:TEMP 'viii-go-cache'
$env:VIII_SPIKE_RESULTS = $Results
$env:VIII_SPIKE_RECOVERY = if ($Recovery) { '1' } else { '0' }
$env:VIII_SPIKE_FAILOVER_PROBE = if ($ProbeFailover) { '1' } else { '0' }
$env:VIII_SPIKE_B_CONTAINER = ''
$env:VIII_SPIKE_B_SSH = $PhysicalHost
$env:VIII_SPIKE_B_PID = ''
$env:VIII_SPIKE_B_TEMP = ''
$env:VIII_SPIKE_API = 'http://127.0.0.1:32234'
$env:VIII_SPIKE_STAGE_A = 'http://127.0.0.1:38080'
$env:VIII_SPIKE_STAGE_B = if ($NativeNodes -or $PhysicalHost) { $env:VIII_SPIKE_STAGE_A } else { 'http://host.docker.internal:38080' }
$processes = [ordered]@{}
$containerStarted = $false
$remotePrepared = $false
$remoteDir = "/tmp/$container"
$ssh = @('-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','ConnectTimeout=8')
$resourceJob = $null
$ownedLog = Join-Path $Results 'owned.jsonl'

function Build($Output, $Package) {
    & $Go build -trimpath -o (Join-Path $Results $Output) $Package
    if ($LASTEXITCODE) { throw "Build failed: $Package" }
}

function Record-Owned($Resource) {
    [IO.File]::AppendAllText($ownedLog, (($Resource | ConvertTo-Json -Compress) + [Environment]::NewLine))
}

function Start-Owned($Name, $Executable, $Arguments) {
    $processes[$Name] = Start-Process -FilePath $Executable -ArgumentList $Arguments -WorkingDirectory $Results -PassThru -WindowStyle Hidden -RedirectStandardOutput "$Results/$Name.stdout.log" -RedirectStandardError "$Results/$Name.stderr.log"
    Record-Owned @{name=$Name; id=$processes[$Name].Id; executable=$Executable; started_utc=$processes[$Name].StartTime.ToUniversalTime().ToString("o")}
}

function Assert-PortFree($Port) {
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $Port)
    $listener.ExclusiveAddressUse = $true
    try { $listener.Start() } finally { $listener.Stop() }
}

function Wait-Node($ID) {
    $deadline = [DateTime]::UtcNow.AddSeconds(35)
    do {
        try {
            $nodes = Invoke-RestMethod "$env:VIII_SPIKE_API/api/v1/orchestrator/nodes" -TimeoutSec 2
            $node = $nodes.Nodes | Where-Object { $_.Info.NodeID -eq $ID }
            if ($node) { return $node }
        } catch { }
        Start-Sleep -Milliseconds 250
    } until ([DateTime]::UtcNow -ge $deadline)
    throw "Node $ID did not register. Inspect $Results"
}

function Set-Membership($ID, $Action) {
    $response = Invoke-RestMethod "$env:VIII_SPIKE_API/api/v1/orchestrator/nodes/$ID" -Method Put -ContentType 'application/json' -Body (@{Action=$Action} | ConvertTo-Json -Compress) -TimeoutSec 10
    if (!$response.Success) { throw "Node $ID $Action failed: $($response.Error)" }
}

try {
    $buildInfo = & $Go version -m $Bacalhau
    if ($LASTEXITCODE -or !($buildInfo -match "vcs.revision=$revision") -or !($buildInfo -match 'bacalhau.*v1\.9\.5')) {
        throw "The Windows Bacalhau binary must identify v1.9.5 and revision $revision"
    }
    foreach ($port in 32234, 34222, 38080, 32235) { Assert-PortFree $port }
    if (!$NativeNodes -and !$PhysicalHost) {
        docker image inspect $image --format '{{.Id}}' | Out-Null
        if ($LASTEXITCODE) {
            docker pull $image
            if ($LASTEXITCODE) { throw 'Pinned Docker image unavailable. Use -NativeNodes for a separate fallback run.' }
        }
    }
    Build 'stage.exe' './cmd/stage'
    if (!$PlacementOnly -and !$Recovery) { Build 'bench-worker.exe' './cmd/latency' }
    $oldOS, $oldArch = $env:GOOS, $env:GOARCH
    try {
        $env:GOOS = 'wasip1'
        $env:GOARCH = 'wasm'
        Build 'worker.wasm' './cmd/worker'
        if (!$PlacementOnly -and !$Recovery) { Build 'bench-worker.wasm' './cmd/latency' }
    } finally {
        $env:GOOS, $env:GOARCH = $oldOS, $oldArch
    }
    if ($Recovery) { New-Item -ItemType Directory (Join-Path $Results 'staged') | Out-Null }
    Start-Owned 'stage' "$Results/stage.exe" '-spike'
    $token = [guid]::NewGuid().ToString('N')
    $configA = @{
        DisableAnalytics=$true; API=@{Host='127.0.0.1';Port=32234}
        Orchestrator=@{Enabled=$true;Host='127.0.0.1';Port=34222;Auth=@{Token=$token};NodeManager=@{ManualApproval=$true}}
        Compute=@{Enabled=$true;Orchestrators=@('nats://127.0.0.1:34222');Auth=@{Token=$token}}
        Engines=@{Disabled=@('docker')}; Publishers=@{Disabled=@('local')}
    }
    $peer = if ($NativeNodes -or $PhysicalHost) {'127.0.0.1'} else {'host.docker.internal'}
    $configB = @{
        DisableAnalytics=$true; API=@{Host='127.0.0.1';Port=32235}
        Compute=@{Enabled=$true;Orchestrators=@("nats://${peer}:34222");Auth=@{Token=$token}}
        Engines=@{Disabled=@('docker')}
        Publishers=@{Disabled=@('local')}
    }
    # JSON is a YAML subset. Bacalhau selects the reader by extension.
    [IO.File]::WriteAllText("$Results/a.yaml", ($configA | ConvertTo-Json -Depth 12))
    [IO.File]::WriteAllText("$Results/b.yaml", ($configB | ConvertTo-Json -Depth 12))
    Start-Owned 'node-a' $Bacalhau 'serve --name viii-a --data-dir node-a -c a.yaml'
    if ((Wait-Node 'viii-a').Membership -eq 'PENDING') { Set-Membership 'viii-a' 'approve' }
    if ($PhysicalHost) {
        Start-Owned 'ssh-tunnel' 'ssh.exe' "-o BatchMode=yes -o StrictHostKeyChecking=yes -o ExitOnForwardFailure=yes -o ConnectTimeout=8 -N -R 127.0.0.1:34222:127.0.0.1:34222 -R 127.0.0.1:38080:127.0.0.1:38080 $PhysicalHost"
        $remotePrepared = $true
        Get-Content -Raw "$Results/b.yaml" | ssh @ssh $PhysicalHost "umask 077; mkdir $remoteDir && cat > $remoteDir/b.yaml && docker run --pull=never -d --network host --name $container --mount type=bind,source=$remoteDir/b.yaml,target=/viii/b.yaml,readonly $image serve --name viii-b --data-dir /viii/node-b -c /viii/b.yaml"
        if ($LASTEXITCODE) { throw 'Physical node startup failed' }
        $env:VIII_SPIKE_B_CONTAINER = $container
        Record-Owned @{name=$container; image=$image; topology='physical-ssh'}
    } elseif ($NativeNodes) {
        $oldTemp, $oldTmp = $env:TEMP, $env:TMP
        try {
            if ($Recovery) {
                $env:VIII_SPIKE_B_TEMP = (New-Item -ItemType Directory (Join-Path $Results 'node-b-temp')).FullName
                $env:TEMP = $env:TMP = $env:VIII_SPIKE_B_TEMP
            }
            Start-Owned 'node-b' $Bacalhau 'serve --name viii-b --data-dir node-b -c b.yaml'
            $env:VIII_SPIKE_B_PID = $processes['node-b'].Id
        } finally {
            $env:TEMP, $env:TMP = $oldTemp, $oldTmp
        }
    } else {
        $containerFlags = if ($Recovery) { @() } else { @('--rm') }
        docker run @containerFlags -d --name $container --mount "type=bind,source=$Results/b.yaml,target=/viii/b.yaml,readonly" $image serve --name viii-b --data-dir /viii/node-b -c /viii/b.yaml | Out-Null
        if ($LASTEXITCODE) { throw 'Docker compute startup failed' }
        $containerStarted = $true
        $env:VIII_SPIKE_B_CONTAINER = $container
        Record-Owned @{name=$container; image=$image}
    }
    Wait-Node 'viii-b' | Out-Null
    & $Go test -v -count=1 ./cmd/desktop -run '^TestDistributedSpike$'
    if ($LASTEXITCODE) { throw 'Placement validation failed' }
    if (!$PlacementOnly -and !$Recovery) {
        $env:VIII_SPIKE_TOPOLOGY = if ($NativeNodes) {'two-native-processes'} else {'windows-and-docker-linux'}
        $ids = @($processes.Values | ForEach-Object Id)
        $containerForSampling = if ($containerStarted) { $container } else { '' }
        $resourceJob = Start-Job -FilePath (Join-Path $PSScriptRoot 'spike-resources.ps1') -ArgumentList $Results,$ids,$containerForSampling
        $flags = @()
        if ($Short) { $flags += '-short' }
        if ($ScaledOnly) { $flags += '-scaled-only' }
        & "$Results/bench-worker.exe" measure @flags
        if ($LASTEXITCODE) { throw "Benchmark failed. Inspect $Results" }
    }
} finally {
    if ($resourceJob) {
        New-Item -ItemType File (Join-Path $Results 'stop-resource-sampling') | Out-Null
        if (!(Wait-Job $resourceJob -Timeout 10)) { Stop-Job $resourceJob }
        Receive-Job $resourceJob -ErrorAction Continue
        Remove-Job $resourceJob
    }
    if ($remotePrepared) {
        ssh @ssh $PhysicalHost "docker logs $container" *> "$Results/node-b.docker.log"
        ssh @ssh $PhysicalHost "docker rm -f $container; rm -f $remoteDir/b.yaml; rmdir $remoteDir" | Out-Null
    }
    if ($containerStarted) {
        docker logs $container *> "$Results/node-b.docker.log"
        docker stop -t 5 $container | Out-Null
        if ($Recovery) { docker rm $container | Out-Null }
    }
    foreach ($process in $processes.Values) {
        if (!$process.HasExited) { Stop-Process -InputObject $process; $process.WaitForExit(5000) | Out-Null }
    }
    foreach ($name in 'a.yaml','b.yaml') {
        $file = Join-Path $Results $name
        if (Test-Path -LiteralPath $file) { Remove-Item -LiteralPath $file }
    }
    Write-Host "Run files: $Results"
}
