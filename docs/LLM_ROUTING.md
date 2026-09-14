# LLM routing — models through the same gateway

One gateway in front of **tools (MCP)** and **brains (LLM API calls)** — same keys, same budgets, same kill switch.

## Client setup

Point any OpenAI-compatible SDK at the gateway origin:

```ts
import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "http://localhost:8080/v1",
  apiKey: process.env.TCP_KEY, // tcp_*
});

const res = await client.chat.completions.create({
  model: "chat", // route alias — not the upstream id
  messages: [{ role: "user", content: "Hello" }],
});
```

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $TCP_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"chat","messages":[{"role":"user","content":"Hello"}]}'
```

Auth is **Bearer header only** (same v1 rule as MCP — no `?key=`).

## Route aliases

`llm_models.name` is the **route alias** clients put in `model`. The gateway rewrites it to the upstream model id on the chosen provider. Swap `"chat"` from local vLLM to OpenAI without changing client config.

Configure aliases under **Providers → Model routes** (admin).

## Fallback

When the primary provider fails **before the first response byte** (connection refused, DNS, timeout, 5xx) and the route has `fallback_model_id`, the gateway retries **once** on the fallback route and logs an `llm_fallback` activity event (`from`, `to`, `reason`).

- **Pre-stream only.** Mid-stream failures surface as 502 with whatever frames the client already received — no fallback (partial bytes cannot be unwound).
- **Chain depth max 2** (primary + one hop). Deeper chains hide the real failure; mutual loops are cut at depth 2.

## Metering — exact vs estimated

LLM responses often include `usage: {prompt_tokens, completion_tokens}`:

- **Non-streaming:** parsed from the JSON body.
- **Streaming:** scanned from the final SSE `data:` frame before `[DONE]` (OpenAI stream-usage semantics). The gateway does **not** mutate the client request to force `stream_options`.

When `usage` is present → **exact** tokens are stored. When missing (error bodies, non-conformant servers) → **bytes/4** estimate (same MCP invariant).

Estimate error direction: bytes/4 **under-counts** dense-token payloads and **over-counts** sparse ones. Dashboard Overview reports exact % vs estimated % for LLM traffic.

Tokens count toward:

1. Per-provider monthly budget (`key_provider_budgets`) — **402 the lane**, does **not** kill the key.
2. Key-total `monthly_budget` — same pool as MCP traffic. Exhaustion **kills the key** (existing kill-switch semantics).

## Timeouts

Per-provider `timeout_seconds` (default **300**) covers **connect + time-to-first-byte**. After the first byte, an **idle** timeout of **120s** between SSE frames applies — a long, actively streaming completion is never hard-killed by the total timeout.

## Budgets vs kill switch

| Condition | Status | Effect |
|-----------|--------|--------|
| Per-provider budget exhausted | 402 | That provider lane only; key stays alive |
| Key monthly budget exhausted | 402 | Key suspended (budget body) |
| Kill switch / auto-kill | 402 | Key suspended (kill body) |
| Provider not granted to key | 403 | Scope deny |

## In scope / out of scope (v1)

**In:** OpenAI-compatible `POST /v1/chat/completions` (OpenAI, vLLM, llama.cpp, Ollama-compat, LM Studio, Together, Groq, OpenRouter).

**Out:** embeddings, `/v1/models` proxying (dashboard health-check only hits upstream `/models`), Anthropic-native / Gemini-native APIs, cost-per-token pricing, prompt caching.

Use OpenRouter or Ollama-compat when you need Claude/Gemini-shaped models behind an OpenAI wire format.
