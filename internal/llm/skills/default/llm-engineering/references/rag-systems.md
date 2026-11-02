# RAG Systems Reference

Production-grade Retrieval-Augmented Generation. Covers chunking, embeddings, vector databases, hybrid search, reranking, and evaluation. Read alongside `llm-evaluation.md`.

## Pipeline Stages

```
Documents → Clean → Chunk → Embed → Index (Vector DB)
                                            ↓
Query → Embed → Hybrid Search (dense+sparse) → Rerank → Top-K → LLM → Answer
                                            ↑
                                       Metadata filters
```

## Chunking Strategies

| Strategy | Best For | Quality | Complexity |
|---|---|---|---|
| Fixed-size | Logs, baseline | Low-Med | Simple |
| Recursive character | General text (LangChain default) | Medium | Simple |
| Sentence-based | Conversational, Q&A | Med-High | Medium |
| Semantic | Technical docs, manuals | High | Medium |
| Document-aware (MD/HTML) | Structured content | High | Medium |
| Late chunking | Long-context embeddings (>=8k tokens) | High | Medium |
| Contextual (Anthropic) | Complex documents w/ chunk context | Very High | Complex |

### Recursive Character Splitter (default starting point)

```python
from langchain.text_splitter import RecursiveCharacterTextSplitter

splitter = RecursiveCharacterTextSplitter(
    chunk_size=800, chunk_overlap=100,
    separators=["\n\n", "\n", ". ", " "],
)
chunks = splitter.create_documents(texts, metadatas)
```

**Tune `chunk_size` on your domain.** Never ship 512 blindly. Sweep `{256, 512, 800, 1200}` against retrieval eval (precision@k) before committing.

### Contextual Chunking (Anthropic 2024)

Each chunk is prefixed with a short LLM-generated context (1–2 sentences) explaining where it sits in the source document. Cost: +1 small LLM call per chunk at ingestion. Benefit: typically 30–50% reduction in retrieval failures on long, structured docs. Worth it for high-value corpora.

### Late Chunking

For long-context embedding models (e.g.,voyage-3, jina-v3): embed the entire document first, then chunk the *embeddings* (preserving cross-chunk context). Avoids the "chunk loses context" problem.

## Embedding Models

| Model | Dim | Strengths | Notes |
|---|---|---|---|
| `text-embedding-3-large` (OpenAI) | 3072 | Best general quality | Cost; reduce dim with MRL |
| `text-embedding-3-small` (OpenAI) | 1536 | Good quality, cheap | Default for prototypes |
| `voyage-3` / `voyage-3-large` | 1024/2048 | Top retrieval quality | Long-context (32k) |
| `embed-english-v3.0` (Cohere) | 1024 | Strong + multilingual option | Needs input_type param |
| `bge-large-en-v1.5` (BAAI) | 1024 | Best open-source | Self-host |
| `e5-mistral-7b-instruct` | 4096 | Open, top quality | Large; self-host |

**Rules:**
- Evaluate 3+ models on your domain data before committing (use a held-out retrieval eval set)
- Dimension reduction (Matryoshka) is fine for cost; verify recall hit is acceptable
- Pin the model version — re-embedding the corpus on a silent upgrade will silently change retrieval
- For multi-lingual: pick a multilingual model OR translate-to-English at ingestion (don't mix)

## Vector Databases

| Feature | Pinecone | Weaviate | Qdrant | Chroma | pgvector |
|---|---|---|---|---|---|
| Hosting | Managed only | Managed + self | Managed + self | Self (cloud beta) | Self |
| Hybrid search | Yes (sparse-dense) | Yes (BM25+vec) | Yes (sparse) | Limited | Manual (+pg_trgm) |
| Filtering | Excellent | Excellent | Excellent | Basic | SQL-native |
| Max dims | 20,000 | Unlimited | 65,535 | Unlimited | 2,000 |
| Multi-tenancy | Namespaces | Multi-tenant class | Collections+payload | Collections | Schema/RLS |
| Best for | Enterprise SaaS | Semantic apps | High-perf self-host | Prototyping | Postgres shops |

**Decision flow:**
- Need managed zero-ops → **Pinecone**
- Have Postgres already + <2k dims → **pgvector**
- Need built-in vectorization → **Weaviate**
- Need max perf + self-host → **Qdrant**
- Prototyping → **Chroma**
- Default → **Qdrant** (balance of features/perf)

### Qdrant Quick Start

```python
from qdrant_client import QdrantClient
from qdrant_client.models import VectorParams, Distance, PointStruct, Filter, FieldCondition, MatchValue

client = QdrantClient(host="localhost", port=6333)
client.create_collection("kb",
    vectors_config=VectorParams(size=1536, distance=Distance.COSINE))

# Always create payload indexes for filter columns
client.create_payload_index("kb", "tenant_id", field_schema="keyword")

import hashlib, uuid
def upsert(text, embedding, meta):
    doc_id = str(uuid.UUID(hashlib.md5(text.encode()).hexdigest()))  # dedup
    client.upsert("kb", points=[PointStruct(id=doc_id, vector=embedding, payload={**meta, "text": text})])

def search(q_emb, tenant, top_k=20):
    return client.search("kb", query_vector=q_emb,
        query_filter=Filter(must=[FieldCondition(key="tenant_id", match=MatchValue(value=tenant))]),
        limit=top_k)
```

### pgvector

```sql
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE docs (id SERIAL PRIMARY KEY, content TEXT, embedding vector(1536), tenant_id TEXT);
CREATE INDEX ON docs USING hnsw (embedding vector_cosine_ops) WITH (m=16, ef_construction=64);
CREATE INDEX ON docs (tenant_id);

-- Hybrid search (semantic + trigram)
SELECT id, content,
  0.6 * (1 - (embedding <=> $1)) + 0.4 * similarity(content, $2) AS score
FROM docs
WHERE tenant_id = $3 AND content % $2
ORDER BY score DESC LIMIT 10;
```

## Hybrid Search (Production Default)

Pure vector search misses exact-match queries (product codes, names, error strings). Pure BM25 misses semantic similarity. Combine via **Reciprocal Rank Fusion (RRF)**:

```python
from rank_bm25 import BM25Okapi

def hybrid_search(query, tenant, top_k=20, alpha=0.6):
    q_emb = embed([query])[0]
    dense = qdrant.search("kb", query_vector=q_emb,
        query_filter=tenant_filter(tenant), limit=top_k)
    corpus = [r.payload["text"] for r in dense]
    bm25 = BM25Okapi([d.split() for d in corpus])
    bm25_scores = bm25.get_scores(query.split())
    # RRF or weighted fusion
    fused = sorted(zip(dense, bm25_scores),
        key=lambda x: alpha*x[0].score + (1-alpha)*x[1], reverse=True)
    return fused[:top_k]
```

**Alpha tuning:** start at 0.5, sweep `[0.3, 0.5, 0.7]` on eval set. Keyword-heavy queries → lower alpha; semantic queries → higher.

## Reranking

Cross-encoder reranking on the top-K (20–50) candidates improves precision@5 by 5–15% typically. Cost: ~$0.002/1k tokens for Cohere rerank-english-v3.0.

```python
import cohere, os
co = cohere.Client(os.environ["COHERE_API_KEY"])

def rerank(query, candidates, top_n=5):
    docs = [r.payload["text"] for r in candidates]
    result = co.rerank(query=query, documents=docs,
        top_n=top_n, model="rerank-english-v3.0")
    return [candidates[r.index] for r in result.results]
```

For self-hosted: `bge-reranker-large` (BAAI) — comparable quality, free, runs on a single GPU.

## Index Tuning (HNSW)

| Parameter | Effect | Trade-off |
|---|---|---|
| `m` | Connections per node | Higher = better recall, more memory |
| `ef_construction` | Build-time search width | Higher = better index, slower build |
| `ef_search` | Query-time width | Higher = better recall, slower query |

Defaults (`m=16, ef_construction=64`) work up to ~1M vectors. For 10M+, increase `m` to 32 and benchmark.

### Quantization for Scale

Qdrant scalar quantization (int8) → 4× memory reduction with <2% recall loss:

```python
from qdrant_client.models import ScalarQuantization, ScalarQuantizationConfig
client.update_collection("kb",
    quantization_config=ScalarQuantization(
        scalar=ScalarQuantizationConfig(type="int8", quantile=0.99, always_ram=True)))
```

## Multi-Tenancy

Three patterns, in increasing isolation:
1. **Metadata filtering** — single collection, `tenant_id` payload + filter. Cheapest. Use for most SaaS.
2. **Namespaces (Pinecone) / partitions** — logical separation, shared infra.
3. **Collection per tenant** — strongest isolation; use only for compliance/legal requirements.

Never expose unfiltered search in multi-tenant code. Always pass `tenant_id` to every query.

## Idempotent Ingestion

Use deterministic IDs (hash of content) so re-ingestion deduplicates:

```python
import hashlib, uuid
doc_id = str(uuid.UUID(hashlib.md5(content.encode()).hexdigest()))
```

Track ingestion state in a `documents` table (source, hash, ingested_at, embedding_model_version) for safe re-ingestion and model migration.

## Common Failure Modes

| Symptom | Cause | Fix |
|---|---|---|
| Recall low across the board | Bad chunk size or wrong embedding model | Sweep chunk size; eval 3+ embeddings |
| Recall good, answer wrong | Retrival OK, context not used well | Add rerank; tune prompt; reduce top-k |
| Exact-match queries fail | Pure vector search | Add BM25/sparse hybrid |
| Multi-tenant data leak | Missing filter on query | Always pass tenant_id; test with adversarial queries |
| Costs explode | Re-embedding corpus on each run | Idempotent ingestion + version tracking |
| Recall degrades over time | Concept drift in source data | Schedule re-eval; alert on metric drift |

## Evaluation (RAGAS)

```python
from ragas import evaluate
from ragas.metrics import context_precision, context_recall, faithfulness, answer_relevancy
from datasets import Dataset

results = evaluate(Dataset.from_dict({
    "question": questions, "contexts": retrieved_contexts,
    "answer": generated_answers, "ground_truth": ground_truth,
}), metrics=[context_precision, context_recall, faithfulness, answer_relevancy])
```

**Targets:** `context_precision ≥ 0.7`, `context_recall ≥ 0.6`, `faithfulness ≥ 0.85` before LLM integration. Below these, fix retrieval first — no prompt change will help.
