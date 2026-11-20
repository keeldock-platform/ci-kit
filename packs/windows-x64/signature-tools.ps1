# Called only in public tool staging, before any private source credential.
function Invoke-PublicTool([string]$Tool, [string[]]$Arguments, [int]$Seconds = 30, [string]$ConfinedGit = '', [bool]$ClosedInput = $false) {
  $info = [System.Diagnostics.ProcessStartInfo]::new($Tool)
  $info.UseShellExecute = $false
  $info.RedirectStandardOutput = $true
  $info.RedirectStandardError = $true
  $info.RedirectStandardInput = $ClosedInput
  foreach ($argument in $Arguments) { $info.ArgumentList.Add($argument) }
  if ($ConfinedGit) {
    # Match internal/process.Environment for the public Git verification probe.
    $allowed = @('PATH','HOME','USERPROFILE','SYSTEMROOT','WINDIR','TMP','TEMP','TMPDIR','LANG','LC_ALL','TZ','CARGO_HOME','RUSTUP_HOME','CARGO_TARGET_DIR','GOCACHE','GOMODCACHE','GOPATH','JAVA_HOME','ANDROID_HOME','ANDROID_SDK_ROOT','DEVELOPER_DIR','SDKROOT','CI','NO_COLOR','TERM')
    $info.Environment.Clear()
    foreach ($entry in [Environment]::GetEnvironmentVariables().GetEnumerator()) {
      if ($entry.Key.ToUpperInvariant() -in $allowed) { $info.Environment[$entry.Key.ToUpperInvariant()] = $entry.Value }
    }
    $info.Environment['PATH'] = Split-Path -Parent $ConfinedGit
    $info.Environment['GIT_CONFIG_NOSYSTEM'] = '1'
    $info.Environment['GIT_CONFIG_GLOBAL'] = 'NUL'
    $info.Environment['GIT_TERMINAL_PROMPT'] = '0'
  }
  $proc = [System.Diagnostics.Process]::new()
  $proc.StartInfo = $info
  $started = $false
  try {
    try { $null = $proc.Start(); $started = $true } catch {
      return @{ Code=-1; Out=''; Err=('public native start failed: ' + $_.Exception.Message) }
    }
    if ($ClosedInput) { $proc.StandardInput.Close() }
    $streams = @(
      @{ Reader=$proc.StandardOutput; Buffer=[char[]]::new(4096); Text=[System.Text.StringBuilder]::new(); Limit=1MB; Done=$false },
      @{ Reader=$proc.StandardError; Buffer=[char[]]::new(4096); Text=[System.Text.StringBuilder]::new(); Limit=64KB; Done=$false }
    )
    foreach ($stream in $streams) { $stream.Task = $stream.Reader.ReadAsync($stream.Buffer, 0, $stream.Buffer.Length) }
    $clock = [System.Diagnostics.Stopwatch]::StartNew()
    while (-not $proc.HasExited -or -not $streams[0].Done -or -not $streams[1].Done) {
      if ($clock.Elapsed.TotalSeconds -ge $Seconds) { throw 'public tool deadline' }
      foreach ($stream in $streams) {
        if ($stream.Done -or -not $stream.Task.IsCompleted) { continue }
        $count = $stream.Task.GetAwaiter().GetResult()
        if ($count -eq 0) { $stream.Done=$true; continue }
        if ($stream.Text.Length + $count -gt $stream.Limit) { throw 'public tool output bound' }
        $null = $stream.Text.Append($stream.Buffer, 0, $count)
        $stream.Task = $stream.Reader.ReadAsync($stream.Buffer, 0, $stream.Buffer.Length)
      }
      Start-Sleep -Milliseconds 10
    }
    return @{ Code=$proc.ExitCode; Out=$streams[0].Text.ToString(); Err=$streams[1].Text.ToString() }
  } finally {
    if ($started -and -not $proc.HasExited) { $proc.Kill($true) }
    $proc.Dispose()
  }
}
function Test-PublicSignatureCandidate([string]$Tool, [string]$Git, [string]$Control, [string]$Signers) {
  if (-not (Test-Path -LiteralPath $Tool -PathType Leaf)) { return $false }
  # Verify the pinned public kit commit through the exact captured Git environment.
  # Only the bounded result selects the tool; exploratory traces are not published.
  $verify = Invoke-PublicTool $Git @('--no-replace-objects','-c','core.hooksPath=NUL','-c','core.fsmonitor=false','-c','credential.helper=','-c','diff.external=','-C',$Control,'-c','gpg.format=ssh','-c',('gpg.ssh.program=' + $Tool),'-c','gpg.program=false','-c','gpg.x509.program=false','-c',('gpg.ssh.allowedSignersFile=' + $Signers),'-c','gpg.ssh.revocationFile=','-c','gpg.minTrustLevel=fully','verify-commit','HEAD') 30 $Git
  return ($verify.Code -eq 0)
}
function Select-PublicSignatureTool([string]$Git, [string]$Base, [string]$Arch) {
  $control = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '../..')).Path
  $allowed = Invoke-PublicTool $Git @('-C', $control, 'show', 'HEAD:docs/commit-signers') 30 $Git
  if ($allowed.Code -ne 0) { throw 'public signer policy unavailable' }
  $signers = Join-Path $Base 'public allowed signers'
  [System.IO.File]::WriteAllText($signers, $allowed.Out, [System.Text.UTF8Encoding]::new($false))
  $system = Join-Path $env:SystemRoot 'System32/OpenSSH/ssh-keygen.exe'
  if (Test-PublicSignatureCandidate $system $Git $control $signers) { return $system }
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
    # Extract the verified archive as data. The SFX launcher also runs a GUI
    # post-install command, which failed in the source-free hosted experiment.
    $extractor = Join-Path $env:ProgramFiles '7-Zip/7z.exe'
    $extract = Invoke-PublicTool $extractor @('x', $archive, ('-o' + $tree), '-y', '-bb0', '-bso0', '-bsp0') 180
    if ($extract.Code -ne 0) { throw 'portable signature extraction' }
    foreach ($entry in @(Get-ChildItem -LiteralPath $tree -Recurse -Force)) {
      if ($entry.Attributes -band [System.IO.FileAttributes]::ReparsePoint) { throw 'portable signature reparse' }
    }
    $bundled = Join-Path $tree 'usr/bin/ssh-keygen.exe'
    if (-not (Test-PublicSignatureCandidate $bundled $Git $control $signers)) { throw 'public bundled signature verification failed' }
    return $bundled
  } finally {
    if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force }
  }
}
