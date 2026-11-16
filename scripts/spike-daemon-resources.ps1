param([Parameter(Mandatory)][string]$Root)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
$probe = 'p=$(pidof dockerd); set -- $p; [ "$#" -eq 1 ] || exit 1; printf "%s " "$p"; getconf CLK_TCK; cut -d " " -f 14,15 /proc/$p/stat; grep "^VmRSS:" /proc/$p/status #'
$output = [IO.StreamWriter]::new((Join-Path $Root 'docker-daemon.jsonl'), $false)
$output.AutoFlush = $true
try {
    while (!(Test-Path -LiteralPath (Join-Path $Root 'stop-resource-sampling'))) {
        $at = [DateTime]::UtcNow.ToString('o')
        $values = @($probe | wsl.exe -d docker-desktop -u root -- sh -s 2>$null)
        $fields = ($values -join ' ') -split '\s+'
        if ($LASTEXITCODE -ne 0 -or $fields.Count -ne 7 -or $fields[4] -ne 'VmRSS:') {
            $output.WriteLine((@{at=$at; error='dockerd process counters unavailable'} | ConvertTo-Json -Compress))
            break
        }
        $cpuSeconds = ([double]$fields[2] + [double]$fields[3]) / [double]$fields[1]
        $output.WriteLine((@{at=$at; pid=[int]$fields[0]; cpu_seconds=$cpuSeconds; rss_bytes=[int64]$fields[5] * 1024} | ConvertTo-Json -Compress))
        Start-Sleep -Milliseconds 1500
    }
} finally {
    $output.Dispose()
}
