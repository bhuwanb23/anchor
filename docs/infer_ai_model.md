# Anchor Infer — Feature Extension: Phased Implementation Plan

---

## Understanding the Build Strategy

```
Anchor Infer is built as an extension on top of Anchor.
It is not a separate project.
It shares the same agent, the same control plane,
the same database, and the same dashboard codebase.

What changes:
  → The agent gets new detection logic and new command handlers
  → The control plane gets new endpoints and new database tables
  → The dashboard gets a new route group for the Infer demo UI

What stays exactly the same:
  → Everything built in Layers 1 through 7
  → All existing deploy logic, WebSocket hub, auth system
  → All existing Docker and Caddy management
  → The entire database foundation

The extension is additive, not disruptive.
No existing feature is modified to make room for this.
```

---

## The Four Phases

```
Phase 1 — Server Readiness (Days 1-2)
  Detect the server's Arm capabilities.
  Know exactly what optimization level is possible
  before attempting anything.

Phase 2 — Model Deployment (Days 3-5)
  Download the model.
  Start the inference server.
  Get a live, working AI endpoint over HTTPS.

Phase 3 — Benchmark Pipeline (Days 6-7)
  Run the generic build.
  Run the optimized build.
  Produce a real, credible before/after comparison.

Phase 4 — Dashboard and Demo Polish (Days 8-10)
  Build the Infer dashboard UI.
  Wire everything together.
  Make the demo flow smooth and impressive.
```

---

## Phase 1 — Server Readiness

### What This Phase Delivers

```
By the end of Phase 1, the agent can look at any server
and answer these questions precisely:

  → Is this server Arm64?
  → Which specific Arm CPU is it?
  → Which acceleration features does it have?
  → How much memory is available for a model?
  → How much disk is available for model weights?
  → Which llama.cpp build should be used on this server?
  → Which quantization level is appropriate?

These answers drive every decision in Phase 2 and 3.
Getting them right here makes everything downstream automatic.
```

### Step 1 — CPU Architecture Identification

```
What to determine:

Basic architecture:
  → Is this aarch64 (Arm 64-bit)?
  → This is already detected in Layer 2 Step A2
  → Phase 1 goes deeper than the basic check

Specific microarchitecture:
  → The file /proc/cpuinfo contains machine-readable CPU identity
  → The relevant fields are:
      CPU implementer (a hex code identifying the CPU vendor)
      CPU architecture (version number)
      CPU variant (a sub-version code)
      CPU part (a code identifying the specific CPU model)

  → Known Arm CPU part codes to recognize:
      0xD0C = Neoverse N1 (Ampere Altra, older Graviton)
      0xD49 = Neoverse N2 (Azure Cobalt)
      0xD40 = Neoverse V1 (AWS Graviton 3)
      0xD4F = Neoverse V2 (AWS Graviton 4, GCP Axion)

  → Map the detected part code to a human-readable name
  → Store this name for display on the dashboard

Cloud provider hint:
  → Some providers expose their identity through other means
  → Check the file /sys/class/dmi/id/board_vendor
  → Check /sys/class/dmi/id/chassis_vendor
  → Check the hostname pattern (graviton, axion, cobalt are common patterns)
  → This is a hint, not a guarantee — the CPU part code is authoritative

What to produce:
  → A structured result containing:
      is_arm64: true or false
      cpu_part_code: the raw hex code
      microarchitecture_name: human-readable (Neoverse V1, etc.)
      cloud_provider_hint: AWS Graviton 3, GCP Axion, etc.
      detection_confidence: high (from CPU part) or low (from hostname hint)
```

### Step 2 — CPU Feature Detection

```
What to determine:

The /proc/cpuinfo Features line on Arm64 lists CPU capabilities.
Each capability is a short string (dotprod, i8mm, sve, sve2, etc.).

Features that matter for KleidiAI acceleration:

DOTPROD (dot product instructions):
  → The baseline Arm vector math instruction
  → Available on most Arm64 CPUs from 2018 onwards
  → Provides basic matrix multiplication acceleration
  → KleidiAI uses this as the minimum baseline

I8MM (int8 matrix multiplication):
  → More powerful than DOTPROD for INT8 inference
  → Available on Neoverse V1, N2, V2 (Graviton 3+, Axion, Cobalt)
  → Provides significant acceleration for quantized models
  → The most important feature for AI inference on Arm

SVE (Scalable Vector Extension):
  → Variable-width vector operations
  → Available on Graviton 3 (Neoverse V1) and newer
  → Particularly good for certain matrix shapes in attention layers
  → Works alongside I8MM

SVE2 (Scalable Vector Extension 2):
  → Enhanced version of SVE
  → Available on Graviton 4 (Neoverse V2), GCP Axion
  → Further improvements for cryptography and signal processing
  → Less impactful for LLM inference than SVE but still beneficial

BF16 (Brain Float 16):
  → Floating point format for AI workloads
  → Available on newer Arm CPUs
  → Used in some advanced llama.cpp builds
  → Detect it but it is lower priority than I8MM/SVE for this project

How to detect each feature:
  → Read /proc/cpuinfo
  → Find the line that starts with "Features"
  → Split the line into individual feature strings
  → Check if each feature of interest is present in the list

What to produce:
  A feature map showing which capabilities are present:
    dotprod: true/false
    i8mm: true/false
    sve: true/false
    sve2: true/false
    bf16: true/false
```

### Step 3 — Build Selection Logic

```
Based on the feature map, select the correct llama.cpp Docker image.

Decision logic (evaluated in order, first match wins):

If sve2 is true AND i8mm is true:
  → Select: the SVE2 + I8MM optimized build
  → Optimization label: "Maximum (SVE2 + I8MM)"
  → Expected hardware: Graviton 4, GCP Axion

If sve is true AND i8mm is true:
  → Select: the SVE + I8MM optimized build
  → Optimization label: "Full (SVE + I8MM)"
  → Expected hardware: Graviton 3

If i8mm is true (no SVE):
  → Select: the I8MM-only optimized build
  → Optimization label: "High (I8MM)"
  → Expected hardware: Azure Cobalt, Ampere Altra

If dotprod is true (no I8MM):
  → Select: the DOTPROD-only build
  → Optimization label: "Basic (DOTPROD)"
  → Expected hardware: older Arm64 servers

If none of the above:
  → Select: the generic Arm64 build
  → Optimization label: "Generic (Arm64)"
  → Still works, just without vectorization acceleration

If not Arm64 at all:
  → Select: the x86_64 build
  → Optimization label: "No Arm optimization"
  → Show a note on the dashboard: "For best results, use an Arm64 server"

Store the selected image tag alongside the server record.
This selection is shown on the dashboard as part of the deploy preview.
```

### Step 4 — Memory and Disk Assessment

```
Memory assessment for quantization selection:

Read available memory from /proc/meminfo (MemAvailable field).
Convert to gigabytes.

Apply this decision table:

Available RAM → Model and Quantization Recommendation:

More than 14GB available:
  → Can run 7B model at INT4 comfortably
  → Recommendation: 7B Q4_K_M
  → Leave 6GB headroom for the OS, Caddy, and other containers

8GB to 14GB available:
  → Can run 7B model at INT4 with tight headroom
  → Recommendation: 7B Q4_K_M (minimum configuration)
  → Show a note: "Memory is limited. Close other apps before deploying."

4GB to 8GB available:
  → Cannot safely run a 7B model
  → Recommendation: 3B model at INT4
  → Show a note: "Your server has limited memory. Using a smaller model."

Less than 4GB available:
  → Cannot run any recommended model
  → Block the deployment with a clear message:
    "AI inference requires at least 4GB of available memory.
     Your server has {N}GB available.
     Stop other running apps or upgrade to a larger server."

Disk assessment for model download:

Model file sizes to know:
  7B Q4_K_M model: approximately 4.1GB
  7B Q2_K model: approximately 2.9GB
  3B Q4_K_M model: approximately 1.9GB

Check available disk on the root filesystem.
If available disk is less than model size plus 1GB buffer:
  → Block deployment:
    "Downloading this model requires {N}GB of free disk space.
     You have {M}GB available.
     Free up space or choose a smaller model."
```

### Step 5 — Storing Readiness Results

```
After all four detection steps complete, store the results.

Where they are stored:
  → The server record in the database gains new fields
  → These fields are populated on first Infer pre-flight check
  → They are refreshed whenever the agent restarts

What is stored:
  → Whether the server is Arm64
  → The detected microarchitecture name
  → The cloud provider hint
  → Which CPU features are present (as a JSON object)
  → The selected optimization label
  → The selected Docker image tag
  → The recommended model size and quantization
  → Whether memory and disk are sufficient

How this information is used:
  → The dashboard reads it and shows a server readiness summary
  → The deploy_inference command reads it to make all decisions automatically
  → The benchmark pipeline reads it to know which builds to compare
  → The user never needs to configure any of this manually
```

### Phase 1 Done Condition

```
Working test on a real Arm64 server (Graviton, Axion, or similar):

□ /proc/cpuinfo is read and parsed correctly
□ CPU part code is identified and mapped to a microarchitecture name
□ All relevant CPU features are detected (DOTPROD, I8MM, SVE, SVE2)
□ The correct optimization level is assigned based on features
□ The correct Docker image tag is selected based on optimization level
□ Available memory is read and the correct model/quantization is recommended
□ Available disk is checked against the model's expected size
□ Insufficient memory blocks deployment with a clear message
□ Insufficient disk blocks deployment with a clear message
□ All results are stored in the server record in the database
□ Running the detection twice produces identical results
□ Detection completes in under 10 seconds

Test on a non-Arm server (x86_64):
□ Detection correctly identifies non-Arm architecture
□ A note is shown (not an error): "For best results, use an Arm64 server"
□ x86 build is selected (deployment still works, just unoptimized)
```

---

## Phase 2 — Model Deployment

### What This Phase Delivers

```
By the end of Phase 2, a user can click one button
and get a working AI inference endpoint over HTTPS.

The endpoint:
  → Accepts POST requests at /v1/chat/completions
  → Returns OpenAI-compatible JSON responses
  → Is accessible over HTTPS with a valid certificate
  → Requires an API key for authentication
  → Stays running until the user stops it

This is the core product promise of Anchor Infer.
Everything else (benchmarks, optimization comparisons)
builds on top of this working foundation.
```

### Step 1 — Template Definition

```
What a model template is:

A template is a structured configuration that describes
everything needed to deploy a specific model.
It is the equivalent of the WordPress or Next.js template
in the existing Anchor template system.

The LLM Chat template contains:

Identity:
  → Template ID: llm-chat-kleidiAI
  → Display name: LLM Chat
  → Description: Deploy a conversational AI endpoint powered by Llama 3.1
  → Category: Language Model

Model specification:
  → Model family: Llama 3.1
  → Model size: 8B parameters
  → Model variant: Instruct (tuned for chat, not just text completion)
  → Source: Hugging Face repository (bartowski/Meta-Llama-3.1-8B-Instruct-GGUF)
  → Default quantization: Q4_K_M (best quality-to-size ratio)
  → Fallback quantization: Q2_K (for servers with less memory)
  → File format: GGUF (the format llama.cpp uses)

Runtime specification:
  → Inference runtime: llama.cpp
  → Server mode: llama.cpp started with --server flag
  → Internal port: 8080 (the port inside the container)
  → API format: OpenAI-compatible
  → API path: /v1/chat/completions
  → Context window: 4096 tokens (reasonable default for chat)
  → Maximum concurrent requests: 4 (appropriate for single-server deployment)

Resource requirements:
  → Minimum available RAM: 6GB
  → Minimum available disk: 5GB
  → Recommended RAM: 10GB or more
  → CPU: Arm64 preferred, x86_64 supported

Benchmark configuration:
  → Prompt set: the fixed 4-prompt set defined in the benchmark plan
  → Warmup runs: 1
  → Measured runs: 2
  → Metrics to capture: tokens per second, time to first token, memory usage

How templates are stored:
  → As JSON files in the agent's embedded filesystem
  → Read at agent startup
  → New templates can be added by adding a new JSON file
  → No code change required to add a new template
  → This is the "pipeline generalizes" claim for the hackathon
```

### Step 2 — The Deploy Inference Command

```
When the user clicks "Deploy" on the dashboard:

What the dashboard sends to the control plane:
  → Which template to use
  → Which server to deploy on
  → No other required input (everything else is automatic)

What the control plane does:
  → Creates a new inference deployment record
  → Creates a command of type deploy_inference
  → Sends the command to the agent via the WebSocket hub
  → Returns a command ID to the dashboard
  → Dashboard subscribes to progress updates for that command ID

What the agent receives:
  → The template specification
  → The server's pre-computed readiness results from Phase 1
  → These two pieces of information are everything needed

The agent then executes the deploy sequence.
Each step sends a progress update to the dashboard.
The user watches it happen in real time.
```

### Step 3 — The Deploy Sequence (Agent Side)

```
The agent executes these steps in order.
Each step is atomic — if it fails, the error is specific
and the previous steps are not undone unnecessarily.

Step 3A: Validate readiness
  → Read the server's pre-computed readiness results
  → Confirm memory and disk are still sufficient
    (they were checked in Phase 1 but server state can change)
  → If insufficient: fail immediately with a clear message
  → Progress update sent: "Checking server capabilities..."
  → Expected duration: under 5 seconds

Step 3B: Pull Docker images
  → Pull the optimized inference runtime image
    (selected in Phase 1 Step 3, based on CPU features)
  → Pull the generic inference runtime image
    (needed for benchmark Phase 1 comparison)
  → Both images need to be present before deployment starts
  → Progress update sent: "Pulling inference runtime images..."
  → Expected duration: 2-10 minutes (depends on image size and network)
  → Docker pull progress is streamed to the live log display

Step 3C: Create the model volume
  → Create a Docker volume named: anchor_{project_name}_model_weights
  → This volume persists the model file across container restarts
  → Check if the volume already exists and contains the model
    → If it does (redeployment scenario): skip Steps 3D and 3E
    → If it does not: proceed with model download
  → Progress update sent: "Preparing model storage..."
  → Expected duration: under 5 seconds

Step 3D: Download the model file
  → Start a temporary Docker container (the downloader)
  → The downloader container's only job: fetch the GGUF file from Hugging Face
  → The GGUF file is written to the model volume
  → The downloader streams its output (download progress) to the agent
  → The agent forwards this to the dashboard live log display
  → After download: validate the file checksum
  → If checksum fails: delete the incomplete file and report error
  → When complete: write a marker file to the volume (.download_complete)
  → Stop and remove the downloader container
  → Progress update sent: "Downloading model (X.XGB of 4.1GB)..."
  → Expected duration: 5-15 minutes on first deploy, 0 on subsequent deploys
  → This step has the longest single-step duration — inform the user:
    "This is a one-time download. Future deployments will start instantly."

Step 3E: Generate API credentials
  → Generate a random 32-byte API key
  → This key will be required in the Authorization header for all API requests
  → Store the key in the project's environment file on the server
    (same mechanism as DATABASE_URL for app deployments)
  → Store only the hash in the database (same as agent secrets and user passwords)
  → Progress update sent: "Generating API credentials..."
  → Expected duration: under 1 second (pure computation)

Step 3F: Start the inference server
  → Create and start the Docker container for the inference server
  → The container uses the optimized runtime image selected in Phase 1
  → The model volume is mounted at /models inside the container
  → llama.cpp is started in server mode pointing at the model file
  → Key llama.cpp flags:
      --model: path to the GGUF file inside the container
      --host 0.0.0.0: listen on all interfaces inside the container
      --port 8080: the port inside the container
      --ctx-size 4096: context window size
      --api-key: the generated API key
      --n-parallel 4: allow up to 4 concurrent requests
  → The container is set to restart automatically if it crashes
  → Progress update sent: "Starting inference server..."
  → Expected duration: 1-2 minutes (model loading into memory takes time)

Step 3G: Wait for the server to be ready
  → The inference server takes time to load the model into memory
  → The agent polls the server's health endpoint until it responds
  → Health endpoint: GET /health on the inference server
  → Poll every 10 seconds
  → Timeout: 3 minutes (model loading on a slow server can be slow)
  → If timeout: report error "Inference server did not start within 3 minutes"
  → Progress update sent: "Waiting for model to load into memory..."
  → Expected duration: 30 seconds to 3 minutes

Step 3H: Configure HTTPS routing
  → Add a Caddy route for this inference endpoint
  → Route: {project_name}.{server_id}.anchor.app → localhost:{container_port}
  → Caddy handles HTTPS certificate automatically (same as any Anchor app)
  → Progress update sent: "Configuring HTTPS endpoint..."
  → Expected duration: under 10 seconds

Step 3I: Test the endpoint
  → Send a simple test prompt through the live endpoint
  → This confirms the full path works:
      HTTPS → Caddy → Container → llama.cpp → Response
  → If the test fails: the deploy is marked as failed despite the server running
    (better to know now than to tell the user it works when it does not)
  → Progress update sent: "Testing endpoint..."
  → Expected duration: 30-60 seconds (model generates a response)
```

### Step 4 — Endpoint Information to Return

```
After the deploy sequence completes successfully:

What is sent back to the dashboard:
  → The HTTPS endpoint URL
    Example: https://my-llm.srv-abc123.anchor.app
  → The API key (the raw key, shown once)
  → The model that was deployed (Llama 3.1 8B Q4_K_M)
  → Which Arm optimization was applied
  → The deployment ID (for future reference)

What the dashboard shows:
  → The endpoint URL in a large, prominent box
  → A one-click copy button next to the URL
  → The API key with a "reveal" button (masked by default)
  → A code example showing how to call the endpoint:
      curl https://my-llm.srv-abc123.anchor.app/v1/chat/completions
           -H "Authorization: Bearer {api-key}"
           -H "Content-Type: application/json"
           -d '{"messages": [{"role": "user", "content": "Hello"}]}'
  → A test input field for immediate testing in the browser

The API key is shown exactly once after deploy.
If the user loses it, they can retrieve it from their environment variables
(via the Anchor dashboard's env var management interface).
```

### Phase 2 Done Condition

```
Working test on a real Arm64 server:

□ Clicking "Deploy LLM Chat" triggers the deploy sequence
□ Progress updates appear for each step in real time
□ Live log output from Docker pull and model download is visible
□ First deploy downloads the model and takes 10-20 minutes total
□ Second deploy (redeployment) skips download and takes under 3 minutes
□ The model volume persists the model file between deploys
□ The inference server container starts and loads the model
□ The HTTPS endpoint is accessible from the public internet
□ Sending a curl request to /v1/chat/completions returns a valid response
□ The API key is required and enforced (request without it returns 401)
□ The endpoint URL is shown on the dashboard after deploy
□ The API key is shown once and can be retrieved from env vars later
□ Container restarts automatically if it crashes
□ The endpoint remains accessible after a server reboot
```

---

## Phase 3 — Benchmark Pipeline

### What This Phase Delivers

```
By the end of Phase 3, every inference deployment automatically
produces a concrete, credible before/after performance comparison.

The comparison shows:
  → How fast the generic build runs on this hardware
  → How fast the Arm-optimized build runs on this hardware
  → The percentage improvement from the optimization

These numbers are measured on the actual server, not estimated.
They are the same numbers Arm Performix would produce.
They are the WOW factor for the hackathon demo.
```

### Step 1 — The Generic Build Baseline

```
Why a baseline is necessary:

Saying "31 tokens per second" means nothing without context.
Saying "158% faster than the generic build" is a concrete claim.
The baseline is what makes the optimized numbers meaningful.

The generic build is the same llama.cpp compiled without KleidiAI kernels.
It represents what a developer would get if they simply ran llama.cpp
without thinking about Arm optimization.

How the baseline is produced:

Timing within the deploy sequence:
  → The baseline benchmark runs after the model is downloaded
  → It runs before the optimized server starts
  → The model volume is already populated (download is done)
  → A generic build container is started temporarily for this purpose

Generic build container lifecycle:
  → Start: docker run with the generic image, mounting the model volume
  → The generic container uses the same model file and same settings
    as the optimized container will use
  → Wait for the model to load (same 30-90 second wait as the optimized build)
  → Run the benchmark prompt set
  → Record the results
  → Stop and remove the generic container completely
  → The generic container is gone — it was only needed for comparison

Why this approach is accurate:
  → Same server, same model file, same quantization
  → Only variable: the runtime binary (generic vs KleidiAI-optimized)
  → Any performance difference is attributable to the optimization
  → This is a valid controlled comparison
```

### Step 2 — The Benchmark Prompt Set

```
A fixed set of four prompts is used every time.
Identical prompts make results comparable across different runs and servers.

The prompts (sent to /v1/chat/completions in server mode):

Prompt 1 — Short factual query:
  Role: user
  Content: "What is the capital of France?"
  Why: measures time to first token primarily
       the model should respond quickly with a single word or short sentence
       this establishes baseline latency

Prompt 2 — Medium explanation task:
  Role: user
  Content: "Explain the difference between a REST API and a GraphQL API
            in two paragraphs."
  Why: measures sustained generation speed over 150-200 tokens
       typical use case for a chat assistant
       long enough to measure tokens/second meaningfully

Prompt 3 — Long generation task:
  Role: user
  Content: "Write a detailed step-by-step tutorial on how to deploy
            a web application to a cloud server. Include at least
            10 steps with detailed explanations for each step."
  Why: measures sustained generation over 400+ tokens
       stress tests the inference speed over longer output
       exposes differences that shorter prompts might not show

Prompt 4 — Code generation:
  Role: user
  Content: "Write a Python function that takes a list of integers
            and returns the top 3 most frequent elements,
            with their counts."
  Why: different type of output from prose
       code generation requires precise token-by-token output
       spot-check for output quality

Measurement approach:
  → Each prompt is sent 3 times in sequence
  → First run: warmup (model's cache is cold — results are misleading)
  → Second and third runs: measured
  → Report the average of the second and third runs

What is measured per prompt:
  → Time to first token: milliseconds from request to first response token
  → Total tokens generated: the length of the model's output
  → Total time to generate all tokens: milliseconds
  → Tokens per second: total tokens divided by total generation time

What is aggregated across all prompts:
  → Median tokens per second (across all measured runs)
  → Median time to first token (across all measured runs)
  → Peak memory usage (from Docker stats during generation)
```

### Step 3 — Arm Performix Integration

```
Arm Performix is the official benchmarking tool from Arm.
Using it gives the results credibility:
  → "Our numbers are from Arm's own benchmarking tool"
  → Judges who know about Arm will recognize Performix
  → Numbers produced by Performix are comparable to published Arm benchmarks

How Performix is used:

Performix runs as a Docker container:
  → Pull the Performix container image before the benchmark starts
  → Run it against the live llama.cpp server endpoint
  → Performix sends its own standardized prompts to the inference server
  → Performix measures and reports the same metrics we care about

The relationship between our prompt set and Performix:
  → Our prompt set runs first (we control it completely)
  → Performix runs second (using its own standardized prompt set)
  → We report both sets of results
  → The Performix numbers are shown with the "Arm Performix" label
    for credibility with judges
  → Our numbers are shown as "Custom benchmark" for the per-prompt breakdown

If Performix is not available (contingency):
  → We have our own measurement infrastructure (the prompt set above)
  → These numbers are still real and valid
  → Label them clearly: "Measured on {microarchitecture} using {model}"
  → The absence of the Performix brand does not invalidate the numbers
  → This contingency exists because Performix availability for a specific
    Arm platform during the hackathon is not guaranteed
```

### Step 4 — Recording and Comparing Results

```
After both benchmark phases complete:

What is recorded for the generic build:
  → Median tokens per second
  → Median time to first token
  → Peak memory usage in GB
  → The Docker image tag used (identifies the generic build)
  → Timestamp of when the benchmark ran

What is recorded for the optimized build:
  → Same metrics as the generic build
  → Plus: which Arm features were used (I8MM, SVE, etc.)
  → Plus: the Docker image tag (identifies the optimized build)
  → Plus: the Arm Performix results (if available)

What is calculated:
  → Tokens per second improvement: ((optimized - generic) / generic) × 100
  → Time to first token improvement: ((generic - optimized) / generic) × 100
    Note: for TTFT, lower is better, so the formula is inverted
  → Memory difference: optimized - generic (usually near zero, same model)

How results are stored:
  → In the inference_benchmarks table in the SQLite database
  → Linked to the specific deployment and server
  → Multiple benchmark runs for the same deployment are all stored
    (so the user can see if results change over time)

How results are sent to the dashboard:
  → The command result message includes the full benchmark comparison
  → The dashboard receives this via the WebSocket hub
  → The benchmark card appears and populates with real numbers
  → No page refresh needed — it just appears
```

### Step 5 — Benchmark Stability and Reliability

```
Benchmarks can vary run to run due to:
  → Other processes using CPU at the same time
  → Memory pressure from other containers
  → Network I/O if the model is still cached in Docker layers

Steps to improve reliability:

Before running the benchmark:
  → Stop non-essential containers temporarily
    (other app containers on the same server can cause interference)
  → Wait 10 seconds after stopping other containers
    (allow OS to reclaim and stabilize resources)
  → Note: this temporarily takes other apps offline
    → Show this clearly to the user:
      "Pausing other apps briefly for accurate benchmarking..."
    → Other apps restart automatically after the benchmark

During the benchmark:
  → Use the 3-run warmup approach (discard first run)
  → Record both runs, report the average
  → If the two measured runs differ by more than 20%: run a third time
    (high variance indicates interference — get a more stable reading)

After the benchmark:
  → Restart the temporarily stopped containers
  → Verify they are healthy before reporting the benchmark complete

When variance is unavoidable:
  → Report the range alongside the average:
    "31 tok/sec (29-33 range across measured runs)"
  → This is more honest than reporting a single number with false precision
  → Judges will appreciate the transparency
```

### Phase 3 Done Condition

```
□ Generic build starts, loads the model, and receives test requests
□ Benchmark prompts are sent to the generic build in the correct order
□ Warmup run is discarded, two measured runs are averaged
□ Generic build is stopped and removed after benchmark completes
□ Optimized build (already deployed) receives the same prompts
□ Both builds use the same model file (same volume, same GGUF)
□ Tokens per second is calculated correctly
□ Time to first token is measured correctly (request sent to first token received)
□ Memory usage is measured correctly from Docker stats
□ Percentage improvement is calculated correctly for both metrics
□ Results are stored in the database
□ Results are sent to the dashboard via the command result message
□ Benchmark card populates with real numbers on the dashboard
□ Running the benchmark twice on the same deployment produces similar results
   (within 20% variance — natural on a shared server)
□ Other containers are paused and restarted correctly around the benchmark
```

---

## Phase 4 — Dashboard and Demo Polish

### What This Phase Delivers

```
By the end of Phase 4, there is a polished, demo-ready
user interface that tells the Anchor Infer story clearly.

The demo flow must work end-to-end in under 5 minutes
for the presentation portion.
The benchmark results must be pre-computed and ready to show
(the actual benchmark takes 20+ minutes — demo on live results
that were produced before the presentation).

The dashboard is clean, focused, and impressive.
No rough edges. No placeholder text. No TODO comments visible to judges.
```

### Step 1 — The Infer Dashboard Page Structure

```
The Anchor Infer dashboard is a single page with four sections.
Sections appear progressively as the deploy advances.
The user does not navigate between pages — everything happens on one URL.

This design choice:
  → Keeps the demo focused on one continuous story
  → Prevents disorientation from page transitions during a presentation
  → Makes the before/after comparison feel like a reveal, not a separate screen

Section visibility rules:

Section 1 (Model Selection): always visible
Section 2 (Deploy Progress): appears when deploy starts, replaces nothing
Section 3 (Live Endpoint): appears when deploy succeeds, Section 2 stays
Section 4 (Benchmark Card): appears when benchmark completes, Sections 2 and 3 stay

By the end of a full deploy and benchmark, all four sections are visible.
The page tells the complete story from top to bottom.
```

### Step 2 — Section 1: Model Selection

```
What this section contains:

Header:
  → "Anchor Infer" as the page title
  → Subtitle: "Deploy AI models on Arm hardware, automatically optimized"

Server status (shown if a server is connected):
  → Server name
  → Connection status dot
  → Detected optimization level: "Full Arm Optimization (I8MM + SVE)"
  → Detected hardware: "AWS Graviton 3 (Neoverse V1)"
  → This information is read from the server's pre-computed readiness results
  → It gives the user confidence that the system understands their hardware

Model template cards:

For each available template, a card showing:
  → Model name (large)
  → One-sentence description of what it does
  → What the user gets: "An OpenAI-compatible chat API endpoint"
  → Resource requirements: "Requires 6GB RAM, 5GB disk"
  → Optimization that will be applied: "Will use I8MM + SVE acceleration"

For the hackathon: two cards minimum, one if time is tight

  Card 1 — LLM Chat:
    → Name: LLM Chat
    → Model: Llama 3.1 8B Instruct
    → Description: Conversational AI for chat applications
    → API: /v1/chat/completions (OpenAI-compatible)
    → This is the primary demo

  Card 2 — Speech to Text (stretch goal):
    → Name: Speech to Text
    → Model: Whisper Base
    → Description: Transcribe audio to text
    → API: /v1/audio/transcriptions (OpenAI-compatible)
    → This shows the pipeline is reusable

Selecting a card:
  → Highlights the selected card (border color change)
  → The deploy button becomes active

Deploy button:
  → "Deploy to Arm Server →"
  → Disabled until a model template is selected
  → Clicking starts the deploy sequence
```

### Step 3 — Section 2: Deploy Progress

```
What this section contains:

Title: "Deploying {model name}..."

Step list (each step is a row with icon, name, and status):

  ○/⏳/✓/✗ Checking server capabilities
  ○/⏳/✓/✗ Pulling runtime images
  ○/⏳/✓/✗ Preparing model storage
  ○/⏳/✓/✗ Downloading model weights (only shown on first deploy)
  ○/⏳/✓/✗ Benchmarking generic build
  ○/⏳/✓/✗ Starting optimized inference server
  ○/⏳/✓/✗ Configuring HTTPS endpoint
  ○/⏳/✓/✗ Benchmarking optimized build

Icons:
  ○ = not started yet (grey circle)
  ⏳ = in progress (animated spinner)
  ✓ = completed successfully (green checkmark)
  ✗ = failed (red X)

Current step is always highlighted with a spinner.
Completed steps show a green checkmark and the time they took.
Example: "✓ Server capabilities checked (3 seconds)"

Below the step list:
  → Live log output area
  → Dark background, monospace font (looks like a terminal)
  → Shows raw output from the agent: Docker pull progress, download progress
  → Auto-scrolls to the bottom as new lines arrive
  → User can scroll up to see earlier output

Duration estimate:
  → Shows elapsed time for the current step
  → Shows total elapsed time since deploy started
  → For the model download step: shows estimated time remaining based on download speed

Note about the one-time download:
  → When the download step is active: a note appears below it:
    "Downloading the model for the first time. Future deployments will skip this step."
  → This sets correct expectations about the time investment

Failed step handling:
  → The failed step shows a red X
  → A clear error message appears below the step list
  → The error is in plain English (not a Docker error code or stack trace)
  → Options shown: "Try Again" or "Change Configuration"
```

### Step 4 — Section 3: Live Endpoint

```
What this section contains:

Title: "✓ Your AI endpoint is live"

Endpoint URL box:
  → The full HTTPS URL in a large, clearly readable box
  → One-click copy button
  → "Open in browser" link (clicking it opens a basic API documentation page)

API Key display:
  → The API key is shown masked: "sk-••••••••••••••••••"
  → "Reveal" button to show the actual key
  → Copy button (copies the real key, not the masked version)
  → Warning: "Save this key now. You can always find it in your environment variables."

Deployment details (smaller, below the URL and key):
  → Model deployed: Llama 3.1 8B Q4_K_M
  → Arm optimization applied: Full (I8MM + SVE)
  → Deployed: just now

Test interface:
  → A text area labeled "Send a test message"
  → Placeholder text: "Ask the model anything..."
  → "Send" button
  → When sent: shows a typing indicator, then the model's response
  → Shows the latency: "Response received in 340ms"
  → This lets judges immediately verify the endpoint works during the demo

Usage example (collapsed by default, expandable):
  → Shows a curl command to call the endpoint
  → Shows a Python code example (3-4 lines)
  → When clicked, the example expands below
```

### Step 5 — Section 4: Benchmark Comparison Card

```
This is the WOW factor of the demo.
It appears after the benchmark pipeline completes.
It should feel like a reveal — the data is shown all at once.

The card design:

Title bar:
  → "Benchmark Results"
  → "Arm Performix" badge (if Performix was used)
  → Date and time of the benchmark

Hardware context (small text below the title):
  → "Measured on AWS Graviton 3 (Neoverse V1) — I8MM + SVE acceleration"
  → "Model: Llama 3.1 8B Q4_K_M"

The comparison table:

  Metric              Generic Build    Arm Optimized    Improvement
  ─────────────────────────────────────────────────────────────────
  Generation speed    12 tok/sec       31 tok/sec       +158% faster
  Time to 1st token   850ms            340ms            60% lower
  Memory usage        4.2GB            4.2GB            Same

Visual treatment:
  → The "Arm Optimized" column is in a highlighted color (green or Arm's brand color)
  → The Improvement column shows percentages in bold
  → Generation speed and latency improvements shown with upward/downward arrows

The WOW number:
  → Below the table, in very large text:
    "158% faster on Arm"
  → This is the headline number — one single claim that summarizes everything
  → It should be large enough to see from across the room during the presentation

Technical detail (collapsed by default):
  → "How is this measured?"
  → When expanded: explains the benchmark methodology
      What prompts were used, how many runs, what was measured
  → This is for judges who want to go deeper
  → Collapsed by default to keep the main view clean

At the bottom of the card:
  → "Run benchmark again" button
    → Triggers run_benchmark command for the existing deployment
    → Useful for: showing judges a second run produces similar numbers
```

### Step 6 — Connection Status and Real-Time Updates

```
How real-time updates work in the Infer dashboard:

WebSocket connection:
  → The Infer dashboard uses the same WebSocket client as the main Anchor dashboard
  → Connects to the same WebSocket hub (Layer 5B)
  → Subscribes to updates for the specific server being used

What updates come through the WebSocket:

command_progress messages:
  → Step name and current status (in progress, complete, failed)
  → Progress percentage
  → Step duration (how long the current step has taken)
  → These update the step list in Section 2

log_lines messages:
  → Raw output from the agent (Docker logs, download progress)
  → These appear in the live log area in Section 2
  → Batched and rate-limited (same mechanism as Layer 7's log viewer)

command_result message:
  → Sent when the deploy command completes (success or failure)
  → Contains the endpoint URL, API key, and benchmark results
  → On success: Section 3 appears with endpoint information
  → The benchmark results are included in the command result
  → Section 4 appears with the benchmark card populated

state_update messages:
  → Sent when the benchmark phase transitions
  → Used to update which step has the spinner

Connection status indicator:
  → A small dot in the page header
  → Green: connected
  → Yellow, pulsing: reconnecting
  → Grey: disconnected
  → If disconnected during a deploy: show a banner
    "Connection to your server was lost. The deploy is still running.
     Reconnecting..."
  → On reconnect: request the current deploy status from the API
    and update the UI to reflect what happened while disconnected
```

### Step 7 — Demo Preparation

```
The actual benchmark takes 20-30 minutes to run.
A hackathon presentation is typically 3-5 minutes.
The demo cannot wait for a live benchmark during the presentation.

The solution: run the benchmark before the presentation.

Pre-demo checklist (do this the night before or morning of):

1. Deploy a fresh instance of the LLM model to the Arm64 server
2. Wait for the benchmark to complete
3. Verify the numbers look correct and are representative
4. Leave the deployment running

During the presentation:

The presenter shows the dashboard.
Section 3 (Live Endpoint) is already populated.
Section 4 (Benchmark Card) is already populated with real numbers.

For the "live" part of the demo:

Option A: Show the test interface in Section 3
  → The presenter sends a prompt in the test interface
  → The model responds live (this is real and takes seconds, not minutes)
  → Judges see a real AI response from a real endpoint
  → The benchmark numbers are already visible above

Option B: Start a new deploy in the background while presenting
  → Begin the deploy at the start of the presentation
  → By the time you get to discussing the results,
    the deploy might be partway through (dramatic live progress)
  → This only works if the model is already downloaded (subsequent deploy)
    → Subsequent deploy skips the model download: 3-5 minutes total
    → If the timing works: a live benchmark completion during the presentation
       is an impressive moment

The right approach for the hackathon:
  → Pre-run the benchmark the night before (Option A for safety)
  → If there is time and confidence in the timing: attempt Option B
  → Option A is the guaranteed impressive demo
  → Option B is higher risk but higher reward
```

### Phase 4 Done Condition

```
□ The Infer dashboard page loads without errors
□ Server readiness information is shown correctly in Section 1
□ Model template cards show correct information
□ Selecting a card enables the Deploy button
□ Clicking Deploy starts the sequence and Section 2 appears
□ Each step updates in real time from WebSocket messages
□ Live logs appear in the log area during deploy
□ Section 3 appears when deploy completes with endpoint URL and API key
□ The test interface sends a prompt and shows the model's response
□ Response latency is shown after the test response
□ Section 4 appears when benchmark completes
□ Benchmark numbers in Section 4 are real (not hardcoded)
□ The percentage improvement is calculated and shown correctly
□ The large "X% faster on Arm" headline is prominent
□ The page looks polished (no placeholder text, no broken layouts)
□ The demo can be presented in under 5 minutes using pre-run results
□ A second deploy on the same server takes under 5 minutes (no re-download)
□ The model responds to test prompts correctly during the presentation
```

---

## Cross-Phase Considerations

### What Must Be Ready Before Phase 1 Starts

```
Infrastructure prerequisites:

An Arm64 server:
  → AWS Graviton 3 or 4 (recommended — widely recognized by judges)
  → GCP Axion (also well-known)
  → Azure Cobalt (less well-known but valid)
  → Minimum: 8 vCPUs, 16GB RAM, 50GB disk
  → The server needs Anchor installed (Layer 1 through Layer 7 already built)

Pre-built Docker images hosted somewhere:
  → The llama.cpp images with different KleidiAI builds
  → These images must exist and be pullable before Phase 2 Step 3B
  → Building them is a one-time infrastructure task
  → They are built from llama.cpp source with specific cmake flags
  → Hosted on a container registry (GitHub Container Registry is free)
  → This must be done before the hackathon begins, not during

Model weights accessibility:
  → The GGUF file is downloaded from Hugging Face during deployment
  → Verify Hugging Face is accessible from the Arm64 server
  → The specific file (bartowski/Meta-Llama-3.1-8B-Instruct-GGUF) must be accessible
  → Test the download before the hackathon to know how long it takes

Arm Performix availability:
  → Check if Arm Performix is available as a container for the target platform
  → If not available: the custom benchmark prompt set is the fallback
  → Do not rely on Performix availability — have the fallback ready
```

### Error Handling Across All Phases

```
At every step, failures must produce messages that are:

Specific: name exactly what failed (not "an error occurred")
Actionable: tell the user what to do next
Non-technical: avoid Docker error codes, stack traces, hex addresses

Failure categories and their messages:

Server not Arm64:
  "Your server is running on x86 architecture.
   Anchor Infer works best on Arm64 hardware for optimization benefits.
   You can still deploy, but Arm-specific acceleration will not be applied."
  → Not a block, just a warning. Deployment proceeds.

Insufficient memory:
  "Your server has {N}GB of available memory.
   Deploying Llama 3.1 8B requires at least 6GB.
   Options: stop other running apps to free memory, or use a larger server."
  → Block with instructions

Model download failure:
  "The model download was interrupted after {N}GB.
   This is usually a temporary network issue.
   The download will resume from where it stopped if you try again."
  → Resumes from partial download if possible

Inference server timeout:
  "The inference server took too long to start ({N} minutes).
   This can happen on servers with very slow disk I/O.
   Try again, or check your server's disk performance."
  → Actionable

Benchmark failure:
  "The benchmark could not complete due to: {specific reason}.
   Your inference endpoint is still running and accessible.
   You can run the benchmark again from the dashboard."
  → Endpoint still works, benchmark is optional
```

### The Stretch Goal Decision Point

```
When to decide whether to build the Whisper template:

After Phase 3 is complete and working:
  → Is the LLM Chat template fully functional?
  → Do the benchmark numbers look correct and credible?
  → Is the dashboard polished?
  → Is there more than 1 full day remaining?

If all of the above: yes, build the Whisper template.
If any are uncertain: no, polish what exists instead.

The Whisper template adds:
  → A second model card in Section 1
  → A different API format (audio file input instead of text)
  → A different metric in the benchmark (words per second for transcription)
  → Proof that the pipeline generalizes

The Whisper template does NOT add:
  → Any new infrastructure
  → Any new phases
  → Any significant new code

The LLM Chat template, fully polished and working, is a complete demo.
The Whisper template is additive evidence, not essential evidence.
```

---

## The Phased Timeline

```
Day 1-2:   Phase 1 — Server readiness detection
           Goal: know exactly what optimization level this server supports

Day 3-5:   Phase 2 — Model deployment
           Goal: a working AI endpoint over HTTPS

Day 6-7:   Phase 3 — Benchmark pipeline
           Goal: real before/after comparison numbers

Day 8-9:   Phase 4 — Dashboard and demo UI
           Goal: a polished, demo-ready interface

Day 10:    Polish and rehearsal
           Goal: the demo works perfectly in under 5 minutes

Decision point after Day 9:
           Is everything working and polished?
           → Yes: build Whisper template (stretch goal)
           → No: polish Phase 4 further

Day before presentation:
           → Run the full deploy and benchmark on the Arm64 server
           → Verify all four sections of the dashboard populate correctly
           → Note the benchmark numbers
           → Leave the deployment running
           → Rehearse the presentation using the pre-run results
```

---

## What Success Looks Like

```
A judge sits down to watch the Anchor Infer demo.

What they see:

"We already built Anchor — a self-hosted deployment platform.
 Here is how we extended it for AI inference on Arm."

  → Dashboard loads showing an Arm64 server
  → Server shows: "AWS Graviton 3 — Full Arm Optimization (I8MM + SVE)"
  → LLM Chat template card is selected
  → A previous deploy is shown: endpoint URL and benchmark card visible
    (pre-run results from the night before)

The presenter explains:
  → "One click deployed Llama 3.1 8B on this Graviton 3 server"
  → "The system automatically detected I8MM and SVE support"
  → "Selected the KleidiAI-optimized llama.cpp build"
  → "Ran a baseline benchmark on the generic build"
  → "Deployed the optimized build and benchmarked that too"

The presenter points to the benchmark card:
  → "Generic build: 12 tokens per second"
  → "Arm-optimized build: 31 tokens per second"
  → "158% faster — same hardware, same model, different binary"

The presenter sends a test prompt in the test interface:
  → A real response appears in a few seconds
  → "This is the live endpoint running on Arm right now"

The presenter explains the architecture:
  → "Adding a new model template is a configuration change, not a code change"
  → "Whisper uses the same pipeline — detect Arm, select build, benchmark"
  → Shows the Whisper card (if built)

The judges see:
  → Real numbers from real hardware
  → A production-quality UI (not a notebook or script output)
  → A reusable system (not a one-off demo)
  → A connection to an existing product (Anchor)
  → A concrete answer to "why Arm?" with evidence
```

---

## Integration status (wired into Anchor)

```
Connected end-to-end through the same agent WS hub, auth, and commands table:

  Dashboard  /servers/{id}/infer
    → GET  /api/v1/servers/{id}/platform
    → POST /api/v1/servers/{id}/platform/detect   (detect_platform command)
    → GET  /api/v1/infer/templates
    → POST /api/v1/servers/{id}/infer/deploy       (deploy_inference, 45m timeout)
    → GET  /api/v1/servers/{id}/infer/status
    → GET  /api/v1/servers/{id}/infer/benchmarks
    → Live progress via browser WS (command_progress / command_result)

  Agent
    → On connect: platform_report → server_platform table
    → detect_platform → nested PlatformInfo in command result → upserted
    → deploy_inference → model pull, dual bench, Caddy route, endpoint test

  Still required for a live Arm demo (operator steps):
    → Build/push images: ./infer/docker/build.sh ghcr.io/<you>/anchor-infer --push
    → export ANCHOR_INFER_IMAGE_BASE=ghcr.io/<you>/anchor-infer on the agent
    → Connected Arm64 agent with enough RAM/disk
    → Run Actions workflow validate.yml and paste numbers into BENCHMARKS.md
    → Optional: Whisper template, Performix, checksum validation
```
