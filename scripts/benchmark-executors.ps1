param(
    [string]$Go = 'C:\Program Files\Go\bin\go.exe',
    [string]$Bacalhau = 'C:\Bacalhau\bin\bacalhau.exe',
    [string]$Results,
    [ValidateSet('sweep','full','control','smoke')][string]$Matrix = 'sweep',
    [ValidateSet('500m','1')][string]$JobCPU = '500m',
    [switch]$FreshImage,
    [switch]$DockerDesktopDaemon
)
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
Set-Location (Split-Path $PSScriptRoot)
$run = "viii-bench-$([guid]::NewGuid().ToString('N').Substring(0,12))"
if (!$Results) { $Results = ".poc-runtime/$run" }
if (Test-Path -LiteralPath $Results) { throw "Use a new directory: $Results" }
$root = (New-Item -ItemType Directory -Path $Results).FullName
$nodeImage = 'ghcr.io/bacalhau-project/bacalhau@sha256:903c4052a4522720adf43d428bdbb267cb24dec5039a759e27dffbcbbd68d878'
$registryImage = 'docker.io/library/registry@sha256:6c5666b861f3505b116bb9aa9b25175e71210414bd010d92035ff64018f9457e'
$builderImage = 'docker.io/moby/buildkit@sha256:e39f6119f134b4811af19fd5c20f495a6a264a85c1b6920daf569b23009dd42c'
$revision = '3a6a73aa6529755ac2c9d96a771f0566ff3ee5da'
$processes = [ordered]@{}
$containers = [Collections.Generic.List[string]]::new()
$images = [Collections.Generic.List[string]]::new()
$volumes = [Collections.Generic.List[string]]::new()
$sampler = $null
$daemonSampler = $null
$events = $null
$builder = $null
$phase = 'preflight'
$env:GOCACHE = Join-Path $env:TEMP 'viii-go-cache'
$env:GOTOOLCHAIN = 'local'

function Write-JSON($File, $Value) {
    $Value | ConvertTo-Json -Depth 12 | Set-Content -Encoding utf8 (Join-Path $root $File)
}
function Docker {
    $output = & docker.exe @args
    if ($LASTEXITCODE) { throw "Docker command failed: $($args[0])" }
    $output
}
function Start-Owned($Name, $Exe, $Arguments) {
    $options = @{
        FilePath=$Exe; ArgumentList=$Arguments; WorkingDirectory=$root; WindowStyle='Hidden'; PassThru=$true
        RedirectStandardOutput="$root/$Name.stdout.log"; RedirectStandardError="$root/$Name.stderr.log"
    }
    $processes[$Name] = Start-Process @options
}
function Build($Architecture, $TargetOS, $Output, $Package) {
    $env:GOARCH, $env:GOOS, $env:CGO_ENABLED = $Architecture, $TargetOS, '0'
    & $Go build -trimpath -buildvcs=false -o (Join-Path $root $Output) $Package
    if ($LASTEXITCODE) { throw "Build failed: $Output" }
}
function Wait-Node($ID, $ExpectedEngines) {
    $deadline = [DateTime]::UtcNow.AddSeconds(45)
    do {
        try {
            $nodes = Invoke-RestMethod 'http://127.0.0.1:32234/api/v1/orchestrator/nodes' -TimeoutSec 2
            $node = $nodes.Nodes | Where-Object { $_.Info.NodeID -eq $ID }
        } catch { }
        if ($node -and $node.Membership -eq 'APPROVED' -and $node.Connection -eq 'CONNECTED') {
            Write-JSON "$ID.json" $node
            $advertised = ($node.Info.ComputeNodeInfo.ExecutionEngines | Sort-Object) -join ','
            if ($advertised -ne $ExpectedEngines -or $node.Info.BacalhauVersion.GitCommit -ne $revision) {
                throw "Unexpected engine set or Bacalhau revision on $ID"
            }
            return
        }
        Start-Sleep -Milliseconds 250
    } until ([DateTime]::UtcNow -ge $deadline)
    throw "Node $ID did not register"
}

try {
    $build = & $Go version
    if ($LASTEXITCODE -or $build -notmatch '^go version go1\.27\.4 windows/amd64$') { throw "Expected Go 1.27.4 windows/amd64: $build" }
    $nodeBuild = & $Go version -m $Bacalhau
    if ($LASTEXITCODE -or !($nodeBuild -match "vcs.revision=$revision") -or !($nodeBuild -match 'bacalhau.*v1\.9\.5')) { throw 'Expected the pinned Bacalhau v1.9.5 Windows binary' }
    foreach ($port in 32234, 34222, 38080, 32236) {
        $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $port)
        $listener.ExclusiveAddressUse = $true
        try { $listener.Start() } finally { $listener.Stop() }
    }
    foreach ($image in $nodeImage,$registryImage) {
        docker.exe image inspect $image --format '{{.Id}}' 2>$null | Out-Null
        if ($LASTEXITCODE) { Docker pull $image | Out-Host }
    }
    $hostInfo = Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,BuildNumber,TotalVisibleMemorySize
    $cpu = Get-CimInstance Win32_Processor | Select-Object Name,NumberOfCores,NumberOfLogicalProcessors
    $daemon = Docker info --format '{{json .}}' | ConvertFrom-Json
    $daemonVersion = Docker version --format '{{json .Server}}' | ConvertFrom-Json
    if ($daemon.OSType -ne 'linux' -or $daemon.Architecture -notin 'x86_64','amd64') { throw 'Expected a Linux amd64 Docker daemon' }
    $phase = 'build'
    Build amd64 windows 'bench-worker.exe' './cmd/latency'
    Build amd64 windows 'stage.exe' './cmd/stage'
    Build amd64 linux 'bench-worker-linux' './cmd/latency'
    Build wasm wasip1 'bench-worker.wasm' './cmd/latency'
    $binaries = @{}
    foreach ($file in 'bench-worker.exe','bench-worker-linux','bench-worker.wasm','stage.exe') {
        $binaries[$file] = @{sha256=(Get-FileHash "$root/$file").Hash.ToLowerInvariant(); bytes=(Get-Item "$root/$file").Length}
    }

    $phase = 'cold-image'
    $registry = "$run-registry"
    $registryVolume = Docker volume create "$registry-data"
    $volumes.Add($registryVolume)
    $registryPort = Get-Random -Minimum 40000 -Maximum 48000
    $endpoint = "127.0.0.1:$registryPort"
    $debugEndpoint = "127.0.0.1:$($registryPort + 8000)"
    Docker run --rm -d --name $registry --network host -e "REGISTRY_HTTP_ADDR=$endpoint" -e "REGISTRY_HTTP_DEBUG_ADDR=$debugEndpoint" --mount "type=volume,source=$registryVolume,target=/var/lib/registry" $registryImage | Out-Null
    $containers.Add($registry)
    $tag = "$endpoint/${run}:worker"
    $context = New-Item -ItemType Directory -Path "$root/image-context"
    Copy-Item "$root/bench-worker-linux" $context
    Copy-Item "$PSScriptRoot/benchmark-worker.Dockerfile" "$context/Dockerfile"
    if ($FreshImage) {
        # A separate builder pushes without loading any worker layers into the
        # daemon. Rewrite only the layer metadata so reruns are cold too.
        $builderName = "$run-build"
        $epoch = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
        (Get-Item "$context/bench-worker-linux").LastWriteTimeUtc = [DateTime]::UtcNow
        Docker buildx create --name $builderName --driver docker-container --driver-opt "network=host,image=$builderImage" | Out-Null
        $builder = $builderName
        Docker buildx build --builder $builder --platform linux/amd64 --provenance=false --sbom=false --progress plain --build-arg "SOURCE_DATE_EPOCH=$epoch" --metadata-file "$root/buildx.json" --output "type=image,name=$tag,push=true,registry.insecure=true,rewrite-timestamp=true" "$context" | Set-Content "$root/image-push.log"
        $manifest = (Get-Content "$root/buildx.json" -Raw | ConvertFrom-Json).'containerimage.digest'
        $workerImage = "$endpoint/$run@$manifest"
        Docker buildx rm $builder | Out-Null
        $builder = $null
    } else {
        Docker build --platform linux/amd64 --provenance=false --sbom=false -t $tag "$context" | Out-Host
        $images.Add($tag)
        Docker push $tag | Set-Content "$root/image-push.log"
        $built = Docker image inspect $tag --format '{{json .}}' | ConvertFrom-Json
        $workerImage = $built.RepoDigests | Where-Object { $_.StartsWith("$endpoint/$run@") } | Select-Object -First 1
        Docker image rm $tag | Out-Host
        [void]$images.Remove($tag)
    }
    if (!$workerImage) { throw 'The local registry did not return a manifest digest' }
    $images.Add($workerImage)
    docker.exe image inspect $workerImage --format '{{.Id}}' 2>$null | Out-Null
    if (!$LASTEXITCODE) { throw 'Worker manifest is still available locally after tag removal' }
    $coldAt = [DateTime]::UtcNow
    $pullTime = [Diagnostics.Stopwatch]::StartNew()
    $pullLog = Docker pull --platform linux/amd64 $workerImage
    $pullTime.Stop()
    $pullLog | Set-Content "$root/image-pull.log"
    $downloaded = [bool]($pullLog -match ': Download complete')
    if ($FreshImage -and !$downloaded) { throw 'No worker layer download was observed' }
    $pulled = Docker image inspect $workerImage --format '{{json .}}' | ConvertFrom-Json
    if ($workerImage -notin $pulled.RepoDigests) { throw 'Pulled image does not match the pinned manifest' }
    Write-JSON 'environment.json' @{
        run=$run; matrix=$Matrix; job_cpu=$JobCPU; job_memory='512mb'; windows=$hostInfo; cpu=$cpu; go=$build; bacalhau_windows_sha256=(Get-FileHash $Bacalhau).Hash.ToLowerInvariant()
        bacalhau_revision=$revision; bacalhau_image=$nodeImage; registry_image=$registryImage
        linux=@{kernel=$daemon.KernelVersion; cpus=$daemon.NCPU; bytes=$daemon.MemTotal; os=$daemon.OperatingSystem; docker=$daemonVersion; architecture=$daemon.Architecture}
        worker=@{base='scratch'; ref=$workerImage; daemon_image_id=$pulled.Id; bytes=$pulled.Size; buildkit=$builderImage; source_date_epoch=$epoch}
        binaries=$binaries; cold_pull=@{at=$coldAt; elapsed_ms=$pullTime.Elapsed.TotalMilliseconds; source='Linux VM loopback registry'; local_manifest_before=$false; layer_downloaded=$downloaded}
    }

    $phase = 'nodes'
    $token = [guid]::NewGuid().ToString('N')
    $config = @{
        'orchestrator'=@{
            DisableAnalytics=$true; API=@{Host='127.0.0.1';Port=32234}
            Orchestrator=@{Enabled=$true;Host='127.0.0.1';Port=34222;Auth=@{Token=$token}}
            Compute=@{Enabled=$false}; Engines=@{Disabled=@('docker')}; Publishers=@{Disabled=@('local')}
        }
        'windows'=@{
            DisableAnalytics=$true; API=@{Host='127.0.0.1';Port=32236}
            Compute=@{Enabled=$true;Orchestrators=@('nats://127.0.0.1:34222');Auth=@{Token=$token}}
            Engines=@{Disabled=@('docker')}; Publishers=@{Disabled=@('local')}
        }
        'linux'=@{
            DisableAnalytics=$true; API=@{Host='127.0.0.1';Port=32236}
            Compute=@{Enabled=$true;Orchestrators=@('nats://host.docker.internal:34222');Auth=@{Token=$token}}
            Publishers=@{Disabled=@('local')}
        }
    }
    foreach ($role in $config.Keys) { Write-JSON "$role.yaml" $config[$role] }
    Start-Owned 'stage' "$root/stage.exe" '-spike -traces'
    $env:OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = 'http://127.0.0.1:38080/v1/traces'
    $env:OTEL_EXPORTER_OTLP_TRACES_PROTOCOL = 'http/protobuf'
    $env:OTEL_EXPORTER_OTLP_TRACES_COMPRESSION = 'none'
    $env:OTEL_BSP_SCHEDULE_DELAY = '1000'
    $env:OTEL_SERVICE_NAME = 'viii-benchmark-orchestrator'
    Start-Owned 'orchestrator' $Bacalhau 'serve --name viii-orchestrator --data-dir orchestrator-data -c orchestrator.yaml'
    $env:OTEL_SERVICE_NAME = 'viii-benchmark-windows-compute'
    Start-Owned 'windows-compute' $Bacalhau 'serve --name viii-a --data-dir windows-data -c windows.yaml'
    $volume = Docker volume create "$run-runtime"
    $volumes.Add($volume)
    $runtimePath = Docker volume inspect $volume --format '{{.Mountpoint}}'
    $linux = "$run-linux"
    # Child mounts are interpreted by the daemon. Mount the dedicated runtime
    # volume at its daemon-side path inside the compute node too.
    $nodeArgs = @(
        '--mount',"type=bind,source=$root,target=/experiment",
        '--mount',"type=volume,source=$volume,target=$runtimePath",
        '--mount','type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock'
    )
    $traceArgs = @(
        '-e','OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://host.docker.internal:38080/v1/traces',
        '-e','OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf','-e','OTEL_EXPORTER_OTLP_TRACES_COMPRESSION=none',
        '-e','OTEL_BSP_SCHEDULE_DELAY=1000','-e','OTEL_SERVICE_NAME=viii-benchmark-linux-compute'
    )
    Docker run --rm -d --name $linux --workdir /experiment -e SKIP_IMAGE_PULL=1 @traceArgs @nodeArgs $nodeImage serve --name viii-b --data-dir "$runtimePath/node" -c /experiment/linux.yaml | Out-Null
    $containers.Add($linux)
    Wait-Node 'viii-a' 'wasm'
    Wait-Node 'viii-b' 'docker,wasm'
    $eventArgs = @{
        FilePath=(Get-Command docker.exe -CommandType Application).Source
        ArgumentList=@('events','--filter','type=container','--filter',"image=$workerImage",'--format','"{{json .}}"')
        WindowStyle='Hidden'; PassThru=$true
        RedirectStandardOutput="$root/docker-events.jsonl"; RedirectStandardError="$root/docker-events.stderr.log"
    }
    $events = Start-Process @eventArgs
    $ids = @($processes.Values | ForEach-Object Id)
    $roles = @{stage=$processes.stage.Id; orchestrator=$processes.orchestrator.Id; windows_compute=$processes['windows-compute'].Id}
    $sampler = Start-Job -FilePath "$PSScriptRoot/spike-resources.ps1" -ArgumentList $root,$ids,$linux,$workerImage,$roles
    if ($DockerDesktopDaemon) {
        $daemonSampler = Start-Job -FilePath "$PSScriptRoot/spike-daemon-resources.ps1" -ArgumentList $root
    }

    $phase = 'linux-matrix'
    $driverArgs = @(
        '-e','VIII_SPIKE_RESULTS=/experiment','-e','VIII_SPIKE_TOPOLOGY=windows-orchestrator-linux-and-windows-compute',
        '-e','VIII_SPIKE_API=http://host.docker.internal:32234','-e','VIII_SPIKE_STAGE_A=http://127.0.0.1:38080',
        '-e','VIII_SPIKE_STAGE_B=http://host.docker.internal:38080','-e',"VIII_SPIKE_DOCKER_IMAGE=$workerImage",
        '-e',"VIII_SPIKE_JOB_CPU=$JobCPU"
    )
    & docker.exe exec @driverArgs $linux /experiment/bench-worker-linux measure -comparison $Matrix
    if ($LASTEXITCODE) { throw 'Linux comparison benchmark failed' }

    $phase = 'windows-matrix'
    $windowsRoot = (New-Item -ItemType Directory "$root/windows-native").FullName
    $env:VIII_SPIKE_RESULTS = $windowsRoot
    $env:VIII_SPIKE_API = ''
    $env:VIII_SPIKE_TOPOLOGY = 'windows-native'
    & "$root/bench-worker.exe" measure -comparison $Matrix
    if ($LASTEXITCODE) { throw 'Windows native benchmark failed' }
    $phase = 'finished'
} catch {
    Write-JSON 'failure.json' @{at=[DateTime]::UtcNow; phase=$phase; error=$_.ToString()}
    throw
} finally {
    if ($sampler -or $daemonSampler) {
        New-Item -ItemType File "$root/stop-resource-sampling" -Force | Out-Null
        foreach ($job in @($sampler, $daemonSampler) | Where-Object { $_ }) {
            if (!(Wait-Job $job -Timeout 10)) { Stop-Job $job }
            Receive-Job $job -ErrorAction Continue
            Remove-Job $job
        }
    }
    if ($events -and !$events.HasExited) { Stop-Process -InputObject $events }
    if ($builder) { docker.exe buildx rm $builder | Out-Null }
    foreach ($process in $processes.Values) {
        if (!$process.HasExited) { Stop-Process -InputObject $process; $process.WaitForExit(5000) | Out-Null }
    }
    foreach ($container in $containers) {
        docker.exe logs $container *> "$root/$container.log"
        docker.exe stop -t 5 $container | Out-Null
    }
    foreach ($image in $images) { docker.exe image rm $image 2>$null | Out-Null }
    foreach ($volume in $volumes) { docker.exe volume rm $volume | Out-Null }
    foreach ($role in 'orchestrator','linux','windows') {
        $file = Join-Path $root "$role.yaml"
        if (Test-Path -LiteralPath $file) { Remove-Item -LiteralPath $file }
    }
    Write-Host "Run files: $root"
}
