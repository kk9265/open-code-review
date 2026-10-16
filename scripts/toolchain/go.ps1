# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$goCommand = (Get-Command go.exe -CommandType Application -ErrorAction Stop).Source
$localGo = Join-Path $projectRoot '.local-go/sdk/go/bin/go.exe'
if (Test-Path -LiteralPath $localGo) {
    $goCommand = $localGo
}
$projectEnvironment = @{
    GOENV = 'off'
    GOTOOLCHAIN = 'go1.26.9'
    GOPATH = Join-Path $projectRoot '.local-go/work'
    GOMODCACHE = Join-Path $projectRoot '.local-go/work/pkg/mod'
    GOCACHE = Join-Path $projectRoot '.local-go/build-cache'
    GOROOT = $null
}
$previousEnvironment = @{}
$goExitCode = 1

try {
    foreach ($name in $projectEnvironment.Keys) {
        $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
        if ($null -eq $projectEnvironment[$name]) {
            Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue
        }
        else {
            [Environment]::SetEnvironmentVariable($name, $projectEnvironment[$name], 'Process')
        }
    }
    Push-Location -LiteralPath $projectRoot
    try {
        & $goCommand @args
        $goExitCode = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
}
finally {
    foreach ($name in $previousEnvironment.Keys) {
        if ($null -eq $previousEnvironment[$name]) {
            Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue
        }
        else {
            [Environment]::SetEnvironmentVariable($name, $previousEnvironment[$name], 'Process')
        }
    }
}

exit $goExitCode
