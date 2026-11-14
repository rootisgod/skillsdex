# Run as the same Windows user who ran `skillsdex agent connect`.
[CmdletBinding()]
param(
    [string]$Executable = (Get-Command skillsdex -ErrorAction Stop).Source,
    [string]$TaskName = 'SkillsDex Agent Sync'
)
$ErrorActionPreference = 'Stop'
$agentExecutable = (Resolve-Path -LiteralPath $Executable).Path
$agentStateDirectory = if ($env:SKILLSDEX_HOME) { $env:SKILLSDEX_HOME } else { Join-Path $env:USERPROFILE '.skillsdex' }
if (-not (Test-Path -LiteralPath (Join-Path $agentStateDirectory 'device.json'))) {
    throw 'Run skillsdex agent connect first, using this Windows account.'
}
function ConvertTo-Literal([string]$Value) { "'" + $Value.Replace("'", "''") + "'" }
$runnerPath = Join-Path $agentStateDirectory 'agent-watch.ps1'
# Preserve the installation environment so user-level agents and custom homes
# resolve the same way after login. No credentials are included in this script.
$runner = @(
    '$ErrorActionPreference = ''Stop'''
    ('$env:SKILLSDEX_HOME = ' + (ConvertTo-Literal $agentStateDirectory))
    ('$env:PATH = ' + (ConvertTo-Literal $env:PATH))
    ('$env:CODEX_HOME = ' + (ConvertTo-Literal ([string]$env:CODEX_HOME)))
    ('$agentProcess = Start-Process -FilePath ' + (ConvertTo-Literal $agentExecutable) + ' -ArgumentList @(''agent'', ''watch'') -WindowStyle Hidden -Wait -PassThru')
    'exit $agentProcess.ExitCode'
) -join [Environment]::NewLine
if (Test-Path -LiteralPath $runnerPath) {
    if ([IO.File]::ReadAllText($runnerPath) -ne $runner) { throw "Runner differs from this setup; review it first: $runnerPath" }
} else {
    $runnerStream = [IO.File]::Open($runnerPath, [IO.FileMode]::CreateNew)
    try {
        $runnerBytes = [Text.Encoding]::UTF8.GetBytes($runner)
        $runnerStream.Write($runnerBytes, 0, $runnerBytes.Length)
    } finally { $runnerStream.Dispose() }
}
$account = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument ('-NoProfile -NonInteractive -WindowStyle Hidden -File "' + $runnerPath + '"')
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $account
$principal = New-ScheduledTaskPrincipal -UserId $account -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
$existingTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($existingTask) {
    $existingAccount = $existingTask.Principal.UserId
    $existingSid = if ($existingAccount -match '^S-1-') { $existingAccount } else {
        ([Security.Principal.NTAccount]::new($existingAccount)).Translate([Security.Principal.SecurityIdentifier]).Value
    }
    $currentSid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    if (@($existingTask.Actions).Count -ne 1 -or $existingTask.Actions[0].Execute -ne $action.Execute -or $existingTask.Actions[0].Arguments -ne $action.Arguments -or $existingSid -ne $currentSid) {
        throw "Task '$TaskName' differs from this setup; review it before reinstalling."
    }
} else {
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description 'Synchronises SkillsDex instructions as the signed-in user.' | Out-Null
}
Start-ScheduledTask -TaskName $TaskName
Write-Output "Installed and started '$TaskName'. It starts at login and stops at logout."
Write-Output "Stop: Stop-ScheduledTask -TaskName '$TaskName'"
Write-Output "Remove: Unregister-ScheduledTask -TaskName '$TaskName'"
