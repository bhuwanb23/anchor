# headroom-opencode.ps1
# Starts headroom proxy (compresses tokens) then launches opencode through it.
# Usage: .\scripts\headroom-opencode.ps1

$ErrorActionPreference = "Stop"

$PROXY_PORT = 8787
$OLLAMA_PORT = 11434
$HEALTH_URL = "http://127.0.0.1:${PROXY_PORT}/health"

# --- Check if headroom proxy is already running ---
$proxyRunning = $false
try {
    $resp = Invoke-WebRequest -Uri $HEALTH_URL -TimeoutSec 2 -UseBasicParsing -ErrorAction SilentlyContinue
    if ($resp.StatusCode -eq 200) {
        $proxyRunning = $true
        Write-Host "[headroom] Proxy already running on port $PROXY_PORT" -ForegroundColor Green
    }
} catch {}

# --- Start proxy if not running ---
if (-not $proxyRunning) {
    # Check Ollama is up (hard requirement)
    $ollamaReady = $false
    try {
        $ollamaCheck = Invoke-WebRequest -Uri "http://127.0.0.1:${OLLAMA_PORT}/api/tags" -TimeoutSec 3 -UseBasicParsing -ErrorAction SilentlyContinue
        if ($ollamaCheck.StatusCode -eq 200) { $ollamaReady = $true }
    } catch {}

    if (-not $ollamaReady) {
        Write-Host "[headroom] Ollama not running on port $OLLAMA_PORT. Starting Ollama..." -ForegroundColor Yellow
        Start-Process "ollama" -ArgumentList "serve" -WindowStyle Hidden
        Start-Sleep -Seconds 5
        try {
            $ollamaCheck2 = Invoke-WebRequest -Uri "http://127.0.0.1:${OLLAMA_PORT}/api/tags" -TimeoutSec 5 -UseBasicParsing -ErrorAction SilentlyContinue
            if ($ollamaCheck2.StatusCode -eq 200) {
                Write-Host "[headroom] Ollama started successfully" -ForegroundColor Green
            }
        } catch {
            Write-Host "[headroom] ERROR: Could not start Ollama. Please start Ollama manually." -ForegroundColor Red
            exit 1
        }
    } else {
        Write-Host "[headroom] Ollama running on port $OLLAMA_PORT" -ForegroundColor Green
    }

    Write-Host "[headroom] Starting proxy on port $PROXY_PORT -> Ollama:${OLLAMA_PORT} ..." -ForegroundColor Cyan

    # Start headroom proxy in background, targeting Ollama
    $env:OPENAI_TARGET_API_URL = "http://127.0.0.1:${OLLAMA_PORT}"
    Start-Process -FilePath "headroom" -ArgumentList "proxy", "--port", "$PROXY_PORT", "--host", "127.0.0.1" -WindowStyle Hidden -PassThru | Out-Null

    # Wait for proxy to become healthy (up to 15 seconds)
    $waited = 0
    while ($waited -lt 15) {
        Start-Sleep -Seconds 1
        $waited++
        try {
            $check = Invoke-WebRequest -Uri $HEALTH_URL -TimeoutSec 2 -UseBasicParsing -ErrorAction SilentlyContinue
            if ($check.StatusCode -eq 200) {
                Write-Host "[headroom] Proxy healthy (took ${waited}s)" -ForegroundColor Green
                break
            }
        } catch {}
    }

    if ($waited -ge 15) {
        Write-Host "[headroom] WARNING: Proxy did not become healthy in 15s. Proceeding anyway." -ForegroundColor Yellow
    }
}

# --- Launch opencode ---
Write-Host "[headroom] Launching opencode (all LLM traffic compressed through headroom proxy)..." -ForegroundColor Cyan
& opencode @args

# --- Cleanup: stop proxy on exit ---
# Uncomment the next line to auto-stop proxy when opencode exits:
# Get-Process -Name "headroom" -ErrorAction SilentlyContinue | Stop-Process -Force
