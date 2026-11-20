# Called only in public tool staging, before any synthetic source credential.
function Invoke-PublicTool([string]$Tool, [string[]]$Arguments, [int]$Seconds = 30) {
  $info = [System.Diagnostics.ProcessStartInfo]::new($Tool)
  $info.UseShellExecute = $false
  $info.RedirectStandardOutput = $true
  $info.RedirectStandardError = $true
  foreach ($argument in $Arguments) { $info.ArgumentList.Add($argument) }
  $proc = [System.Diagnostics.Process]::new()
  $proc.StartInfo = $info
  try {
    $null = $proc.Start()
    $stdout = $proc.StandardOutput.ReadToEndAsync()
    $stderr = $proc.StandardError.ReadToEndAsync()
    if (-not $proc.WaitForExit($Seconds * 1000)) { $proc.Kill($true); $proc.WaitForExit(); throw 'public tool deadline' }
    $out = $stdout.GetAwaiter().GetResult()
    $err = $stderr.GetAwaiter().GetResult()
    if ($out.Length -gt 1MB -or $err.Length -gt 64KB) { throw 'public tool output bound' }
    return @{ Code=$proc.ExitCode; Out=$out; Err=$err }
  } finally { $proc.Dispose() }
}
function Select-PublicSignatureTool([string]$Git, [string]$Base, [string]$Arch) {
  $control = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '../..')).Path
  $raw = Invoke-PublicTool $Git @('-C', $control, 'cat-file', 'commit', 'HEAD')
  $allowed = Invoke-PublicTool $Git @('-C', $control, 'show', 'HEAD:docs/commit-signers')
  if ($raw.Code -ne 0 -or $allowed.Code -ne 0) { throw 'public signer fixture' }
  $signature = [System.Text.StringBuilder]::new()
  $inside = $false
  foreach ($line in $raw.Out.Split("`n")) {
    if ($line.StartsWith('gpgsig ')) { $inside=$true; $null=$signature.AppendLine($line.Substring(7)); continue }
    if ($inside) {
      if (-not $line.StartsWith(' ')) { break }
      $null=$signature.AppendLine($line.Substring(1))
    }
  }
  if (-not $signature.ToString().StartsWith('-----BEGIN SSH SIGNATURE-----')) { throw 'public SSH signature fixture' }
  $signers = Join-Path $Base 'public allowed signers'
  $sig = Join-Path $Base 'public signature'
  $utf8 = [System.Text.UTF8Encoding]::new($false)
  [System.IO.File]::WriteAllText($signers, $allowed.Out, $utf8)
  [System.IO.File]::WriteAllText($sig, $signature.ToString().Replace("`r`n", "`n"), $utf8)
  Write-Host ('PUBLIC_SIGNERS cr=' + @($allowed.Out.ToCharArray() | Where-Object { $_ -eq "`r" }).Count + ' bytes=' + (Get-Item -LiteralPath $signers).Length + ' space-path=true argv=ArgumentList')
  $time = Invoke-PublicTool $Git @('-C', $control, 'show', '-s', '--format=%ct', 'HEAD')
  if ($time.Code -ne 0) { throw 'public signature time' }
  $verifyTime = [DateTimeOffset]::FromUnixTimeSeconds([long]$time.Out.Trim()).UtcDateTime.ToString('yyyyMMddHHmmss')
  $arguments = @('-vvv','-Y','find-principals','-f',$signers,'-s',$sig,('-Overify-time=' + $verifyTime))
  $system = Join-Path $env:SystemRoot 'System32/OpenSSH/ssh-keygen.exe'
  $systemSSH = Join-Path $env:SystemRoot 'System32/OpenSSH/ssh.exe'
  $version = Invoke-PublicTool $systemSSH @('-V')
  Write-Host ('PUBLIC_OPENSSH system ' + $version.Out + $version.Err)
  $probe = Invoke-PublicTool $system $arguments
  Write-Host ('PUBLIC_FIND_PRINCIPALS system exit=' + $probe.Code + "`n" + $probe.Out + $probe.Err)
  if ($probe.Code -eq 0 -and $probe.Out.Trim()) { return $system }
  # Full PortableGit is required: the pinned MinGit ZIP excludes ssh-keygen.
  $assets = @{
    X64 = @('PortableGit-2.56.1-64-bit.7z.exe','9dd04a608ceb5f6ab32a73709e0ecb159d211da71b697c45ff81f6dd0c6b49c7')
    Arm64 = @('PortableGit-2.56.1-arm64.7z.exe','0dca0d9f760924a7e6752235d7dd6eb1a9b508ee714b6ed15d89252e5a58c5c9')
  }
  if (-not $assets.ContainsKey($Arch)) { throw 'portable signature architecture' }
  $asset = $assets[$Arch]
  $archive = Join-Path $Base 'portable.7z.exe'
  $tree = Join-Path $Base 'portable'
  try {
    Invoke-WebRequest -Uri ('https://github.com/git-for-windows/git/releases/download/v2.56.1.windows.1/' + $asset[0]) -OutFile $archive -TimeoutSec 180
    if ((Get-Item -LiteralPath $archive).Length -gt 128MB -or (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $asset[1]) { throw 'portable signature digest' }
    $extract = Invoke-PublicTool $archive @('-y', ('-o' + $tree)) 180
    if ($extract.Code -ne 0) { throw 'portable signature extraction' }
    foreach ($entry in @(Get-ChildItem -LiteralPath $tree -Recurse -Force)) {
      if ($entry.Attributes -band [System.IO.FileAttributes]::ReparsePoint) { throw 'portable signature reparse' }
    }
    $bundled = Join-Path $tree 'usr/bin/ssh-keygen.exe'
    $bundledSSH = Join-Path $tree 'usr/bin/ssh.exe'
    $version = Invoke-PublicTool $bundledSSH @('-V')
    Write-Host ('PUBLIC_OPENSSH bundled ' + $version.Out + $version.Err)
    $probe = Invoke-PublicTool $bundled $arguments
    Write-Host ('PUBLIC_FIND_PRINCIPALS bundled exit=' + $probe.Code + "`n" + $probe.Out + $probe.Err)
    if ($probe.Code -ne 0 -or -not $probe.Out.Trim()) { throw 'public bundled find-principals failed' }
    return $bundled
  } finally {
    if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force }
  }
}
