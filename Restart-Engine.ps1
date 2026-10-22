[CmdletBinding()]
param([switch]$Apply)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$engineDir = Join-Path $PSScriptRoot 'engine'
$stableExe = Join-Path $engineDir 'desk.exe'
$candidateExe = Join-Path $engineDir 'desk.next.exe'
$dataDir = Join-Path $engineDir 'data'
$baseUrl = 'http://127.0.0.1:8787'

function Read-EngineApi([string]$Route) {
    $response = Invoke-RestMethod -Uri ($baseUrl + $Route) -TimeoutSec 5
    # Explicitly enumerate JSON arrays; Invoke-RestMethod can emit one array object.
    return $response
}

function Assert-Idle($State) {
    foreach ($name in @('running', 'busy')) {
        if ($null -eq $State.PSObject.Properties[$name] -or $State.$name -isnot [bool]) {
            throw ('Cannot verify trader state: missing boolean ' + $name)
        }
        if ($State.$name) {
            throw 'Stop the trading agent in the UI and wait until it is idle before restarting.'
        }
    }
}

function Get-Connection($Settings) {
    if ($null -eq $Settings.PSObject.Properties['binanceConnection']) { return 'configured' }
    return $Settings.binanceConnection
}

function Assert-ModeUnchanged($Before, $After) {
    foreach ($name in @('testnet', 'dryRun', 'keySource')) {
        if ($null -eq $Before.PSObject.Properties[$name] -or
            $null -eq $After.PSObject.Properties[$name] -or $Before.$name -cne $After.$name) {
            throw ('Trading configuration changed unexpectedly: ' + $name)
        }
    }
    if ((Get-Connection $Before) -cne (Get-Connection $After)) {
        throw 'The active Binance connection changed unexpectedly.'
    }
}

function Assert-Listener([int]$ProcessId) {
    $listeners = @(Get-NetTCPConnection -LocalPort 8787 -State Listen -ErrorAction Stop)
    if ($listeners.Count -eq 0 -or @($listeners | Where-Object { $_.OwningProcess -ne $ProcessId }).Count -gt 0) {
        throw 'Port 8787 is not exclusively owned by the expected engine. No unrelated process will be stopped.'
    }
}

function Start-Engine([string]$LogPrefix) {
    Start-Process -FilePath $stableExe -WorkingDirectory $engineDir -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput ($LogPrefix + '.stdout.log') `
        -RedirectStandardError ($LogPrefix + '.stderr.log')
}

function Wait-Engine($Process, [bool]$RequireDemo) {
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    $lastError = 'Engine did not become healthy.'
    do {
        $Process.Refresh()
        if ($Process.HasExited) { throw 'Engine exited during startup; inspect the startup logs.' }
        try {
            $health = Read-EngineApi '/api/health'
            if ($health.status -ne 'ok') { throw 'Unexpected health response.' }
            Assert-Listener $Process.Id
            $settings = Read-EngineApi '/api/settings'
            if ($RequireDemo -and ($null -eq $settings.PSObject.Properties['binanceConnection'] -or
                $settings.binanceConnection -notin @('configured', 'demo'))) {
                throw 'The new engine does not expose Binance demo support.'
            }
            return $settings
        } catch { $lastError = $_.Exception.Message }
        Start-Sleep -Milliseconds 300
    } while ([DateTime]::UtcNow -lt $deadline)
    throw $lastError
}

# Keep the existing database and address. Refuse unexpected inherited overrides.
# This launcher intentionally supports this desk installation's loopback setup.
foreach ($entry in @{ DESK_LISTEN = '127.0.0.1:8787'; DESK_DATA = 'data/desk.db' }.GetEnumerator()) {
    $inherited = [Environment]::GetEnvironmentVariable($entry.Key, 'Process')
    if ($inherited -and $inherited -ne $entry.Value) {
        throw ('Unexpected environment override: ' + $entry.Key)
    }
    $envFile = Join-Path $engineDir '.env'
    if (Test-Path -LiteralPath $envFile) {
        $lines = @(Get-Content -LiteralPath $envFile -Encoding UTF8 |
            Where-Object { $_ -match ('^\s*(?:export\s+)?' + $entry.Key + '\s*=') })
        foreach ($line in $lines) {
            $value = ($line -split '=', 2)[1].Trim().Trim([char]34).Trim([char]39)
            if ($value -ne $entry.Value) { throw ('Unsupported .env setting: ' + $entry.Key) }
        }
    }
}

$processes = @(Get-Process -Name 'desk', 'desk.next' -ErrorAction SilentlyContinue |
    Where-Object { $_.Path -ieq $stableExe -or $_.Path -ieq $candidateExe })
$health = Read-EngineApi '/api/health'
if ($health.status -ne 'ok') { throw 'The current engine is not healthy.' }
$beforeState = Read-EngineApi '/api/trader'
$beforeSettings = Read-EngineApi '/api/settings'
Assert-Idle $beforeState
if ($processes.Count -ne 1 -or $processes[0].Path -ine $stableExe) {
    throw 'Expected exactly one engine running from engine/desk.exe. Inspect existing processes before switching.'
}
$oldProcess = $processes[0]
if (-not (Test-Path -LiteralPath $candidateExe -PathType Leaf)) {
    throw 'Build engine/desk.next.exe before applying an update.'
}
$candidateHash = (Get-FileHash -LiteralPath $candidateExe -Algorithm SHA256).Hash
$currentHash = (Get-FileHash -LiteralPath $stableExe -Algorithm SHA256).Hash
$hasDemoSupport = $null -ne $beforeSettings.PSObject.Properties['binanceConnection']

if (-not $Apply) {
    [PSCustomObject]@{
        Action = 'Check only; no processes or files changed'
        ProcessId = $oldProcess.Id
        Healthy = $true
        TraderIdle = $true
        DemoSupported = $hasDemoSupport
        Connection = Get-Connection $beforeSettings
        DryRun = $beforeSettings.dryRun
        UpdatePending = $candidateHash -ne $currentHash
        CandidateSHA256 = $candidateHash
    }
    return
}

if ($candidateHash -eq $currentHash -and $hasDemoSupport) {
    Write-Output 'The current engine already matches desk.next.exe; no restart needed.'
    return
}

# This is ordinary maintenance, not an approval bypass. Run -Apply only through
# the normal authorized execution path. No policy or execution-policy is changed.
$lock = [System.IO.File]::Open((Join-Path $dataDir 'engine-restart.lock'),
    [System.IO.FileMode]::OpenOrCreate, [System.IO.FileAccess]::ReadWrite, [System.IO.FileShare]::None)
$oldStopped = $false
$newProcess = $null
$backupExe = $null
try {
    Assert-Listener $oldProcess.Id
    Assert-Idle (Read-EngineApi '/api/trader')
    $beforeLog = @(Read-EngineApi '/api/agent/log')
    $stamp = [DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss-fff')
    $backupDir = Join-Path $dataDir ('engine-backups/' + $stamp)
    New-Item -ItemType Directory -Path $backupDir -ErrorAction Stop | Out-Null
    $backupExe = Join-Path $backupDir 'desk.exe'
    Copy-Item -LiteralPath $stableExe -Destination $backupExe -ErrorAction Stop
    if ((Get-FileHash -LiteralPath $backupExe -Algorithm SHA256).Hash -ne $currentHash) {
        throw 'Executable backup verification failed.'
    }
    # Check identity immediately before stopping the exact previously inspected PID.
    $currentProcess = Get-Process -Id $oldProcess.Id -ErrorAction Stop
    if ($currentProcess.Path -ine $stableExe -or $currentProcess.StartTime -ne $oldProcess.StartTime) {
        throw 'Engine process identity changed; refusing to stop it.'
    }
    Assert-Idle (Read-EngineApi '/api/trader')
    Stop-Process -Id $currentProcess.Id -Force -ErrorAction Stop
    $oldStopped = $true
    if (-not $currentProcess.WaitForExit(10000)) { throw 'The old engine did not exit within 10 seconds.' }
    # Copy SQLite together with its journals only after the engine has exited.
    foreach ($name in @('desk.db', 'desk.db-wal', 'desk.db-shm', 'desk.db-journal')) {
        $source = Join-Path $dataDir $name
        if (Test-Path -LiteralPath $source) { Copy-Item -LiteralPath $source -Destination $backupDir }
    }
    if ((Get-FileHash -LiteralPath $candidateExe -Algorithm SHA256).Hash -ne $candidateHash) {
        throw 'Candidate executable changed during restart.'
    }
    Copy-Item -LiteralPath $candidateExe -Destination $stableExe -Force
    $newProcess = Start-Engine (Join-Path $dataDir ('engine-' + $stamp))
    $afterSettings = Wait-Engine $newProcess $true
    Assert-Idle (Read-EngineApi '/api/trader')
    Assert-ModeUnchanged $beforeSettings $afterSettings
    $afterIds = @((Read-EngineApi '/api/agent/log') | ForEach-Object { $_.id })
    foreach ($row in $beforeLog) {
        if ($row.id -notin $afterIds) { throw 'Agent history verification failed.' }
    }
    [PSCustomObject]@{
        Action = 'Updated and healthy'
        ProcessId = $newProcess.Id
        DemoSupported = $true
        TraderIdle = $true
        Connection = Get-Connection $afterSettings
        DryRun = $afterSettings.dryRun
        HistoryPreserved = $true
        BackupDirectory = $backupDir
    }
} catch {
    $failure = $_.Exception.Message
    if ($oldStopped) {
        try {
            if ($null -ne $newProcess) {
                $newProcess.Refresh()
                if (-not $newProcess.HasExited) {
                    Stop-Process -Id $newProcess.Id -Force -ErrorAction Stop
                    if (-not $newProcess.WaitForExit(10000)) { throw 'New engine did not stop for rollback.' }
                }
            }
            if (-not $oldProcess.WaitForExit(10000)) { throw 'Old process is still exiting; no replacement started.' }
            Copy-Item -LiteralPath $backupExe -Destination $stableExe -Force
            $restored = Start-Engine (Join-Path $dataDir ('rollback-' + $stamp))
            $restoredSettings = Wait-Engine $restored $false
            Assert-Idle (Read-EngineApi '/api/trader')
            Assert-ModeUnchanged $beforeSettings $restoredSettings
        } catch {
            throw ('Update failed: ' + $failure + ' Rollback also failed: ' + $_.Exception.Message)
        }
        throw ('Update failed; the previous engine is restored and healthy. Cause: ' + $failure)
    }
    throw
} finally { $lock.Dispose() }
