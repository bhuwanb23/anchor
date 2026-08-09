# Anchor visual diagrams

Canonical SVG sources for project storytelling. Emerald workbench DNA (matches the dashboard theme). PNGs are 2× renders for markdown hosts that prefer raster.

| File | Story | Shape |
|------|-------|-------|
| [`ownership-map.svg`](ownership-map.svg) | What you own vs what only orchestrates | Split territory |
| [`calm-ops-loop.svg`](calm-ops-loop.svg) | Connect → deploy → watch → heal → backup | Radial loop |
| [`deploy-signal.svg`](deploy-signal.svg) | One deploy as a WebSocket signal path | Dark wire / stations |

## Regenerate PNGs

From the repo root (Chrome or Edge headless):

```powershell
$chrome = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
$base = (Resolve-Path 'docs\assets\diagrams').Path
@(
  @{ svg = 'ownership-map.svg';   w = 2800; h = 1640 },
  @{ svg = 'calm-ops-loop.svg';    w = 2400; h = 2400 },
  @{ svg = 'deploy-signal.svg';    w = 3000; h = 1440 }
) | ForEach-Object {
  $in  = "file:///$($base.Replace('\','/'))/$($_.svg)"
  $out = Join-Path $base ($_.svg -replace '\.svg$','.png')
  & $chrome --headless --disable-gpu --hide-scrollbars --window-size="$($_.w),$($_.h)" --screenshot="$out" $in
}
```

## Design notes

- No invented metrics or fake “+47%” proof.
- Accent is deep emerald (`#0F6B4D`), not purple/indigo.
- Distinct compositions — not three clones of a 3-column architecture board.
- Existing blue diagrams in [`../readme/`](../readme/) remain for the original README layout; these are the emerald set.
