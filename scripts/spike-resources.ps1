param([string]$Root, [int[]]$OwnedIDs, [string]$Container, [string]$WorkerImage, [hashtable]$Roles)
$ErrorActionPreference = 'Stop'
$log = [IO.StreamWriter]::new((Join-Path $Root 'resources.jsonl'), $false)
$log.AutoFlush = $true
try {
    while (!(Test-Path -LiteralPath (Join-Path $Root 'stop-resource-sampling'))) {
        $at = [DateTime]::UtcNow.ToString('o')
        $native = @(Get-Process -Id $OwnedIDs -ErrorAction SilentlyContinue)
        $native += @(Get-Process -Name 'bench-worker' -ErrorAction SilentlyContinue | Where-Object {
            $_.Path -eq (Join-Path $Root 'bench-worker.exe')
        })
        if ($WorkerImage) {
            $native += @(Get-Process -Name 'com.docker.backend','vmmemWSL' -ErrorAction SilentlyContinue)
        }
        $windows = @($native | ForEach-Object {
            $processID = $_.Id
            @{
                id=$_.Id; name=$_.ProcessName; cpu_seconds=$_.CPU
                role=($Roles.Keys | Where-Object { $Roles[$_] -eq $processID } | Select-Object -First 1)
                working_set_bytes=$_.WorkingSet64; private_bytes=$_.PrivateMemorySize64
            }
        })
        $linux = $null
        $workers = @()
        if ($Container) {
            $workerIDs = @()
            if ($WorkerImage) {
                $workerIDs = @(docker ps --filter "ancestor=$WorkerImage" --format '{{.ID}}' 2>$null)
            }
            $stats = docker stats --no-stream --format '{{json .}}' $Container @workerIDs 2>$null
            if ($LASTEXITCODE -eq 0 -and $stats) {
                $rows = @($stats | ForEach-Object { $_ | ConvertFrom-Json })
                $linux = $rows | Where-Object Name -eq $Container
                $workers = @($rows | Where-Object Name -ne $Container)
            }
        }
        $log.WriteLine((@{at=$at; docker_observed_at=[DateTime]::UtcNow.ToString('o'); windows=$windows; docker=$linux; docker_workers=$workers} | ConvertTo-Json -Compress -Depth 6))
        Start-Sleep -Milliseconds 500
    }
} finally {
    $log.Dispose()
}
