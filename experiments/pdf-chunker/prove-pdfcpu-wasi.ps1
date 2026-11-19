param(
    [Parameter(Mandatory=$true)][string]$PDF,
    [Parameter(Mandatory=$true)][string]$Worker,
    [Parameter(Mandatory=$true)][string]$Results,
    [int]$First=29961,
    [int]$Last=30000,
    [string]$Bacalhau='C:\Bacalhau\bin\bacalhau.exe',
    [int]$TimeoutSeconds=900
)
# Windows/Bacalhau v1.9.5 integration gate for the Go pdfcpu WASI chunker.
# Uses an isolated node and fresh data dir; does not modify the running PDF MVP.
$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot -Parent | Split-Path -Parent)
if (Test-Path -LiteralPath $Results) { throw 'Use a fresh results path' }
$PDF=(Resolve-Path -LiteralPath $PDF).Path
$Worker=(Resolve-Path -LiteralPath $Worker).Path
$Bacalhau=(Resolve-Path -LiteralPath $Bacalhau).Path
$before=(Get-FileHash -LiteralPath $PDF -Algorithm SHA256).Hash
$details=& go version -m $Bacalhau
if ($LASTEXITCODE -or !($details -match 'bacalhau.*v1\.9\.5') -or !($details -match 'vcs.revision=3a6a73aa6529755ac2c9d96a771f0566ff3ee5da')) {
    throw 'Bacalhau binary does not match pinned VIII 1.9.5 revision'
}
$Results=(New-Item -ItemType Directory -Force -Path $Results).FullName
$control=(New-Item -ItemType Directory -Path (Join-Path $Results 'control')).FullName
$artifacts=(New-Item -ItemType Directory -Path (Join-Path $Results 'artifacts')).FullName
$apiPort=44290
$natsPort=44291
foreach ($port in @($apiPort,$natsPort)) {
    $listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,$port)
    try { $listener.Start() } finally { $listener.Stop() }
}
function LocalInput([string]$source,[string]$target) {
    @{Source=@{Type='local';Params=@{SourcePath=($source -replace '\\','/');ReadWrite=$false}};Target=$target}
}
$config=@{
    DisableAnalytics=$true
    API=@{Host='127.0.0.1';Port=$apiPort}
    Orchestrator=@{Enabled=$true;Host='127.0.0.1';Port=$natsPort}
    Compute=@{Enabled=$true;Orchestrators=@("nats://127.0.0.1:$natsPort");AllowListedLocalPaths=@(
        "$($PDF -replace '\\','/'):ro", "$($Worker -replace '\\','/'):ro", "$($control -replace '\\','/'):ro"
    )}
    Engines=@{Disabled=@('docker')}
    Publishers=@{Disabled=@('local')}
}
[IO.File]::WriteAllText((Join-Path $Results 'node.yaml'),($config|ConvertTo-Json -Depth 20))
$nodeName='viii-chunker-wasi'
$api="http://127.0.0.1:$apiPort/api/v1/orchestrator"
$node=$null
$job=$null
$sw=[Diagnostics.Stopwatch]::new()
$peak=0L
try {
    $node=Start-Process -FilePath $Bacalhau -ArgumentList 'serve --name viii-chunker-wasi --data-dir node -c node.yaml' -WorkingDirectory $Results -WindowStyle Hidden -PassThru -RedirectStandardOutput "$Results/node.stdout.log" -RedirectStandardError "$Results/node.stderr.log"
    [IO.File]::WriteAllText("$Results/owned.json",(@{pid=$node.Id;started=[DateTime]::UtcNow.ToString('o')}|ConvertTo-Json))
    $deadline=[DateTime]::UtcNow.AddSeconds(45)
    $ready=$false
    do {
        try {
            $nodes=Invoke-RestMethod "$api/nodes" -TimeoutSec 3
            if (@($nodes.Nodes|Where-Object{$_.Info.NodeID -eq $nodeName}).Count -gt 0) {$ready=$true;break}
        } catch {}
        Start-Sleep -Milliseconds 250
    } until ([DateTime]::UtcNow -gt $deadline)
    if (!$ready) { throw 'Chunker Bacalhau node unavailable' }
    $spec=@{Job=@{
        Name="viii-chunker-$([guid]::NewGuid().ToString('N'))"
        Type='batch';Count=1
        Constraints=@(@{Key='name';Operator='in';Values=@($nodeName)})
        Tasks=@(@{
            Name='main'
            Engine=@{Type='wasm';Params=@{EntryModule='/worker';Entrypoint='_start';Parameters=@('/original.pdf',"$First","$Last",'/out','/control/accepted')}}
            InputSources=@((LocalInput $Worker '/worker'),(LocalInput $PDF '/original.pdf'),(LocalInput $control '/control'))
            ResultPaths=@(@{Name='out';Path='C:/out'})
            Publisher=@{Type='noop'}
            Resources=@{CPU='1';Memory='3gb'}
            Timeouts=@{TotalTimeout=$TimeoutSeconds}
            Network=@{Type='none'}
        })
    }}
    [IO.File]::WriteAllText("$Results/job.json",($spec|ConvertTo-Json -Depth 24))
    $sw.Start()
    $submitted=Invoke-RestMethod "$api/jobs" -Method Put -ContentType application/json -Body ($spec|ConvertTo-Json -Depth 24) -TimeoutSec 30
    $job=$submitted.JobID
    if (!$job) { throw 'Missing job ID' }
    $deadline=[DateTime]::UtcNow.AddSeconds($TimeoutSeconds+20)
    $acked=$false
    do {
        $description=Invoke-RestMethod "$api/jobs/$job" -TimeoutSec 10
        # Some Bacalhau installations require include=executions to return execution IDs.
        $description=Invoke-RestMethod "$api/jobs/$($job)?include=executions" -TimeoutSec 10
        [IO.File]::WriteAllText("$Results/description.json",($description|ConvertTo-Json -Depth 35))
        $node.Refresh()
        if (!$node.HasExited) {$peak=[Math]::Max($peak,$node.WorkingSet64)}
        $execution=@($description.Executions.Items|Select-Object -First 1)
        if ($execution.Count -gt 0 -and $execution[0].ID) {
            $out="$Results/node/compute/executions/output/$($execution[0].ID)/results/out"
            if ((Test-Path "$out/ready") -and !$acked) {
                Copy-Item -Path "$out/*" -Destination $artifacts -Force
                if (!(Test-Path "$artifacts/chunk.pdf") -or !(Test-Path "$artifacts/info.json")) {throw 'Missing chunk artifacts'}
                $actual=Get-FileHash -LiteralPath "$artifacts/chunk.pdf" -Algorithm SHA256
                $meta=Get-Content -Raw "$artifacts/info.json"|ConvertFrom-Json
                if ($meta.chunk_sha256 -ne $actual.Hash.ToLowerInvariant() -or $meta.range_first -ne $First -or $meta.range_last -ne $Last) {throw 'Chunk manifest/hash mismatch'}
                [IO.File]::WriteAllText("$control/accepted",'accepted')
                $acked=$true
            }
        }
        if ($description.Job.State.StateType -in @('Completed','Failed','Stopped')) { break }
        Start-Sleep -Milliseconds 250
    } until ([DateTime]::UtcNow -gt $deadline)
    $sw.Stop()
    $after=(Get-FileHash -LiteralPath $PDF -Algorithm SHA256).Hash
    $complete=($description.Job.State.StateType -eq 'Completed') -and $acked -and ($before -eq $after)
    $summary=@{
        passed=$complete;input_sha256=$before;unchanged=($before -eq $after)
        first=$First;last=$Last;elapsed_ms=$sw.Elapsed.TotalMilliseconds
        sampled_node_peak_working_set_bytes=$peak;acknowledged=$acked
        job=$job;node=$nodeName;state=$description.Job.State.StateType
        worker_sha256=(Get-FileHash -LiteralPath $Worker -Algorithm SHA256).Hash
        bacalhau_revision='3a6a73aa6529755ac2c9d96a771f0566ff3ee5da'
        docker_disabled=$true
    }
    [IO.File]::WriteAllText("$Results/summary.json",($summary|ConvertTo-Json -Depth 8))
    if (!$complete) { throw 'WASI Bacalhau chunker gate failed (inspect summary and description)' }
    $summary|ConvertTo-Json -Depth 8
} finally {
    if ($job) {try{Invoke-RestMethod "$api/jobs/$job" -Method Delete -TimeoutSec 4|Out-Null}catch{}}
    if ($node -and !$node.HasExited) {Stop-Process -Id $node.Id}
}
