param(
    [string]$Proof = '.poc-runtime/pdf/pages-ocr-proof-01',
    [string]$Results = '.poc-runtime/pdf/answer-proof',
    [string]$Server = '.poc-runtime/pdf/inference/llama/llama-server.exe',
    [string]$Model = '.poc-runtime/pdf/inference/qwen.gguf'
)
$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot)
if (Test-Path $Results) { throw 'Choose a new results directory' }
$Results=(New-Item -ItemType Directory $Results).FullName
$Proof=(Resolve-Path $Proof).Path; $Server=(Resolve-Path $Server).Path; $Model=(Resolve-Path $Model).Path
$listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,48081)
$listener.ExclusiveAddressUse=$true
try { $listener.Start() } finally { $listener.Stop() }
$process=$null; $oldProof=$env:VIII_PDF_PAGES_PROOF; $oldInference=$env:VIII_PDF_INFERENCE
try {
    $process=Start-Process $Server -ArgumentList "-m `"$Model`" --host 127.0.0.1 --port 48081 --alias local --ctx-size 4096 --threads 4 --parallel 1 --n-gpu-layers 0" -WindowStyle Hidden -PassThru -RedirectStandardOutput "$Results/server.stdout.log" -RedirectStandardError "$Results/server.stderr.log"
    [IO.File]::WriteAllText("$Results/owned.json",(@{pid=$process.Id;started=$process.StartTime.ToUniversalTime().ToString('o');executable=$Server} | ConvertTo-Json))
    $ready=$false; $deadline=[DateTime]::UtcNow.AddSeconds(45)
    do {
        if ($process.HasExited) { throw 'llama-server exited; inspect server.stderr.log' }
        try { $health=Invoke-RestMethod http://127.0.0.1:48081/health -TimeoutSec 2; if ($health.status -eq 'ok') { $ready=$true; break } } catch {}
        Start-Sleep -Milliseconds 250
    } until ([DateTime]::UtcNow -ge $deadline)
    if (!$ready) { throw 'Local model did not become ready' }
    $env:VIII_PDF_PAGES_PROOF=$Proof
    $env:VIII_PDF_INFERENCE='http://127.0.0.1:48081'
    $env:GOCACHE=Join-Path $env:TEMP 'viii-go-cache'
    & go test ./cmd/desktop -run '^TestPDFMixedArtifacts$' -count=1 -v
    if ($LASTEXITCODE) { throw 'Real worker artifact ingestion failed' }
    & go test ./cmd/desktop -run '^TestPDFRealLocalAnswer$' -count=1 -v
    if ($LASTEXITCODE) { throw 'Real local-model question failed' }
    Copy-Item -LiteralPath "$Proof/answer.json" -Destination "$Results/answer.json"
    $summary=@{passed=$true;backend='llama.cpp';model_sha256=(Get-FileHash $Model).Hash;server_sha256=(Get-FileHash $Server).Hash;source_proof=$Proof;model_file=(Split-Path $Model -Leaf);physical_page=11}
    [IO.File]::WriteAllText("$Results/summary.json",($summary | ConvertTo-Json))
    $summary | ConvertTo-Json
} finally {
    $env:VIII_PDF_PAGES_PROOF=$oldProof; $env:VIII_PDF_INFERENCE=$oldInference
    if ($process -and !$process.HasExited) { Stop-Process -Id $process.Id }
}
