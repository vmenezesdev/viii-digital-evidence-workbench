param([string]$Root, [int[]]$OwnedIDs, [string]$Container)
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
        $windows = @($native | ForEach-Object {
            @{
                id=$_.Id; name=$_.ProcessName; cpu_seconds=$_.CPU
                working_set_bytes=$_.WorkingSet64; private_bytes=$_.PrivateMemorySize64
            }
        })
        $linux = $null
        if ($Container) {
            $stats = docker stats --no-stream --format '{{json .}}' $Container 2>$null
            if ($LASTEXITCODE -eq 0 -and $stats) { $linux = $stats | ConvertFrom-Json }
        }
        $log.WriteLine((@{at=$at; docker_observed_at=[DateTime]::UtcNow.ToString('o'); windows=$windows; docker=$linux} | ConvertTo-Json -Compress -Depth 6))
        Start-Sleep -Milliseconds 500
    }
} finally {
    $log.Dispose()
}
