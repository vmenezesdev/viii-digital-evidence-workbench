param(
    [string]$Output='bin/pdf-mvp',
    [string]$Bacalhau='C:\Bacalhau\bin\bacalhau.exe',
    [string]$Worker='.poc-runtime/pdf/rust-target-simd/wasm32-wasip1/release/viii-pdf-worker.wasm',
    [string]$Model='.poc-runtime/pdf/models/por.traineddata'
)
$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot)
if (Test-Path $Output) { throw 'Choose a new package directory' }
$Output=(New-Item -ItemType Directory $Output).FullName
New-Item -ItemType Directory "$Output/runtime/models" | Out-Null
$info=& go version -m $Bacalhau
if ($LASTEXITCODE -or !($info -match 'bacalhau.*v1\.9\.5') -or !($info -match 'vcs.revision=3a6a73aa6529755ac2c9d96a771f0566ff3ee5da')) { throw 'Wrong Bacalhau version' }
Copy-Item -LiteralPath $Bacalhau -Destination "$Output/runtime/bacalhau.exe"
Copy-Item -LiteralPath $Worker -Destination "$Output/runtime/pdf-worker.wasm"
Copy-Item -LiteralPath $Model -Destination "$Output/runtime/models/por.traineddata"
$manifest=@{WorkerSHA256=(Get-FileHash $Worker).Hash;ModelSHA256=(Get-FileHash $Model).Hash;BacalhauSHA256=(Get-FileHash $Bacalhau).Hash;DockerImage='viii-pdf-worker:local'}
[IO.File]::WriteAllText("$Output/runtime/manifest.json",($manifest | ConvertTo-Json))
$env:GOCACHE=Join-Path $env:TEMP 'viii-go-cache'
& go build -trimpath -ldflags '-H=windowsgui' -o "$Output/VIII.exe" ./cmd/desktop
if ($LASTEXITCODE) { throw 'Desktop build failed' }
Copy-Item -LiteralPath LICENSE -Destination "$Output/LICENSE"
Write-Output $Output
