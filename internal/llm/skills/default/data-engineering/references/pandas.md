# pandas 2.x Reference

Production patterns for pandas 2.x with the Arrow-backed dtype system. Read alongside `pipeline-orchestration.md` for ETL context.

## What's New in 2.x

- **PyArrow-backed dtypes** — `pd.ArrowDtype(pa.string())`, `string[pyarrow]`, optional but default-able via `future.infer_string`
- **Copy-on-Write (CoW)** — default in 3.x; opt-in via `pd.options.mode.copy_on_write = True` in 2.x. Eliminates `SettingWithCopyWarning`
- **Nullable types** — `Int64`, `Float64`, `boolean`, `string` (no more silent NaN coercion for integer columns)
- **Better PyArrow interop** — `pd.DataFrame(pa.Table)` and back are zero-copy
- **`pd.concat` everywhere** — `.append()` removed; `.ix` long gone

## Enabling Modern Defaults

```python
import pandas as pd
import pyarrow as pa

pd.options.mode.copy_on_write = True       # opt-in for 2.x; default in 3.x
pd.options.future.infer_string = True       # pyarrow-backed strings by default
pd.options.mode.dtype_backend = "pyarrow"   # arrow dtypes for new DataFrames
```

## Reading & Writing

```python
# Parquet — preferred format (columnar, typed, compressed)
df = pd.read_parquet("data.parquet", engine="pyarrow")
df.to_parquet("out.parquet", compression="snappy", index=False)

# Multiple parquet files (partitioned dataset)
df = pd.read_parquet("s3://bucket/events/", engine="pyarrow",
    filters=[("date", ">=", "2024-01-01")])  # predicate pushdown

# CSV — only for legacy interchange
df = pd.read_csv("data.csv", dtype={"user_id": "string", "amount": "float32"},
    parse_dates=["timestamp"], na_values=["", "NULL", "N/A"])
```

### Format Selection

| Format | When | Notes |
|---|---|---|
| **Parquet** | Default for analytics | Columnar, typed, compressed, predicate pushdown |
| **Arrow IPC** | Cross-language interchange | Faster than parquet for repeated reads |
| **Iceberg / Delta** | Lakehouse with ACID | Use pyiceberg / delta-rs |
| **CSV** | Legacy interchange only | No types; slow; use only when forced |
| **JSON** | Nested/semi-structured | Use `pd.json_normalize` for nested |
| **Feather** | Short-term cache | Fast R/W; not for long-term storage |

## Indexing & Selection

```python
# .loc (label-based) and .iloc (position-based) — never use chained indexing
subset = df.loc[df["status"] == "active", ["user_id", "amount"]].copy()

# .query() for readable filtering on large frames
active = df.query("status == 'active' and amount > 100")

# Multi-index
df_idx = df.set_index(["region", "category"])
df_idx.loc[("US", "electronics")]  # tuple for multi-index lookup

# .isin for filtering
df[df["region"].isin(["US", "CA", "MX"])]

# Conditional assignment with .loc
df.loc[df["amount"] < 0, "amount"] = 0
```

### Anti-patterns

```python
# WRONG: chained indexing → SettingWithCopyWarning
df["amount"][df["amount"] < 0] = 0

# RIGHT: .loc with single operation
df.loc[df["amount"] < 0, "amount"] = 0

# WRONG: iterrows for vectorizable op
for i, row in df.iterrows():
    df.at[i, "tax"] = row["amount"] * 0.2

# RIGHT: vectorized
df["tax"] = df["amount"] * 0.2
```

## GroupBy & Aggregation

```python
# Named aggregations (clear column names)
summary = (df
    .groupby(["region", "category"], observed=True)
    .agg(
        total_revenue=("revenue", "sum"),
        avg_price=("price", "mean"),
        order_count=("order_id", "nunique"),
        first_order=("timestamp", "min"),
        last_order=("timestamp", "max"),
    )
    .reset_index())

# Multiple aggregations on same column
df.groupby("region")["amount"].agg(["mean", "median", "std", "count"])

# Custom aggregation
df.groupby("region").agg(
    p95_amount=("amount", lambda x: x.quantile(0.95)),
    skew=("amount", "skew"),
)

# Transform — broadcast aggregate back to original rows
df["region_avg"] = df.groupby("region")["amount"].transform("mean")
df["z_score"] = (df["amount"] - df["region_avg"]) / df.groupby("region")["amount"].transform("std")
```

### `observed=True` for categoricals

```python
# Without observed=True, groupby on categoricals includes empty groups
df.groupby("category").size()           # includes categories with 0 rows
df.groupby("category", observed=True).size()  # only categories with data
```

Always pass `observed=True` for categorical groupby in production.

## Window Functions

```python
# Rolling window
df["rolling_7d_avg"] = df.set_index("timestamp")["amount"].rolling("7D").mean()

# Expanding window
df["cumulative_sum"] = df["amount"].cumsum()
df["expanding_max"] = df["amount"].expanding().max()

# Per-group rolling (need sort first)
df = df.sort_values(["user_id", "timestamp"])
df["user_7d_avg"] = df.groupby("user_id")["amount"].transform(
    lambda s: s.rolling(7, min_periods=1).mean())

# Rank / percentiles within group
df["rank_in_region"] = df.groupby("region")["amount"].rank(pct=True)

# Lead / lag
df = df.sort_values("timestamp")
df["prev_amount"] = df.groupby("user_id")["amount"].shift(1)
df["pct_change"] = df.groupby("user_id")["amount"].pct_change()
```

## Merging & Joining

```python
# Always validate merge keys
merged = pd.merge(
    left_df, right_df,
    on=["customer_id", "date"],
    how="left",
    validate="m:1",     # asserts right key is unique (1)
    indicator=True,     # adds _merge column: left_only / both / right_only
)
unmatched = merged[merged["_merge"] != "both"]
print(f"Unmatched rows: {len(unmatched)}")  # investigate before proceeding

# Concat (not the removed .append)
combined = pd.concat([df1, df2, df3], ignore_index=True)

# Join on index
df1.join(df2, how="left", lsuffix="_l", rsuffix="_r")
```

### Merge Validation Matrix

| `validate` value | Asserts | Use when |
|---|---|---|
| `"1:1"` | Both keys unique | Joining dimension tables |
| `"1:m"` | Left key unique | Expanding one row to many |
| `"m:1"` | Right key unique | Adding a dimension to facts |
| `"m:m"` | Nothing (default) | Avoid — usually a bug |

## Time Series

```python
# Resample (groupby for time)
daily = (df.set_index("timestamp")
    .resample("D")
    .agg({"revenue": "sum", "sessions": "count"})
    .fillna(0))

# As-of join (last known value at each timestamp)
pd.merge_asof(events, prices, on="timestamp", by="symbol",
    direction="backward", tolerance=pd.Timedelta("1h"))

# Timezone handling
df["timestamp"] = pd.to_datetime(df["timestamp"], utc=True)
df["timestamp_local"] = df["timestamp"].dt.tz_convert("US/Eastern")

# Period arithmetic
df["month"] = df["timestamp"].dt.to_period("M")
```

## Memory Optimization

```python
# Profile
print(df.memory_usage(deep=True).sum() / 1e6, "MB")

# 1. Downcast numerics
df["count"] = pd.to_numeric(df["count"], downcast="integer")    # int64 → int32
df["score"] = pd.to_numeric(df["score"], downcast="float")      # float64 → float32

# 2. Categorical for low-cardinality strings (< 50% unique)
df["region"] = df["region"].astype("category")

# 3. Arrow-backed strings (2.x+)
df["description"] = df["description"].astype("string[pyarrow]")

# 4. Nullable types where appropriate
df["count"] = df["count"].astype("Int32")  # supports NaN

# 5. Drop unused columns before loading
df = pd.read_parquet("data.parquet", columns=["user_id", "amount", "timestamp"])
```

Typical savings: 50–80% memory reduction with the above patterns.

## Chunked Processing

When data > RAM but you're stuck on pandas (no Spark/Polars):

```python
chunks = pd.read_csv("big.csv", chunksize=100_000)
results = []
for chunk in chunks:
    # Process each chunk
    result = chunk.groupby("region")["amount"].sum()
    results.append(result)
final = pd.concat(results).groupby(level=0).sum()
```

Better: switch to **Polars** (lazy, streaming) or **DuckDB** (SQL over parquet, no Spark needed).

## Validation Patterns

```python
def validate(df, expected_rows=None, expected_cols=None):
    assert df.shape[0] > 0, "Empty DataFrame"
    if expected_rows:
        assert abs(df.shape[0] - expected_rows) / expected_rows < 0.05, \
            f"Row count drift: {df.shape[0]} vs {expected_rows}"
    if expected_cols:
        assert set(df.columns) == set(expected_cols), \
            f"Column mismatch: {set(df.columns) ^ set(expected_cols)}"
    assert df["user_id"].isna().sum() == 0, "user_id has nulls"
    return df
```

For richer validation, use **Great Expectations** (see `pipeline-orchestration.md`).

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| SettingWithCopyWarning | Silent data loss | Enable CoW; use `.copy()` on subsets |
| Iterrows on big DataFrame | 100x slower than vectorized | Use vectorized ops or `.itertuples()` |
| Categorical without `observed=True` | Empty groups in output | Pass `observed=True` to groupby |
| String columns as `object` dtype | 10x memory vs `string[pyarrow]` | Use Arrow-backed strings |
| `.append()` call | AttributeError on 2.x+ | Use `pd.concat()` |
| Merge without `validate` | Silent duplication | Always pass `validate="m:1"` etc. |
| `pd.read_csv` without `dtype` | Slow + wrong types | Specify dtypes explicitly |
| Timezone-naive datetime arithmetic | Silent wrong results | Use `utc=True` in `to_datetime` |
| Modifying slice of DataFrame | Affects original unexpectedly | Use `.copy()` or enable CoW |
