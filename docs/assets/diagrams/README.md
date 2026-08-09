# Anchor visual diagrams

Canonical SVG sources for project storytelling. Emerald workbench DNA (matches the dashboard theme). PNGs are rendered at 1:1 viewBox size via `_render.html`.

## Set A — territory / loop / wire / stack

| File | Story | Shape |
|------|-------|-------|
| [`ownership-map.svg`](ownership-map.svg) | What you own vs what only orchestrates | Wide 1400×820 |
| [`calm-ops-loop.svg`](calm-ops-loop.svg) | Connect → deploy → watch → heal → backup | Square 1200×1200 |
| [`deploy-signal.svg`](deploy-signal.svg) | One deploy as a WebSocket signal path | Wide 1500×720 |
| [`runtime-stack.svg`](runtime-stack.svg) | L0–L7 cut from browser down to disk | Tall 720×1600 |

## Set B — sequences / state / pipeline / timeline / swimlanes

| File | Story | Shape |
|------|-------|-------|
| [`seq-deploy.svg`](seq-deploy.svg) | Deploy sequence across dashboard → API → hub → agent → Docker | Square 1200×1200 |
| [`state-agent.svg`](state-agent.svg) | Agent connection state machine | Square 1200×1200 |
| [`seq-connect.svg`](seq-connect.svg) | Connect-server registration sequence | Tall 720×1600 |
| [`pipe-alert.svg`](pipe-alert.svg) | Alert → heal pipeline (check → alert → rule → action) | Tall 720×1600 |
| [`timeline-heartbeat.svg`](timeline-heartbeat.svg) | Agent heartbeat over one hour | Wide 1500×720 |
| [`swim-backup.svg`](swim-backup.svg) | Backup vs restore swimlanes | Wide 1500×720 |

## Regenerate PNGs

Use `_render.html` so the diagram fills the entire screenshot (raw SVG open in Chrome leaves empty margins). Rewrite SVG as UTF-8 **without BOM** first if Chrome fails to parse.

```powershell
$chrome = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
$base = (Resolve-Path 'docs\assets\diagrams').Path
$html = (Join-Path $base '_render.html').Replace('\','/')
$utf8 = New-Object System.Text.UTF8Encoding $false
Get-ChildItem $base -Filter *.svg | ForEach-Object {
  [System.IO.File]::WriteAllText($_.FullName, [System.IO.File]::ReadAllText($_.FullName), $utf8)
}
@(
  @{ svg = 'ownership-map.svg';      w = 1400; h = 820 },
  @{ svg = 'calm-ops-loop.svg';       w = 1200; h = 1200 },
  @{ svg = 'deploy-signal.svg';       w = 1500; h = 720 },
  @{ svg = 'runtime-stack.svg';       w = 720;  h = 1600 },
  @{ svg = 'seq-deploy.svg';          w = 1200; h = 1200 },
  @{ svg = 'state-agent.svg';          w = 1200; h = 1200 },
  @{ svg = 'seq-connect.svg';         w = 720;  h = 1600 },
  @{ svg = 'pipe-alert.svg';          w = 720;  h = 1600 },
  @{ svg = 'timeline-heartbeat.svg';  w = 1500; h = 720 },
  @{ svg = 'swim-backup.svg';         w = 1500; h = 720 }
) | ForEach-Object {
  $out = Join-Path $base ($_.svg -replace '\.svg$','.png')
  $url = "file:///$html`?src=$($_.svg)"
  & $chrome --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=1 `
    --window-size="$($_.w),$($_.h)" "--screenshot=$out" $url
}
```

## Design notes

- No invented metrics or fake “+47%” proof.
- Accent is deep emerald (`#0F6B4D`), not purple/indigo.
- Distinct compositions — sequences, state machines, pipelines, timelines, swimlanes — not clones of a 3-column architecture board.
- Existing blue diagrams in [`../readme/`](../readme/) remain for the original README layout; these are the emerald set.
