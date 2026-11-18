param(
    [string]$Package='bin/pdf-mvp',
    [string]$PDF='.poc-runtime/pdf/fixtures/stream-small.pdf',
    [string]$Results=(".poc-runtime/pdf/cli lifecycle-"+[DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss')),
    [int]$Port=49183
)
$ErrorActionPreference='Stop'
Import-Module (Join-Path $PSHOME 'Modules/Microsoft.PowerShell.Utility/Microsoft.PowerShell.Utility.psd1') -Force
Set-Location (Split-Path $PSScriptRoot)
$Results=[IO.Path]::GetFullPath($Results)
$Package=(Resolve-Path $Package).Path;$PDF=(Resolve-Path $PDF).Path
$cli=Join-Path $PSScriptRoot 'pdf-experiment.ps1'
function Assert($Condition,$Message){if(!$Condition){throw $Message}}
function Command($Action,$Extra=@()){
    $lines=& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $cli $Action -Results $Results @Extra
    if($LASTEXITCODE){throw "CLI failed: $Action"}
    return (($lines -join [Environment]::NewLine)|ConvertFrom-Json)
}
function JSON($Path){Get-Content -LiteralPath $Path -Raw|ConvertFrom-Json}
$complete=$false
try{
    Assert (!(Test-Path $Results)) 'Choose a fresh test results directory'
    $hash=(Get-FileHash $PDF).Hash
    $launch=[Diagnostics.Stopwatch]::StartNew()
    $start=Command 'start' @('-Package',$Package,'-PDF',$PDF,'-Port',$Port)
    Assert ($launch.Elapsed.TotalSeconds -lt 15) 'Background start did not return promptly'
    $deadline=[DateTime]::UtcNow.AddMinutes(3)
    do{
        $snapshot=Command 'status'
        $status=$snapshot.progress.status
        if($status.document.phase -eq 'ocr' -and $status.document.attempt -and $status.searchable -gt 0){
            $ports=JSON "$Results/data/runtime/ports.json"
            $job=Invoke-RestMethod "http://127.0.0.1:$($ports.API)/api/v1/orchestrator/jobs/viii-$($status.document.attempt)?include=executions" -TimeoutSec 10
            if($job.Job.State.StateType -eq 'Running'){break}
        }
        Assert ($snapshot.control.state -ne 'failed') 'Detached collector failed; inspect runner logs'
        Start-Sleep -Seconds 1
    }until([DateTime]::UtcNow -gt $deadline)
    Assert ($job.Job.State.StateType -eq 'Running') 'No live OCR job observed'
    $accepted=@{}
    Get-ChildItem "$Results/data/$($status.document.evidence_id)/pages" -Filter '*.json'|ForEach-Object{$accepted[$_.Name]=(Get-FileHash $_.FullName).Hash}
    $paused=Command 'pause'
    Assert $paused.completed 'Pause did not finish'
    $terminal=JSON "$Results/pause-job.json"
    Assert ($terminal.Job.State.StateType -in 'Stopped','Completed') 'Pause did not resolve actual job'
    Assert (@($terminal.Executions.Items|Where-Object{$_.ComputeState.StateType -notin 9,10,11}).Count -eq 0) 'Execution still active after pause'
    $offline=Command 'status'
    Assert (!$offline.live -and !$offline.app_running -and !$offline.collector_running -and !$offline.runtime_running) 'Pause left owned processes running'
    $again=Command 'pause'
    Assert ($again.id -eq $paused.id) 'Repeated pause created another interval'
    $checkpoint=Command 'results' @('-Output',"$Results/checkpoint.json")
    Assert ($checkpoint.scope -eq 'partial' -and !$checkpoint.summary -and $checkpoint.progress.status.pending -gt 0) 'Partial results were presented as complete'
    Assert (Test-Path "$Results/checkpoint.json") 'Partial result export missing'
    Command 'resume'|Out-Null
    $deadline=[DateTime]::UtcNow.AddMinutes(2)
    do{
        $snapshot=Command 'status'
        if($snapshot.live -and $snapshot.progress.status.document.attempt -and !$snapshot.progress.status.document.paused){break}
        Assert ($snapshot.control.state -ne 'failed') 'Resume failed; inspect runner logs'
        Start-Sleep -Seconds 1
    }until([DateTime]::UtcNow -gt $deadline)
    Assert $snapshot.live 'Resumed product unreachable'
    $state=JSON "$Results/state.json"
    Assert ($state.user_pause_ms -gt 0 -and $state.last_accounted_pause -eq $paused.id) 'Pause was not accounted'
    $pauseMs=$state.user_pause_ms
    $duplicate=Command 'resume'
    Assert ($duplicate.state -eq 'already-running') 'Duplicate resume started another collector'
    Assert ((JSON "$Results/state.json").user_pause_ms -eq $pauseMs) 'Pause counted twice'
    foreach($name in $accepted.Keys){Assert ((Get-FileHash "$Results/data/$($status.document.evidence_id)/pages/$name").Hash -eq $accepted[$name]) 'Accepted page changed'}
    $deadline=[DateTime]::UtcNow.AddMinutes(5)
    do{
        $snapshot=Command 'status'
        Assert ($snapshot.control.state -ne 'failed') 'Collector failed before final summary'
        if($snapshot.control.state -eq 'completed'){break}
        Start-Sleep -Seconds 2
    }until([DateTime]::UtcNow -gt $deadline)
    Assert ($snapshot.control.state -eq 'completed') 'Experiment did not finish'
    $summary=Command 'results' @('-Output',"$Results/exported-summary.json")
    Assert ($summary.passed -and $summary.final_status.pending -eq 0) 'Final summary did not pass'
    Assert (!$snapshot.live -and !$snapshot.app_running -and !$snapshot.runtime_running) 'Completed experiment retained owned processes'
    Assert ((Get-FileHash $PDF).Hash -eq $hash) 'Original fixture changed'
    $complete=$true
    $record=@{passed=$true;results=$Results;detached_runner_pid=$start.pid;commands_use_separate_shells=$true;pause_id=$paused.id;confirmed_hashes_preserved=$accepted.Count;user_pause_ms=$summary.user_pause_ms;summary=$summary}
    [IO.File]::WriteAllText("$Results/cli-test.json",($record|ConvertTo-Json -Depth 20))
    $record|ConvertTo-Json -Depth 3
}finally{
    if(!$complete -and (Test-Path "$Results/experiment.json")){try{Command 'pause'|Out-Null}catch{Write-Warning 'Test failed; inspect saved runtime/cancellation state before retrying'}}
}
