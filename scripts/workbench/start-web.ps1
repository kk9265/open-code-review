# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

param(
    [switch]$Gitea,
    [switch]$UpdateGiteaToken,
    [switch]$Restart,
    [string]$Addr = '127.0.0.1:5484',
    [string]$DataDir = ''
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$webExecutable = [System.IO.Path]::GetFullPath((Join-Path $projectRoot 'dist/ocr-web.exe'))
if (-not (Test-Path -LiteralPath $webExecutable -PathType Leaf)) {
    throw 'Build the application first: .\scripts\toolchain\go.ps1 build -o .\dist\ocr-web.exe .\cmd\ocr-web'
}
if ([string]::IsNullOrWhiteSpace($DataDir)) { $DataDir = Join-Path $projectRoot '.ocr-data' }
$DataDir = [System.IO.Path]::GetFullPath($DataDir)
$tokenFile = Join-Path $DataDir 'gitea-token.dpapi'
$previousToken = [Environment]::GetEnvironmentVariable('OCR_GITEA_TOKEN', 'Process')
$webExitCode = 1
try {
    $runningWeb = @(Get-Process -Name 'ocr-web' -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $webExecutable })
    if ($runningWeb.Count -gt 0 -and -not $Restart) { throw 'This workbench is already running. Use -Restart to replace its current process.' }
    $secureToken = $null
    $saveToken = $false
    if ($UpdateGiteaToken -or ([string]::IsNullOrWhiteSpace($previousToken) -and -not (Test-Path -LiteralPath $tokenFile) -and $Gitea)) {
        $secureToken = Read-Host 'Paste the Gitea token (saved encrypted for this Windows user), then press Enter' -AsSecureString
        $saveToken = $true
    }
    elseif ([string]::IsNullOrWhiteSpace($previousToken) -and (Test-Path -LiteralPath $tokenFile -PathType Leaf)) {
        try {
            $secureToken = ConvertTo-SecureString -String ([System.IO.File]::ReadAllText($tokenFile, [System.Text.Encoding]::UTF8))
        }
        catch {
            throw 'Cannot decrypt the saved Gitea token. Run with -UpdateGiteaToken to replace it for this Windows user.'
        }
    }
    if ($null -ne $secureToken) {
        $tokenPointer = [IntPtr]::Zero
        try {
            $tokenPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureToken)
            $plainToken = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($tokenPointer).Trim()
            if ([string]::IsNullOrWhiteSpace($plainToken)) { throw 'A Gitea token is required.' }
            if ($saveToken) {
                [System.IO.Directory]::CreateDirectory($DataDir) | Out-Null
                $encryptedToken = ConvertFrom-SecureString -SecureString $secureToken
                [System.IO.File]::WriteAllText($tokenFile, $encryptedToken, [System.Text.UTF8Encoding]::new($false))
                Write-Host 'Gitea token saved encrypted. Future script launches will load it automatically.'
            }
            [Environment]::SetEnvironmentVariable('OCR_GITEA_TOKEN', $plainToken, 'Process')
        }
        finally {
            $plainToken = $null
            if ($tokenPointer -ne [IntPtr]::Zero) { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($tokenPointer) }
            $secureToken.Dispose()
        }
    }
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
