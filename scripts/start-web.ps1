# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

param(
    [switch]$Gitea,
    [switch]$Restart,
    [string]$Addr = '127.0.0.1:5484',
    [string]$DataDir = ''
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$webExecutable = [System.IO.Path]::GetFullPath((Join-Path $projectRoot 'dist/ocr-web.exe'))
if (-not (Test-Path -LiteralPath $webExecutable -PathType Leaf)) {
    throw 'Build the application first: .\scripts\go.ps1 build -o .\dist\ocr-web.exe .\cmd\ocr-web'
}
if ([string]::IsNullOrWhiteSpace($DataDir)) { $DataDir = Join-Path $projectRoot '.ocr-data' }
$previousToken = [Environment]::GetEnvironmentVariable('OCR_GITEA_TOKEN', 'Process')
$webExitCode = 1
try {
    if ($Gitea) {
        $secureToken = Read-Host 'Paste the Gitea token (input is hidden), then press Enter' -AsSecureString
        if ($secureToken.Length -eq 0) { throw 'A Gitea token is required.' }
        $tokenPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureToken)
        try {
            [Environment]::SetEnvironmentVariable('OCR_GITEA_TOKEN', [Runtime.InteropServices.Marshal]::PtrToStringBSTR($tokenPointer).Trim(), 'Process')
        }
        finally {
            [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($tokenPointer)
            $secureToken.Dispose()
        }
    }
    $runningWeb = @(Get-Process -Name 'ocr-web' -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $webExecutable })
    if ($runningWeb.Count -gt 0 -and -not $Restart) { throw 'This workbench is already running. Use -Restart to replace its current process.' }
    foreach ($webProcess in $runningWeb) {
        Stop-Process -Id $webProcess.Id
        $webProcess.WaitForExit(5000) | Out-Null
    }
    Write-Host "Opening workbench at http://$Addr (Ctrl+C to stop)."
    & $webExecutable -addr $Addr -data-dir $DataDir
    $webExitCode = $LASTEXITCODE
}
finally {
    [Environment]::SetEnvironmentVariable('OCR_GITEA_TOKEN', $previousToken, 'Process')
}
exit $webExitCode
