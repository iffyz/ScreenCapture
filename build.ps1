# Build ScreenCapture (patched): yoga -> Ling -> ScreenCapture
#
# ASCII only. PowerShell 5.1 reads .ps1 as ANSI when there is no BOM, and any
# non-ASCII comment would corrupt the parser.
#
# Works from any directory: every path is derived from the script location, so the
# same file is used on this machine and inside GitHub Actions.

param(
    [string]$Configuration = "Release",
    [string]$Platform = "x64"
)

$ErrorActionPreference = "Stop"

# The parent shell may hand down an environment block that contains both "http_proxy"
# and "HTTP_PROXY". MSBuild turns that block into a case-insensitive dictionary and
# dies with MSB6001 "an item with the same key has already been added".
foreach ($n in @("http_proxy", "https_proxy", "all_proxy", "no_proxy")) {
    try { [Environment]::SetEnvironmentVariable($n, $null, "Process") } catch { }
}

$ROOT = $PSScriptRoot
$LING = Join-Path $ROOT "ling"
$SRC  = Join-Path $ROOT "src\Src"
$LOG  = Join-Path $ROOT "build.log"

function Find-MSBuild {
    if ($env:MSBUILD_EXE_PATH -and (Test-Path $env:MSBUILD_EXE_PATH)) { return $env:MSBUILD_EXE_PATH }

    $vswhere = Join-Path ${env:ProgramFiles(x86)} "Microsoft Visual Studio\Installer\vswhere.exe"
    if (Test-Path $vswhere) {
        $found = & $vswhere -latest -products * -requires Microsoft.Component.MSBuild `
            -find "MSBuild\**\Bin\MSBuild.exe" 2>$null | Select-Object -First 1
        if ($found -and (Test-Path $found)) { return $found }
    }

    foreach ($guess in @(
        "C:\BuildTools2022\MSBuild\Current\Bin\MSBuild.exe",
        (Join-Path ${env:ProgramFiles} "Microsoft Visual Studio\2022\BuildTools\MSBuild\Current\Bin\MSBuild.exe"),
        (Join-Path ${env:ProgramFiles} "Microsoft Visual Studio\2022\Community\MSBuild\Current\Bin\MSBuild.exe"),
        (Join-Path ${env:ProgramFiles} "Microsoft Visual Studio\2022\Professional\MSBuild\Current\Bin\MSBuild.exe"),
        (Join-Path ${env:ProgramFiles} "Microsoft Visual Studio\2022\Enterprise\MSBuild\Current\Bin\MSBuild.exe")
    )) {
        if (Test-Path $guess) { return $guess }
    }

    $cmd = Get-Command msbuild -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    throw "MSBuild.exe not found. Install Visual Studio 2022 Build Tools with the C++ workload."
}

$MSBUILD = Find-MSBuild
Write-Output "MSBuild: $MSBUILD"
Write-Output "Root   : $ROOT"

Remove-Item $LOG -ErrorAction SilentlyContinue

function Build($proj, $solutionDir, $tag) {
    if (-not (Test-Path $proj)) { throw "project not found: $proj" }
    Write-Output "=== build $tag ==="
    $msbArgs = @(
        $proj,
        "/p:Configuration=$Configuration",
        "/p:Platform=$Platform",
        "/p:SolutionDir=$solutionDir",
        "/m",
        "/nologo",
        "/v:m",
        "/p:CL_MPCount=4"
    )
    & $MSBUILD $msbArgs 2>&1 | Tee-Object -FilePath $LOG -Append
    if ($LASTEXITCODE -ne 0) { throw ("build failed: " + $tag + " (exit " + $LASTEXITCODE + ")") }
    Write-Output "=== ok $tag ==="
}

Build (Join-Path $LING "yoga\yoga.vcxproj") ($LING + "\") "yoga"
Build (Join-Path $LING "Ling.vcxproj")      ($LING + "\") "Ling"
Build (Join-Path $SRC  "ScreenCapture.vcxproj") ($SRC + "\") "ScreenCapture"

Write-Output "=== output ==="
Get-ChildItem (Join-Path $SRC "$Platform\$Configuration") -File -ErrorAction SilentlyContinue |
    Where-Object { $_.Extension -in ".exe", ".dll" } | Select-Object Name, Length
Get-ChildItem (Join-Path $LING "$Platform\$Configuration") -File -ErrorAction SilentlyContinue |
    Where-Object { $_.Extension -in ".lib", ".dll" } | Select-Object Name, Length
