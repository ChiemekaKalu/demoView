# Stops every local dev server and helper that demoView's sessions start:
# vite dev servers (viewer, wiki site), wrangler dev/tail, workerd, esbuild,
# astro (chiemeka.dev), and the pipeline's poller scripts.
#
# It lists matches by default and only kills with -Force. It never touches
# Claude Code, VS Code, shells, Steam or the game.
#
#   pwsh tools/local/stop-local.ps1          # list what would stop
#   pwsh tools/local/stop-local.ps1 -Force   # stop it
param([switch]$Force)

$roots = 'demoView', 'chiemeka\.dev'
$tools = 'vite', 'wrangler', 'workerd', 'esbuild', 'astro', 'poller\.mjs', 'vitest'
$never = '^(claude|Code|cs2|steam|steamwebhelper|bash|pwsh|powershell|conhost|explorer)(\.exe)?$'

$rootPattern = "($($roots -join '|'))"
$toolPattern = "($($tools -join '|'))"

$targets = Get-CimInstance Win32_Process | Where-Object {
  $_.CommandLine -and
  $_.Name -notmatch $never -and
  $_.CommandLine -match $toolPattern -and
  ($_.CommandLine -match $rootPattern -or $_.ExecutablePath -match $rootPattern -or $_.Name -eq 'cmd.exe')
} | Where-Object {
  # A cmd.exe shim counts only when it launched one of the tools by name.
  $_.Name -ne 'cmd.exe' -or $_.CommandLine -match "/c\s+`"?($($tools -join '|'))\b"
}

if (-not $targets) { Write-Output 'Nothing running.'; exit 0 }

$targets | Sort-Object Name | ForEach-Object {
  $cmd = ($_.CommandLine -replace '\s+', ' ')
  [pscustomobject]@{ PID = $_.ProcessId; Name = $_.Name; Command = $cmd.Substring(0, [Math]::Min(120, $cmd.Length)) }
} | Format-Table -AutoSize | Out-String -Width 200 | Write-Output

if (-not $Force) { Write-Output 'Dry run. Re-run with -Force to stop these.'; exit 0 }

foreach ($p in $targets) {
  # /T takes the process tree down with it, so a dev server's children go too.
  taskkill /PID $p.ProcessId /T /F *> $null
  Write-Output ("stopped {0} ({1})" -f $p.ProcessId, $p.Name)
}
