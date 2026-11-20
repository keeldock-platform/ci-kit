$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
# A pinned public distribution lives under an ancestry already accepted by the
# native ACL gate. Neither the installed Git ACLs nor the gate are changed.
$arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
$assets = @{
  X64 = @('MinGit-2.56.1-64-bit.zip', '4a92850b6e917da4fd010669b614b55dccf5b0ec333a534cc4be4700dfefdafd', 0x8664)
  Arm64 = @('MinGit-2.56.1-arm64.zip', '96571e442e90523a90e5fd8661b4c4e43bc192910f3c6db938816e6955b4cc8c', 0xaa64)
}
if (-not $assets.ContainsKey($arch)) { throw 'unsupported public tool architecture' }
$asset = $assets[$arch]
$identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().User
function Set-UserOnly([string]$Path, [bool]$Directory) {
  if ($Directory) {
    $acl = [System.Security.AccessControl.DirectorySecurity]::new()
    $rule = [System.Security.AccessControl.FileSystemAccessRule]::new($identity, 'FullControl', 'ContainerInherit, ObjectInherit', 'None', 'Allow')
  } else {
    $acl = [System.Security.AccessControl.FileSecurity]::new()
    $rule = [System.Security.AccessControl.FileSystemAccessRule]::new($identity, 'FullControl', 'Allow')
  }
  $acl.SetAccessRuleProtection($true, $false)
  $acl.SetOwner($identity)
  $acl.AddAccessRule($rule)
  Set-Acl -LiteralPath $Path -AclObject $acl
}
$base = Join-Path $env:SystemRoot ('System32\ci-kit-' + [guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $base
Set-UserOnly $base $true
$archive = Join-Path $base 'public.zip'
$tree = Join-Path $base 'tool'
$null = New-Item -ItemType Directory -Path $tree
Set-UserOnly $tree $true
$ownedConfig = $false
$configDir = ''
try {
  Invoke-WebRequest -Uri ('https://github.com/git-for-windows/git/releases/download/v2.56.1.windows.1/' + $asset[0]) -OutFile $archive -TimeoutSec 180
  if ((Get-Item -LiteralPath $archive).Length -gt 256MB -or (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $asset[1]) { throw 'public archive digest' }
  $zip = [System.IO.Compression.ZipFile]::OpenRead($archive)
  $expected = @{}
  $total = 0L
  try {
    if ($zip.Entries.Count -gt 20000) { throw 'archive count' }
    foreach ($entry in $zip.Entries) {
      $name = $entry.FullName
      if ($name -match '\\|:|[\x00-\x1f]' -or $name.StartsWith('/') -or ($name.Split('/') | Where-Object { $_ -eq '..' -or $_ -eq '.' }) -or (($entry.ExternalAttributes -shr 16) -band 0xf000) -eq 0xa000 -or ($entry.ExternalAttributes -band 0x400)) { throw 'archive path or link' }
      $target = [System.IO.Path]::GetFullPath((Join-Path $tree $name))
      if (-not $target.StartsWith($tree + '\', [System.StringComparison]::OrdinalIgnoreCase)) { throw 'archive escape' }
      if ($expected.ContainsKey($name)) { throw 'archive duplicate' }
      $expected[$name] = $null
      $total += $entry.Length
      if ($entry.Length -gt 128MB -or $total -gt 512MB) { throw 'archive size' }
      if ($name.EndsWith('/')) { $null = [System.IO.Directory]::CreateDirectory($target); continue }
      $null = [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($target))
      $inputStream = $entry.Open()
      $outputStream = [System.IO.File]::Open($target, [System.IO.FileMode]::CreateNew)
      try { $inputStream.CopyTo($outputStream) } finally { $inputStream.Dispose(); $outputStream.Dispose() }
      $inputStream = $entry.Open()
      try { $expected[$name] = [Convert]::ToHexString([System.Security.Cryptography.SHA256]::HashData($inputStream)) } finally { $inputStream.Dispose() }
      if ((Get-FileHash -LiteralPath $target).Hash -ne $expected[$name]) { throw 'archive bytes' }
    }
  } finally { $zip.Dispose() }
  $files = @(Get-ChildItem -LiteralPath $tree -Recurse -File)
  $expectedFiles = @($expected.Keys | Where-Object { -not $_.EndsWith('/') })
  if ($files.Count -ne $expectedFiles.Count) { throw 'archive tree' }
  foreach ($file in $files) {
    if ($file.Attributes -band [System.IO.FileAttributes]::ReparsePoint) { throw 'archive reparse' }
    $relative = [System.IO.Path]::GetRelativePath($tree, $file.FullName).Replace('\', '/')
    if (-not $expected.ContainsKey($relative) -or (Get-FileHash -LiteralPath $file.FullName).Hash -ne $expected[$relative]) { throw 'archive tree bytes' }
  }
  foreach ($dir in @(Get-ChildItem -LiteralPath $tree -Recurse -Directory)) {
    if ($dir.Attributes -band [System.IO.FileAttributes]::ReparsePoint) { throw 'archive directory reparse' }
  }
  $git = Join-Path $tree 'cmd\git.exe'
  $pe = [System.IO.File]::ReadAllBytes($git)
  if ($pe.Length -lt 256 -or $pe[0] -ne 0x4d -or $pe[1] -ne 0x5a) { throw 'native public tool' }
  $offset = [BitConverter]::ToInt32($pe, 0x3c)
  if ($offset -lt 0 -or $offset + 6 -gt $pe.Length -or [BitConverter]::ToUInt32($pe, $offset) -ne 0x4550 -or [BitConverter]::ToUInt16($pe, $offset + 4) -ne $asset[2]) { throw 'native public tool architecture' }
  $configDir = Join-Path ([Environment]::GetFolderPath('ApplicationData')) 'KeelDock'
  if (Test-Path -LiteralPath $configDir) { throw 'existing machine policy' }
  $null = New-Item -ItemType Directory -Path $configDir
  $ownedConfig = $true
  Set-UserOnly $configDir $true
  $configFile = Join-Path $configDir 'kd-ci-machine.json'
  @{ schema_version=1; local_policy='developer-machine'; allowed_local_concerns=@(); source_roots=@{}; tools=@{git=$git}; planner_directory=''; kit_root='' } | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $configFile -Encoding utf8NoBOM
  Set-UserOnly $configFile $false
  'CI_OWNED_WINDOWS_TOOL=' + $base | Out-File -FilePath $env:GITHUB_ENV -Encoding utf8 -Append
  'STAGE windows-tool-staging PASS'
} catch {
  if ($ownedConfig) { Remove-Item -LiteralPath $configDir -Recurse -Force }
  Remove-Item -LiteralPath $base -Recurse -Force
  throw
} finally {
  if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force }
}
