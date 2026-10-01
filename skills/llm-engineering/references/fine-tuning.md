# Fine-Tuning Reference

Adapting foundation models via parameter-efficient methods (PEFT). Read alongside `llm-evaluation.md` — fine-tuning without an eval harness is vibes.

## Method Selection

| Method | Trainable params | Memory | When to use |
|---|---|---|---|
| Full fine-tune | 100% | Very high (model + optimizer state) | Small models (<3B) or research |
| LoRA | 0.1–1% | Medium (base in fp16) | Default for >7B instruction tuning |
| QLoRA | 0.1–1% | Low (base 4-bit nf4) | Constrained GPU (single 24GB GPU for 70B) |
| AdaLoRA | 0.1–1% | Medium | When you want adaptive rank allocation |
| IA³ | <0.1% | Low | Lightest touch; style/voice adaptation |
| Prompt tuning | <0.1% | Very low | Multi-task serving; one base, many soft prompts |

**Default choice:** LoRA. Move to QLoRA only when GPU memory forces it (you pay a small quality tax for 4-bit). Skip full fine-tuning unless you have a clear reason and the compute.

## Dataset Preparation

### Format: Alpaca-style JSONL

```json
{"instruction": "Classify the ticket", "input": "I was charged twice", "output": "{\"category\":\"billing\",\"urgency\":3}"}
```

For chat models, prefer the ShareGPT / chat template:

```json
{"messages": [{"role":"system","content":"You are a classifier..."},{"role":"user","content":"..."},{"role":"assistant","content":"..."}]}
```

### Quality Checks (always run before training)

```python
import json
from collections import Counter

with open("train.jsonl") as f:
    data = [json.loads(l) for l in f]

# 1. Schema check
required = {"instruction", "output"}
assert all(required.issubset(d.keys()) for d in data), "Missing required keys"

# 2. Length distribution (token estimate: chars/4)
lengths = [len(d["instruction"])+len(d.get("input",""))+len(d["output"]) for d in data]
print(f"Length p50={sorted(lengths)[len(lengths)//2]}, p95={sorted(lengths)[int(len(lengths)*0.95)]}, max={max(lengths)}")

# 3. Deduplication (exact)
seen = set(); deduped = []
for d in data:
    key = (d["instruction"], d.get("input",""))
    if key not in seen:
        seen.add(key); deduped.append(d)
print(f"Dedup: {len(data)} -> {len(deduped)}")

# 4. Output distribution (catch imbalance)
out_dist = Counter(d["output"][:30] for d in data)
print("Output distribution (top 10):", out_dist.most_common(10))
```

**Gate:** max length < model's context window (with room for the prompt template); no exact duplicates; output distribution matches production expectations (within 2x).

### Catastrophic-Forgetting Mix

Before fine-tuning, set aside a **general capability eval set** (50–200 examples that test things the model already does well — MMLU subset, generic QA, code generation). After fine-tuning, re-run it. If general accuracy drops >5 points:

1. Mix in 5–15% general-domain data (e.g., FLAN, OpenOrca) into the fine-tune set
2. Lower the learning rate (halve it)
3. Reduce epochs to 1–2
4. Consider EWC (Elastic Weight Consolidation) if you have the engineering budget

## LoRA Configuration

```python
from peft import LoraConfig, get_peft_model, TaskType

lora_config = LoraConfig(
    task_type=TaskType.CAUSAL_LM,
    r=16,                  # rank — try 8/16/32; higher = more capacity, more overfitting risk
    lora_alpha=32,         # scaling; typically 2× rank
    target_modules=["q_proj", "v_proj"],  # minimum; add k_proj, o_proj for quality
    lora_dropout=0.05,     # regularization
    bias="none",
)
```

### Target Modules

| Target | Effect | Use when |
|---|---|---|
| `["q_proj","v_proj"]` | Baseline | Default; lowest memory |
| `["q_proj","k_proj","v_proj","o_proj"]` | Better quality | +50% trainable params, ~5% better |
| All linear (q,k,v,o,gate,up,down) | Best quality | Worth it for instruction-heavy tasks |

### Rank (`r`) Selection

- `r=8` — light touch, fast; for style/voice tasks
- `r=16` — default; works for most instruction tuning
- `r=32–64` — heavy adaptation (domain shift, new tasks); watch overfitting

## QLoRA (4-bit quantization)

```python
from transformers import BitsAndBytesConfig

bnb_config = BitsAndBytesConfig(
    load_in_4bit=True,
    bnb_4bit_quant_type="nf4",          # NormalFloat 4 — best for LLMs
    bnb_4bit_compute_dtype=torch.bfloat16,
    bnb_4bit_use_double_quant=True,      # quantize the quantization constants
)
model = AutoModelForCausalLM.from_pretrained(
    model_id, quantization_config=bnb_config, device_map="auto")
```

**Memory budget (approx, bf16 LoRA, seq_len 2048):**
- 7B model, LoRA: ~16GB
- 7B model, QLoRA: ~6GB
- 13B model, QLoRA: ~10GB
- 70B model, QLoRA: ~40GB (A100 80GB single GPU)

## Training Arguments (production defaults)

```python
from transformers import TrainingArguments

args = TrainingArguments(
    output_dir="./ckpt",
    num_train_epochs=3,                  # 1–5; more = overfit risk
    per_device_train_batch_size=4,
    gradient_accumulation_steps=4,       # effective batch = 16
    learning_rate=2e-4,                  # LoRA: 1e-4 to 5e-4; full FT: 1e-5 to 5e-5
    lr_scheduler_type="cosine",          # cosine > linear for fine-tuning
    warmup_ratio=0.03,                   # always use warmup
    bf16=True,                           # use bf16 not fp16 on Ampere+
    logging_steps=10,
    eval_strategy="steps",
    eval_steps=100,
    save_steps=200,
    save_total_limit=3,
    load_best_model_at_end=True,
    metric_for_best_model="eval_loss",
    greater_is_better=False,
    report_to="tensorboard",
)
```

### Hyperparameter Ranges

| Hyperparameter | Range | Default | Notes |
|---|---|---|---|
| Learning rate | 1e-5 to 5e-4 (LoRA) | 2e-4 | Higher for LoRA than full FT |
| Batch size (effective) | 8 to 64 | 16 | Higher = more stable, more memory |
| Epochs | 1 to 5 | 3 | Watch eval loss for plateau |
| Warmup ratio | 0.01 to 0.05 | 0.03 | Critical for stability |
| Weight decay | 0.01 to 0.1 | 0.01 | Regularization |
| LoRA rank | 8, 16, 32, 64 | 16 | Sweep on small grid |

## Monitoring Training

Watch in TensorBoard / Weights & Biases:
- **Train loss** — should decrease smoothly
- **Eval loss** — should decrease; **plateau or increase = overfitting → stop**
- **Learning rate** — verify scheduler shape
- **Gradient norm** — spikes >10 indicate instability; lower LR
- **GPU memory** — spikes indicate leak; check for unexpected accumulation

**Early stop rule:** if eval_loss doesn't improve for 2 eval_steps, stop and use the best checkpoint.

## Merge & Deploy

```python
from peft import PeftModel
from transformers import AutoModelForCausalLM

# Merge adapter into base for deployment (saves inference cost — no adapter overhead)
base = AutoModelForCausalLM.from_pretrained(model_id, torch_dtype=torch.bfloat16)
merged = PeftModel.from_pretrained(base, "./lora-adapter").merge_and_unload()
merged.save_pretrained("./merged-model")
```

### Quantize for Serving

| Format | Use case |
|---|---|
| GGUF (llama.cpp) | CPU/Mac inference, edge devices |
| AWQ | GPU serving (vLLM, TGI) |
| GPTQ | GPU serving (alternative to AWQ) |
| fp16/bf16 | Full-precision serving; fastest if memory allows |

### Serving Options

| Server | Strengths |
|---|---|
| **vLLM** | Highest throughput; PagedAttention; OpenAI-compatible API |
| **TGI** (HuggingFace) | Production-tested; easy deployment |
| **llama.cpp** | CPU/Mac/edge; GGUF quantization |
| **Ollama** | Local dev; one-command run |
| **SGLang** | Best for structured outputs / function calling |

## Evaluation Harness

```python
# eval_finetuned.py
import json, torch
from transformers import AutoModelForCausalLM, AutoTokenizer
from peft import PeftModel

base = AutoModelForCausalLM.from_pretrained(BASE_MODEL, torch_dtype=torch.bfloat16, device_map="auto")
ft = PeftModel.from_pretrained(base, "./lora-adapter")

tok = AutoTokenizer.from_pretrained(BASE_MODEL)
eval_set = [json.loads(l) for l in open("eval.jsonl")]

def predict(instruction, input_text=""):
    prompt = f"### Instruction:\n{instruction}\n\n### Input:\n{input_text}\n\n### Response:\n"
    inputs = tok(prompt, return_tensors="pt").to(ft.device)
    with torch.no_grad():
        out = ft.generate(**inputs, max_new_tokens=256, do_sample=False)
    return tok.decode(out[0][inputs.input_ids.shape[1]:], skip_special_tokens=True)

results = [{"pred": predict(d["instruction"], d.get("input","")), "expected": d["output"]} for d in eval_set]
# Compute your task metric here (exact match, F1, BLEU, LLM-as-judge)
```

**Critical comparison:** always run this same eval against the **un-fine-tuned base model** on the same eval set. If the fine-tune doesn't beat base by a clear margin (>noise), the fine-tune didn't help — don't ship it.

## Common Failure Modes

| Symptom | Cause | Fix |
|---|---|---|
| Train loss flat | LR too low; bad data | Increase LR 2x; check dataset format |
| Train loss drops, eval rises | Overfitting | Reduce epochs; add dropout; smaller rank |
| Output is gibberish | Wrong chat template; bad merge | Use model's official chat template; re-merge |
| Model forgot general tasks | Catastrophic forgetting | Add general data mix; lower LR; reduce epochs |
| Slow training | Small batch; no flash-attn | Install flash-attn; increase batch; use gradient checkpointing |
| OOM on 24GB GPU | Full fp16 + Adam state | Switch to QLoRA; enable gradient checkpointing |
