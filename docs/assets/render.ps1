# Regenera los visuales de la documentacion: GIFs (VHS), diagramas (d2) y videos (HyperFrames).
#
#   powershell -File docs/assets/render.ps1 [-DryRun] [todo|gifs|diagramas|videos] [nombres...]
#
# Mismo contrato que render.sh: sin nombres regenera todo el grupo; con nombres, solo esos.
# Todo corre en Docker y nunca baja ni construye una imagen: si falta, imprime el comando y sale con 2.
param(
    [switch]$DryRun,
    [Parameter(Position = 0)][string]$Group = 'todo',
    [Parameter(Position = 1, ValueFromRemainingArguments = $true)][string[]]$Names = @()
)

$ErrorActionPreference = 'Stop'

$VhsImage = 'nxy-docs-vhs:v0.12.1'
$VhsSize = '~30 MB sobre ghcr.io/charmbracelet/vhs:v0.12.1, que pesa ~887 MB comprimida / ~3 GB en disco; si no la tenés, antes: docker pull ghcr.io/charmbracelet/vhs:v0.12.1'
$VhsBuild = 'docker build -t nxy-docs-vhs:v0.12.1 docs/assets/tapes'
$VideoImage = 'nxy-docs-video:0.9.10'
$VideoSize = 'unos 2 GB en disco (node, chromium, ffmpeg, fuentes y HyperFrames)'
$VideoBuild = 'docker build -t nxy-docs-video:0.9.10 docs/assets/video'
$D2Image = 'terrastruct/d2:v0.9.0'
$D2Size = '~95 MB'

if (@('todo', 'gifs', 'diagramas', 'videos') -notcontains $Group) {
    [Console]::Error.WriteLine("Grupo desconocido: $Group. Opciones: todo, gifs, diagramas, videos.")
    exit 1
}

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
Set-Location $Root

function Get-BaseNames($dir, $filter) {
    $p = Join-Path $Root $dir
    if (-not (Test-Path $p)) { return @() }
    @(Get-ChildItem -Path $p -Filter $filter -File | ForEach-Object { $_.BaseName } | Sort-Object)
}
$Tapes = Get-BaseNames 'docs/assets/tapes' '*.tape'
$Diagrams = Get-BaseNames 'docs/assets/diagrams' '*.d2'
$Videos = @()
$videoDir = Join-Path $Root 'docs/assets/video'
if (Test-Path $videoDir) {
    $Videos = @(Get-ChildItem -Path $videoDir -Directory |
        Where-Object { @('out', 'node_modules') -notcontains $_.Name -and (Test-Path (Join-Path $_.FullName 'index.html')) } |
        ForEach-Object { $_.Name } | Sort-Object)
}

switch ($Group) {
    'gifs' { $Valid = $Tapes }
    'diagramas' { $Valid = $Diagrams }
    'videos' { $Valid = $Videos }
    default { $Valid = @($Tapes) + @($Diagrams) + @($Videos) }
}
foreach ($n in $Names) {
    if ($Valid -notcontains $n) {
        [Console]::Error.WriteLine("No existe '$n' en el grupo $Group. Nombres validos:")
        $Valid | ForEach-Object { [Console]::Error.WriteLine("  $_") }
        exit 1
    }
}

function Test-Wanted($n) { ($Names.Count -eq 0) -or ($Names -contains $n) }

function Invoke-Step {
    # Imprime el comando; lo ejecuta salvo con -DryRun.
    param([string]$Exe, [string[]]$ArgList)
    Write-Host ("+ $Exe " + ($ArgList -join ' '))
    if ($DryRun) { return }
    & $Exe @ArgList
    if ($LASTEXITCODE -ne 0) { exit 1 }
}

function Test-Docker {
    if ($DryRun) { return }
    & docker info *> $null
    if ($LASTEXITCODE -ne 0) {
        [Console]::Error.WriteLine('Docker no responde: abrí Docker Desktop y volvé a correr este comando.')
        exit 2
    }
}

function Test-Image($image, $size, $cmd) {
    if ($DryRun) { return }
    & docker image inspect $image *> $null
    if ($LASTEXITCODE -ne 0) {
        [Console]::Error.WriteLine("Falta la imagen $image (pesa $size). Este script no la baja solo; conseguila con:")
        [Console]::Error.WriteLine("  $cmd")
        exit 2
    }
}

function Step-Build {
    Write-Host '== Binario de nxy para Linux (lo usan los contenedores) =='
    Write-Host '+ CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o docs/assets/.build/nxy ./cmd/nxy'
    if ($DryRun) { return }
    New-Item -ItemType Directory -Force -Path (Join-Path $Root 'docs/assets/.build') | Out-Null
    $old = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
    try {
        $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
        & go build -o 'docs/assets/.build/nxy' ./cmd/nxy
        if ($LASTEXITCODE -ne 0) { exit 1 }
    }
    finally {
        foreach ($k in $old.Keys) {
            if ($null -eq $old[$k]) { Remove-Item "Env:$k" -ErrorAction SilentlyContinue }
            else { Set-Item "Env:$k" $old[$k] }
        }
    }
}

function Step-Gifs {
    Write-Host '== GIFs (VHS) =='
    if ($Tapes.Count -eq 0) {
        Write-Host 'No hay tapes en docs/assets/tapes/*.tape: nada que grabar.'
        return
    }
    Test-Docker
    Test-Image $VhsImage $VhsSize $VhsBuild
    New-Item -ItemType Directory -Force -Path (Join-Path $Root 'docs/assets/gifs') | Out-Null
    foreach ($n in $Tapes) {
        if (-not (Test-Wanted $n)) { continue }
        Invoke-Step 'docker' @('run', '--rm', '-v', "${Root}:/vhs", $VhsImage, "docs/assets/tapes/$n.tape")
    }
}

function Step-Diagramas {
    Write-Host '== Diagramas (d2) =='
    if ($Diagrams.Count -eq 0) {
        Write-Host 'No hay diagramas en docs/assets/diagrams/*.d2: nada que dibujar.'
        return
    }
    $localD2 = $null -ne (Get-Command d2 -ErrorAction SilentlyContinue)
    if (-not $localD2) {
        Test-Docker
        Test-Image $D2Image $D2Size "docker pull $D2Image"
    }
    foreach ($n in $Diagrams) {
        if (-not (Test-Wanted $n)) { continue }
        $src = "docs/assets/diagrams/$n.d2"; $dst = "docs/assets/diagrams/$n.svg"
        if ($localD2) {
            Invoke-Step 'd2' @('--theme', '0', '--dark-theme', '200', '--pad', '24', $src, $dst)
        }
        else {
            Invoke-Step 'docker' @('run', '--rm', '-v', "${Root}:/work", '-w', '/work', $D2Image,
                '--theme', '0', '--dark-theme', '200', '--pad', '24', $src, $dst)
        }
    }
}

function Step-Videos {
    Write-Host '== Videos (HyperFrames) =='
    if ($Videos.Count -eq 0) {
        Write-Host 'No hay composiciones en docs/assets/video/*/index.html: nada que renderizar.'
        return
    }
    Test-Docker
    Test-Image $VideoImage $VideoSize $VideoBuild
    New-Item -ItemType Directory -Force -Path (Join-Path $Root 'docs/assets/video/out') | Out-Null
    foreach ($n in $Videos) {
        if (-not (Test-Wanted $n)) { continue }
        Invoke-Step 'docker' @('run', '--rm', '--shm-size=1g', '-v', "${Root}:/work", '-w', '/work/docs/assets/video',
            $VideoImage, 'hyperframes', 'render', $n, '-o', "out/$n.mp4", '--quiet')
    }
}

switch ($Group) {
    'todo' { Step-Build; Step-Gifs; Step-Diagramas; Step-Videos }
    'gifs' { Step-Build; Step-Gifs }
    'diagramas' { Step-Diagramas }
    'videos' { Step-Videos }
}
