# Project Instructions

## Headroom Token Compression

This project uses [Headroom](https://github.com/headroomlabs-ai/headroom) for automatic token compression. All LLM traffic is routed through the headroom proxy, and MCP tools are available for on-demand compression.

### How it works

- **Automatic**: All messages to the LLM are compressed transparently by the headroom proxy (port 8787). No action needed.
- **On-demand (MCP tools)**: Use these when you receive large tool outputs (file reads, grep results, logs, API responses) to compress them before including in context.

### Available MCP Tools

1. **`headroom_compress`** — Compress large content before reasoning over it.
   - Use when: tool output exceeds ~200 lines, large JSON, log files, search results with many matches.
   - Params: `content` (required) — the text to compress.
   - Returns: compressed text, hash for retrieval, savings stats.

2. **`headroom_retrieve`** — Retrieve original uncompressed content by hash.
   - Use when: you need the full original after compressing it.
   - Params: `hash` (required), `query` (optional filter).

3. **`headroom_stats`** — Check session compression stats.
   - Use when: user asks about token savings or compression performance.

### Best Practices

- Compress large tool outputs (>200 lines) before reasoning over them.
- Always compress when a tool returns more than 5000 tokens of output.
- Check `headroom_stats` if the user asks about savings or token usage.
- Retrieve originals with `headroom_retrieve` when full detail is needed.
