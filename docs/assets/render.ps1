# Regenera los visuales de la documentacion: capturas de la TUI y portada (volcados de `nxy tui --demo --dump`
# sobre el marco de marca de docs/assets/brand/, con Chromium), diagramas (HTML -> PNG con Chromium) y
# videos (HyperFrames).
#
#   powershell -File docs/assets/render.ps1 [-DryRun] [todo|capturas|diagramas|videos] [nombres...]
#
# Mismo contrato que render.sh: sin nombres regenera todo el grupo; con nombres, solo esos.
# Los volcados corren en el host con `go run`; el resto en Docker, que nunca baja ni construye una imagen:
# si falta, imprime el comando y sale con 2.
param(
    [switch]$DryRun,
    [Parameter(Position = 0)][string]$Group = 'todo',
    [Parameter(Position = 1, ValueFromRemainingArguments = $true)][string[]]$Names = @()
)

$ErrorActionPreference = 'Stop'

$VideoImage = 'nxy-docs-video:0.9.10'
$VideoSize = 'unos 2 GB en disco (node, chromium, ffmpeg, fuentes y HyperFrames)'
$VideoBuild = 'docker build -t nxy-docs-video:0.9.10 docs/assets/video'

if (@('todo', 'capturas', 'diagramas', 'videos') -notcontains $Group) {
    [Console]::Error.WriteLine("Grupo desconocido: $Group. Opciones: todo, capturas, diagramas, videos.")
    exit 1
}

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
Set-Location $Root

$Tsv = Join-Path $Root 'docs/assets/capturas/capturas.tsv'

function Get-BaseNames($dir, $filter) {
    $p = Join-Path $Root $dir
    if (-not (Test-Path $p)) { return @() }
    @(Get-ChildItem -Path $p -Filter $filter -File | ForEach-Object { $_.BaseName } | Sort-Object)
}
function Get-Captures {
    # Una fila por captura del manifiesto: nombre, dump, feature.
    if (-not (Test-Path $Tsv)) { return @() }
    $lines = @([IO.File]::ReadAllLines($Tsv) | Where-Object { $_.Trim() -ne '' })
    @($lines | Select-Object -Skip 1 | ForEach-Object {
            $c = $_.Split("`t")
            [pscustomobject]@{ Name = $c[0].Trim(); Dump = $c[1].Trim(); Feature = $c[2].Trim() }
        })
}
$Captures = Get-Captures
$CaptureNames = @($Captures | ForEach-Object { $_.Name })
$Diagrams = Get-BaseNames 'docs/assets/diagrams' '*.html'
$Videos = @()
$videoDir = Join-Path $Root 'docs/assets/video'
if (Test-Path $videoDir) {
    $Videos = @(Get-ChildItem -Path $videoDir -Directory |
        Where-Object { @('out', 'node_modules') -notcontains $_.Name -and (Test-Path (Join-Path $_.FullName 'index.html')) } |
        ForEach-Object { $_.Name } | Sort-Object)
}

switch ($Group) {
    'capturas' { $Valid = @($CaptureNames) + @('banner') }
    'diagramas' { $Valid = $Diagrams }
    'videos' { $Valid = $Videos }
    default { $Valid = @($CaptureNames) + @('banner') + @($Diagrams) + @($Videos) }
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

function Step-Capturas {
    Write-Host '== Capturas de la TUI y portada =='
    $wanted = @($Captures | Where-Object { Test-Wanted $_.Name })
    $wantBanner = Test-Wanted 'banner'
    if ($wanted.Count -eq 0 -and -not $wantBanner) {
        Write-Host 'Nada que capturar.'
        return
    }
    Test-Docker
    Test-Image $VideoImage $VideoSize $VideoBuild
    if (-not $DryRun) {
        foreach ($d in @('docs/assets/.build/dumps', 'docs/assets/capturas')) {
            New-Item -ItemType Directory -Force -Path (Join-Path $Root $d) | Out-Null
        }
    }
    # 1) volcados de la TUI con datos de ejemplo, en el host (mismo resultado en Windows y Linux)
    foreach ($c in $wanted) {
        $out = "docs/assets/.build/dumps/$($c.Name).ansi"
        $goArgs = @('run', './cmd/nxy', 'tui', '--demo', '--dump', $c.Dump)
        if ($c.Feature -ne '-' -and $c.Feature -ne '') { $goArgs += @('--feature', $c.Feature) }
        $goArgs += @('--width', '120', '--icons', 'unicode')
        Write-Host ("+ go " + ($goArgs -join ' ') + " > $out")
        if ($DryRun) { continue }
        # UTF-8 sin BOM: con `>` PowerShell escribiria UTF-16 y el volcado no se podria leer.
        [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
        $text = (& go @goArgs) -join "`n"
        if ($LASTEXITCODE -ne 0) { exit 1 }
        if (-not $text.Contains([string][char]27 + '[38;2')) {
            [Console]::Error.WriteLine("El volcado de $($c.Name) no trae color (ESC[38;2): sin color la captura no sirve.")
            exit 1
        }
        [IO.File]::WriteAllText((Join-Path $Root $out), $text + "`n", (New-Object System.Text.UTF8Encoding($false)))
    }
    # 2) capturas y portada (Chromium)
    if ($wanted.Count -gt 0) {
        Invoke-Step 'docker' (@('run', '--rm', '--shm-size=1g', '-v', "${Root}:/work", '-w', '/work', $VideoImage,
                'node', 'docs/assets/brand/captura.cjs', 'captura') + @($wanted | ForEach-Object { $_.Name }))
    }
    if ($wantBanner) {
        Invoke-Step 'docker' @('run', '--rm', '--shm-size=1g', '-v', "${Root}:/work", '-w', '/work', $VideoImage,
            'node', 'docs/assets/brand/captura.cjs', 'banner')
    }
}

function Step-Diagramas {
    Write-Host '== Diagramas (HTML -> PNG) =='
    if ($Diagrams.Count -eq 0) {
        Write-Host 'No hay diagramas en docs/assets/diagrams/*.html: nada que dibujar.'
        return
    }
    $wanted = @($Diagrams | Where-Object { Test-Wanted $_ })
    if ($wanted.Count -eq 0) { return }
    Test-Docker
    Test-Image $VideoImage $VideoSize $VideoBuild
    Invoke-Step 'docker' (@('run', '--rm', '--shm-size=1g', '-v', "${Root}:/work", '-w', '/work', $VideoImage,
            'node', 'docs/assets/brand/captura.cjs', 'diagrama') + $wanted)
}

function Step-Videos {
    Write-Host '== Videos (HyperFrames) =='
    if ($Videos.Count -eq 0) {
        Write-Host 'No hay composiciones en docs/assets/video/*/index.html: nada que renderizar.'
        return
    }
    Test-Docker
    Test-Image $VideoImage $VideoSize $VideoBuild
    foreach ($n in $Videos) {
        if (-not (Test-Wanted $n)) { continue }
        # HyperFrames puede no servir archivos fuera de la carpeta de la composicion: la marca se copia antes
        # del render (marca/ esta en .gitignore) y se borra despues.
        $marca = Join-Path $Root "docs/assets/video/$n/marca"
        if ($DryRun) {
            Write-Host "+ cp docs/assets/brand/marca.css docs/assets/brand/perro-*.png docs/assets/video/$n/marca/"
        } else {
            New-Item -ItemType Directory -Force -Path $marca | Out-Null
            Copy-Item (Join-Path $Root 'docs/assets/brand/marca.css') $marca
            Copy-Item (Join-Path $Root 'docs/assets/brand/perro-*.png') $marca
        }
        try {
            Invoke-Step 'docker' @('run', '--rm', '--shm-size=1g', '-v', "${Root}:/work", '-w', '/work/docs/assets/video',
                $VideoImage, 'hyperframes', 'render', $n, '-o', "$n.mp4", '--quiet')
        } finally {
            if (-not $DryRun -and (Test-Path $marca)) { Remove-Item -Recurse -Force $marca }
        }
    }
}

switch ($Group) {
    'todo' { Step-Capturas; Step-Diagramas; Step-Videos }
    'capturas' { Step-Capturas }
    'diagramas' { Step-Diagramas }
    'videos' { Step-Videos }
}
