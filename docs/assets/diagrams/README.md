# Anchor visual diagrams

Canonical SVG sources for project storytelling. Emerald workbench DNA (matches the dashboard theme). PNGs are 2× renders for markdown hosts that prefer raster.

| File | Story | Shape |
|------|-------|-------|
| [`ownership-map.svg`](ownership-map.svg) | What you own vs what only orchestrates | Split territory (wide) |
| [`calm-ops-loop.svg`](calm-ops-loop.svg) | Connect → deploy → watch → heal → backup | Radial loop (square) |
| [`deploy-signal.svg`](deploy-signal.svg) | One deploy as a WebSocket signal path | Dark wire / stations (wide) |
| [`runtime-stack.svg`](runtime-stack.svg) | L0–L7 cut from browser down to disk | Vertical stack (tall / narrow) |

## Regenerate PNGs

Use `_render.html` so the diagram fills the entire screenshot (raw SVG open in Chrome leaves empty margins).

```powershell
$chrome = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
$base = (Resolve-Path 'docs\assets\diagrams').Path
$html = (Join-Path $base '_render.html').Replace('\','/')
@(
  @{ svg = 'ownership-map.svg'; w = 1400; h = 820 },
  @{ svg = 'calm-ops-loop.svg';  w = 1200; h = 1200 },
  @{ svg = 'deploy-signal.svg';  w = 1500; h = 720 },
  @{ svg = 'runtime-stack.svg';  w = 720;  h = 1600 }
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
- Distinct compositions — not three clones of a 3-column architecture board.
- Existing blue diagrams in [`../readme/`](../readme/) remain for the original README layout; these are the emerald set.
