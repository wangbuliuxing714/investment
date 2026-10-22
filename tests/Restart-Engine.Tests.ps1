# Process and HTTP commands are mocked. These tests never launch/stop engines
# or send requests to the real trading API. All file writes use isolated fixtures.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$sourceScript = Join-Path (Split-Path $PSScriptRoot -Parent) 'Restart-Engine.ps1'
$fixtureRoot = Join-Path ([System.IO.Path]::GetTempPath()) ('desk-restart-tests-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixtureRoot | Out-Null
$script:passed = 0

function Assert([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw ('ASSERTION FAILED: ' + $Message) }
}

function New-FakeProcess([int]$Number, [string]$Version) {
    $process = [PSCustomObject]@{
        Id = $Number; Path = $script:stablePath; Version = $Version
        StartTime = [datetime]'2026-01-01T00:00:00'; HasExited = $false
    }
    $process | Add-Member -MemberType ScriptMethod -Name Refresh -Value {}
    $process | Add-Member -MemberType ScriptMethod -Name WaitForExit -Value { param($Milliseconds) return $this.HasExited }
    return $process
}

function Get-Process {
    [CmdletBinding()]
    param([string[]]$Name, [int]$Id)
    if ($Id -and $script:scenario -eq 'identity-changed') {
        $replacement = New-FakeProcess $Id 'old'
        $replacement.StartTime = $replacement.StartTime.AddSeconds(10)
        return $replacement
    }
    return $script:activeProcess
}

function Get-NetTCPConnection {
    [CmdletBinding()]
    param([int]$LocalPort, [string]$State)
    $owner = $script:activeProcess.Id
    if ($script:scenario -eq 'foreign-port') { $owner++ }
    [PSCustomObject]@{ OwningProcess = $owner }
}

function Stop-Process {
    [CmdletBinding()]
    param([int]$Id, [switch]$Force)
    Assert ($Id -ge 91000001 -and $Id -eq $script:activeProcess.Id) 'Only fixture processes may be stopped'
    $script:activeProcess.HasExited = $true
    $script:stops++
}

function Start-Process {
    [CmdletBinding()]
    param([string]$FilePath, [string]$WorkingDirectory, [string]$WindowStyle,
        [switch]$PassThru, [string]$RedirectStandardOutput, [string]$RedirectStandardError)
    Assert ($FilePath -eq $script:stablePath) 'Start the stable executable'
    Assert ($WorkingDirectory -eq (Split-Path $script:stablePath -Parent)) 'Preserve the database working directory'
    Assert ($WindowStyle -eq 'Hidden') 'Do not open an interactive window'
    Assert ($RedirectStandardOutput.EndsWith('.stdout.log')) 'Capture stdout'
    Assert ($RedirectStandardError.EndsWith('.stderr.log')) 'Capture stderr'
    $script:starts++
    $version = (Get-Content -LiteralPath $FilePath -Raw).Trim()
    $script:activeProcess = New-FakeProcess (91000001 + $script:starts) $version
    if ($script:scenario -eq 'startup-failure' -and $script:starts -eq 1) {
        $script:activeProcess.HasExited = $true
    }
    return $script:activeProcess
}

function Invoke-RestMethod {
    [CmdletBinding()]
    param([string]$Uri, [int]$TimeoutSec)
    $isNew = $script:activeProcess.Version -eq 'candidate'
    switch (([uri]$Uri).AbsolutePath) {
        '/api/health' { return [PSCustomObject]@{ status = 'ok' } }
        '/api/trader' { return [PSCustomObject]@{ running = ($script:scenario -eq 'busy'); busy = $false } }
        '/api/settings' {
            $settings = [PSCustomObject]@{ testnet = $false; dryRun = $true; keySource = 'freqtrade' }
            if ($isNew) {
                $settings | Add-Member -NotePropertyName binanceConnection -NotePropertyValue 'configured'
                if ($script:scenario -eq 'mode-changed') { $settings.dryRun = $false }
            }
            return $settings
        }
        '/api/agent/log' {
            if ($isNew -and $script:scenario -eq 'lost-history') { return @([PSCustomObject]@{ id = 18 }) }
            return @(1..18 | ForEach-Object { [PSCustomObject]@{ id = $_ } })
        }
        default { throw ('Unexpected HTTP request: ' + $Uri) }
    }
}

function Run-Case([string]$Name, [string]$ExpectedError = '', [switch]$CheckOnly) {
    $script:scenario = $Name
    $script:starts = 0
    $script:stops = 0
    $caseDir = Join-Path $fixtureRoot $Name
    $engineDir = Join-Path $caseDir 'engine'
    $dataDir = Join-Path $engineDir 'data'
    New-Item -ItemType Directory -Path $dataDir | Out-Null
    $script:stablePath = Join-Path $engineDir 'desk.exe'
    $initialVersion = 'old'
    if ($Name -eq 'already-updated') { $initialVersion = 'candidate' }
    Set-Content -LiteralPath $script:stablePath -Value $initialVersion -Encoding ASCII
    Set-Content -LiteralPath (Join-Path $engineDir 'desk.next.exe') -Value 'candidate' -Encoding ASCII
    Set-Content -LiteralPath (Join-Path $dataDir 'desk.db') -Value 'database-fixture' -Encoding ASCII
    Set-Content -LiteralPath (Join-Path $dataDir 'desk.db-wal') -Value 'journal-fixture' -Encoding ASCII
    Set-Content -LiteralPath (Join-Path $engineDir '.env') -Value @('DESK_LISTEN=127.0.0.1:8787', 'DESK_DATA=data/desk.db') -Encoding ASCII
    $fixtureScript = Join-Path $caseDir 'Restart-Engine.ps1'
    Copy-Item -LiteralPath $sourceScript -Destination $fixtureScript
    $script:activeProcess = New-FakeProcess 91000001 $initialVersion
    $caught = ''
    $output = $null
    try {
        # Keep the test's script scope for mock state, but isolate launcher
        # locals (including case-insensitive $name) from the case assertions.
        $output = & { if ($CheckOnly) { . $fixtureScript } else { . $fixtureScript -Apply } }
    } catch { $caught = $_.Exception.Message }
    if ($ExpectedError) {
        Assert ($caught.Contains($ExpectedError)) ($Name + ': expected error ' + $ExpectedError + '; got ' + $caught)
    } else { Assert (-not $caught) ($Name + ': unexpected error ' + $caught) }
    $finalVersion = (Get-Content -LiteralPath $script:stablePath -Raw).Trim()
    Assert (((Get-Content -LiteralPath (Join-Path $dataDir 'desk.db') -Raw).Trim()) -eq 'database-fixture') 'Keep the database intact'
    if ($Name -eq 'success') {
        Assert ($finalVersion -eq 'candidate') 'Promote candidate to stable'
        Assert ($script:starts -eq 1 -and $script:stops -eq 1) 'Restart exactly once'
        Assert $output.HistoryPreserved 'Verify history before reporting success'
        $backup = @(Get-ChildItem -LiteralPath (Join-Path $dataDir 'engine-backups') -Directory)[0].FullName
        Assert (((Get-Content -LiteralPath (Join-Path $backup 'desk.exe') -Raw).Trim()) -eq 'old') 'Back up the old executable'
        Assert (Test-Path -LiteralPath (Join-Path $backup 'desk.db-wal')) 'Include SQLite journals in the backup'
    } elseif ($Name -in @('startup-failure', 'lost-history', 'mode-changed')) {
        Assert ($finalVersion -eq 'old') 'Restore the old executable on failure'
        Assert ($script:starts -eq 2) 'Restart the old engine after rollback'
        Assert (-not $script:activeProcess.HasExited) 'Leave the rollback engine running'
    } else {
        Assert ($script:starts -eq 0 -and $script:stops -eq 0) 'Do not change processes during checks or rejected updates'
        Assert ($finalVersion -eq $initialVersion) 'Do not replace a binary on preflight failure'
        if ($CheckOnly) {
            Assert $output.UpdatePending 'Detect a pending candidate'
            Assert (-not (Test-Path -LiteralPath (Join-Path $dataDir 'engine-restart.lock'))) 'Read-only check does not create a lock file'
        }
    }
    $script:passed++
    Write-Output ('PASS ' + $Name)
}

try {
    Run-Case 'check' -CheckOnly
    Run-Case 'success'
    Run-Case 'startup-failure' 'previous engine is restored and healthy'
    Run-Case 'lost-history' 'Agent history verification failed'
    Run-Case 'mode-changed' 'Trading configuration changed unexpectedly'
    Run-Case 'busy' 'Stop the trading agent'
    Run-Case 'foreign-port' 'not exclusively owned'
    Run-Case 'identity-changed' 'process identity changed'
    Run-Case 'already-updated'
    Write-Output ('All ' + $script:passed + ' maintenance tests passed. Real processes and network were not used.')
} finally {
    $resolvedFixture = [System.IO.Path]::GetFullPath($fixtureRoot)
    $tempPrefix = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath()).TrimEnd([char]92) + [System.IO.Path]::DirectorySeparatorChar
    if (-not $resolvedFixture.StartsWith($tempPrefix, [System.StringComparison]::OrdinalIgnoreCase) -or
        -not ([System.IO.Path]::GetFileName($resolvedFixture)).StartsWith('desk-restart-tests-')) {
        throw 'Refusing cleanup outside the test fixture directory.'
    }
    Remove-Item -LiteralPath $resolvedFixture -Recurse -Force
}
