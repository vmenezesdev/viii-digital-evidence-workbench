param(
    [ValidateSet('wasi','docker','switch')][string]$Mode='wasi',
    [string]$Package='bin/pdf-mvp',
    [string]$PDF='.poc-runtime/pdf/fixtures/small.pdf',
    [string]$Results='.poc-runtime/pdf/product-validation',
    [switch]$LeaveRunning
)
$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot)
if (Test-Path $Results) { throw 'Choose a fresh results directory' }
$Results=(New-Item -ItemType Directory $Results).FullName
$Package=(Resolve-Path $Package).Path; $PDF=(Resolve-Path $PDF).Path
$data=(New-Item -ItemType Directory "$Results/data").FullName
$before=(Get-FileHash $PDF).Hash.ToLowerInvariant()
$app=$null; $inference=$null; $base='http://127.0.0.1:49081'
function Start-App {
    $script:app=Start-Process "$Package/VIII.exe" -ArgumentList "--data-dir `"$data`" --http 127.0.0.1:49081" -WindowStyle Hidden -PassThru -RedirectStandardOutput "$Results/app.stdout.log" -RedirectStandardError "$Results/app.stderr.log"
    [IO.File]::WriteAllText("$Results/app-owned.json",(@{pid=$app.Id;started=$app.StartTime.ToUniversalTime().ToString('o');data=$data} | ConvertTo-Json))
    $deadline=[DateTime]::UtcNow.AddSeconds(20)
    do {try { Invoke-RestMethod "$base/api/pdf/status" -TimeoutSec 2 | Out-Null;return } catch {}; Start-Sleep -Milliseconds 200 } until ([DateTime]::UtcNow -gt $deadline)
    throw 'Product did not start'
}
function Save-Event($Kind,$Value) { [IO.File]::AppendAllText("$Results/events.jsonl",((@{at=[DateTime]::UtcNow.ToString('o');kind=$Kind;value=$Value}|ConvertTo-Json -Depth 15 -Compress)+[Environment]::NewLine)) }
try {
    foreach ($port in 49081,48081) { $listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,$port);$listener.ExclusiveAddressUse=$true;try{$listener.Start()}finally{$listener.Stop()} }
    $model=(Resolve-Path .poc-runtime/pdf/inference/qwen.gguf).Path
    $server=(Resolve-Path .poc-runtime/pdf/inference/llama/llama-server.exe).Path
    $inference=Start-Process $server -ArgumentList "-m `"$model`" --host 127.0.0.1 --port 48081 --alias local --ctx-size 4096 --threads 4 --parallel 1 --n-gpu-layers 0" -WindowStyle Hidden -PassThru -RedirectStandardOutput "$Results/inference.stdout.log" -RedirectStandardError "$Results/inference.stderr.log"
    Start-App
    $watch=[Diagnostics.Stopwatch]::StartNew()
    Invoke-RestMethod "$base/api/pdf/import?name=small.pdf" -Method Post -ContentType application/pdf -InFile $PDF | Out-Null
    if ($Mode -eq 'docker') {Invoke-RestMethod "$base/api/pdf/engine?engine=docker" -Method Post -TimeoutSec 65 | Out-Null}
    $firstSearch=$null; $switched=$false; $restarted=$false; $confirmed=@{}; $liveJob=$null
    $deadline=[DateTime]::UtcNow.AddMinutes(15)
    do {
        $status=Invoke-RestMethod "$base/api/pdf/status" -TimeoutSec 10
        Save-Event 'status' $status
        if ($status.searchable -gt 0 -and $null -eq $firstSearch) {
            $hits=Invoke-RestMethod "$base/api/pdf/search?q=ARARA" -TimeoutSec 5
            if (@($hits).Count -gt 0) { $firstSearch=$watch.Elapsed.TotalMilliseconds;Save-Event 'first-useful-search' @{elapsed_ms=$firstSearch;hits=$hits;pending=$status.pending} }
        }
        if ($Mode -eq 'switch' -and !$restarted -and $status.document.phase -eq 'ocr' -and $status.document.attempt) {
            $ports=Get-Content -Raw "$data/runtime/ports.json" | ConvertFrom-Json
            $attempt=$status.document.attempt
            $record="$data/$before/$attempt.json"
            $revision=Get-ChildItem "$data/$before/$attempt" -Filter '*.json' -ErrorAction SilentlyContinue | Sort-Object Name | Select-Object -Last 1
            if ($revision) {$record=$revision.FullName}
            if (Test-Path $record) {
                $a=Get-Content -Raw $record | ConvertFrom-Json
                if ($a.job_id) {
                    $description=Invoke-RestMethod "http://127.0.0.1:$($ports.API)/api/v1/orchestrator/jobs/$($a.job_id)?include=executions"
                    if ($description.Job.State.StateType -eq 'Running') {
                        $liveJob=$a.job_id
                        Get-ChildItem "$data/$before/pages" -Filter '*.json' | ForEach-Object {$confirmed[$_.Name]=(Get-FileHash $_.FullName).Hash}
                        Save-Event 'interrupt-live-job' @{job=$liveJob;confirmed=$confirmed}
                        Stop-Process -Id $app.Id; $app.WaitForExit(); Start-App
                        $reopened=Invoke-RestMethod "$base/api/pdf/status"
                        if ($reopened.document.attempt -ne $attempt) {throw 'Reopening replaced an active attempt'}
                        Save-Event 'reopened-same-attempt' $reopened
                        $restarted=$true
                    }
                }
            }
        }
        if ($Mode -eq 'switch' -and $restarted -and !$switched) {
            Invoke-RestMethod "$base/api/pdf/engine?engine=docker" -Method Post -TimeoutSec 65 | Out-Null
            $switched=$true; Save-Event 'requested-docker' @{previous_job=$liveJob}
        }
        if ($status.document.paused) {throw "Product paused: $($status.document.diagnostic)"}
        if ($status.document.complete) {break}
        Start-Sleep -Seconds 1
    } until ([DateTime]::UtcNow -ge $deadline)
    if (!$status.document.complete -or $status.searchable -ne 11 -or $status.failed -ne 1 -or $status.pending -ne 0) {throw 'Product progress/finish assertion failed'}
    if ($Mode -eq 'switch' -and (!$switched -or !$restarted)) {throw 'Live restart/switch was not exercised'}
    $hits=Invoke-RestMethod "$base/api/pdf/search?q=JABUTICABA"
    $hits=@($hits)
    if ($hits.Count -ne 1 -or $hits[0].page -ne 11 -or $hits[0].url -notmatch '#page=11$') {throw 'Physical citation/search assertion failed'}
    Invoke-WebRequest "$base/api/pdf/original?evidence=$before" -OutFile "$Results/served-original.pdf" -UseBasicParsing | Out-Null
    if ((Get-FileHash "$Results/served-original.pdf").Hash.ToLowerInvariant() -ne $before -or (Get-FileHash $PDF).Hash.ToLowerInvariant() -ne $before) {throw 'Original hash changed'}
    $answer=Invoke-RestMethod "$base/api/pdf/ask" -Method Post -ContentType application/json -Body ([Text.Encoding]::UTF8.GetBytes((@{query='JABUTICABA';question='Qual a data prevista para pagamento?'}|ConvertTo-Json))) -TimeoutSec 120
    if (@($answer.sources).Count -ne 1 -or $answer.sources[0].page -ne 11 -or $answer.answer -notmatch '15') {throw 'Product real-model answer failed'}
    [IO.File]::WriteAllText("$Results/answer.json",($answer|ConvertTo-Json -Depth 10))
    foreach ($name in $confirmed.Keys) {if ((Get-FileHash "$data/$before/pages/$name").Hash -ne $confirmed[$name]) {throw 'Previously accepted page was overwritten'}}
    if (@(Get-ChildItem "$data/$before/pages" -Filter '*.json').Count -ne 12) {throw 'Duplicate/missing physical pages'}
    $summary=@{passed=$true;mode=$Mode;sha256=$before;first_useful_search_ms=$firstSearch;elapsed_ms=$watch.Elapsed.TotalMilliseconds;restarted=$restarted;switched=$switched;searchable=$status.searchable;failed=$status.failed;physical_page=11}
    [IO.File]::WriteAllText("$Results/summary.json",($summary|ConvertTo-Json));$summary|ConvertTo-Json
} finally {
    if (!$LeaveRunning) {
        if ($app -and !$app.HasExited) {Stop-Process -Id $app.Id}
        if ($inference -and !$inference.HasExited) {Stop-Process -Id $inference.Id}
        $owned="$data/runtime/owned.json"
        if (Test-Path $owned) {$owner=Get-Content -Raw $owned | ConvertFrom-Json; $node=Get-Process -Id $owner.pid -ErrorAction SilentlyContinue;if ($node -and $node.Path -eq (Join-Path $Package 'runtime/bacalhau.exe') -and [Math]::Abs(($node.StartTime.ToUniversalTime()-([DateTime]$owner.started).ToUniversalTime()).TotalSeconds) -lt 1) {Stop-Process -InputObject $node}}
        if (Test-Path "$data/runtime/docker-owned.json") {
            $dockerOwner=Get-Content -Raw "$data/runtime/docker-owned.json"|ConvertFrom-Json
            if ($dockerOwner.container -match '^viii-pdf-[a-f0-9]{12}-compute$' -and $dockerOwner.volume -match '^viii-pdf-[a-f0-9]{12}-data$') {
                docker logs $dockerOwner.container > "$Results/docker-compute.log" 2>&1
                docker rm -f $dockerOwner.container | Out-Null
                docker volume rm $dockerOwner.volume | Out-Null
            }
        }
    }
}
