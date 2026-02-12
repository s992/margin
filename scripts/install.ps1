#!/usr/bin/env pwsh
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Source = 'release'
$Version = ''
$TargetEnv = 'auto'
$TargetArch = 'auto'
$MarginRoot = ''
$CliPath = ''
$SublimeInstalledPackagesDir = ''
$SublimeUserDir = ''
$PluginMode = 'package'
$Yes = $false
$DryRun = $false
$GithubRepo = if ($env:MARGIN_INSTALL_REPO) { $env:MARGIN_INSTALL_REPO } else { '' }

$MarginRootExplicit = $false
$CliPathExplicit = $false
$InstalledExplicit = $false
$UserDirExplicit = $false

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = Resolve-Path (Join-Path $ScriptDir '..')

function Log([string]$Message) {
  Write-Host "margin-install: $Message"
}

function Fail([string]$Message) {
  throw "margin-install: ERROR: $Message"
}

function Show-Usage {
  @"
Usage: install.ps1 [options]

Options:
  --source release|local|auto
  --version <tag>
  --target-env auto|linux|macos|windows
  --target-arch auto|amd64|arm64
  --margin-root <path>
  --cli-path <path>
  --sublime-installed-packages-dir <path>
  --sublime-user-dir <path>
  --plugin-mode package|unpacked
  --github-repo <owner/repo>
  --yes
  --dry-run
  -h, --help
"@
}

function Resolve-HostEnv {
  if ($IsWindows) { return 'windows' }
  if ($IsLinux) { return 'linux' }
  if ($IsMacOS) { return 'macos' }
  Fail "unsupported host platform"
}

function Resolve-HostArch {
  switch -Regex ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()) {
    '^X64$' { 'amd64'; break }
    '^Arm64$' { 'arm64'; break }
    default { Fail "unsupported architecture: $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)" }
  }
}

function Resolve-TargetEnv([string]$HostEnv) {
  if ($TargetEnv -ne 'auto') {
    return $TargetEnv
  }
  return $HostEnv
}

function Resolve-TargetArch([string]$HostArch) {
  if ($TargetArch -eq 'auto') { return $HostArch }
  return $TargetArch
}

function Normalize-PathForTarget([string]$Target, [string]$PathValue) {
  if ($Target -eq 'windows') {
    return $PathValue
  }
  return $PathValue -replace '\\', '/'
}

function Default-MarginRoot([string]$Target) {
  switch ($Target) {
    'windows' {
      if (-not $env:APPDATA) { Fail 'APPDATA is required to resolve default Windows margin root' }
      return (Join-Path $env:APPDATA 'Margin')
    }
    'linux' {
      return (Join-Path $HOME '.local/share/margin')
    }
    'macos' {
      return (Join-Path $HOME 'Library/Application Support/Margin')
    }
    default { Fail "unsupported target env: $Target" }
  }
}

function Default-SublimeBase([string]$Target) {
  switch ($Target) {
    'windows' {
      if (-not $env:APPDATA) { Fail 'APPDATA is required to resolve default Windows Sublime path' }
      return (Join-Path $env:APPDATA 'Sublime Text')
    }
    'linux' {
      return (Join-Path $HOME '.config/sublime-text')
    }
    'macos' {
      return (Join-Path $HOME 'Library/Application Support/Sublime Text')
    }
    default { Fail "unsupported target env: $Target" }
  }
}

function Resolve-RepoDefault {
  if ($GithubRepo) { return $GithubRepo }
  try {
    $origin = git -C $RepoRoot remote get-url origin 2>$null
    if ($origin -match 'github\.com[:/]([^/]+/[^/.]+)(\.git)?$') {
      return $Matches[1]
    }
  } catch {
  }
  return 's992/margin'
}

function Resolve-LatestTag([string]$Repo) {
  $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
  if (-not $release.tag_name) { Fail "failed to determine latest release for $Repo" }
  return [string]$release.tag_name
}

function Resolve-AssetName([string]$VersionNoV, [string]$Target, [string]$Arch) {
  $goos = switch ($Target) {
    'linux' { 'linux' }
    'macos' { 'darwin' }
    'windows' { 'windows' }
    default { Fail "unsupported target env: $Target" }
  }
  $ext = if ($Target -eq 'windows') { 'zip' } else { 'tar.gz' }
  return "margin_${VersionNoV}_${goos}_${Arch}.${ext}"
}

function Download-ReleaseAsset([string]$Repo, [string]$Tag, [string]$AssetName, [string]$OutPath) {
  Invoke-WebRequest -Uri "https://github.com/$Repo/releases/download/$Tag/$AssetName" -OutFile $OutPath
}

function Get-Sha256([string]$PathValue) {
  return (Get-FileHash -Algorithm SHA256 -Path $PathValue).Hash.ToLowerInvariant()
}

function Get-ChecksumForAsset([string]$ChecksumsPath, [string]$AssetName) {
  if (-not (Test-Path $ChecksumsPath)) { return $null }
  $pattern = "^(?<hash>[0-9a-fA-F]{64})\s+\*?(?<name>.+)$"
  foreach ($line in (Get-Content -Path $ChecksumsPath)) {
    if ($line -notmatch $pattern) { continue }
    if ($Matches['name'] -eq $AssetName) {
      return $Matches['hash'].ToLowerInvariant()
    }
  }
  return $null
}

function Build-LocalCli([string]$Target, [string]$Arch, [string]$OutPath) {
  $goos = switch ($Target) {
    'linux' { 'linux' }
    'macos' { 'darwin' }
    'windows' { 'windows' }
    default { Fail "unsupported target env: $Target" }
  }
  Log "building local CLI for $goos/$Arch"
  if ($DryRun) { return }
  Push-Location (Join-Path $RepoRoot 'cli-go')
  try {
    $env:CGO_ENABLED = '0'
    $env:GOOS = $goos
    $env:GOARCH = $Arch
    go build -o $OutPath ./cmd/margin
  } finally {
    Pop-Location
  }
}

function Build-LocalPluginPackage([string]$OutPath) {
  if ($DryRun) { return }
  Add-Type -AssemblyName System.IO.Compression.FileSystem
  $sourceDir = Join-Path (Join-Path $RepoRoot 'sublime-plugin') 'Margin'
  if (Test-Path $OutPath) { Remove-Item -Force $OutPath }
  [System.IO.Compression.ZipFile]::CreateFromDirectory($sourceDir, $OutPath)
}

function Install-File([string]$Src, [string]$Dst) {
  if ($DryRun) {
    Log "dry-run: install $Src -> $Dst"
    return
  }
  $parent = Split-Path -Parent $Dst
  New-Item -ItemType Directory -Path $parent -Force | Out-Null
  Copy-Item -Path $Src -Destination $Dst -Force
}

function Install-UnpackedPlugin([string]$PluginPackagePath, [string]$Dst) {
  if ($DryRun) {
    Log "dry-run: install unpacked plugin -> $Dst"
    return
  }
  $extractDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Path $extractDir -Force | Out-Null
  try {
    Expand-Archive -Path $PluginPackagePath -DestinationPath $extractDir -Force
    $extracted = $extractDir
    $legacyExtracted = Join-Path $extractDir 'Margin'
    if (Test-Path $legacyExtracted) {
      $extracted = $legacyExtracted
    }
    if (-not (Test-Path (Join-Path $extracted 'margin.py'))) {
      Fail 'plugin package is missing margin.py at package root'
    }
    if (Test-Path $Dst) { Remove-Item -Path $Dst -Recurse -Force }
    New-Item -ItemType Directory -Path $Dst -Force | Out-Null
    Copy-Item -Path (Join-Path $extracted '*') -Destination $Dst -Recurse -Force
  } finally {
    if (Test-Path $extractDir) { Remove-Item -Path $extractDir -Recurse -Force }
  }
}

function Merge-Settings([string]$SettingsPath, [string]$CliValue, [string]$RootValue) {
  if ($DryRun) {
    Log "dry-run: merge settings at $SettingsPath"
    return
  }
  New-Item -ItemType Directory -Path (Split-Path -Parent $SettingsPath) -Force | Out-Null
  $obj = @{}
  if (Test-Path $SettingsPath) {
    $raw = Get-Content -Raw -Path $SettingsPath
    if ($raw.Trim().Length -gt 0) {
      $parsed = $raw | ConvertFrom-Json -AsHashtable
      if (-not ($parsed -is [hashtable])) {
        Fail "existing settings file must be a JSON object: $SettingsPath"
      }
      $obj = $parsed
    }
  }
  $obj['margin_cli_path'] = $CliValue
  if ($RootValue) {
    $obj['margin_root'] = $RootValue
  }
  $obj | ConvertTo-Json -Depth 10 | Set-Content -Path $SettingsPath -Encoding UTF8
}

function Get-RequiredArgValue([System.Collections.Generic.List[string]]$ArgList, [int]$Index, [string]$Flag) {
  if (($Index + 1) -ge $ArgList.Count) {
    Fail "missing value for $Flag"
  }
  return $ArgList[$Index + 1]
}

$argsList = [System.Collections.Generic.List[string]]::new()
foreach ($argValue in $args) {
  $argsList.Add([string]$argValue)
}
$i = 0
while ($i -lt $argsList.Count) {
  $arg = $argsList[$i]
  switch ($arg) {
    '--source' { $Source = Get-RequiredArgValue $argsList $i '--source'; $i += 2; continue }
    '--version' { $Version = Get-RequiredArgValue $argsList $i '--version'; $i += 2; continue }
    '--target-env' { $TargetEnv = Get-RequiredArgValue $argsList $i '--target-env'; $i += 2; continue }
    '--target-arch' { $TargetArch = Get-RequiredArgValue $argsList $i '--target-arch'; $i += 2; continue }
    '--margin-root' { $MarginRoot = Get-RequiredArgValue $argsList $i '--margin-root'; $MarginRootExplicit = $true; $i += 2; continue }
    '--cli-path' { $CliPath = Get-RequiredArgValue $argsList $i '--cli-path'; $CliPathExplicit = $true; $i += 2; continue }
    '--sublime-installed-packages-dir' { $SublimeInstalledPackagesDir = Get-RequiredArgValue $argsList $i '--sublime-installed-packages-dir'; $InstalledExplicit = $true; $i += 2; continue }
    '--sublime-user-dir' { $SublimeUserDir = Get-RequiredArgValue $argsList $i '--sublime-user-dir'; $UserDirExplicit = $true; $i += 2; continue }
    '--plugin-mode' { $PluginMode = Get-RequiredArgValue $argsList $i '--plugin-mode'; $i += 2; continue }
    '--github-repo' { $GithubRepo = Get-RequiredArgValue $argsList $i '--github-repo'; $i += 2; continue }
    '--yes' { $Yes = $true; $i += 1; continue }
    '--dry-run' { $DryRun = $true; $i += 1; continue }
    '-h' { Show-Usage; exit 0 }
    '--help' { Show-Usage; exit 0 }
    default { Fail "unknown argument: $arg" }
  }
}

if ($Source -notin @('release', 'local', 'auto')) { Fail "invalid --source: $Source" }
if ($PluginMode -notin @('package', 'unpacked')) { Fail "invalid --plugin-mode: $PluginMode" }

$HostEnv = Resolve-HostEnv
$HostArch = Resolve-HostArch
$ResolvedTargetEnv = Resolve-TargetEnv $HostEnv
$ResolvedTargetArch = Resolve-TargetArch $HostArch

if (-not $MarginRootExplicit) {
  $MarginRoot = Default-MarginRoot $ResolvedTargetEnv
} else {
  $MarginRoot = Normalize-PathForTarget $ResolvedTargetEnv $MarginRoot
}
if (-not $CliPathExplicit) {
  if ($ResolvedTargetEnv -eq 'windows') {
    $CliPath = Join-Path (Join-Path $MarginRoot 'bin') 'margin.exe'
  } else {
    $CliPath = (Join-Path (Join-Path $MarginRoot 'bin') 'margin').Replace('\\', '/')
  }
} else {
  $CliPath = Normalize-PathForTarget $ResolvedTargetEnv $CliPath
}

$sublimeBase = Default-SublimeBase $ResolvedTargetEnv
if (-not $InstalledExplicit) {
  $SublimeInstalledPackagesDir = Join-Path $sublimeBase 'Installed Packages'
}
if (-not $UserDirExplicit) {
  $SublimeUserDir = Join-Path (Join-Path $sublimeBase 'Packages') 'User'
}

$tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null
try {
  $cliArtifact = if ($ResolvedTargetEnv -eq 'windows') { Join-Path $tmpDir 'margin.exe' } else { Join-Path $tmpDir 'margin' }
  $pluginArtifact = Join-Path $tmpDir 'Margin.sublime-package'

  $repo = Resolve-RepoDefault
  $tag = $Version
  if ($Source -ne 'local' -and -not $tag) {
    try {
      $tag = Resolve-LatestTag $repo
    } catch {
      if ($Source -eq 'release') { throw }
    }
  }

  $sourceUsed = $Source
  $releaseOk = $false
  if ($Source -in @('release', 'auto')) {
    if (-not $tag) {
      if ($Source -eq 'release') { Fail "failed to determine latest release for $repo" }
    } else {
      try {
        $assetName = Resolve-AssetName ($tag -replace '^v', '') $ResolvedTargetEnv $ResolvedTargetArch
        $archivePath = Join-Path $tmpDir $assetName
        $checksumsPath = Join-Path $tmpDir 'checksums.txt'
        if (-not $DryRun) {
          Download-ReleaseAsset $repo $tag $assetName $archivePath
          Download-ReleaseAsset $repo $tag 'checksums.txt' $checksumsPath
          Download-ReleaseAsset $repo $tag 'Margin.sublime-package' $pluginArtifact
          $expected = Get-ChecksumForAsset $checksumsPath $assetName
          $pluginExpected = Get-ChecksumForAsset $checksumsPath 'Margin.sublime-package'
          $actual = Get-Sha256 $archivePath
          $pluginActual = Get-Sha256 $pluginArtifact
          if (-not $expected -or $actual -ne $expected) {
            Fail "checksum validation failed for $assetName"
          }
          if (-not $pluginExpected -or $pluginActual -ne $pluginExpected) {
            Fail "checksum validation failed for Margin.sublime-package"
          }
          $extractDir = Join-Path $tmpDir 'extract'
          New-Item -ItemType Directory -Path $extractDir -Force | Out-Null
          if ($archivePath.EndsWith('.zip')) {
            Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force
          } else {
            tar -xzf $archivePath -C $extractDir
          }
          $binaryName = if ($ResolvedTargetEnv -eq 'windows') { 'margin.exe' } else { 'margin' }
          $found = Get-ChildItem -Path $extractDir -Recurse -File | Where-Object { $_.Name -eq $binaryName } | Select-Object -First 1
          if (-not $found) { Fail "failed to find $binaryName in archive" }
          Copy-Item -Path $found.FullName -Destination $cliArtifact -Force
        }
        $releaseOk = $true
        $sourceUsed = 'release'
      } catch {
        if ($Source -eq 'release') { throw }
      }
    }
  }

  if (-not $releaseOk) {
    $sourceUsed = 'local'
    Build-LocalCli $ResolvedTargetEnv $ResolvedTargetArch $cliArtifact
    Build-LocalPluginPackage $pluginArtifact
  }

  Log "host=$HostEnv/$HostArch target=$ResolvedTargetEnv/$ResolvedTargetArch source=$sourceUsed plugin_mode=$PluginMode"
  Log "margin_root=$MarginRoot"
  Log "cli_path=$CliPath"
  Log "installed_packages_dir=$SublimeInstalledPackagesDir"
  Log "user_dir=$SublimeUserDir"

  Install-File $cliArtifact $CliPath
  if ($PluginMode -eq 'package') {
    Install-File $pluginArtifact (Join-Path $SublimeInstalledPackagesDir 'Margin.sublime-package')
  } else {
    Install-UnpackedPlugin $pluginArtifact (Join-Path (Split-Path -Parent $SublimeUserDir) 'Margin')
  }

  $explicitRoot = if ($MarginRootExplicit) { $MarginRoot } else { '' }
  Merge-Settings (Join-Path $SublimeUserDir 'Margin.sublime-settings') $CliPath $explicitRoot

  Log 'installation complete'
} finally {
  if (Test-Path $tmpDir) {
    Remove-Item -Path $tmpDir -Recurse -Force
  }
}
