param(
    [string]$Bacalhau = 'C:\Bacalhau\bin\bacalhau.exe',
    [string]$Results = '.poc-runtime/pdf/ocr-proof',
    [string]$Worker = '.poc-runtime/pdf/artifacts/ocr-proof.wasm',
    [string]$Model = '.poc-runtime/pdf/models/por.traineddata'
)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot)
if (Test-Path -LiteralPath $Results) { throw "Use a new results directory: $Results" }
$Results = (New-Item -ItemType Directory $Results).FullName
$Worker = (Resolve-Path -LiteralPath $Worker).Path
$Model = (Resolve-Path -LiteralPath $Model).Path
$modelDirectory = Split-Path $Model
$build = & go version -m $Bacalhau
if ($LASTEXITCODE -or !($build -match 'bacalhau.*v1\.9\.5') -or !($build -match 'vcs.revision=3a6a73aa6529755ac2c9d96a771f0566ff3ee5da')) { throw 'Wrong Bacalhau build' }
New-Item -ItemType Directory "$Results/input" | Out-Null
Add-Type -AssemblyName System.Drawing
$bitmap = [Drawing.Bitmap]::new(1400, 280)
$graphics = [Drawing.Graphics]::FromImage($bitmap)
$font = [Drawing.Font]::new('Arial', 32)
try {
    $graphics.Clear([Drawing.Color]::White)
    $graphics.DrawString(('A prova documental confirma a obriga' + [char]0xe7 + [char]0xe3 + 'o.'), $font, [Drawing.Brushes]::Black, 30, 40)
    $graphics.DrawString(('Localizador: JABUTICABA. P' + [char]0xe1 + 'gina f' + [char]0xed + 'sica 1.'), $font, [Drawing.Brushes]::Black, 30, 120)
    $bitmap.Save("$Results/input/scan.bmp", [Drawing.Imaging.ImageFormat]::Bmp)
} finally { $graphics.Dispose(); $font.Dispose(); $bitmap.Dispose() }
$before = (Get-FileHash "$Results/input/scan.bmp").Hash
$config = @{
    DisableAnalytics=$true; API=@{Host='127.0.0.1';Port=42234}
    Orchestrator=@{Enabled=$true;Host='127.0.0.1';Port=44222}
    Compute=@{Enabled=$true;Orchestrators=@('nats://127.0.0.1:44222');AllowListedLocalPaths=@("$($Worker -replace '\\','/'):ro", "$($modelDirectory -replace '\\','/'):ro", "$($Results -replace '\\','/')/input:ro")}
    Engines=@{Disabled=@('docker')}; Publishers=@{Disabled=@('local')}
}
[IO.File]::WriteAllText("$Results/node.yaml", ($config | ConvertTo-Json -Depth 12))
$process = $null
$job = $null
$api = 'http://127.0.0.1:42234/api/v1/orchestrator'
try {
    $process = Start-Process $Bacalhau -ArgumentList 'serve --name viii-pdf-wasi --data-dir node -c node.yaml' -WorkingDirectory $Results -WindowStyle Hidden -PassThru -RedirectStandardOutput "$Results/node.stdout.log" -RedirectStandardError "$Results/node.stderr.log"
    [IO.File]::WriteAllText("$Results/owned.json", (@{pid=$process.Id;start=$process.StartTime.ToUniversalTime().ToString('o');executable=$Bacalhau} | ConvertTo-Json))
    $deadline = [DateTime]::UtcNow.AddSeconds(40)
    do {
        try { $nodes = Invoke-RestMethod "$api/nodes" -TimeoutSec 2; if ($nodes.Nodes) { break } } catch { }
        Start-Sleep -Milliseconds 250
    } until ([DateTime]::UtcNow -ge $deadline)
    if (!$nodes.Nodes) { throw 'Node did not become ready' }
    function New-ReadOnlyInput($Path, $Target) { @{Source=@{Type='local';Params=@{SourcePath=($Path -replace '\\','/');ReadWrite=$false}};Target=$Target} }
    $spec = @{Job=@{
        Name="viii-pdf-ocr-$([guid]::NewGuid().ToString('N'))";Type='batch';Count=1
        Constraints=@(@{Key='name';Operator='in';Values=@('viii-pdf-wasi')})
        Tasks=@(@{
            Name='main';Engine=@{Type='wasm';Params=@{EntryModule='/worker';Entrypoint='_start';Parameters=@('/evidence/scan.bmp','/model','/out/page-1.json','/evidence/accepted')}}
            InputSources=@((New-ReadOnlyInput $Worker '/worker'),(New-ReadOnlyInput $modelDirectory '/model'),(New-ReadOnlyInput "$Results/input" '/evidence'))
            # v1.9.5 validates Path with Windows filepath.IsAbs; WASI mounts Name.
            ResultPaths=@(@{Name='out';Path='C:/out'})
            Publisher=@{Type='noop'}
            Network=@{Type='none'}
            Resources=@{CPU='1';Memory='1gb'};Timeouts=@{TotalTimeout=300}
        })
    }}
    [IO.File]::WriteAllText("$Results/job.json", ($spec | ConvertTo-Json -Depth 20))
    $watch = [Diagnostics.Stopwatch]::StartNew()
    $job = (Invoke-RestMethod "$api/jobs" -Method Put -ContentType 'application/json' -Body ($spec | ConvertTo-Json -Depth 20)).JobID
    if (!$job) { throw 'No job ID' }
    $deadline = [DateTime]::UtcNow.AddSeconds(310)
    do {
        $description = Invoke-RestMethod "$api/jobs/${job}?include=executions" -TimeoutSec 5
        [IO.File]::WriteAllText("$Results/description.json", ($description | ConvertTo-Json -Depth 35))
        if (!(Test-Path "$Results/page-1.json")) {
            $artifact = Get-ChildItem "$Results/node" -Filter page-1.json -Recurse | Select-Object -First 1
            if ($artifact) {
                try {
                    $page = Get-Content -Raw -Encoding UTF8 $artifact.FullName | ConvertFrom-Json
                    if ($page.text -match 'JABUTICABA' -and $page.page -eq 1 -and $page.source -eq 'ocr') {
                        Copy-Item -LiteralPath $artifact.FullName -Destination "$Results/page-1.json"
                        [IO.File]::WriteAllText("$Results/input/accepted", 'accepted')
                    }
                } catch { }
            }
        }
        if ($description.Job.State.StateType -in @('Completed','Failed','Stopped')) { break }
        Start-Sleep -Milliseconds 500
    } until ([DateTime]::UtcNow -ge $deadline)
    $execution = @($description.Executions.Items)
    if ($description.Job.State.StateType -ne 'Completed' -or $execution.Count -ne 1 -or $execution[0].RunOutput.ExitCode -ne 0 -or $execution[0].NodeID -ne 'viii-pdf-wasi') { throw "OCR failed: inspect $Results/description.json" }
    if (!(Test-Path "$Results/page-1.json")) { throw 'No OCR artifact on disk' }
    $page = Get-Content -Raw -Encoding UTF8 "$Results/page-1.json" | ConvertFrom-Json
    if ($page.text -notmatch 'JABUTICABA' -or $page.source -ne 'ocr' -or $page.page -ne 1) { throw 'OCR assertion failed' }
    if ($execution[0].RunOutput.Stdout -match 'JABUTICABA|documental') { throw 'Evidence leaked into stdout' }
    if ((Get-FileHash "$Results/input/scan.bmp").Hash -ne $before) { throw 'Input mutated' }
    $summary = @{passed=$true;job=$job;execution=$execution[0].ID;node=$execution[0].NodeID;elapsed_ms=$watch.Elapsed.TotalMilliseconds;input_sha256=$before;worker_sha256=(Get-FileHash $Worker).Hash;model_sha256=(Get-FileHash $Model).Hash;tool=$page.tool;version=$page.version;docker_disabled=$true;bacalhau_revision='3a6a73aa6529755ac2c9d96a771f0566ff3ee5da'}
    [IO.File]::WriteAllText("$Results/summary.json", ($summary | ConvertTo-Json))
    $oldProof = $env:VIII_PDF_OCR_PROOF
    try {
        $env:VIII_PDF_OCR_PROOF = $Results
        $env:GOCACHE = Join-Path $env:TEMP 'viii-go-cache'
        & go test ./cmd/desktop -run '^TestPDFOCRArtifact$' -count=1 -v
        if ($LASTEXITCODE) { throw 'VIII persistence gate failed' }
    } finally { $env:VIII_PDF_OCR_PROOF = $oldProof }
    $summary | ConvertTo-Json
} finally {
    if ($job) { try { Invoke-RestMethod "$api/jobs/$job" -Method Delete -TimeoutSec 3 | Out-Null } catch { } }
    if ($process -and !$process.HasExited) { Stop-Process -Id $process.Id }
}
