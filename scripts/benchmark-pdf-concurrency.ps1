param(
    [string]$PDF='.poc-runtime/pdf/fixtures/mixed-30000.pdf',
    [int]$First=29961,
    [int]$Last=30000,
    [string]$Package='bin/pdf-mvp',
    [Parameter(Mandatory=$true)][string]$UIData,
    [string]$Results='.poc-runtime/pdf/benchmark-concurrency',
    [switch]$Resume
)
$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot)
$PDF=(Resolve-Path $PDF).Path;$Package=(Resolve-Path $Package).Path;$UIData=(Resolve-Path $UIData).Path
$manifest=Get-Content -Raw ([IO.Path]::ChangeExtension($PDF,'.json'))|ConvertFrom-Json
if((Get-FileHash $PDF).Hash.ToLowerInvariant() -ne $manifest.sha256){throw 'Fixture identity mismatch'}
$total=$Last-$First+1
$ocrPages=@($manifest.scanned|Where-Object{$_ -ge $First -and $_ -le $Last}).Count
if($First -lt 1 -or $Last -gt $manifest.pages -or $total -lt 4 -or $manifest.target_page -lt $First -or $manifest.target_page -gt $Last -or $ocrPages -lt 4){throw 'Choose a mixed range including the scanned target and at least four OCR pages'}
$completed=Get-Content -Raw "$UIData/pdf-document.json"|ConvertFrom-Json
if(!$completed.complete){throw 'UI probe requires a previously completed product profile'}
if($Resume){$Results=(Resolve-Path $Results).Path}else{if(Test-Path $Results){throw 'Use fresh results or -Resume'};$Results=(New-Item -ItemType Directory $Results).FullName}
function JSON($File,$Value){[IO.File]::WriteAllText($File,($Value|ConvertTo-Json -Depth 35))}
function Event($Kind,$Value){[IO.File]::AppendAllText("$run/events.jsonl",((@{at=[DateTime]::UtcNow.ToString('o');kind=$Kind;value=$Value}|ConvertTo-Json -Depth 12 -Compress)+[Environment]::NewLine))}
function Owned($File,$Exe){if(Test-Path $File){$o=Get-Content -Raw $File|ConvertFrom-Json;$p=Get-Process -Id $o.pid -ErrorAction SilentlyContinue;if($p -and $p.Path -eq $Exe -and [Math]::Abs(($p.StartTime.ToUniversalTime()-([DateTime]$o.started).ToUniversalTime()).TotalSeconds) -lt 1){return $p}};return $null}
function Start-Owned($Exe,$Arguments,$Owner,$Log,$Directory){$p=Start-Process $Exe -ArgumentList $Arguments -WorkingDirectory $Directory -WindowStyle Hidden -PassThru -RedirectStandardOutput "$Log.stdout.log" -RedirectStandardError "$Log.stderr.log";JSON $Owner @{pid=$p.Id;started=$p.StartTime.ToUniversalTime().ToString('o')};return $p}
function ReadOnly($Source,$Target){@{Source=@{Type='local';Params=@{SourcePath=($Source -replace '\\','/');ReadWrite=$false}};Target=$Target}}
$baca=Join-Path $Package 'runtime/bacalhau.exe';$worker=Join-Path $Package 'runtime/pdf-worker.wasm';$model=Join-Path $Package 'runtime/models'
$version=& go version -m $baca
if($LASTEXITCODE -or !($version -match 'bacalhau.*v1\.9\.5') -or !($version -match 'vcs.revision=3a6a73aa6529755ac2c9d96a771f0566ff3ee5da')){throw 'Wrong Bacalhau build'}
$uiExe=Join-Path $Package 'VIII.exe';$ui=Owned "$Results/ui-owned.json" $uiExe
if(!$ui){
    $listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,49182);$listener.ExclusiveAddressUse=$true;try{$listener.Start()}finally{$listener.Stop()}
    $ui=Start-Owned $uiExe "--data-dir `"$UIData`" --http 127.0.0.1:49182" "$Results/ui-owned.json" "$Results/ui" $Results
}
$uiDeadline=[DateTime]::UtcNow.AddSeconds(20)
do{try{$uiStatus=Invoke-RestMethod 'http://127.0.0.1:49182/api/pdf/status' -TimeoutSec 2;break}catch{Start-Sleep -Milliseconds 250}}until([DateTime]::UtcNow -gt $uiDeadline)
if(!$uiStatus.document.complete -or $uiStatus.document.sha256 -ne $completed.sha256){throw 'Wrong UI profile'}
foreach($count in 1,2,4){
    $run=Join-Path $Results "jobs-$count"
    if(Test-Path "$run/summary.json"){continue}
    if(!(Test-Path $run)){New-Item -ItemType Directory "$run/control" -Force|Out-Null;New-Item -ItemType Directory "$run/artifacts"|Out-Null}
    $nodeName="viii-pdf-sweep-$count"
    $api='http://127.0.0.1:42234/api/v1/orchestrator'
    $node=Owned "$run/owned.json" $baca
    if(!$node){
        foreach($port in 42234,44222){$listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,$port);$listener.ExclusiveAddressUse=$true;try{$listener.Start()}finally{$listener.Stop()}}
        $control=(Join-Path $run 'control') -replace '\\','/'
        JSON "$run/node.yaml" @{DisableAnalytics=$true;API=@{Host='127.0.0.1';Port=42234};Orchestrator=@{Enabled=$true;Host='127.0.0.1';Port=44222};Compute=@{Enabled=$true;Orchestrators=@('nats://127.0.0.1:44222');AllowListedLocalPaths=@("$($worker -replace '\\','/'):ro","$($PDF -replace '\\','/'):ro","${control}/**:ro","$($model -replace '\\','/'):ro")};Engines=@{Disabled=@('docker')};Publishers=@{Disabled=@('local')}}
        $previousHeapLimit=$env:GOMEMLIMIT
        try {
            $env:GOMEMLIMIT='3GiB'
            $node=Start-Owned $baca "serve --name $nodeName --data-dir node -c node.yaml" "$run/owned.json" "$run/node" $run
        } finally { $env:GOMEMLIMIT=$previousHeapLimit }
    }
    $deadline=[DateTime]::UtcNow.AddSeconds(40)
    do{try{$nodes=Invoke-RestMethod "$api/nodes" -TimeoutSec 2;if($nodes.Nodes|Where-Object{$_.Info.NodeID -eq $nodeName}){break}}catch{};Start-Sleep -Milliseconds 250}until([DateTime]::UtcNow -gt $deadline)
    if([DateTime]::UtcNow -gt $deadline){throw 'Sweep node did not become reachable'}
    if(!(Test-Path "$run/state.json")){JSON "$run/state.json" @{started=[DateTime]::UtcNow.ToString('o');concurrency=$count;sha256=$manifest.sha256;first=$First;last=$Last;pages=$total;peak_ram=0L;peak_private=0L;peak_active=0}}
    $state=Get-Content -Raw "$run/state.json"|ConvertFrom-Json
    if($state.sha256 -ne $manifest.sha256 -or $state.first -ne $First -or $state.last -ne $Last){throw 'Resume fixture/range mismatch'}
    $jobs=@()
    for($i=0;$i -lt $count;$i++){
        $rangeFirst=$First+[Math]::Floor($i*$total/$count);$rangeLast=$First-1+[Math]::Floor(($i+1)*$total/$count)
        $dir=New-Item -ItemType Directory -Force "$run/control/$i"
        $specPath="$run/spec-$i.json"
        if(!(Test-Path $specPath)){
            $spec=@{Job=@{Name="viii-sweep-$count-$i-$([guid]::NewGuid().ToString('N'))";Type='batch';Count=1;Constraints=@(@{Key='name';Operator='in';Values=@($nodeName)});Tasks=@(@{Name='main';Engine=@{Type='wasm';Params=@{EntryModule='/worker';Entrypoint='_start';Parameters=@('/original.pdf',"$rangeFirst","$rangeLast",'/out','/control/accepted','ocr','/model')}};InputSources=@((ReadOnly $worker '/worker'),(ReadOnly $PDF '/original.pdf'),(ReadOnly $model '/model'),(ReadOnly $dir.FullName '/control'));ResultPaths=@(@{Name='out';Path='C:/out'});Publisher=@{Type='noop'};Resources=@{CPU='1';Memory='2gb'};Timeouts=@{TotalTimeout=3600};Network=@{Type='none'}})}}
            JSON $specPath $spec
        }
        $body=Get-Content -Raw $specPath;$spec=$body|ConvertFrom-Json
        try{$existing=Invoke-RestMethod "$api/jobs/$($spec.Job.Name)?include=executions" -TimeoutSec 5;$jobID=$existing.Job.ID}catch{if([int]$_.Exception.Response.StatusCode -ne 404){throw};$jobID=(Invoke-RestMethod "$api/jobs" -Method Put -ContentType application/json -Body $body -TimeoutSec 30).JobID}
        if(!$jobID){throw 'Missing job ID'}
        $jobs+=@{id=$jobID;first=$rangeFirst;last=$rangeLast;control=$dir.FullName;index=$i}
    }
    JSON "$run/jobs.json" $jobs
    $stop=Join-Path $run 'probe.stop'
    if(Test-Path $stop){Remove-Item -LiteralPath $stop}
    $probe=Owned "$run/probe-owned.json" (Get-Command node.exe).Source
    if(!$probe){$probe=Start-Owned (Get-Command node.exe).Source "`"$PWD/scripts/probe-pdf-responsiveness.cjs`" http://127.0.0.1:49182/pdf.html `"$stop`" `"$run/ui.jsonl`"" "$run/probe-owned.json" "$run/probe" $Results}
    Event 'monitor-start' @{concurrency=$count;resume=[bool]$Resume;go_heap_limit='3GiB'}
    do{
        $allDone=$true;$active=0;$descriptions=@()
        foreach($job in $jobs){
            $d=Invoke-RestMethod "$api/jobs/$($job.id)?include=executions" -TimeoutSec 10
            JSON "$run/job-$($job.index).json" $d
            if(@($d.Executions.Items).Count -gt 1){throw 'Unexpected duplicate execution'}
            $execution=$d.Executions.Items|Select-Object -First 1
            if($execution -and $execution.NodeID -ne $nodeName){throw 'Unauthorized execution node'}
            if($execution){
                $out="$run/node/compute/executions/output/$($execution.ID)/results/out"
                if(Test-Path "$out/ready"){
                    Get-ChildItem $out -Filter 'page-*.json'|Copy-Item -Destination "$run/artifacts"
                    $pages=@(Get-ChildItem "$run/artifacts" -Filter 'page-*.json'|Where-Object{[int]($_.BaseName -replace 'page-','') -ge $job.first -and [int]($_.BaseName -replace 'page-','') -le $job.last})
                    if($pages.Count -ne ($job.last-$job.first+1)){throw 'Incomplete range artifacts'}
                    [IO.File]::WriteAllText("$($job.control)/accepted",'accepted')
                }
            }
            if($d.Job.State.StateType -in 'Failed','Stopped'){throw 'Sweep job failed; inspect job description'}
            if($d.Job.State.StateType -ne 'Completed'){$allDone=$false}
            if($execution.ComputeState.StateType -in 5,6){$active++}
            $descriptions+=@{id=$job.id;state=$d.Job.State.StateType;execution_state=$execution.ComputeState.StateType}
        }
        $node.Refresh();$state.peak_ram=[Math]::Max([long]$state.peak_ram,$node.WorkingSet64);$state.peak_private=[Math]::Max([long]$state.peak_private,$node.PrivateMemorySize64)
        $state.peak_active=[Math]::Max($state.peak_active,$active)
        $timer=[Diagnostics.Stopwatch]::StartNew();Invoke-RestMethod "$api/nodes" -TimeoutSec 10|Out-Null;$latency=$timer.Elapsed.TotalMilliseconds
        Event 'sample' @{working_set=$node.WorkingSet64;private_bytes=$node.PrivateMemorySize64;process_peak_working_set=$node.PeakWorkingSet64;cpu_seconds=$node.TotalProcessorTime.TotalSeconds;active=$active;jobs=$descriptions;api_latency_ms=$latency;free_memory_kib=(Get-CimInstance Win32_OperatingSystem).FreePhysicalMemory}
        JSON "$run/state.json" $state
        if($probe.HasExited){throw 'UI responsiveness probe exited unexpectedly'}
        if(!$allDone){Start-Sleep -Seconds 2}
    }until($allDone)
    [IO.File]::WriteAllText($stop,'stop');if(!$probe.WaitForExit(15000) -or $probe.ExitCode){throw 'UI probe did not finish successfully'}
    $pages=@(Get-ChildItem "$run/artifacts" -Filter 'page-*.json'|ForEach-Object{Get-Content -Raw -Encoding UTF8 $_.FullName|ConvertFrom-Json})
    $target=$pages|Where-Object{$_.page -eq $manifest.target_page}
    if($pages.Count -ne $total -or $target.text -notmatch 'JABUTICABA' -or @($pages|Where-Object{$_.source -eq 'ocr'}).Count -ne $ocrPages -or $state.peak_active -ne $count){throw 'Physical page/OCR/concurrency assertion failed'}
    if((Get-FileHash $PDF).Hash.ToLowerInvariant() -ne $manifest.sha256){throw 'Original mutated'}
    $uiSamples=@(Get-Content "$run/ui.jsonl"|ForEach-Object{$_|ConvertFrom-Json})
    if($uiSamples.Count -lt 3){throw 'Insufficient UI responsiveness samples'}
    $elapsed=([DateTime]::UtcNow-([DateTime]$state.started).ToUniversalTime()).TotalSeconds
    JSON "$run/summary.json" @{passed=$true;concurrency=$count;peak_active=$state.peak_active;pages=$pages.Count;first=$First;last=$Last;ocr_pages=$ocrPages;document_pages=$manifest.pages;input_bytes=$manifest.bytes;sha256=$manifest.sha256;elapsed_seconds=$elapsed;ocr_pages_per_second=$ocrPages/$elapsed;sampled_peak_node_working_set=$state.peak_ram;sampled_peak_node_private=$state.peak_private;ui_samples=$uiSamples.Count;ui_search_max_ms=($uiSamples|Measure-Object ui_search_ms -Maximum).Maximum;scheduler_delay_max_ms=($uiSamples|Measure-Object scheduler_delay_ms -Maximum).Maximum;note='Same physical range of the full original PDF, CPU=1 per job; no inference. UI is a completed local index in Chrome. Detailed samples retain the actual simultaneous active count.'}
    Stop-Process -InputObject $node
    if(!$node.WaitForExit(10000)){throw 'Concluded sweep node did not exit'}
}
Stop-Process -InputObject $ui
Get-ChildItem "$Results/jobs-*/summary.json"|ForEach-Object{Get-Content -Raw $_.FullName}
