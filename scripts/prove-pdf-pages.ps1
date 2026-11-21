param(
    [string]$Bacalhau = 'C:\Bacalhau\bin\bacalhau.exe',
    [string]$PDF = '.poc-runtime/pdf/fixtures/small.pdf',
    [string]$Results = '.poc-runtime/pdf/pages-proof',
    [string]$Worker = '.poc-runtime/pdf/rust-target-simd/wasm32-wasip1/release/viii-pdf-worker.wasm',
    [int]$First = 1,
    [int]$Last = 12,
    [ValidateSet('render','extract','ocr')][string]$Mode = 'render',
    [string]$ModelDirectory = '.poc-runtime/pdf/models'
)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot)
if (Test-Path -LiteralPath $Results) { throw 'Choose a fresh results directory' }
$Results = (New-Item -ItemType Directory $Results).FullName
$Worker = (Resolve-Path $Worker).Path -replace '\\','/'
$PDF = (Resolve-Path $PDF).Path -replace '\\','/'
$ModelDirectory = (Resolve-Path $ModelDirectory).Path -replace '\\','/'
$before = (Get-FileHash $PDF).Hash
$build = & go version -m $Bacalhau
if ($LASTEXITCODE -or !($build -match 'bacalhau.*v1\.9\.5') -or !($build -match 'vcs.revision=3a6a73aa6529755ac2c9d96a771f0566ff3ee5da')) { throw 'Wrong Bacalhau build' }
foreach ($port in 42234,44222) {
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,$port)
    $listener.ExclusiveAddressUse = $true
    try { $listener.Start() } finally { $listener.Stop() }
}
$control = (New-Item -ItemType Directory "$Results/control").FullName -replace '\\','/'
New-Item -ItemType Directory "$Results/artifacts" | Out-Null
$config = @{
    DisableAnalytics=$true; API=@{Host='127.0.0.1';Port=42234}
    Orchestrator=@{Enabled=$true;Host='127.0.0.1';Port=44222}
    Compute=@{Enabled=$true;Orchestrators=@('nats://127.0.0.1:44222');AllowListedLocalPaths=@("${Worker}:ro","${PDF}:ro","${control}:ro","${ModelDirectory}:ro")}
    Engines=@{Disabled=@('docker')};Publishers=@{Disabled=@('local')}
}
[IO.File]::WriteAllText("$Results/node.yaml",($config | ConvertTo-Json -Depth 10))
$api='http://127.0.0.1:42234/api/v1/orchestrator'
$process=$null; $job=$null; $nodes=$null
$peak=0L; $peakDisk=0L; $firstPageMS=$null
try {
    $process=Start-Process $Bacalhau -ArgumentList 'serve --name viii-pdf-wasi --data-dir node -c node.yaml' -WorkingDirectory $Results -WindowStyle Hidden -PassThru -RedirectStandardOutput "$Results/node.stdout.log" -RedirectStandardError "$Results/node.stderr.log"
    [IO.File]::WriteAllText("$Results/owned.json",(@{pid=$process.Id;started=$process.StartTime.ToUniversalTime().ToString('o')} | ConvertTo-Json))
    $deadline=[DateTime]::UtcNow.AddSeconds(40)
    do {
        try { $nodes=Invoke-RestMethod "$api/nodes" -TimeoutSec 2; if ($nodes.Nodes) { break } } catch {}
        Start-Sleep -Milliseconds 250
    } until ([DateTime]::UtcNow -ge $deadline)
    if (!$nodes.Nodes) { throw 'Node not ready' }
    function New-ReadOnlyInput($Path,$Target) { @{Source=@{Type='local';Params=@{SourcePath=$Path;ReadWrite=$false}};Target=$Target} }
    $spec=@{Job=@{
        Name="viii-pdf-pages-$([guid]::NewGuid().ToString('N'))";Type='batch';Count=1
        Constraints=@(@{Key='name';Operator='in';Values=@('viii-pdf-wasi')})
        Tasks=@(@{Name='main';Engine=@{Type='wasm';Params=@{EntryModule='/worker';Entrypoint='_start';Parameters=@('/original.pdf',"$First","$Last",'/out','/control/accepted',$Mode,'/model')}}
            InputSources=@((New-ReadOnlyInput $Worker '/worker'),(New-ReadOnlyInput $PDF '/original.pdf'),(New-ReadOnlyInput $control '/control'),(New-ReadOnlyInput $ModelDirectory '/model'))
            ResultPaths=@(@{Name='out';Path='C:/out'});Publisher=@{Type='noop'};Resources=@{CPU='1';Memory='2gb'};Timeouts=@{TotalTimeout=600}
            Network=@{Type='none'}
        })
    }}
    [IO.File]::WriteAllText("$Results/job.json",($spec | ConvertTo-Json -Depth 20))
    $watch=[Diagnostics.Stopwatch]::StartNew()
    $job=(Invoke-RestMethod "$api/jobs" -Method Put -ContentType 'application/json' -Body ($spec | ConvertTo-Json -Depth 20)).JobID
    if (!$job) { throw 'No job ID' }
    $deadline=[DateTime]::UtcNow.AddSeconds(610)
    do {
        $d=Invoke-RestMethod "$api/jobs/${job}?include=executions" -TimeoutSec 5
        [IO.File]::WriteAllText("$Results/description.json",($d | ConvertTo-Json -Depth 35))
        $process.Refresh(); $peak=[Math]::Max($peak,$process.WorkingSet64)
        if (@($d.Executions.Items).Count -eq 1) {
            $execution=$d.Executions.Items[0]
            $out="$Results/node/compute/executions/output/$($execution.ID)/results/out"
            if ((Test-Path "$out/info.json") -and $null -eq $firstPageMS) { $firstPageMS=$watch.Elapsed.TotalMilliseconds }
            if (Test-Path "$out/ready") {
                Get-ChildItem -LiteralPath $out -File | Copy-Item -Destination "$Results/artifacts"
                $disk=(Get-ChildItem -LiteralPath $out -File | Measure-Object Length -Sum).Sum
                $peakDisk=[Math]::Max($peakDisk,$disk)
                [IO.File]::WriteAllText("$control/accepted",'accepted')
            }
        }
        if ($d.Job.State.StateType -in @('Completed','Failed','Stopped')) { break }
        Start-Sleep -Milliseconds 250
    } until ([DateTime]::UtcNow -ge $deadline)
    if ($d.Job.State.StateType -ne 'Completed' -or @($d.Executions.Items).Count -ne 1 -or $execution.RunOutput.ExitCode -ne 0 -or $execution.NodeID -ne 'viii-pdf-wasi') { throw 'PDF worker failed; inspect description.json' }
    foreach ($number in $First..$Last) {
        $page=Get-Content -Raw -Encoding UTF8 (Join-Path "$Results/artifacts" ('page-{0:D6}.json' -f $number)) | ConvertFrom-Json
        if ($page.page -ne $number) { throw 'Wrong physical page reference' }
    }
    if ((Get-FileHash $PDF).Hash -ne $before) { throw 'Original PDF mutated' }
    $summary=@{passed=$true;job=$job;execution=$execution.ID;input_sha256=$before;bytes=(Get-Item $PDF).Length;first=$First;last=$Last;mode=$Mode;elapsed_ms=$watch.Elapsed.TotalMilliseconds;first_artifact_ms=$firstPageMS;node_peak_working_set_bytes=$peak;output_disk_bytes=$peakDisk;worker_sha256=(Get-FileHash $Worker).Hash;docker_disabled=$true}
    [IO.File]::WriteAllText("$Results/summary.json",($summary | ConvertTo-Json))
    $summary | ConvertTo-Json
} finally {
    if ($job) { try { Invoke-RestMethod "$api/jobs/$job" -Method Delete -TimeoutSec 3 | Out-Null } catch {} }
    if ($process -and !$process.HasExited) { Stop-Process -Id $process.Id }
}
