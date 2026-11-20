# Trusted source-free setup; no source credentials are available in this step.
$ErrorActionPreference = 'Stop'
$arch = switch ($env:CI_PLATFORM) {
    'windows-x64' { 'x64' }
    'windows-arm64' { 'arm64' }
    default { throw 'Unsupported MSVC platform' }
}
if ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString().ToLowerInvariant() -ne $arch) {
    throw 'MSVC setup requires the native host architecture'
}
if ($arch -eq 'arm64') {
    # Catalogue R15: native ARM64 uses the installed Microsoft Developer shell.
    $vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio/Installer/vswhere.exe'
    $vs = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.ARM64 -property installationPath
    if ($LASTEXITCODE -ne 0 -or -not $vs) { throw 'Native ARM64 Visual Studio not found' }
    $launch = Join-Path $vs 'Common7/Tools/Launch-VsDevShell.ps1'
    & $launch -Arch arm64 -HostArch arm64 -SkipAutomaticLocation
}
if (-not $env:VCToolsInstallDir) { throw 'MSVC tools environment unavailable' }
$bin = Join-Path $env:VCToolsInstallDir "bin/Host$arch/$arch"
$link = Join-Path $bin 'link.exe'
if (-not (Test-Path -LiteralPath $link -PathType Leaf)) { throw 'Native MSVC linker missing' }
# Check PE machine, not just a filename or an architecture label.
$bytes = [IO.File]::ReadAllBytes($link)
$pe = [BitConverter]::ToInt32($bytes, 0x3c)
$machine = [BitConverter]::ToUInt16($bytes, $pe + 4)
$expected = if ($arch -eq 'arm64') { 0xaa64 } else { 0x8664 }
if ($machine -ne $expected) { throw 'MSVC linker machine differs from native host' }
$env:PATH = "$bin;$env:PATH"
$selected = (Get-Command link.exe -CommandType Application | Select-Object -First 1).Source
if ($selected -ine $link) { throw 'link resolves outside native MSVC tools' }
$target = if ($arch -eq 'arm64') { 'AARCH64' } else { 'X86_64' }
$values = @{
    'CI_MSVC_BIN' = $bin
    "CARGO_TARGET_${target}_PC_WINDOWS_MSVC_LINKER" = $link
    'PATH' = $env:PATH
    'LIB' = $env:LIB
    'LIBPATH' = $env:LIBPATH
    'INCLUDE' = $env:INCLUDE
}
foreach ($entry in $values.GetEnumerator()) {
    if (-not $entry.Value -or $entry.Value -match "[\r\n]") { throw 'Invalid MSVC build value' }
    "$($entry.Key)=$($entry.Value)" | Out-File -FilePath $env:GITHUB_ENV -Encoding utf8 -Append
}
# GITHUB_PATH precedes ambient PATH in subsequent steps. Bash also explicitly
# prepends this directory because Git Bash may prepend its own usr/bin at startup.
$bin | Out-File -FilePath $env:GITHUB_PATH -Encoding utf8 -Append
Write-Output "MSVC native $arch linker: $link; PE machine: $machine"
Write-Output 'STAGE native-msvc-preparation PASS'
