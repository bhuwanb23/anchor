# Infer diagrams (for judges)

SVG sources + full-bleed PNGs used by [`docs/infer-for-judges.md`](../../infer-for-judges.md).

| File | Size | Story |
|---|---|---|
| `infer-pipeline.svg/.png` | 1400×720 | Detect → deploy → measure flow |
| `arm-target.svg/.png` | 1400×820 | Use / Target / Improve on Arm |
| `bench-bars.svg/.png` | 1400×780 | Honest before/after tg bars |

## Regenerate PNGs (fills entire viewport)

```powershell
$chrome = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
$base = (Resolve-Path 'docs\assets\infer').Path
# Rebuild *.html wrappers from SVGs (svg width/height -> 100%), then:
@(
  @{ name='infer-pipeline'; w=1400; h=720 },
  @{ name='arm-target';     w=1400; h=820 },
  @{ name='bench-bars';     w=1400; h=780 }
) | ForEach-Object {
  $html = (Join-Path $base "$($_.name).html").Replace('\','/')
  & $chrome --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=1 `
    --window-size="$($_.w),$($_.h)" "--screenshot=$(Join-Path $base "$($_.name).png")" "file:///$html"
}
```
