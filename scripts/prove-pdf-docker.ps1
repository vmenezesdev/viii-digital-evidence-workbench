param(
    [string]$Bacalhau = 'C:\Bacalhau\bin\bacalhau.exe',
    [string]$PDF = '.poc-runtime/pdf/fixtures/small.pdf',
    [string]$Results = '.poc-runtime/pdf/docker-proof',
    [string]$WorkerImage = 'viii-pdf-worker:local',
    [string]$Model = '.poc-runtime/pdf/models/por.traineddata'
)
$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot)
if (Test-Path $Results) { throw 'Use a fresh results directory' }
$Results=(New-Item -ItemType Directory $Results).FullName
$PDF=(Resolve-Path $PDF).Path; $Model=(Resolve-Path $Model).Path
$hash=(Get-FileHash $PDF).Hash
$image='ghcr.io/bacalhau-project/bacalhau@sha256:903c4052a4522720adf43d428bdbb267cb24dec5039a759e27dffbcbbd68d878'
$owned="viii-pdf-$([guid]::NewGuid().ToString('N'))"
$volume="$owned-data"; $process=$null; $job=$null; $containerStarted=$false; $volumeCreated=$false
$api='http://127.0.0.1:42234/api/v1/orchestrator'
foreach ($port in 42234,44222) {
    $listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,$port)
    $listener.ExclusiveAddressUse=$true
    try { $listener.Start() } finally { $listener.Stop() }
}
$build=& go version -m $Bacalhau
if ($LASTEXITCODE -or !($build -match 'bacalhau.*v1\.9\.5') -or !($build -match 'vcs.revision=3a6a73aa6529755ac2c9d96a771f0566ff3ee5da')) { throw 'Wrong Bacalhau build' }
try {
    docker image inspect $WorkerImage --format '{{.Id}}' > "$Results/worker-image.txt"
    if ($LASTEXITCODE) { throw 'Build scripts/pdf-docker-worker.Dockerfile first' }
    docker volume create $volume | Out-Null
    if ($LASTEXITCODE) { throw 'Docker volume creation failed' }
    $volumeCreated=$true
    $linux=(docker volume inspect $volume --format '{{.Mountpoint}}').Trim()
    $token=[guid]::NewGuid().ToString('N')
    $a=@{DisableAnalytics=$true;API=@{Host='127.0.0.1';Port=42234};Orchestrator=@{Enabled=$true;Host='127.0.0.1';Port=44222;Auth=@{Token=$token}};Compute=@{Enabled=$false};Engines=@{Disabled=@('docker')};Publishers=@{Disabled=@('local')}}
    $b=@{DisableAnalytics=$true;API=@{Host='127.0.0.1';Port=42235};Compute=@{Enabled=$true;Orchestrators=@('nats://host.docker.internal:44222');Auth=@{Token=$token};AllowListedLocalPaths=@("${linux}/original.pdf:ro","${linux}/models:ro","${linux}/control:ro","${linux}/out:rw")};Publishers=@{Disabled=@('local')}}
    [IO.File]::WriteAllText("$Results/a.yaml",($a|ConvertTo-Json -Depth 12))
    [IO.File]::WriteAllText("$Results/b.yaml",($b|ConvertTo-Json -Depth 12))
    $process=Start-Process $Bacalhau -ArgumentList 'serve --name viii-pdf-orchestrator --data-dir node -c a.yaml' -WorkingDirectory $Results -WindowStyle Hidden -PassThru -RedirectStandardOutput "$Results/node.stdout.log" -RedirectStandardError "$Results/node.stderr.log"
    docker run -d --name $owned --mount "type=volume,source=$volume,target=$linux" --mount "type=bind,source=$Results/b.yaml,target=/viii.yaml,readonly" --mount 'type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock' $image serve --name viii-pdf-docker --data-dir "$linux/node" -c /viii.yaml | Out-Null
    if ($LASTEXITCODE) { throw 'Compute node failed to start' }
    $containerStarted=$true
    [IO.File]::WriteAllText("$Results/owned.json",(@{pid=$process.Id;started=$process.StartTime.ToUniversalTime().ToString('o');container=$owned;volume=$volume;path=$linux} | ConvertTo-Json))
    # Ordinary local artifact staging; the PDF processing itself is a Bacalhau job.
    docker run --rm --mount "type=volume,source=$volume,target=/data" alpine:3 mkdir -p /data/models /data/control /data/out | Out-Null
    if ($LASTEXITCODE) { throw 'Volume initialization failed' }
    docker cp $PDF "${owned}:${linux}/original.pdf"
    if ($LASTEXITCODE) { throw 'PDF staging failed' }
    docker cp $Model "${owned}:${linux}/models/por.traineddata"
    if ($LASTEXITCODE) { throw 'Model staging failed' }
    $deadline=[DateTime]::UtcNow.AddSeconds(45)
    do {
        try { $nodes=Invoke-RestMethod "$api/nodes" -TimeoutSec 2; if ($nodes.Nodes | Where-Object { $_.Info.NodeID -eq 'viii-pdf-docker' }) { break } } catch {}
        Start-Sleep -Milliseconds 250
    } until ([DateTime]::UtcNow -ge $deadline)
    if (!($nodes.Nodes | Where-Object { $_.Info.NodeID -eq 'viii-pdf-docker' })) { throw 'Docker compute did not register' }
    function New-LocalInput($Path,$Target,$Writable=$false) { @{Source=@{Type='local';Params=@{SourcePath=$Path;ReadWrite=$Writable}};Target=$Target} }
    $spec=@{Job=@{Name="viii-pdf-docker-$([guid]::NewGuid().ToString('N'))";Type='batch';Count=1;Constraints=@(@{Key='name';Operator='in';Values=@('viii-pdf-docker')});Tasks=@(@{
        Name='main';Engine=@{Type='docker';Params=@{Image=$WorkerImage;Entrypoint=@('/pdf-worker');Parameters=@('/original.pdf','1','12','/out','/control/accepted','ocr','/model');SkipImagePull=$true}}
        InputSources=@((New-LocalInput "$linux/original.pdf" '/original.pdf'),(New-LocalInput "$linux/models" '/model'),(New-LocalInput "$linux/control" '/control'),(New-LocalInput "$linux/out" '/out' $true));Resources=@{CPU='1';Memory='2gb'};Timeouts=@{TotalTimeout=600};Network=@{Type='none'}
    })}}
    [IO.File]::WriteAllText("$Results/job.json",($spec|ConvertTo-Json -Depth 20))
    $watch=[Diagnostics.Stopwatch]::StartNew()
    $job=(Invoke-RestMethod "$api/jobs" -Method Put -ContentType 'application/json' -Body ($spec|ConvertTo-Json -Depth 20)).JobID
    if (!$job) { throw 'No job ID' }
    $deadline=[DateTime]::UtcNow.AddSeconds(610)
    New-Item -ItemType Directory "$Results/artifacts" | Out-Null
    do {
        $d=Invoke-RestMethod "$api/jobs/${job}?include=executions" -TimeoutSec 5
        [IO.File]::WriteAllText("$Results/description.json",($d|ConvertTo-Json -Depth 35))
        docker cp "${owned}:${linux}/out/." "$Results/artifacts" 2> "$Results/copy.log"
        if (Test-Path "$Results/artifacts/ready") {
            [IO.File]::WriteAllText("$Results/accepted",'accepted')
            docker cp "$Results/accepted" "${owned}:${linux}/control/accepted"
            if ($LASTEXITCODE) { throw 'Acknowledgement failed' }
        }
        if ($d.Job.State.StateType -in @('Completed','Failed','Stopped')) { break }
        Start-Sleep -Milliseconds 500
    } until ([DateTime]::UtcNow -ge $deadline)
    $execution=@($d.Executions.Items)
    if ($d.Job.State.StateType -ne 'Completed' -or $execution.Count -ne 1 -or $execution[0].RunOutput.ExitCode -ne 0 -or $execution[0].NodeID -ne 'viii-pdf-docker') { throw 'Docker job failed; inspect description.json' }
    $page=Get-Content -Raw -Encoding UTF8 "$Results/artifacts/page-000011.json" | ConvertFrom-Json
    if ($page.page -ne 11 -or $page.source -ne 'ocr' -or $page.text -notmatch 'JABUTICABA') { throw 'Scanned page OCR assertion failed' }
    if ((Get-FileHash $PDF).Hash -ne $hash) { throw 'Original mutated' }
    docker cp "${owned}:${linux}/original.pdf" "$Results/staged-original.pdf"
    if ($LASTEXITCODE -or (Get-FileHash "$Results/staged-original.pdf").Hash -ne $hash) { throw 'Staged original mutated' }
    $summary=@{passed=$true;engine='docker';execution=$execution[0].ID;job=$job;elapsed_ms=$watch.Elapsed.TotalMilliseconds;input_sha256=$hash;tool_version=$page.version;image=$WorkerImage}
    [IO.File]::WriteAllText("$Results/summary.json",($summary | ConvertTo-Json))
    $summary | ConvertTo-Json
} finally {
    if ($job) { try { Invoke-RestMethod "$api/jobs/$job" -Method Delete -TimeoutSec 3 | Out-Null } catch {} }
    if ($containerStarted) { docker logs $owned > "$Results/compute.log" 2>&1; docker rm -f $owned | Out-Null }
    if ($volumeCreated) { docker volume rm $volume > "$Results/volume-cleanup.log" 2>&1 }
    if ($process -and !$process.HasExited) { Stop-Process -Id $process.Id }
}
