# Prints the track that is playing in Windows (System Media Transport Controls) as one JSON line
# every ~400 ms. Yandex Music (app or browser tab) and most players publish themselves here.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
Add-Type -AssemblyName System.Runtime.WindowsRuntime

$asTaskGeneric = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
    $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and
    $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1' })[0]

function Await($op, [Type]$type) {
    $task = $asTaskGeneric.MakeGenericMethod($type).Invoke($null, @($op))
    $null = $task.Wait(-1)
    $task.Result
}

$null = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager, Windows.Media.Control, ContentType = WindowsRuntime]
$null = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties, Windows.Media.Control, ContentType = WindowsRuntime]
$mgr = Await ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager]::RequestAsync()) ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager])

while ($true) {
    $o = @{ app = ''; title = ''; artist = ''; album = ''; status = 'None'; posMs = 0; endMs = 0; updated = 0 }
    try {
        $session = $null
        # prefer Yandex Music, then whatever is playing, then the "current" session
        foreach ($s in $mgr.GetSessions()) {
            if ($s.SourceAppUserModelId -match '(?i)yandex') { $session = $s; break }
        }
        if (-not $session) {
            foreach ($s in $mgr.GetSessions()) {
                if ([string]$s.GetPlaybackInfo().PlaybackStatus -eq 'Playing') { $session = $s; break }
            }
        }
        if (-not $session) { $session = $mgr.GetCurrentSession() }
        if ($session) {
            $p = Await ($session.TryGetMediaPropertiesAsync()) ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties])
            $t = $session.GetTimelineProperties()
            $o.app = [string]$session.SourceAppUserModelId
            $o.title = [string]$p.Title
            $o.artist = [string]$p.Artist
            $o.album = [string]$p.AlbumTitle
            $o.status = [string]$session.GetPlaybackInfo().PlaybackStatus
            $o.posMs = [int64]$t.Position.TotalMilliseconds
            $o.endMs = [int64]$t.EndTime.TotalMilliseconds
            if ($t.LastUpdatedTime.Year -gt 2000) { $o.updated = [int64]$t.LastUpdatedTime.ToUnixTimeMilliseconds() }
        }
    } catch {
        $o.status = 'Error'
        $o.title = ''
    }
    [Console]::Out.WriteLine(($o | ConvertTo-Json -Compress))
    [Console]::Out.Flush()
    Start-Sleep -Milliseconds 400
}
