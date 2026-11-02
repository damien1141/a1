# Decision & Orchestration Reference

Deep dive on the RAG-vs-fine-tune-vs-prompt decision table, plus patterns for combining them and orchestrating agentic LLM systems.

## The Three Levers in Detail

### Lever 1: Prompt Engineering

**What it changes:** the input text the model sees. No model weights touched.

**Cost:** $0 to iterate (just API calls); minimal infra.

**Best for:**
- Prototyping (always start here)
- Tasks the base model already nearly does
- Tasks where context window + examples suffice
- Fast-changing requirements (iterate in minutes)

**Limits:**
- Bounded by the model's existing capability
- Context window ceiling (examples + retrieved context must fit)
- Latency scales with prompt length
- Can't teach truly new behavior (e.g., a new language, a new output style the model can't mimic from examples)

### Lever 2: Retrieval-Augmented Generation (RAG)

**What it changes:** the context the model sees at inference time. Still no weight changes.

**Cost:** moderate infra (vector DB, embedding pipeline, ingestion); per-query retrieval cost.

**Best for:**
- Knowledge that changes (docs, tickets, code, product catalogs)
- Private facts the model never saw
- Tasks requiring citations / provenance
- Multi-tenant systems (per-tenant corpora)

**Limits:**
- Retrieval quality is the ceiling — bad retrieval = bad answers, no matter how good the LLM is
- Doesn't change model behavior (style, format consistency, reasoning patterns)
- Latency cost: +200–800ms typical for retrieval + rerank
- Doesn't help if the model lacks the *capability* (e.g., can't do the math even with the right formula retrieved)

### Lever 3: Fine-Tuning

**What it changes:** the model's weights (or adapter weights via PEFT).

**Cost:** high — dataset prep (10s of hours), training compute (hours to days), eval, deployment, monitoring.

**Best for:**
- Style/voice/format consistency that prompts can't reliably enforce
- Tasks where the base model lacks the capability (rare domain, new pattern)
- Compressing cost (fine-tune a small model to match a big model's prompt-heavy performance)
- Tasks where you've hit the context-window ceiling with examples

**Limits:**
- Expensive to iterate (each run = hours + compute)
- Risk of catastrophic forgetting (model loses general capability)
- Static — to update knowledge, you re-train or pair with RAG
- Requires labeled data (hundreds to thousands of examples for PEFT; more for full FT)
- Hard to debug (which training example caused this behavior?)

## Decision Table (Extended)

| Situation | Recommended Path |
|---|---|
| New project, no data, fast iteration needed | Prompt (zero-shot → few-shot) → measure |
| Need to answer questions over private docs | RAG → measure → maybe fine-tune for style |
| Need consistent JSON output from a strong model | Prompt + function calling → measure |
| Need consistent JSON output at scale, cheaply | Fine-tune a small model with function calling |
| Need a specific writing style (brand voice) | Few-shot prompt → fine-tune if not enough |
| Need to extract fields from 10M documents | Fine-tune (cost per call dominates) |
| Model needs to call APIs (search, calc, DB) | Prompt + tool calling (ReAct / function calling) |
| Need math/logic accuracy | Prompt + CoT + tool-augmented; fine-tune a reasoning model only if all else fails |
| Multi-turn dialog with persona | System prompt + RAG for memory → fine-tune if persona drifts |
| Need to handle a rare language | Fine-tune (continued pretrain + instruction) |
| Need to reduce latency 10x | Fine-tune a smaller model; quantize; serve on vLLM |
| Need to update knowledge hourly | RAG (do NOT fine-tune) |
| Need to update behavior/style weekly | Prompt (do NOT fine-tune — too expensive to iterate) |
| Need both fresh knowledge AND consistent style | RAG (for knowledge) + fine-tune (for style) |

## Combining Levers

### RAG + Fine-tune (most common combo)

- Fine-tune for **style/format/capability** (e.g., always answer in your brand voice, with citations, in JSON)
- RAG for **knowledge** (latest docs, per-tenant data)

The fine-tuned model learns *how* to use retrieved context; the RAG system provides *what* to say. Order of operations: get RAG working first (knowledge is usually the bigger lever), then fine-tune to fix persistent style/format issues.

### Prompt + RAG + Fine-tune (full stack)

For production systems at scale:
1. Fine-tune a small model (8B) for the task's style/format/capability
2. RAG for grounding in fresh knowledge
3. Prompt (system prompt + few-shot) for task framing and guardrails
4. Function calling for structured output

This stack typically beats any single lever alone and is cheaper than prompting GPT-4 at scale.

## Agentic Patterns

When a single LLM call isn't enough, orchestrate multiple calls.

### Single-turn (most production systems)

```
User → LLM (with RAG + tools) → Response
```

Sufficient for 80% of production LLM applications. Don't add agents unless this fails the eval bar.

### Tool-augmented (ReAct)

```
User → LLM → tool call → observation → LLM → ... → Response
```

LLM decides which tool to call (search, DB, calculator, API) based on the question. Implement with function calling (not free-text ReAct — function calling is more reliable).

```python
tools = [
    {"name": "search_docs", "description": "Search internal docs", "schema": ...},
    {"name": "query_db", "description": "Run SQL query", "schema": ...},
    {"name": "calculate", "description": "Math calculation", "schema": ...},
]
# Model picks tool → you execute → feed result back → repeat until "final_answer"
```

**Cost warning:** agentic loops can cost 10–100x a single call. Set a max-iteration cap (3–5) and per-request token budget.

### Multi-agent (rare in production)

Multiple specialized agents (planner, researcher, writer, critic) collaborate. Use for complex workflows (research reports, code review pipelines). **Caveat:** hard to debug, expensive, often overkill. Start with single-agent + tools; only escalate to multi-agent when you've measured a clear gap.

### Workflow Engines

For production multi-step LLM workflows, use:
- **LangGraph** — explicit graph of states/edges; best for complex branching
- **LlamaIndex Workflows** — event-driven; good for RAG-heavy flows
- **Instructor** — structured output extraction; pairs with any orchestrator
- **Custom** — for simple flows, plain Python async is often enough; don't over-engineer

## Cost & Latency Budgets

### Typical per-call budgets

| Application | Quality bar | p95 latency | $/call |
|---|---|---|---|
| Internal tool (low-stakes) | 80% accuracy | 3s | $0.01 |
| Customer-facing chat | 90% satisfaction | 2s | $0.005 |
| Structured extraction (batch) | 95% F1 | 30s | $0.001 |
| Code generation | 70% acceptance | 10s | $0.02 |
| Mission-critical (medical, legal) | 99%+ accuracy | 5s | $0.05+ |

### Cost Reduction Patterns

| Pattern | Savings | Trade-off |
|---|---|---|
| Smaller model (GPT-4o → mini) | 10–30x | Quality drop on hard tasks |
| Fine-tune small model for narrow task | 50–100x | Up-front training cost |
| Prompt caching (Anthropic) | 50–90% of prompt cost | Cache invalidation complexity |
| Batch API (50% discount) | 2x | Async only (not real-time) |
| Reduce k in top-k retrieval | 30–50% retrieval cost | Slight recall hit |
| Skip rerank for easy queries | 30% latency | Slight quality hit |
| Route easy queries to cheaper model | 50–80% | Routing logic complexity |

### Latency Reduction Patterns

| Pattern | Speedup | Trade-off |
|---|---|---|
| Streaming responses | perceived -50% | No real speedup, just UX |
| Speculative decoding | 2–3x | Requires draft model |
| Smaller model | 2–10x | Quality |
| Quantization (4-bit) | 1.5–2x | Small quality hit |
| vLLM (PagedAttention) | 2–10x | Self-host complexity |
| Reduce prompt length | linear | Quality (if you cut too much) |
| Parallel tool calls | -50% on multi-tool | More API load |

## Model Selection Guide (2024–2025)

### By Tier

| Tier | Models (2024) | When |
|---|---|---|
| Frontier | GPT-4o, Claude 3.5 Sonnet, Gemini 1.5 Pro | Hardest tasks; prototyping; high-stakes |
| Mid | GPT-4o-mini, Claude 3.5 Haiku, Gemini 1.5 Flash | Production default; cost/quality balance |
| Open frontier | Llama 3.1 405B, Qwen 2.5 72B | Self-host; privacy; research |
| Open mid | Llama 3.1 8B, Mistral 7B, Qwen 2.5 7B | Fine-tuning targets; edge |
| Small/edge | Phi-3 mini, Gemma 2 2B | On-device; very tight latency |

### Selection Heuristics

- **Start prototyping** on the strongest frontier model (Claude 3.5 Sonnet or GPT-4o)
- **For production**: measure on a mid-tier model first; only escalate to frontier if eval fails
- **For fine-tuning**: start with Llama 3.1 8B (best open-weights at the size); only go larger if eval demands
- **For cost-sensitive batch**: GPT-4o-mini or Claude Haiku at scale; consider fine-tuned 8B if volume justifies
- **For privacy/on-prem**: Llama 3.1 family self-hosted on vLLM
- **For edge/mobile**: Phi-3, Gemma 2, or fine-tuned Llama 3.2 1B/3B

## Common Pitfalls

1. **Fine-tuning to fix a prompt problem** — if you can't get a good prompt, fine-tuning won't save you (the model is already capable; you're not giving it the right context). Fix the prompt first.

2. **RAG for style** — RAG gives the model facts, not style. If the issue is "answers don't sound like our brand," that's a prompt or fine-tune problem, not RAG.

3. **Fine-tuning for fresh knowledge** — fine-tuned models have a training cutoff. To update knowledge, retrain (expensive) or add RAG (cheap).

4. **Agentic overkill** — agents are 10–100x the cost of single calls. Most production tasks are single-call. Reach for agents only when you've measured that single-call can't meet the bar.

5. **No online eval** — eval set quality is necessary but not sufficient. Production drifts. Add LLM-as-judge sampling and user feedback monitoring.

6. **Switching models without re-eval** — prompts transfer imperfectly. Always re-run the full eval set on any model swap.
