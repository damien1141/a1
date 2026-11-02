# LLM Evaluation Reference

Evaluation is the discipline that separates LLM engineering from prompt hobbyism. Read alongside every other reference in this skill — eval is the gate every change must pass.

## Eval Set Construction

### Sourcing Examples

| Source | Quality | Volume | Notes |
|---|---|---|---|
| Production logs (labeled) | Best | High | Watch for privacy; sample diverse inputs |
| Synthetic generation (LLM) | Med-High | High | Use a strong model; manually review 10% |
| Hand-crafted | Highest | Low | Best for edge cases + adversarial |
| Public benchmarks (MMLU, etc.) | Med | Med | Good for sanity; not for production fit |
| User feedback (thumbs up/down) | High | Growing | Use for online eval, not iteration |

**Minimum size:** 50 examples. **Preferred:** 200+. For classification with rare classes, ensure ≥20 examples per class.

### Composition Rules

- **Happy path (60%)** — typical inputs the model should handle easily
- **Edge cases (25%)** — empty input, very long input, unusual formats, near-misses
- **Adversarial (15%)** — prompt injection, jailbreaks, PII in input, off-topic requests, ambiguous inputs

### Hold-Out Discipline

- **Train set** — for few-shot examples and fine-tuning. Model can see this.
- **Eval set** — for iteration. Model never sees these as few-shot. **Never** mix with train.
- **Test set** — frozen, run only at major milestones (release candidates). 100–500 examples.

Leakage inflates every metric. If you accidentally use an eval example in a few-shot prompt, **that example is now contaminated** — remove it from eval.

## Task-Specific Metrics

### Classification

| Metric | When | Notes |
|---|---|---|
| Accuracy | Balanced classes | Misleading on imbalanced |
| F1 (macro) | Imbalanced classes | Treats all classes equally |
| F1 (weighted) | Imbalanced + frequency matters | Production default |
| Precision@k | Retrieval / top-k | Use with recall@k |
| Recall@k | Retrieval | Critical for RAG |
| ROC-AUC | Binary, threshold tuning | Use for picking operating point |

### Generation

| Metric | When | Notes |
|---|---|---|
| Exact match | Extractive tasks (QA, extraction) | Strict; use normalize first |
| BLEU | Translation | N-gram overlap; correlates poorly with quality |
| ROUGE | Summarization | N-gram recall; weak signal |
| BERTScore | Semantic similarity | Better than BLEU/ROUGE |
| Perplexity | LM quality (training) | Only meaningful comparing same tokenizer |
| LLM-as-judge | Open-ended | Default for chat / creative |

### Structured Output

| Metric | When |
|---|---|
| Schema validity | Always (Pydantic raises on violation) |
| Field accuracy | Per-field exact match or F1 |
| Parse failure rate | Track % of calls that fail schema validation |

## LLM-as-Judge

Use a strong model (GPT-4o, Claude 3.5 Sonnet) to grade outputs on a rubric.

```python
JUDGE_PROMPT = """You are evaluating an AI assistant's response.

Question: {question}
Reference answer: {reference}
Assistant's response: {response}

Grade on a 1-5 scale:
- 5: Equivalent or better than reference
- 4: Mostly correct, minor issues
- 3: Partially correct, missing key info
- 2: Mostly wrong
- 1: Completely wrong or harmful

Output JSON: {{"score": <1-5>, "reasoning": "<one sentence>"}}
"""
```

### Rubric Design

1. **Concrete anchors** for each score (what does a "4" look like?)
2. **Single dimension** per judge call (don't ask "score correctness AND style" — split into two calls)
3. **Examples** in the rubric (few-shot the judge)
4. **Reference answer** when available (grounded judging > absolute judging)

### Position Bias Mitigation

LLM judges prefer the first option presented. Mitigate:
- Run pairwise comparison both directions (A vs B, then B vs A); accept only consistent verdicts
- Randomize position
- Use absolute scoring (1–5) rather than pairwise when possible

### Reliability Checks

- **Inter-judge agreement:** run 2 different judge models on a subset; if agreement <80%, your rubric is unclear
- **Self-consistency:** run same judge 3x at temp 0.7; if variance >1 point, the case is ambiguous
- **Human spot-check:** manually grade 5% to verify judge quality

## Regression Testing

```python
# tests/test_regression.py
import pytest, json
from myapp.pipeline import run

REGRESSION_SET = [json.loads(l) for l in open("eval/regression.jsonl")]

@pytest.mark.parametrize("case", REGRESSION_SET)
def test_no_regression(case):
    out = run(case["input"])
    # Compare against the recorded golden output
    assert fuzzy_match(out, case["golden_output"]) >= 0.9, \
        f"Regression: {out} vs {case['golden_output']}"
```

**Golden outputs:** the best-known output for each eval input. Update goldens **only** when a deliberate improvement is verified — never silently.

## Online Evaluation

After deployment, monitor in production:

| Signal | What it tells you | Alert threshold |
|---|---|---|
| Success rate (parse failures, errors) | System health | < 95% |
| p50 / p95 latency | User experience | p95 > 2x baseline |
| Cost per call | Budget | > 1.5x baseline |
| User feedback (thumbs) | Quality drift | negative rate > 10% |
| LLM-as-judge (sampled 1%) | Silent quality regression | < 0.5 points vs baseline |
| Input distribution | Concept drift | KL divergence > 0.2 |

### Shadow Evaluation

Run new prompts/models in shadow mode: serve from the old model, but also call the new model and log both outputs. Compare metrics offline before promoting.

## A/B Testing

For high-stakes changes (new prompt, new model, fine-tune):

1. **Define hypothesis** — "fine-tuned model increases F1 by ≥3 points"
2. **Power analysis** — calculate sample size needed to detect the effect (typically 1k–10k requests per arm)
3. **Randomize** by user (not by request — same user sees same variant)
4. **Run until significance** — p < 0.05 AND practical significance (>noise floor)
5. **Watch for novelty effects** — first-week performance ≠ steady-state

## Cost & Latency Budgeting

Define before iterating:

```yaml
budget:
  quality:
    metric: f1_weighted
    bar: 0.85
  latency:
    p50_ms: 800
    p95_ms: 2000
  cost:
    per_call_usd: 0.005
    monthly_usd: 5000
```

Track per-technique:
| Technique | Quality | p95 latency | $/call | vs baseline |
|---|---|---|---|---|
| Baseline (zero-shot, gpt-4o-mini) | 0.78 | 600ms | $0.0008 | — |
| + few-shot (5 examples) | 0.83 | 900ms | $0.0015 | +5 pts, +50% cost |
| + RAG (top-5 reranked) | 0.88 | 1400ms | $0.003 | +10 pts, +3x cost |
| + fine-tuned 8B (local) | 0.89 | 800ms | $0.0005 (GPU amortized) | +11 pts, -40% cost |

Pick the row that meets the bar at lowest cost. Often that's not the highest quality.

## Reproducibility Checklist

Before claiming a result:
- [ ] Random seed set (Python, NumPy, PyTorch, model SDK)
- [ ] Model version pinned (e.g., `gpt-4o-2024-08-06`, not `gpt-4o`)
- [ ] Prompt version tagged in git
- [ ] Eval set versioned (hash or git ref)
- [ ] Temperature and all params recorded
- [ ] Cost and latency numbers from production-scale run, not single call
- [ ] Compare to baseline measured on the **same eval set, same day**

## Common Failure Modes

| Symptom | Cause | Fix |
|---|---|---|
| Eval looks great, prod fails | Eval set unrepresentative | Sample from prod logs; expand edge cases |
| Metric improves, users hate it | Wrong metric | Add user feedback signal; LLM-as-judge |
| Huge variance run-to-run | Small eval set; nondeterminism | Expand set; fix temperature; average runs |
| Quality regresses silently | Model provider changed version | Pin model version; monitor online |
| Cost balloons | Added CoT/few-shot without budget check | Add cost gate to operating loop |
| Can't reproduce a number | Missing tags / unpinned versions | Use reproducibility checklist |
