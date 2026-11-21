param(
    [Parameter(Position=0,Mandatory=$true)][ValidateSet('start','resume','pause','status','logs','results','run')][string]$Action,
    [Parameter(Mandatory=$true)][string]$Results,
    [string]$Package='bin/pdf-mvp',
    [string]$PDF='.poc-runtime/pdf/fixtures/mixed-30000.pdf',
    [ValidateSet('wasi','docker')][string]$Engine='wasi',
    [ValidateRange(1,65535)][int]$Port=49181,
    # Simultaneous page jobs on chunks (VIII --pdf-jobs); 0 keeps the product default.
    [ValidateRange(0,16)][int]$Jobs=0,
    [string]$Output,
    [ValidateRange(1,1000)][int]$Tail=10
)
$ErrorActionPreference='Stop'
Import-Module (Join-Path $PSHOME 'Modules/Microsoft.PowerShell.Utility/Microsoft.PowerShell.Utility.psd1') -Force
Set-Location (Split-Path $PSScriptRoot)
$Results=[IO.Path]::GetFullPath($Results)
$shellExe=Join-Path $env:WINDIR 'System32/WindowsPowerShell/v1.0/powershell.exe'
function Read-JSON($Path){
    if(!(Test-Path -LiteralPath $Path)){return $null}
    for($n=0;;$n++){try{return (Get-Content -LiteralPath $Path -Raw|ConvertFrom-Json)}catch{if($n -ge 5){throw};Start-Sleep -Milliseconds 20}}
}
function Write-JSON($Path,$Value){
    $temp="$Path.$PID.tmp"
    [IO.File]::WriteAllText($temp,($Value|ConvertTo-Json -Depth 25),[Text.UTF8Encoding]::new($false))
    for($n=0;;$n++){
        try{if([IO.File]::Exists($Path)){[IO.File]::Replace($temp,$Path,[NullString]::Value)}else{[IO.File]::Move($temp,$Path)};return}
        catch{if($n -ge 5){throw};Start-Sleep -Milliseconds (10*[Math]::Pow(2,$n))}
    }
}
function Owned-Process($Record,$Exe){
    $owner=Read-JSON $Record
    if(!$owner){return}
    $p=Get-Process -Id $owner.pid -ErrorAction SilentlyContinue
    if($p -and $p.Path -eq [IO.Path]::GetFullPath($Exe) -and [Math]::Abs(($p.StartTime.ToUniversalTime()-([DateTime]$owner.started).ToUniversalTime()).TotalSeconds) -lt 1){return $p}
}
function Stop-Owned($Record,$Exe){
    $p=Owned-Process $Record $Exe
    if($p){Stop-Process -Id $p.Id -Force -ErrorAction Stop;$p.WaitForExit()}
}
function Event($Kind,$Value){[IO.File]::AppendAllText("$Results/commands.jsonl",((@{at=[DateTime]::UtcNow.ToString('o');kind=$Kind;value=$Value}|ConvertTo-Json -Depth 25 -Compress)+[Environment]::NewLine))}
function Close-Runtime {
    Stop-Owned "$Results/app-owned.json" "$($config.package)/VIII.exe"
    $docker=Read-JSON "$Results/data/runtime/docker-owned.json"
    if($docker){
        $owner=Read-JSON "$Results/data/runtime/owned.json"
        if($owner.node -notmatch '^viii-pdf-[a-f0-9]{12}-wasi$' -or $docker.container -ne ($owner.node -replace '-wasi$','-compute')){throw 'Docker ownership mismatch'}
        & docker stop --time 10 $docker.container | Out-Null
        if($LASTEXITCODE){throw 'Could not stop owned Docker compute container'}
    }
    Stop-Owned "$Results/data/runtime/owned.json" "$($config.package)/runtime/bacalhau.exe"
}
function Snapshot {
    $progress=Read-JSON "$Results/progress.json"
    $app=Owned-Process "$Results/app-owned.json" "$($config.package)/VIII.exe"
    $live=$false
    if($app){try{$status=Invoke-RestMethod "http://127.0.0.1:$($config.port)/api/pdf/status" -TimeoutSec 5;$progress=@{at=[DateTime]::UtcNow.ToString('o');status=$status};$live=$true}catch{}}
    $state=Read-JSON "$Results/state.json"
    if($progress -and $progress.status.document){$progress.status.document=$progress.status.document|Select-Object evidence_id,sha256,phase,attempt,first,last,paused,complete,diagnostic}
    $control=Read-JSON "$Results/runner-status.json"
    $paused=Read-JSON "$Results/experiment-pause.json"
    $monitor=Owned-Process "$Results/monitor-owned.json" $shellExe
    $runtime=Owned-Process "$Results/data/runtime/owned.json" "$($config.package)/runtime/bacalhau.exe"
    $summary=Read-JSON "$Results/summary.json"
    $scope=if($summary){'complete'}else{'partial'}
    return @{scope=$scope;results=$Results;live=$live;app_running=[bool]$app;collector_running=[bool]$monitor;runtime_running=[bool]$runtime;control=$control;pause=$paused;progress=$progress;summary=$summary;metrics=@{started=$state.started;first_search_ms=$state.first_search_ms;user_pause_ms=$state.user_pause_ms;peak_windows_working_set=$state.peak_windows_working_set;peak_windows_private_bytes=$state.peak_windows_private_bytes;peak_temporary_bytes=$state.peak_temporary_bytes};events="$Results/events.jsonl";commands="$Results/commands.jsonl"}
}
if(!(Test-Path -LiteralPath $Results)){
    if($Action -ne 'start'){throw 'Results directory does not exist; use start for a new experiment'}
    New-Item -ItemType Directory -Path $Results|Out-Null
}
$commandLock=$null;$runnerLock=$null
try{
    if($Action -in 'start','resume','pause'){$commandLock=[IO.File]::Open("$Results/command.lock",'OpenOrCreate','ReadWrite','None')}
    $config=Read-JSON "$Results/experiment.json"
    if(!$config){
        if($Action -notin 'start','resume'){throw 'Use resume -Package <existing bundle> once to adopt an existing benchmark'}
        $state=Read-JSON "$Results/state.json"
        if($state){$PDF=$state.pdf;$Port=$state.port;$Engine=$state.engine;if($state.jobs){$Jobs=[int]$state.jobs}}
        $config=@{package=(Resolve-Path $Package).Path;pdf=(Resolve-Path $PDF).Path;engine=$Engine;port=$Port;jobs=$Jobs}
        if(!(Test-Path "$($config.package)/VIII.exe")){throw 'Packaged VIII.exe is missing'}
        Write-JSON "$Results/experiment.json" $config
    }
    if($Action -in 'status','results'){
        $snapshot=Snapshot
        $value=if($Action -eq 'results' -and $snapshot.summary){$snapshot.summary}else{$snapshot}
        if($Output){Write-JSON ([IO.Path]::GetFullPath($Output)) $value}
        $value|ConvertTo-Json -Depth 25
        return
    }
    if($Action -eq 'logs'){
        foreach($name in 'runner-status.json','commands.jsonl','events.jsonl'){
            $path=Join-Path $Results $name
            if(Test-Path $path){Write-Output "--- $name";Get-Content -LiteralPath $path -Tail $Tail}
        }
        return
    }
    if($Action -in 'start','resume'){
        if(Test-Path "$Results/summary.json"){throw 'Experiment already finished; use results'}
        if($Action -eq 'start' -and (Test-Path "$Results/state.json")){throw 'Existing benchmark; use resume'}
        $pause=Read-JSON "$Results/experiment-pause.json"
        if($pause -and !$pause.completed){throw 'Pause cancellation is pending; run pause again before resume'}
        $monitor=Owned-Process "$Results/monitor-owned.json" $shellExe
        if($monitor){@{state='already-running';pid=$monitor.Id;results=$Results}|ConvertTo-Json;return}
        $probe=[IO.File]::Open("$Results/runner.lock",'OpenOrCreate','ReadWrite','None');$probe.Dispose()
        # ShellExecute avoids inheriting the caller's redirected pipe handles.
        $launchLog="$Results/launcher-$([DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss-ffff')).log"
        $bootstrap="& '"+$PSCommandPath.Replace("'","''")+"' run -Results '"+$Results.Replace("'","''")+"' *> '"+$launchLog.Replace("'","''")+"'"
        $encoded=[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($bootstrap))
        $process=Start-Process $shellExe -ArgumentList "-NoProfile -ExecutionPolicy Bypass -EncodedCommand $encoded" -Verb Open -WindowStyle Hidden -WorkingDirectory $PWD.Path -PassThru
        Write-JSON "$Results/monitor-owned.json" @{pid=$process.Id;started=$process.StartTime.ToUniversalTime().ToString('o');path=$shellExe}
        Event $Action @{pid=$process.Id}
        $deadline=[DateTime]::UtcNow.AddSeconds(10)
        do{
            $ready=Read-JSON "$Results/runner-status.json"
            $pause=Read-JSON "$Results/experiment-pause.json"
            if($ready.pid -eq $process.Id -and $ready.state -eq 'running' -and (!$pause -or $pause.accounted_at)){break}
            $process.Refresh()
            if($process.HasExited -or ($ready.pid -eq $process.Id -and $ready.state -eq 'failed')){throw "Runner failed to initialize; inspect $launchLog and runner-status.json"}
            Start-Sleep -Milliseconds 100
        }until([DateTime]::UtcNow -gt $deadline)
        if([DateTime]::UtcNow -gt $deadline){throw 'Runner initialization pending; inspect status before retrying'}
        @{state='starting';pid=$process.Id;results=$Results;logs=$launchLog}|ConvertTo-Json
        return
    }
    if($Action -eq 'pause'){
        $pause=Read-JSON "$Results/experiment-pause.json"
        if(!$pause -or $pause.accounted_at){$pause=[pscustomobject]@{id=[Guid]::NewGuid().ToString('n');requested_at=[DateTime]::UtcNow.ToString('o');completed=$false;stopped_at=$null;accounted_at=$null}}
        if($pause.completed){$pause|ConvertTo-Json;return}
        Write-JSON "$Results/experiment-pause.json" $pause
        Stop-Owned "$Results/monitor-owned.json" $shellExe
        Write-JSON "$Results/runner-status.json" @{state='pausing';at=[DateTime]::UtcNow.ToString('o')}
        $snapshot=Snapshot
        if($snapshot.progress){Write-JSON "$Results/progress.json" $snapshot.progress}
        # The product cancels the preparation and every page job and keeps
        # reconciling them; wait until none is active before closing it.
        if(Owned-Process "$Results/app-owned.json" "$($config.package)/VIII.exe"){
            $base="http://127.0.0.1:$([int]$config.port)"
            Invoke-RestMethod "$base/api/pdf/pause" -Method Post -TimeoutSec 10|Out-Null
            $deadline=[DateTime]::UtcNow.AddSeconds(60)
            do{
                Start-Sleep -Milliseconds 500
                $live=Invoke-RestMethod "$base/api/pdf/status" -TimeoutSec 10
                Write-JSON "$Results/pause-status.json" $live
            }until(([int]$live.active_jobs -eq 0 -and !$live.document.prepare) -or [DateTime]::UtcNow -gt $deadline)
            if([int]$live.active_jobs -ne 0 -or $live.document.prepare){throw 'Cancellation pending; run pause again, do not resume'}
        }
        Stop-Owned "$Results/app-owned.json" "$($config.package)/VIII.exe"
        $doc=Read-JSON "$Results/data/pdf-document.json"
        if($doc.attempt){
            if($doc.evidence_id -notmatch '^[a-f0-9]{64}$' -or $doc.attempt -notmatch '^[a-z0-9]+$'){throw 'Invalid persisted document identity'}
            $record="$Results/data/$($doc.evidence_id)/$($doc.attempt).json"
            $latest=Get-ChildItem "$Results/data/$($doc.evidence_id)/$($doc.attempt)" -Filter '*.json' -ErrorAction SilentlyContinue|Sort-Object Name|Select-Object -Last 1
            if($latest){$record=$latest.FullName}
            $attempt=Read-JSON $record
            if(!$attempt){throw 'Attempt record missing; cannot confirm cancellation'}
            $runtime=Owned-Process "$Results/data/runtime/owned.json" "$($config.package)/runtime/bacalhau.exe"
            $ports=Read-JSON "$Results/data/runtime/ports.json"
            $api="http://127.0.0.1:$([int]$ports.API)/api/v1/orchestrator/jobs"
            $key=if($attempt.job_id){$attempt.job_id}else{"viii-$($doc.attempt)"}
            $job=$null
            if($runtime){try{$job=Invoke-RestMethod "$api/${key}?include=executions" -TimeoutSec 10}catch{if([int]$_.Exception.Response.StatusCode -ne 404){throw}}}
            else{$job=Read-JSON "$Results/pause-job.json";if(!$job){throw 'Owned runtime unavailable; cannot confirm cancellation'}}
            if($job){
                if($job.Job.Name -ne "viii-$($doc.attempt)" -or @($job.Executions.Items|Where-Object{$_.NodeID -ne $attempt.requested_node_id}).Count){throw 'Job/execution identity mismatch'}
                if($attempt.job_id -and $job.Job.ID -ne $attempt.job_id){throw 'Job ID mismatch'}
                if($runtime){
                    if($job.Job.State.StateType -notin 'Stopped','Completed','Failed','Cancelled','Canceled'){Invoke-RestMethod "$api/$($job.Job.ID)" -Method Delete -TimeoutSec 10|Out-Null}
                    $deadline=[DateTime]::UtcNow.AddSeconds(30)
                    do{
                        $job=Invoke-RestMethod "$api/$($job.Job.ID)?include=executions" -TimeoutSec 10
                        Write-JSON "$Results/pause-job.json" $job
                        $live=@($job.Executions.Items|Where-Object{$_.ComputeState.StateType -notin 9,10,11})
                        if($job.Job.State.StateType -in 'Stopped','Completed','Failed','Cancelled','Canceled' -and !$live.Count){break}
                        Start-Sleep -Milliseconds 250
                    }until([DateTime]::UtcNow -gt $deadline)
                }
                $live=@($job.Executions.Items|Where-Object{$_.ComputeState.StateType -notin 9,10,11})
                if($live.Count -or $job.Job.State.StateType -notin 'Stopped','Completed','Failed','Cancelled','Canceled'){throw 'Cancellation pending; run pause again, do not resume'}
            }
        }
        Close-Runtime
        $pause.completed=$true;$pause.stopped_at=[DateTime]::UtcNow.ToString('o')
        Write-JSON "$Results/experiment-pause.json" $pause
        Write-JSON "$Results/runner-status.json" @{state='paused';at=$pause.stopped_at}
        Event 'paused' $pause
        $pause|ConvertTo-Json
        return
    }
    $runnerLock=[IO.File]::Open("$Results/runner.lock",'OpenOrCreate','ReadWrite','None')
    $self=Get-Process -Id $PID
    Write-JSON "$Results/monitor-owned.json" @{pid=$PID;started=$self.StartTime.ToUniversalTime().ToString('o');path=$self.Path}
    Write-JSON "$Results/runner-status.json" @{state='running';pid=$PID;at=[DateTime]::UtcNow.ToString('o')}
    $resume=Test-Path "$Results/state.json"
    $continuePaused=$resume -and (!(Owned-Process "$Results/app-owned.json" "$($config.package)/VIII.exe") -or (Read-JSON "$Results/data/pdf-document.json").paused)
    if($resume){
        $state=Read-JSON "$Results/state.json"
        $pause=Read-JSON "$Results/experiment-pause.json"
        if($pause -and $pause.completed -and !$pause.accounted_at){
            $now=[DateTime]::UtcNow;$from=([DateTime]$pause.stopped_at).ToUniversalTime()
            if($state.last_accounted_pause -ne $pause.id){
                $state|Add-Member -NotePropertyName user_pause_ms -NotePropertyValue ([double]$state.user_pause_ms+($now-$from).TotalMilliseconds) -Force
                $ocr=0.0
                if($state.ocr_started){$ocrFrom=([DateTime]$state.ocr_started).ToUniversalTime();if($ocrFrom -gt $from){$from=$ocrFrom};$ocr=[Math]::Max(0,($now-$from).TotalMilliseconds)}
                $state|Add-Member -NotePropertyName ocr_user_pause_ms -NotePropertyValue ([double]$state.ocr_user_pause_ms+$ocr) -Force
                $state|Add-Member -NotePropertyName last_accounted_pause -NotePropertyValue $pause.id -Force
                Write-JSON "$Results/state.json" $state
            }
            $pause.accounted_at=$now.ToString('o');Write-JSON "$Results/experiment-pause.json" $pause
            Event 'resumed' @{pause=$pause.id;user_pause_ms=$state.user_pause_ms}
        }
        foreach($path in $config.pdf,"$Results/data/originals/$($state.sha256).pdf"){
            if(Test-Path $path){if((Get-FileHash -LiteralPath $path).Hash.ToLowerInvariant() -ne $state.sha256){throw 'Original PDF identity mismatch'}}
            elseif($path -eq $config.pdf){throw 'Source PDF missing'}
        }
    }
    if(!$resume){
        $pause=Read-JSON "$Results/experiment-pause.json"
        if($pause -and $pause.completed -and !$pause.accounted_at){$pause.accounted_at=[DateTime]::UtcNow.ToString('o');Write-JSON "$Results/experiment-pause.json" $pause}
    }
    & "$PSScriptRoot/benchmark-pdf.ps1" -Engine $config.engine -PDF $config.pdf -Package $config.package -Results $Results -Port $config.port -Jobs ([int]$config.jobs) -Resume:$resume -ContinuePaused:$continuePaused > "$Results/runner-$PID.stdout.log" 2> "$Results/runner-$PID.stderr.log"
    if(!(Test-Path "$Results/summary.json")){throw 'Collector ended without a successful summary'}
    Close-Runtime
    Write-JSON "$Results/runner-status.json" @{state='completed';at=[DateTime]::UtcNow.ToString('o')}
    Event 'completed' @{summary="$Results/summary.json"}
}catch{
    if($Action -eq 'run'){Write-JSON "$Results/runner-status.json" @{state='failed';pid=$PID;at=[DateTime]::UtcNow.ToString('o');error=$_.Exception.Message};Event 'failed' @{message=$_.Exception.Message};[IO.File]::AppendAllText("$Results/runner-$PID.stderr.log",($_|Out-String))}
    throw
}finally{
    if($runnerLock){$runnerLock.Dispose()}
    if($commandLock){$commandLock.Dispose()}
}
