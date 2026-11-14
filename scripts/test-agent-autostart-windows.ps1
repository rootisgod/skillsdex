[CmdletBinding()]
param([Parameter(Mandatory=$true)][string]$Executable)
$ErrorActionPreference = 'Stop'
$testDirectory = Join-Path ([IO.Path]::GetTempPath()) ('skillsdex-startup-' + [Guid]::NewGuid().ToString('N'))
$taskName = 'SkillsDex Test ' + [Guid]::NewGuid().ToString('N')
$testExecutable = Join-Path $testDirectory 'skillsdex.exe'
$priorStateHome = $env:SKILLSDEX_HOME
New-Item -ItemType Directory -Path $testDirectory | Out-Null
try {
    Copy-Item -LiteralPath $Executable -Destination $testExecutable
    # Invalid credentials prevent network access or instruction file writes.
    [IO.File]::WriteAllText((Join-Path $testDirectory 'device.json'), '{}')
    $env:SKILLSDEX_HOME = $testDirectory
    & (Join-Path $PSScriptRoot 'install-agent-task.ps1') -Executable $testExecutable -TaskName $taskName
    & (Join-Path $PSScriptRoot 'install-agent-task.ps1') -Executable $testExecutable -TaskName $taskName
    Start-Sleep -Seconds 2
    $testTask = Get-ScheduledTask -TaskName $taskName
    if ($testTask.State -ne 'Running') { throw "Test task did not remain running: $($testTask.State)" }
    Write-Output 'Native startup, retry, and task activation passed.'
} finally {
    if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
        Stop-ScheduledTask -TaskName $taskName
        Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
    }
    Get-Process -Name skillsdex -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $testExecutable } | Stop-Process
    $env:SKILLSDEX_HOME = $priorStateHome
    # Each cleanup path is fixed within the unique test directory.
    foreach ($testFile in @('skillsdex.exe','device.json','agent-watch.ps1','agent-watch.lock','agent-sync.lock')) {
        Remove-Item -LiteralPath (Join-Path $testDirectory $testFile) -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $testDirectory -ErrorAction SilentlyContinue
}
