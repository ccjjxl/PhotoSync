# Build the desktop client on Windows (amd64).
$ErrorActionPreference = "Stop"
$version = if ($env:VERSION) { $env:VERSION } else { "1.0.0" }
New-Item -ItemType Directory -Force -Path dist | Out-Null
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w -X main.version=$version" -o dist/sync-client.exe .
Write-Host "已生成 dist\sync-client.exe"
