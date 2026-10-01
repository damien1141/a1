---
name: llm-engineering
description: Designs, builds, and evaluates LLM applications end-to-end — RAG (chunking, embeddings, vector DBs, hybrid search, reranking), fine-tuning (LoRA/QLoRA/PEFT, instruction tuning, catastrophic-forgetting mitigation), and prompt engineering (system prompts, few-shot, CoT, structured output, function calling, guardrails). Enforces eval-first discipline: no technique ships without a measurable lift over baseline on a held-out set.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: ai
  triggers: LLM, RAG, retrieval-augmented generation, fine-tuning, LoRA, QLoRA, PEFT, instruction tuning, prompt engineering, chain-of-thought, few-shot, function calling, structured output, embeddings, vector database, reranking, eval harness
  role: specialist
  scope: implementation
  output-format: code
  related-skills: python-pro, data-engineering, api-design, testing-master
---

# LLM Engineering

Three levers to make a foundation model useful on **your** task: **prompt** (steer with context), **retrieve** (ground with external knowledge), **fine-tune** (adapt weights). The discipline that ties them together is **evaluation** — every change is a hypothesis tested against a labeled set.

## When to Use

- Chatbot, copilot, classifier, extractor, summarizer on top of an LLM
- Grounding answers in private documents (RAG)
- Adapting a foundation model to a domain/style/task (fine-tuning)
- Designing or refactoring prompts for accuracy, latency, or cost
- Shipping structured outputs (JSON / function calling) consumed by code
- Adding guardrails (PII, prompt-injection defense, topic restrictions)
- Diagnosing regressions after a model or prompt upgrade

## Eval-First Discipline (read this first)

Before writing a prompt or chunking a document, answer three questions. If you cannot, stop and define them:

1. **Task** — one sentence: input → output. ("Given a support ticket, return `{category, urgency, summary}`.")
2. **Eval set** — ≥50 labeled pairs (200+ preferred), held out, never used for prompt iteration. Include edge + adversarial cases.
3. **Metric + bar** — task metric (accuracy/F1/exact-match/BLEU-ROUGE/LLM-as-judge) AND a latency/cost budget. Define the **pass bar** before iterating.

**Iron rule:** a change ships only if it beats the previous best by a margin wider than eval noise (±2 typical). No "it looks better."

## Operating Loop

1. **Frame** — Task, eval set, metric, budget written down. **Gate:** ≥50 labeled examples covering happy paths, edge cases, adversarial inputs.
2. **Baseline** — Cheapest plausible approach: zero-shot prompt against the strongest general model. Measure. **Gate:** baseline metric + latency + cost recorded — this is the number to beat.
3. **Choose lever** — Decision table below. Resist fine-tuning until prompt + RAG are exhausted.
4. **Iterate one lever at a time** — Change one thing, re-eval, commit or revert. Tag every run (model, prompt, retrieval, checkpoint). **Gate:** diff vs previous best exceeds noise floor and is reproducible.
5. **Harden** — Add guardrails (input/output validation, injection defense, PII redaction, rate limits). Re-run eval. **Gate:** adversarial set passes; failure modes documented.
6. **Ship + monitor** — Deploy behind a flag; log inputs, outputs, latency, cost, feedback. Sample 1–5% for human review. **Gate:** production metric within 5% of eval; alerting on drift, latency, cost.

## RAG vs Fine-tune vs Prompt — Decision Table

| Signal | Try first | Then |
|---|---|---|
| Knowledge changes frequently (docs, tickets, code) | **RAG** | — |
| Private facts model never saw | **RAG** | — |
| Task needs citations / provenance | **RAG** | — |
| Latency budget tight, output short | **Prompt** (small model) | Distill to fine-tune |
| Behavior/style consistency (tone, format) | **Prompt + few-shot** | **Fine-tune** if examples don't fit context |
| Fixed-schema extraction | **Prompt + function calling** | Fine-tune if accuracy < bar |
| Reasoning depth required | **Prompt + CoT/ReAct + tools** | Fine-tune a reasoning model |
| Prompt hits context window with examples | **Fine-tune** (instruction) | — |
| Compress cost/latency at scale | **Fine-tune smaller model** | Quantize |
| Model lacks capability (rare language) | **Fine-tune** (continued pretrain) | — |
| Facts + style combined | **RAG + prompt** | Add fine-tune for style only |

**Default order:** prompt → RAG → fine-tune. Each step is ~10× more expensive than the previous; descend only when the eval set proves the cheaper lever is exhausted.

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| RAG systems | `references/rag-systems.md` | Chunking, embeddings, vector DBs (Pinecone/Qdrant/Weaviate/pgvector/Chroma), hybrid search, reranking |
| Fine-tuning | `references/fine-tuning.md` | LoRA/QLoRA, PEFT, instruction-tuning data prep, catastrophic-forgetting mitigation, eval harness |
| Prompt engineering | `references/prompt-engineering.md` | System prompts, few-shot, CoT/ReAct, structured output, function calling, guardrails |
| Evaluation | `references/llm-evaluation.md` | Metrics, test suites, LLM-as-judge, regression testing, online monitoring |
| Decision & orchestration | `references/decision-and-orchestration.md` | Decision-table deep dive, RAG+fine-tune combos, agentic patterns, cost/latency budgets |

## Code Examples

### Eval harness (pytest) — build this FIRST

```python
# tests/test_classifier.py
import pytest, json, time
from myapp.pipeline import classify

EVAL_SET = json.load(open("eval/classifier.jsonl"))  # [{input, expected}]

@pytest.mark.parametrize("case", EVAL_SET)
def test_classifier(case):
    start = time.perf_counter()
    out = classify(case["input"])
    latency = time.perf_counter() - start
    assert out["category"] == case["expected"]["category"], out
    assert latency < 1.5, f"latency budget exceeded: {latency:.2f}s"
```

**Gate:** baseline pass rate + p95 latency recorded before any iteration.

### RAG — hybrid search + rerank (minimal; full version in references)

```python
def retrieve(query, tenant, top_k=20):
    q_emb = embed([query])[0]
    dense = qdrant.search("kb", query_vector=q_emb,
        query_filter=tenant_filter(tenant), limit=top_k)
    bm25 = BM25Okapi([r.payload["text"].split() for r in dense])
    fused = sorted(zip(dense, bm25.get_scores(query.split())),
        key=lambda x: 0.6*x[0].score + 0.4*x[1], reverse=True)[:top_k]
    reranked = co.rerank(query=query,
        documents=[r.payload["text"] for r,_ in fused],
        top_n=5, model="rerank-english-v3.0")
    return [fused[r.index][0] for r in reranked.results]
```

**Gate:** `context_precision@5 ≥ 0.7` on the eval set. Cosine-only is a prototype; hybrid + rerank is the production default.

### Fine-tuning — LoRA with PEFT (full config in references)

```python
from peft import LoraConfig, get_peft_model, TaskType
from transformers import AutoModelForCausalLM, TrainingArguments
from trl import SFTTrainer
import torch

model = AutoModelForCausalLM.from_pretrained(
    "meta-llama/Llama-3-8B", torch_dtype=torch.bfloat16, device_map="auto")
model = get_peft_model(model, LoraConfig(
    task_type=TaskType.CAUSAL_LM, r=16, lora_alpha=32,
    target_modules=["q_proj","v_proj"], lora_dropout=0.05, bias="none"))
model.print_trainable_parameters()  # expect ~0.1–1%

trainer = SFTTrainer(model=model,
    args=TrainingArguments(output_dir="./ckpt", num_train_epochs=3,
        per_device_train_batch_size=4, gradient_accumulation_steps=4,
        learning_rate=2e-4, lr_scheduler_type="cosine", warmup_ratio=0.03,
        bf16=True, eval_strategy="steps", load_best_model_at_end=True),
    train_dataset=ds["train"], eval_dataset=ds["test"],
    dataset_text_field="text", max_seq_length=2048)
trainer.train(); model.save_pretrained("./lora-adapter")
```

**Catastrophic-forgetting guard:** capture baseline accuracy on a *general-capability* eval set before training. After training, re-run. If general accuracy drops >5 points, mix 5–15% general data into the fine-tune set (rehearsal) and re-train.

### Prompt — structured output via function calling

```python
from pydantic import BaseModel, Field
from openai import OpenAI

class Ticket(BaseModel):
    category: str = Field(description="billing|bug|howto|account|other")
    urgency: int = Field(ge=1, le=5)
    summary: str = Field(max_length=120)

client = OpenAI()
out = client.beta.chat.completions.parse(
    model="gpt-4o-mini",
    messages=[{"role":"system","content":SYSTEM_PROMPT},
              {"role":"user","content": user_msg}],
    response_format=Ticket)
ticket: Ticket = out.choices[0].message.parsed  # validated
```

Wrap `parse()` in try/except; on validation failure, fall back to a safe default and log. Never trust LLM output unchecked downstream.

## Constraints

### MUST DO
- Define eval set, metric, and pass bar **before** iterating
- Keep a baseline; never ship a change that doesn't beat it beyond noise
- Tag every eval run (model, prompt, retrieval, checkpoint) — reproducibility non-negotiable
- Validate all structured LLM outputs against a schema (Pydantic/Zod) before use
- Measure cost and latency per call, not just quality
- RAG: hybrid search + rerank is the production default
- Fine-tuning: PEFT (LoRA/QLoRA) for >7B models; always include warmup; monitor validation loss
- Fine-tuning: measure catastrophic forgetting on a general eval set
- Test prompts on adversarial inputs (injection, empty, oversized, non-English)
- Version prompts/datasets/checkpoints; treat them as code

### MUST NOT DO
- Fine-tune to "fix" a prompt problem you haven't measured
- Use cosine similarity alone for multi-domain production retrieval
- Deploy without a recorded baseline and post-deploy metric
- Trust model self-reports — measure
- Skip prompt-injection defense on user-controlled inputs
- Hardcode API keys; load from env / secrets manager
- Mix train and eval sets; leakage inflates all later numbers
- Assume `temperature=0` is deterministic — providers don't guarantee it
- Assume a prompt transfers between model families — re-eval on every swap
- Ship a fine-tuned model without comparing against the base on the same eval set

## Output Template

1. **Task framing** — one-sentence task, eval set size/source, metric + pass bar, latency/cost budget
2. **Baseline** — model + prompt used, measured metric/latency/cost
3. **Chosen lever(s)** — prompt / RAG / fine-tune, justified per decision table
4. **Implementation** — code with eval harness, chosen technique, guardrails
5. **Eval report** — table: technique → metric → latency → cost → delta vs baseline; pass/fail vs bar
6. **Risks & mitigations** — failure modes observed, guardrails added, monitoring plan
7. **Reproducibility** — version tags for model, prompt, dataset, checkpoint; commands to re-run eval

## Knowledge Reference

Deep dives in `references/`: RAG (chunking/embeddings/vector DBs/hybrid/rerank), fine-tuning (PEFT/LoRA/QLoRA, instruction tuning, eval harness, catastrophic forgetting), prompt engineering (patterns/structured output/guardrails/context management), evaluation (metrics/LLM-as-judge/regression/online), and decision/orchestration (lever combos, agentic patterns, cost & latency budgets, model selection).
