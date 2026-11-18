param(
    [ValidateSet('wasi','docker')][string]$Engine='wasi',
    [string]$PDF='.poc-runtime/pdf/fixtures/mixed-30000.pdf',
    [string]$Package='bin/pdf-mvp',
    [string]$Results='.poc-runtime/pdf/benchmark-wasi-30000',
    [int]$Port=49181,
    [switch]$Resume,
    [switch]$ContinuePaused
)
$ErrorActionPreference='Stop'
Import-Module (Join-Path $PSHOME 'Modules/Microsoft.PowerShell.Utility/Microsoft.PowerShell.Utility.psd1') -Force
Set-Location (Split-Path $PSScriptRoot)
$PDF=(Resolve-Path $PDF).Path; $Package=(Resolve-Path $Package).Path
$exe=Join-Path $Package 'VIII.exe'
function Write-JSON($Path,$Value) {
    $temp="$Path.$PID.tmp"
    [IO.File]::WriteAllText($temp,($Value|ConvertTo-Json -Depth 20),[Text.UTF8Encoding]::new($false))
    for($n=0;;$n++){
        try{if([IO.File]::Exists($Path)){[IO.File]::Replace($temp,$Path,[NullString]::Value)}else{[IO.File]::Move($temp,$Path)};return}
        catch{if($n -ge 5){throw};Start-Sleep -Milliseconds (10*[Math]::Pow(2,$n))}
    }
}
function Event($Kind,$Value) { [IO.File]::AppendAllText("$Results/events.jsonl",((@{at=[DateTime]::UtcNow.ToString('o');kind=$Kind;value=$Value}|ConvertTo-Json -Depth 20 -Compress)+[Environment]::NewLine)) }
function Tree-Bytes($Path) {
    [long]$bytes=0
    if (Test-Path $Path) { foreach($file in [IO.Directory]::EnumerateFiles($Path,'*',[IO.SearchOption]::AllDirectories)) {try{$bytes+=([IO.FileInfo]$file).Length}catch{}} }
    return $bytes
}
function Owned-Process($Owner,$Path) {
    $p=Get-Process -Id $Owner.pid -ErrorAction SilentlyContinue
    if ($p -and $p.Path -eq $Path -and [Math]::Abs(($p.StartTime.ToUniversalTime()-([DateTime]$Owner.started).ToUniversalTime()).TotalSeconds) -lt 1) {return $p}
    return $null
}
if ($Resume) {
    $Results=(Resolve-Path $Results).Path
    $state=Get-Content -Raw "$Results/state.json" | ConvertFrom-Json
    if ($state.engine -ne $Engine -or $state.port -ne $Port) {throw 'Resume arguments differ from saved benchmark'}
    if ($state.pdf -ne $PDF) {
        if ((Get-FileHash $PDF).Hash.ToLowerInvariant() -ne $state.sha256 -or (Get-Item $PDF).Length -ne $state.manifest.bytes) {throw 'Relocated fixture identity mismatch'}
        Event 'source-relocated' @{previous=$state.pdf;current=$PDF;sha256=$state.sha256}
        $state.pdf=$PDF
        Write-JSON "$Results/state.json" $state
    }
    if (Test-Path "$Results/summary.json") {throw 'Benchmark already finished'}
} else {
    if((Test-Path "$Results/state.json") -or (Test-Path "$Results/data")){throw 'Choose a fresh results directory or use -Resume'}
    $Results=(New-Item -ItemType Directory $Results -Force).FullName
    $manifest=Get-Content -Raw ([IO.Path]::ChangeExtension($PDF,'.json')) | ConvertFrom-Json
    $hash=(Get-FileHash $PDF).Hash.ToLowerInvariant()
    if($hash -ne $manifest.sha256 -or (Get-Item $PDF).Length -ne $manifest.bytes){throw 'Fixture identity mismatch'}
    New-Item -ItemType Directory "$Results/data" | Out-Null
    $state=[pscustomobject]@{engine=$Engine;pdf=$PDF;port=$Port;started=[DateTime]::UtcNow.ToString('o');sha256=$hash;manifest=$manifest;import_started=$null;first_search_ms=$null;ocr_started=$null;peak_windows_working_set=0L;peak_windows_private_bytes=0L;peak_temporary_bytes=0L}
    Write-JSON "$Results/state.json" $state
    Write-JSON "$Results/machine.json" @{os=Get-CimInstance Win32_OperatingSystem|Select-Object Caption,Version,TotalVisibleMemorySize;cpu=Get-CimInstance Win32_Processor|Select-Object Name,NumberOfCores,NumberOfLogicalProcessors;disk=Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='C:'"|Select-Object Size,FreeSpace;package_sha256=(Get-FileHash $exe).Hash}
}
$data=Join-Path $Results 'data';$base="http://127.0.0.1:$Port"
$interrupted=if(Test-Path "$data/pdf-document.json"){(Get-Content -Raw "$data/pdf-document.json"|ConvertFrom-Json).attempt}else{$null}
$app=$null
if(Test-Path "$Results/app-owned.json") {$app=Owned-Process (Get-Content -Raw "$Results/app-owned.json"|ConvertFrom-Json) $exe}
if(!$app){
    $listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,$Port);$listener.ExclusiveAddressUse=$true;try{$listener.Start()}finally{$listener.Stop()}
    $suffix=[DateTime]::UtcNow.ToString('yyyyMMddHHmmss')
    $app=Start-Process $exe -ArgumentList "--data-dir `"$data`" --http 127.0.0.1:$Port" -WindowStyle Hidden -PassThru -RedirectStandardOutput "$Results/app-$suffix.stdout.log" -RedirectStandardError "$Results/app-$suffix.stderr.log"
    Write-JSON "$Results/app-owned.json" @{pid=$app.Id;started=$app.StartTime.ToUniversalTime().ToString('o')}
    Event 'app-start' @{pid=$app.Id;resume=[bool]$Resume}
}
$deadline=[DateTime]::UtcNow.AddSeconds(30)
do {try{$status=Invoke-RestMethod "$base/api/pdf/status" -TimeoutSec 2;break}catch{Start-Sleep -Milliseconds 250}} until([DateTime]::UtcNow -gt $deadline)
if([DateTime]::UtcNow -gt $deadline){throw 'App did not become reachable; owned process left running for inspection'}
if(!$status -or !$status.document){
    $state | Add-Member -NotePropertyName import_started -NotePropertyValue ([DateTime]::UtcNow.ToString('o')) -Force
    Write-JSON "$Results/state.json" $state
    Event 'upload-start' @{bytes=$state.manifest.bytes}
    & curl.exe --silent --show-error --fail -X POST -H 'Content-Type: application/pdf' --upload-file $PDF "$base/api/pdf/import?name=$([IO.Path]::GetFileName($PDF))" > "$Results/import.log" 2>&1
    if($LASTEXITCODE){throw 'Import failed; see import.log'}
    Event 'upload-finished' @{}
    $status=Invoke-RestMethod "$base/api/pdf/status" -TimeoutSec 10
}
if($status.document.sha256 -ne $state.sha256){throw 'Imported wrong evidence'}
if($ContinuePaused){
    $deadline=[DateTime]::UtcNow.AddSeconds(60)
    do{
        if($status.document.paused -and !$status.document.attempt){Invoke-RestMethod "$base/api/pdf/resume" -Method Post -TimeoutSec 10|Out-Null;break}
        if($status.document.complete -or (!$status.document.paused -and (!$status.document.attempt -or $status.document.attempt -ne $interrupted))){break}
        if(!$status.document.paused -and $status.document.attempt -eq $interrupted){
            try{
                $ports=Get-Content -Raw "$data/runtime/ports.json"|ConvertFrom-Json
                $job=Invoke-RestMethod "http://127.0.0.1:$($ports.API)/api/v1/orchestrator/jobs/viii-${interrupted}?include=executions" -TimeoutSec 5
                if($job.Job.State.StateType -notin 'Stopped','Completed','Failed','Cancelled','Canceled'){break}
            }catch{}
        }
        Start-Sleep -Milliseconds 250
        $status=Invoke-RestMethod "$base/api/pdf/status" -TimeoutSec 10
    }until([DateTime]::UtcNow -gt $deadline)
    if([DateTime]::UtcNow -gt $deadline){throw 'Stopped attempt not reconciled; inspect existing runtime before retrying'}
}
if($Engine -eq 'docker' -and $status.document.desired_engine -ne 'docker'){
    Invoke-RestMethod "$base/api/pdf/engine?engine=docker" -Method Post -TimeoutSec 65|Out-Null
    Event 'docker-requested' @{}
}
Event 'monitor-start' @{resume=[bool]$Resume;sampling_seconds=5;disk_sampling_seconds=30;note='Processes remain running on monitor failure. Resume preserves the original clock and logs telemetry gaps.'}
$lastDisk=[DateTime]::MinValue
$consecutiveErrors=0
while($true){
    $elapsed=([DateTime]::UtcNow-([DateTime]$state.started).ToUniversalTime()).TotalMilliseconds
    try {
        $timer=[Diagnostics.Stopwatch]::StartNew()
        $status=Invoke-RestMethod "$base/api/pdf/status" -TimeoutSec 15
        $latency=$timer.Elapsed.TotalMilliseconds
        $query=[Diagnostics.Stopwatch]::StartNew();$hits=Invoke-RestMethod "$base/api/pdf/search?q=ARARA" -TimeoutSec 15;$hits=@($hits);$queryMs=$query.Elapsed.TotalMilliseconds
        if($null -eq $state.first_search_ms -and $status.document.pages -eq $state.manifest.pages -and $hits.Count -gt 0 -and $hits[0].page -ge 1 -and $hits[0].url -match '#page=\d+$'){
            $state.first_search_ms=$elapsed
            $fromImport=if($state.import_started){([DateTime]::UtcNow-([DateTime]$state.import_started).ToUniversalTime()).TotalMilliseconds}else{$null}
            Event 'first-useful-search' @{elapsed_ms=$elapsed;from_import_ms=$fromImport;query_ms=$queryMs;pending=$status.pending;hits=@($hits|Select-Object page,url)}
        }
        if(!$state.ocr_started -and $status.document.phase -eq 'ocr'){$state.ocr_started=[DateTime]::UtcNow.ToString('o')}
        $processes=@($app)
        if(Test-Path "$data/runtime/owned.json"){$owner=Get-Content -Raw "$data/runtime/owned.json"|ConvertFrom-Json;$node=Owned-Process $owner (Join-Path $Package 'runtime/bacalhau.exe');if($node){$processes+=$node}}
        [long]$working=0;[long]$private=0;$metrics=@()
        foreach($p in $processes){$p.Refresh();$working+=$p.WorkingSet64;$private+=$p.PrivateMemorySize64;$metrics+=@{pid=$p.Id;working_set=$p.WorkingSet64;private_bytes=$p.PrivateMemorySize64;peak_working_set=$p.PeakWorkingSet64;cpu_seconds=$p.TotalProcessorTime.TotalSeconds}}
        $state.peak_windows_working_set=[Math]::Max([long]$state.peak_windows_working_set,$working)
        $state.peak_windows_private_bytes=[Math]::Max([long]$state.peak_windows_private_bytes,$private)
        $os=Get-CimInstance Win32_OperatingSystem
        Event 'sample' @{elapsed_ms=$elapsed;active_elapsed_ms=$elapsed-[double]$state.user_pause_ms;status=$status;status_latency_ms=$latency;search_latency_ms=$queryMs;search_hits_count=$hits.Count;search_first_page=$hits[0].page;windows_processes=$metrics;windows_working_set_sum=$working;windows_private_sum=$private;machine_free_memory_kib=$os.FreePhysicalMemory}
        if(([DateTime]::UtcNow-$lastDisk).TotalSeconds -ge 30){
            $temporary=(Tree-Bytes "$data/control")+(Tree-Bytes "$data/artifacts")+(Tree-Bytes "$data/runtime")
            $state.peak_temporary_bytes=[Math]::Max([long]$state.peak_temporary_bytes,$temporary)
            Event 'disk' @{windows_temporary_bytes=$temporary;accepted_pages_bytes=Tree-Bytes "$data/$($state.sha256)";original_bytes=Tree-Bytes "$data/originals";drive_free_bytes=(Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='C:'").FreeSpace}
            if($Engine -eq 'docker'){
                $stats=& docker stats --no-stream --format '{{json .}}' 2>&1
                Event 'docker-stats' @{exit=$LASTEXITCODE;lines=@($stats|ForEach-Object{"$_"})}
                if(Test-Path "$data/runtime/docker-owned.json"){
                    $owned=Get-Content -Raw "$data/runtime/docker-owned.json"|ConvertFrom-Json
                    $size=& docker run --rm --mount "type=volume,source=$($owned.volume),target=/data,readonly" alpine:3 du -sb /data 2>&1
                    Event 'docker-volume' @{exit=$LASTEXITCODE;lines=@($size|ForEach-Object{"$_"});includes_staged_original=$true}
                }
            }
            $lastDisk=[DateTime]::UtcNow
        }
        Write-JSON "$Results/state.json" $state
        Write-JSON "$Results/progress.json" @{at=[DateTime]::UtcNow.ToString('o');elapsed_ms=$elapsed;status=$status}
        if($status.document.paused){Event 'paused' $status;throw "Product paused: $($status.document.diagnostic)"}
        if($status.document.complete){break}
        $consecutiveErrors=0
    }catch{
        $consecutiveErrors++
        Event 'monitor-error' @{message=$_.Exception.Message;details=$_.ErrorDetails.Message;consecutive=$consecutiveErrors}
        if($status.document.paused -or $app.HasExited -or $consecutiveErrors -ge 5){throw}
    }
    Start-Sleep -Seconds 5
}
$finalHits=Invoke-RestMethod "$base/api/pdf/search?q=JABUTICABA" -TimeoutSec 30
$finalHits=@($finalHits)
if($status.document.pages -ne $state.manifest.pages -or $status.pending -ne 0 -or !($finalHits|Where-Object{$_.page -eq $state.manifest.target_page -and $_.url.EndsWith("#page=$($state.manifest.target_page)")})){throw 'Final page/search assertion failed'}
$original=Join-Path "$data/originals" "$($state.sha256).pdf"
if((Get-FileHash $PDF).Hash.ToLowerInvariant() -ne $state.sha256 -or (Get-FileHash $original).Hash.ToLowerInvariant() -ne $state.sha256){throw 'Original identity changed'}
$ocrWallSeconds=([DateTime]::UtcNow-([DateTime]$state.ocr_started).ToUniversalTime()).TotalSeconds
$ocrSeconds=$ocrWallSeconds-[double]$state.ocr_user_pause_ms/1000
if($ocrSeconds -le 0){throw 'Invalid OCR duration after subtracting recorded user pause'}
Write-JSON "$Results/summary.json" @{passed=$true;engine=$Engine;pages=$status.document.pages;bytes=$state.manifest.bytes;sha256=$state.sha256;ocr_pages=$status.ocr_processed;ocr_ratio=$state.manifest.ocr_ratio;first_useful_search_ms=$state.first_search_ms;elapsed_ms=$elapsed;user_pause_ms=[double]$state.user_pause_ms;active_elapsed_ms=$elapsed-[double]$state.user_pause_ms;ocr_phase_wall_seconds=$ocrWallSeconds;ocr_phase_seconds=$ocrSeconds;ocr_pages_per_second=$status.ocr_processed/$ocrSeconds;peak_windows_working_set_sum=$state.peak_windows_working_set;peak_windows_private_sum=$state.peak_windows_private_bytes;peak_windows_temporary_bytes=$state.peak_temporary_bytes;final_status=$status;target_hits=$finalHits;sampling='RAM 5s, disk/docker 30s. Sampled peaks are lower bounds; Windows processes exclude Docker VM. Docker stats and volume samples retained separately in events.jsonl. Active times exclude only explicitly recorded user pauses.'}
Get-Content "$Results/summary.json"
