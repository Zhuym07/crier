param([string]$Go = 'go', [string]$ISCC = 'ISCC')
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
  $version = (Get-Content VERSION -Raw).Trim()
  if ($version -notmatch '^\d+\.\d+\.\d+$') { throw "VERSION must be x.y.z, got '$version'" }
  $env:CGO_ENABLED = '0'
  $env:GOOS = 'windows'
  $env:GOARCH = 'amd64'
  New-Item -ItemType Directory -Path build -Force | Out-Null
  & $Go build -trimpath -tags production -ldflags "-s -w -H windowsgui -X github.com/Zhuym07/crier/internal/version.Value=$version" -o build/Crier.exe .
  if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed' }
  & $Go build -trimpath -ldflags '-s -w' -o build/crier-email.exe ./cmd/crier-email
  if ($LASTEXITCODE -ne 0) { throw 'Email plugin build failed' }
  & $Go build -trimpath -ldflags '-s -w' -o build/crier-telegram.exe ./cmd/crier-telegram
  if ($LASTEXITCODE -ne 0) { throw 'Telegram plugin build failed' }
  $iss = Join-Path $PSScriptRoot ('installer/crier-' + $version + '.iss')
  $text = (Get-Content installer/crier.iss -Raw).Replace('AppVersion=0.0.0', "AppVersion=$version")
  if ($text -notmatch "AppVersion=$([regex]::Escape($version))") { throw 'Installer version was not stamped' }
  [IO.File]::WriteAllText($iss, $text)
  & $ISCC $iss
  if ($LASTEXITCODE -ne 0) { throw 'Installer build failed' }
  Remove-Item $iss -ErrorAction SilentlyContinue
  $hash = (Get-FileHash dist/Crier-Setup-x64.exe -Algorithm SHA256).Hash.ToLower()
  "$hash  Crier-Setup-x64.exe`n" | Set-Content -NoNewline -Encoding ascii dist/SHA256SUMS
} finally { Pop-Location }
