# Same workload, 1/2/4 simultaneous WASI OCR jobs on verified 40-page chunks.
# Each job OCRs one whole chunk (production unit); the queue keeps at most
# $count jobs active. Measures throughput including per-job startup, node
# memory, host CPU/memory and UI responsiveness on a completed local index.
param(
    [Parameter(Mandatory=$true)][string]$Chunks,
    [string]$Package='bin/pdf-mvp',
    [Parameter(Mandatory=$true)][string]$UIData,
    [string]$Probe='scripts/probe-pdf-responsiveness.cjs',
    [string]$Results='.poc-runtime/pdf/benchmark-chunk-concurrency',
    [int[]]$Counts=@(1,2,4),
    [string]$Memory='1gb',
    [int]$TargetPage=29999,
    [string]$TargetTerm='JABUTICABA',
    [switch]$Resume
)
$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot)
$Chunks=(Resolve-Path $Chunks).Path;$Package=(Resolve-Path $Package).Path;$UIData=(Resolve-Path $UIData).Path;$Probe=(Resolve-Path $Probe).Path
if($Resume){$Results=(Resolve-Path $Results).Path}else{if(Test-Path $Results){throw 'Use fresh results or -Resume'};$Results=(New-Item -ItemType Directory $Results).FullName}
function JSON($File,$Value){[IO.File]::WriteAllText($File,($Value|ConvertTo-Json -Depth 35))}
function Event($Kind,$Value){[IO.File]::AppendAllText("$run/events.jsonl",((@{at=[DateTime]::UtcNow.ToString('o');kind=$Kind;value=$Value}|ConvertTo-Json -Depth 12 -Compress)+[Environment]::NewLine))}
function Owned($File,$Exe){if(Test-Path $File){$o=Get-Content -Raw $File|ConvertFrom-Json;$p=Get-Process -Id $o.pid -ErrorAction SilentlyContinue;if($p -and $p.Path -eq $Exe -and [Math]::Abs(($p.StartTime.ToUniversalTime()-([DateTime]$o.started).ToUniversalTime()).TotalSeconds) -lt 1){return $p}};return $null}
function Start-Owned($Exe,$Arguments,$Owner,$Log,$Directory){$p=Start-Process $Exe -ArgumentList $Arguments -WorkingDirectory $Directory -WindowStyle Hidden -PassThru -RedirectStandardOutput "$Log.stdout.log" -RedirectStandardError "$Log.stderr.log";JSON $Owner @{pid=$p.Id;started=$p.StartTime.ToUniversalTime().ToString('o')};return $p}
function ReadOnly($Source,$Target){@{Source=@{Type='local';Params=@{SourcePath=($Source -replace '\\','/');ReadWrite=$false}};Target=$Target}}
function Free-Port($Port){$l=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,$Port);$l.ExclusiveAddressUse=$true;try{$l.Start()}finally{$l.Stop()}}

# Verified chunk queue: sha256 and exact local->physical map from each sidecar.
$queue=@()
foreach($sidecar in Get-ChildItem $Chunks -Filter 'chunk-*.json'|Sort-Object Name){
    $s=Get-Content -Raw $sidecar.FullName|ConvertFrom-Json
    $file=Join-Path $Chunks $s.file
    if((Get-FileHash $file).Hash.ToLowerInvariant() -ne $s.chunk_sha256){throw "Chunk hash mismatch: $($s.file)"}
    $n=$s.physical_last-$s.physical_first+1
    if($s.chunk_page_count -ne $n -or @($s.pages).Count -ne $n){throw "Chunk map mismatch: $($s.file)"}
    for($i=0;$i -lt $n;$i++){if($s.pages[$i].local -ne $i+1 -or $s.pages[$i].physical -ne $s.physical_first+$i){throw "Chunk map mismatch: $($s.file)"}}
    $queue+=@{file=$file;name=[IO.Path]::GetFileNameWithoutExtension($s.file);first=$s.physical_first;last=$s.physical_last;sha256=$s.chunk_sha256}
}
if($queue.Count -lt 4){throw 'Need at least four chunks'}
$totalPages=($queue|ForEach-Object{$_.last-$_.first+1}|Measure-Object -Sum).Sum
$baca=Join-Path $Package 'runtime/bacalhau.exe';$worker=Join-Path $Package 'runtime/pdf-worker.wasm';$model=Join-Path $Package 'runtime/models'
$version=& go version -m $baca
if($LASTEXITCODE -or !($version -match 'bacalhau.*v1\.9\.5') -or !($version -match 'vcs.revision=3a6a73aa6529755ac2c9d96a771f0566ff3ee5da')){throw 'Wrong Bacalhau build'}
$workerSHA=(Get-FileHash $worker).Hash.ToLowerInvariant()
JSON "$Results/identity.json" @{chunks=$queue;worker_sha256=$workerSHA;bacalhau_sha256=(Get-FileHash $baca).Hash.ToLowerInvariant();model_sha256=(Get-FileHash "$model/por.traineddata").Hash.ToLowerInvariant();job_memory=$Memory;machine=@{os=(Get-CimInstance Win32_OperatingSystem).Caption;build=[Environment]::OSVersion.Version.ToString();logical_cpus=[Environment]::ProcessorCount;physical_cores=(Get-CimInstance Win32_Processor|Measure-Object NumberOfCores -Sum).Sum;ram_bytes=(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory}}

# A copy of a completed product profile serves the responsiveness probe.
$uiCopy="$Results/ui-data"
if(!(Test-Path $uiCopy)){Copy-Item -Recurse $UIData $uiCopy}
$completed=Get-Content -Raw "$uiCopy/pdf-document.json"|ConvertFrom-Json
if(!$completed.complete){throw 'UI probe requires a completed product profile'}
$uiExe=Join-Path $Package 'VIII.exe';$ui=Owned "$Results/ui-owned.json" $uiExe
if(!$ui){Free-Port 49182;$ui=Start-Owned $uiExe "--data-dir `"$uiCopy`" --http 127.0.0.1:49182" "$Results/ui-owned.json" "$Results/ui" $Results}
$uiDeadline=[DateTime]::UtcNow.AddSeconds(20)
do{try{$uiStatus=Invoke-RestMethod 'http://127.0.0.1:49182/api/pdf/status' -TimeoutSec 2;break}catch{Start-Sleep -Milliseconds 250}}until([DateTime]::UtcNow -gt $uiDeadline)
if(!$uiStatus.document.complete){throw 'Wrong UI profile'}
$cpu=New-Object Diagnostics.PerformanceCounter('Processor','% Processor Time','_Total');[void]$cpu.NextValue()

foreach($count in $Counts){
    $run=Join-Path $Results "jobs-$count"
    if(Test-Path "$run/summary.json"){continue}
    if(Test-Path $run){throw "Incomplete run $($run): inspect and remove it before repeating"}
    New-Item -ItemType Directory "$run/control","$run/artifacts" -Force|Out-Null
    $nodeName="viii-pdf-chunk-sweep-$count";$api='http://127.0.0.1:42234/api/v1/orchestrator'
    foreach($port in 42234,44222){Free-Port $port}
    $control=(Join-Path $run 'control') -replace '\\','/'
    JSON "$run/node.yaml" @{DisableAnalytics=$true;API=@{Host='127.0.0.1';Port=42234};Orchestrator=@{Enabled=$true;Host='127.0.0.1';Port=44222};Compute=@{Enabled=$true;Orchestrators=@('nats://127.0.0.1:44222');AllowListedLocalPaths=@("$($worker -replace '\\','/'):ro","$($Chunks -replace '\\','/')/**:ro","${control}/**:ro","$($model -replace '\\','/'):ro")};Engines=@{Disabled=@('docker')};Publishers=@{Disabled=@('local')}}
    $previous=$env:GOMEMLIMIT
    try{$env:GOMEMLIMIT='3GiB';$node=Start-Owned $baca "serve --name $nodeName --data-dir node -c node.yaml" "$run/owned.json" "$run/node" $run}finally{$env:GOMEMLIMIT=$previous}
    $deadline=[DateTime]::UtcNow.AddSeconds(40)
    do{try{$nodes=Invoke-RestMethod "$api/nodes" -TimeoutSec 2;if($nodes.Nodes|Where-Object{$_.Info.NodeID -eq $nodeName}){break}}catch{};Start-Sleep -Milliseconds 250}until([DateTime]::UtcNow -gt $deadline)
    if([DateTime]::UtcNow -gt $deadline){throw 'Sweep node did not become reachable'}
    Start-Sleep -Seconds 3;$node.Refresh();$idle=$node.WorkingSet64
    $stop=Join-Path $run 'probe.stop'
    $probeProc=Start-Owned (Get-Command node.exe).Source "`"$Probe`" http://127.0.0.1:49182/pdf.html `"$stop`" `"$run/ui.jsonl`"" "$run/probe-owned.json" "$run/probe" $Results
    $state=@{started=[DateTime]::UtcNow.ToString('o');concurrency=$count;idle_node_working_set=$idle;peak_ram=0L;peak_private=0L;peak_active=0;min_free_kib=[long]::MaxValue;cpu=@()}
    $watch=[Diagnostics.Stopwatch]::StartNew()
    $jobs=@();$next=0
    Event 'monitor-start' @{concurrency=$count;chunks=$queue.Count;pages=$totalPages;idle_node_working_set=$idle}
    do{
        $running=@($jobs|Where-Object{!$_.done}).Count
        while($running -lt $count -and $next -lt $queue.Count){
            $c=$queue[$next];$dir=(New-Item -ItemType Directory -Force "$run/control/$($c.name)").FullName
            $n=$c.last-$c.first+1
            $spec=@{Job=@{Name="viii-chunk-sweep-$count-$next-$([guid]::NewGuid().ToString('N'))";Type='batch';Count=1;Constraints=@(@{Key='name';Operator='in';Values=@($nodeName)});Tasks=@(@{Name='main';Engine=@{Type='wasm';Params=@{EntryModule='/worker';Entrypoint='_start';Parameters=@('/original.pdf','1',"$n",'/out','/control/accepted','ocr','/model')}};InputSources=@((ReadOnly $worker '/worker'),(ReadOnly $c.file '/original.pdf'),(ReadOnly $model '/model'),(ReadOnly $dir '/control'));ResultPaths=@(@{Name='out';Path='C:/out'});Publisher=@{Type='noop'};Resources=@{CPU='1';Memory=$Memory};Timeouts=@{TotalTimeout=3600};Network=@{Type='none'}})}}
            $body=$spec|ConvertTo-Json -Depth 20;[IO.File]::WriteAllText("$run/spec-$next.json",$body)
            $id=(Invoke-RestMethod "$api/jobs" -Method Put -ContentType application/json -Body $body -TimeoutSec 30).JobID
            if(!$id){throw 'Missing job ID'}
            $jobs+=@{id=$id;chunk=$c;control=$dir;index=$next;submitted_s=$watch.Elapsed.TotalSeconds;done=$false;copied=$false}
            Event 'submit' @{index=$next;job=$id;chunk=$c.name};$next++;$running++
        }
        $active=0;$descriptions=@()
        foreach($job in $jobs|Where-Object{!$_.done}){
            $d=Invoke-RestMethod "$api/jobs/$($job.id)?include=executions" -TimeoutSec 10
            if(@($d.Executions.Items).Count -gt 1){throw 'Unexpected duplicate execution'}
            $x=$d.Executions.Items|Select-Object -First 1
            if($x -and $x.NodeID -ne $nodeName){throw 'Unauthorized execution node'}
            if($x -and $x.ComputeState.StateType -in 5,6){$active++;if(!$job.started_s){$job.started_s=$watch.Elapsed.TotalSeconds}}
            if($x -and !$job.copied){
                $out="$run/node/compute/executions/output/$($x.ID)/results/out"
                if(Test-Path "$out/ready"){
                    $dest=(New-Item -ItemType Directory -Force "$run/artifacts/$($job.chunk.name)").FullName
                    Get-ChildItem $out -Filter 'page-*.json'|Copy-Item -Destination $dest
                    $n=$job.chunk.last-$job.chunk.first+1
                    if(@(Get-ChildItem $dest -Filter 'page-*.json').Count -ne $n){throw 'Incomplete chunk artifacts'}
                    [IO.File]::WriteAllText("$($job.control)/accepted",'accepted');$job.copied=$true;$job.ready_s=$watch.Elapsed.TotalSeconds
                }
            }
            if($d.Job.State.StateType -in 'Failed','Stopped'){JSON "$run/failed-$($job.index).json" $d;throw 'Sweep job failed; inspect job description'}
            if($d.Job.State.StateType -eq 'Completed'){
                if(!$job.copied -or $x.ComputeState.StateType -ne 9 -or [int]$x.RunOutput.ExitCode -ne 0){JSON "$run/failed-$($job.index).json" $d;throw 'Job completed without accepted results or with a non-zero exit'}
                $job.done=$true;$job.completed_s=$watch.Elapsed.TotalSeconds;JSON "$run/job-$($job.index).json" $d;Event 'complete' @{index=$job.index;wall_s=$job.completed_s-$job.submitted_s}
            }
            $descriptions+=@{index=$job.index;state=$d.Job.State.StateType;execution_state=$x.ComputeState.StateType}
        }
        $node.Refresh();$free=[long](Get-CimInstance Win32_OperatingSystem).FreePhysicalMemory;$load=$cpu.NextValue()
        $state.peak_ram=[Math]::Max([long]$state.peak_ram,$node.WorkingSet64);$state.peak_private=[Math]::Max([long]$state.peak_private,$node.PrivateMemorySize64)
        $state.peak_active=[Math]::Max($state.peak_active,$active);$state.min_free_kib=[Math]::Min($state.min_free_kib,$free);$state.cpu+=$load
        Event 'sample' @{working_set=$node.WorkingSet64;private_bytes=$node.PrivateMemorySize64;cpu_seconds=$node.TotalProcessorTime.TotalSeconds;host_cpu_percent=$load;active=$active;jobs=$descriptions;free_memory_kib=$free}
        if($probeProc.HasExited){throw 'UI responsiveness probe exited unexpectedly'}
        $allDone=($next -ge $queue.Count) -and !(@($jobs|Where-Object{!$_.done}).Count)
        if(!$allDone){Start-Sleep -Seconds 1}
    }until($allDone)
    $elapsed=$watch.Elapsed.TotalSeconds
    [IO.File]::WriteAllText($stop,'stop');if(!$probeProc.WaitForExit(15000) -or $probeProc.ExitCode){throw 'UI probe did not finish successfully'}
    # Map chunk-local pages to physical pages through the verified sidecar map.
    $pages=@()
    foreach($job in $jobs){
        foreach($f in Get-ChildItem "$run/artifacts/$($job.chunk.name)" -Filter 'page-*.json'){
            $p=Get-Content -Raw -Encoding UTF8 $f.FullName|ConvertFrom-Json;$local=[int]($f.BaseName -replace 'page-','')
            if($p.page -ne $local){throw 'Worker page reference mismatch'}
            $pages+=@{physical=$job.chunk.first+$local-1;source=$p.source;status=$p.status;text=$p.text}
        }
    }
    $target=$pages|Where-Object{$_.physical -eq $TargetPage}
    $ocr=@($pages|Where-Object{$_.source -eq 'ocr'}).Count
    if($pages.Count -ne $totalPages -or @($pages.physical|Sort-Object -Unique).Count -ne $totalPages -or ($TargetPage -and $target.text -notmatch $TargetTerm) -or $state.peak_active -ne $count){throw 'Physical page/OCR/concurrency assertion failed'}
    foreach($c in $queue){if((Get-FileHash $c.file).Hash.ToLowerInvariant() -ne $c.sha256){throw 'Chunk mutated'}}
    $samples=@(Get-Content "$run/ui.jsonl"|ForEach-Object{$_|ConvertFrom-Json})
    if($samples.Count -lt 3){throw 'Insufficient UI responsiveness samples'}
    $sorted=@($samples.ui_search_ms|Sort-Object)
    $walls=@($jobs|ForEach-Object{$_.completed_s-$_.submitted_s})
    JSON "$run/summary.json" @{passed=$true;concurrency=$count;peak_active=$state.peak_active;chunks=$queue.Count;pages=$pages.Count;ocr_pages=$ocr;target_page=$TargetPage;elapsed_seconds=$elapsed;pages_per_second=$pages.Count/$elapsed;ocr_pages_per_hour=3600*$ocr/$elapsed;job_wall_seconds=$walls;job_wall_mean=($walls|Measure-Object -Average).Average;sampled_peak_node_working_set=$state.peak_ram;sampled_peak_node_private=$state.peak_private;idle_node_working_set=$state.idle_node_working_set;min_host_free_bytes=1024*$state.min_free_kib;host_cpu_mean_percent=($state.cpu|Measure-Object -Average).Average;host_cpu_max_percent=($state.cpu|Measure-Object -Maximum).Maximum;ui_samples=$samples.Count;ui_search_median_ms=$sorted[[int][Math]::Floor(($sorted.Count-1)/2)];ui_search_p95_ms=$sorted[[int][Math]::Floor(0.95*($sorted.Count-1))];ui_search_max_ms=$sorted[-1];scheduler_delay_max_ms=($samples|Measure-Object scheduler_delay_ms -Maximum).Maximum;note='Same verified chunks, one whole chunk per job, CPU=1 per job, warm cache, sampled every ~1 s (peaks are lower bounds); UI is a completed local index in headless Chrome; no inference.'}
    Event 'summary' (Get-Content -Raw "$run/summary.json"|ConvertFrom-Json)
    Stop-Process -InputObject $node;if(!$node.WaitForExit(10000)){throw 'Concluded sweep node did not exit'}
}
Stop-Process -InputObject $ui -ErrorAction SilentlyContinue
Get-ChildItem "$Results/jobs-*/summary.json"|ForEach-Object{Get-Content -Raw $_.FullName}
