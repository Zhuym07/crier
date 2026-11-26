param([string]$Go = 'go', [string]$ISCC = 'ISCC')
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
  $env:CGO_ENABLED = '0'
  $env:GOOS = 'windows'
  $env:GOARCH = 'amd64'
  New-Item -ItemType Directory -Path build -Force | Out-Null
  & $Go build -trimpath -tags production -ldflags '-s -w -H windowsgui' -o build/Crier.exe .
  if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed' }
  & $Go build -trimpath -ldflags '-s -w' -o build/crier-email.exe ./cmd/crier-email
  if ($LASTEXITCODE -ne 0) { throw 'Email plugin build failed' }
  & $Go build -trimpath -ldflags '-s -w' -o build/crier-telegram.exe ./cmd/crier-telegram
  if ($LASTEXITCODE -ne 0) { throw 'Telegram plugin build failed' }
  & $ISCC installer/crier.iss
  if ($LASTEXITCODE -ne 0) { throw 'Installer build failed' }
} finally { Pop-Location }
