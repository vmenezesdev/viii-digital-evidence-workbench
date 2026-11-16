param([string]$Bacalhau = 'C:\Bacalhau\bin\bacalhau.exe', [int]$Port = 2234)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot
Set-Location $root
$scratch = Join-Path $root '.poc-runtime'
New-Item -ItemType Directory -Force $scratch | Out-Null
$go = 'C:\Program Files\Go\bin\go.exe'
$env:GOCACHE = Join-Path $env:TEMP 'viii-go-cache'
& $go test ./...
if ($LASTEXITCODE) { throw 'Tests failed' }
& $go build -o .poc-runtime/worker.exe ./cmd/worker
if ($LASTEXITCODE) { throw 'Native build failed' }
try {
 $env:GOOS = 'wasip1'; $env:GOARCH = 'wasm'
 & $go build -o .poc-runtime/worker.wasm ./cmd/worker
 if ($LASTEXITCODE) { throw 'WASI build failed' }
} finally { Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue }
$fixture = Join-Path $root 'docs/research/fixtures/ufdr/messages-minimal.xml'
$before = (Get-FileHash $fixture).Hash
$inputBytes = [IO.File]::ReadAllBytes($fixture)
$workerBytes = [IO.File]::ReadAllBytes((Join-Path $scratch 'worker.wasm'))
$spec = @{
 Name=('viii-synthetic-normalize-'+[DateTime]::UtcNow.Ticks); Type='batch'; Count=1
 Tasks=@(@{
  Name='main'; Engine=@{Type='wasm';Params=@{EntryModule='/worker.wasm';Entrypoint='_start';Parameters=@('/input.xml')}}
  InputSources=@(
   @{Target='/worker.wasm';Source=@{Type='urldownload';Params=@{URL='http://127.0.0.1:28080/worker.wasm'}}},
   @{Target='/input.xml';Source=@{Type='inline';Params=@{URL=('data:text/xml;base64,'+[Convert]::ToBase64String($inputBytes))}}}
  )
 })
}
[IO.File]::WriteAllText((Join-Path $scratch 'job.json'), ($spec | ConvertTo-Json -Depth 15))
$clientArgs = @('--data-dir', (Join-Path $scratch 'client'), '--api-host','127.0.0.1','--api-port',"$Port")
$clock = [Diagnostics.Stopwatch]::StartNew()
$job = & $Bacalhau job run .poc-runtime/job.json --no-template --id-only --wait=false @clientArgs
if ($LASTEXITCODE) { throw 'Submission failed' }
$job = ($job | Select-Object -Last 1).Trim()
Write-Host "Submitted $job"
# 'job wait' is deliberately avoided: interface varies between upstream versions.
& $Bacalhau job describe $job @clientArgs | Set-Content .poc-runtime/job-description.txt
Write-Host "Submission elapsed milliseconds: $($clock.ElapsedMilliseconds)"
$deadline = [DateTime]::UtcNow.AddSeconds(30)
do {
 $description = & $Bacalhau job describe $job --output json @clientArgs | ConvertFrom-Json
 if ($LASTEXITCODE) { throw 'Status request failed' }
 if ($description.Job.State.StateType -eq 'Completed') { break }
 if ($description.Job.State.StateType -eq 'Failed') { throw 'Bacalhau job failed' }
 Start-Sleep -Milliseconds 200
} while ([DateTime]::UtcNow -lt $deadline)
if ($description.Job.State.StateType -ne 'Completed') { throw 'Bacalhau completion timeout' }
$execution = @($description.Executions.Items)[-1]
if (-not $execution) { $execution = @($description.Executions)[-1] }
$remote = $execution.RunOutput.Stdout
if (-not $remote) { throw 'No stdout in execution record; inspect description schema' }
[IO.File]::WriteAllText((Join-Path $scratch 'bacalhau.json'), $remote)
Write-Host "Bacalhau completion wall milliseconds: $($clock.ElapsedMilliseconds)"
$samples = 1..5 | ForEach-Object {
 $watch = [Diagnostics.Stopwatch]::StartNew()
 $out = & .poc-runtime/worker.exe $fixture
 if ($LASTEXITCODE) { throw 'Native execution failed' }
 $watch.Stop()
 $out | Set-Content .poc-runtime/native.json
 $watch.Elapsed.TotalMilliseconds
}
Write-Host "Native process wall milliseconds: $samples"
if ((Get-FileHash $fixture).Hash -ne $before) { throw 'Original bytes changed' }
Write-Host "Original SHA256: $before"
$native = Get-Content .poc-runtime/native.json -Raw | ConvertFrom-Json | ConvertTo-Json -Depth 20 -Compress
$actual = $remote | ConvertFrom-Json | ConvertTo-Json -Depth 20 -Compress
if ($native -ne $actual) { throw 'Native and WASI outputs differ' }
Write-Host 'Native/WASI output equivalence PASSED'
