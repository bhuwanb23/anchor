# README assets

The SVG files in this directory are the **canonical sources** for the diagrams in the root `README.md`. The PNG files are derived renders (2× scale) for contexts where SVG isn't supported (email clients, some markdown renderers).

If you edit a diagram, edit the **SVG** and regenerate the PNG before committing:

```powershell
# from the repo root — requires Chrome or Edge headless
& 'C:\Program Files\Google\Chrome\Application\chrome.exe' --headless --disable-gpu --hide-scrollbars --window-size=2400,720 --screenshot="$PWD\docs\assets\readme\hero-banner.png" 'file:///C:\path\to\anchor\docs\assets\readme\hero-banner.svg'
```
