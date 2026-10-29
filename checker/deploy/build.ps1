# Builds ready-to-copy packages in dist\:
#   dist\domain-checker-linux-amd64.tar.gz
#   dist\domain-checker-linux-arm64.tar.gz
#   dist\domain-checker-windows-amd64.exe
# Needs Go and tar (included in Windows 10 and later). Run it from anywhere.
$ErrorActionPreference = 'Stop'

$root = Split-Path $PSScriptRoot -Parent
$dist = Join-Path $root 'dist'

# A checker started from dist locks its file, so it is closed first. What it
# checked is saved, and the check can be resumed.
$running = Get-Process | Where-Object { $_.Path -and $_.Path.StartsWith($dist + '\', 'OrdinalIgnoreCase') }
foreach ($process in $running) {
    "closing $($process.Path) (process $($process.Id))"
    Stop-Process -Id $process.Id -Force
    $process.WaitForExit(10000) | Out-Null
}

Remove-Item -Recurse -Force $dist -ErrorAction SilentlyContinue
if (Test-Path $dist) { throw "cannot remove $dist, close the programs that use it" }

$env:CGO_ENABLED = '0'
$env:GOOS = 'linux'
try {
    foreach ($arch in 'amd64', 'arm64') {
        $pkg = Join-Path $dist 'domain-checker'
        New-Item -ItemType Directory -Force $pkg | Out-Null

        $env:GOARCH = $arch
        Push-Location $root
        try {
            go build -trimpath -ldflags='-s -w' -o (Join-Path $pkg 'checker') ./cmd/checker
            if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
        } finally {
            Pop-Location
        }

        # Line endings must stay LF for Linux
        foreach ($file in 'service.sh', 'README.md') {
            $text = [IO.File]::ReadAllText((Join-Path $PSScriptRoot $file)) -replace "`r`n", "`n"
            [IO.File]::WriteAllText((Join-Path $pkg $file), $text)
        }

        # Windows' own tar, since GNU tar from Git reads "D:" as a remote host.
        # Relative paths work with both.
        $tar = Join-Path $env:SystemRoot 'System32\tar.exe'
        if (-not (Test-Path $tar)) { $tar = 'tar' }
        $archive = "domain-checker-linux-$arch.tar.gz"
        Push-Location $dist
        try {
            # Owned by root, not by the user who builds
            & $tar --uid 0 --gid 0 -czf $archive domain-checker
            if ($LASTEXITCODE -ne 0) { throw 'tar failed' }
        } finally {
            Pop-Location
        }
        Remove-Item -Recurse -Force $pkg
        "built $(Join-Path $dist $archive)"
    }

    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $exe = Join-Path $dist 'domain-checker-windows-amd64.exe'
    Push-Location $root
    try {
        go build -trimpath -ldflags='-s -w' -o $exe ./cmd/checker
        if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
    } finally {
        Pop-Location
    }
    "built $exe"
} finally {
    Remove-Item Env:CGO_ENABLED, Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
}
