# Feature Stores & Data Validation Reference

Feature stores (Feast) for train/serve consistency, and Great Expectations for data quality gates. Read alongside `ml-pipelines.md` for model lifecycle context.

## Why Feature Stores

**Problem:** Training uses batch features (from parquet/warehouse). Serving uses online features (from Redis/DynamoDB). If features are computed differently in each path, models silently degrade in production. This is the **train/serve skew** problem.

**Solution:** A feature store is the single source of truth. Features are defined once, materialized to both offline (training) and online (serving) stores, with point-in-time correctness for training.

## Feast

### Install + Init

```bash
pip install feast
feast init my_feature_repo
cd my_feature_repo
```

### Define Feature Views

```python
# feature_repo/features.py
from datetime import timedelta
from feast import Entity, FeatureView, Field, FileSource, ValueType
from feast.types import Float32, Int64, String

# Entity (join key)
user = Entity(name="user_id", join_keys=["user_id"])

# Source — batch (offline) store
user_stats_source = FileSource(
    name="user_stats_source",
    path="data/user_stats.parquet",
    timestamp_field="event_ts",
)

# Feature view
user_features = FeatureView(
    name="user_features",
    entities=[user],
    ttl=timedelta(days=30),
    schema=[
        Field(name="total_spent", dtype=Float32),
        Field(name="order_count", dtype=Int64),
        Field(name="avg_order_value", dtype=Float32),
        Field(name="days_since_last_order", dtype=Int64),
    ],
    source=user_stats_source,
    online=True,  # materialize to online store
)
```

### Apply + Materialize

```bash
feast apply                              # registers feature views
feast materialize-incremental $(date -u +"%Y-%m-%dT%H:%M:%S")
```

### Training (Offline) — Point-in-Time Correct

```python
from feast import FeatureStore
import pandas as pd

store = FeatureStore(repo_path=".")

# Entity DataFrame with event timestamps (prevents leakage)
entity_df = pd.DataFrame({
    "user_id": ["u1", "u2", "u3"],
    "event_ts": pd.to_datetime(["2024-01-15", "2024-01-20", "2024-01-25"]),
})

# Fetch features as-of each event_ts — no future data leaks in
training_df = store.get_historical_features(
    entity_df=entity_df,
    features=[
        "user_features:total_spent",
        "user_features:order_count",
        "user_features:avg_order_value",
    ],
).to_df()
```

### Serving (Online)

```python
# Low-latency lookup for inference
features = store.get_online_features(
    features=[
        "user_features:total_spent",
        "user_features:order_count",
    ],
    entity_rows=[{"user_id": "u1"}],
).to_dict()
# {'user_id': ['u1'], 'total_spent': [1234.5], 'order_count': [42]}
```

### Online Store Options

| Store | Latency | Use |
|---|---|---|
| Redis | ~1ms | Default online store |
| DynamoDB | ~5ms | AWS-native |
| Firestore | ~5ms | GCP-native |
| Cassandra | ~2ms | High write throughput |
| Postgres | ~10ms | Single-store simplicity |

### On-Demand Feature Views (computed at fetch time)

```python
from feast import OnDemandFeatureView

def compute_bucket(features_df: pd.DataFrame) -> pd.DataFrame:
    features_df["spend_bucket"] = pd.cut(features_df["total_spent"],
        bins=[0, 100, 1000, float("inf")], labels=["low", "mid", "high"])
    return features_df

spend_bucket_view = OnDemandFeatureView(
    name="spend_bucket",
    sources=[user_features],
    schema=[Field(name="spend_bucket", dtype=String)],
    udf=compute_bucket,
)
```

## Feature Engineering Patterns

### Temporal Features (avoid leakage)

```python
# WRONG — uses future data
df["future_avg"] = df.groupby("user_id")["amount"].transform("mean")

# RIGHT — rolling window up to current row only
df = df.sort_values(["user_id", "timestamp"])
df["rolling_7d_avg"] = df.groupby("user_id")["amount"].transform(
    lambda s: s.rolling(7, min_periods=1).mean())

# RIGHT — lagged features
df["prev_day_amount"] = df.groupby("user_id")["amount"].shift(1)
df["amount_diff"] = df["amount"] - df["prev_day_amount"]
```

### Categorical Encoding

| Encoding | When | Notes |
|---|---|---|
| One-hot | Low cardinality (<20) | Sparse but safe |
| Target/mean encoding | High cardinality, has target | **Must be cross-validated** to avoid leakage |
| Hash encoding | Very high cardinality | Fixed-size; collisions |
| Embedding | Categorical + neural net | Train embedding layer |
| Frequency encoding | Tree models | Often as good as target |

```python
# Target encoding with leakage prevention (out-of-fold)
from sklearn.model_selection import KFold
import numpy as np

def target_encode(df, col, target, n_splits=5, smoothing=10):
    df = df.copy()
    df["encoded"] = np.nan
    global_mean = df[target].mean()

    kf = KFold(n_splits=n_splits, shuffle=True, random_state=42)
    for train_idx, val_idx in kf.split(df):
        agg = df.iloc[train_idx].groupby(col)[target].agg(["mean", "count"])
        smooth = (agg["mean"] * agg["count"] + global_mean * smoothing) / (agg["count"] + smoothing)
        df.loc[df.index[val_idx], "encoded"] = df.iloc[val_idx][col].map(smooth)

    df["encoded"] = df["encoded"].fillna(global_mean)
    return df
```

### Scaling

```python
from sklearn.preprocessing import StandardScaler, RobustScaler, MinMaxScaler

# Standard (z-score) — most models
# Robust — when outliers are real (uses median/IQR)
# MinMax — when bounded range needed (neural nets, distance-based)

scaler = StandardScaler().fit(X_train)  # fit on TRAIN ONLY
X_train_scaled = scaler.transform(X_train)
X_test_scaled = scaler.transform(X_test)   # transform with train params
```

**Never fit scaler on full dataset** — that's leakage. Always fit on train, transform test/val.

### Train/Validation/Test Split (Time Series)

```python
# WRONG — random split leaks future to past
from sklearn.model_selection import train_test_split
X_train, X_test = train_test_split(df, test_size=0.2)

# RIGHT — temporal split
train = df[df["timestamp"] < "2024-10-01"]
val = df[(df["timestamp"] >= "2024-10-01") & (df["timestamp"] < "2024-11-01")]
test = df[df["timestamp"] >= "2024-11-01"]
```

For time-series, always split chronologically. Random splits leak future information.

## Great Expectations

### Install + Setup

```bash
pip install great-expectations
great_expectations init
```

### Define a Suite

```python
import great_expectations as gx

ctx = gx.get_context()

# Add a pandas datasource
source = ctx.data_sources.add_pandas("events")
asset = source.add_dataframe_asset(name="events_df")
batch_request = asset.build_batch_request(dataframe=df)

# Build expectation suite
suite = ctx.add_expectation_suite("events_suite")

validator = ctx.get_validator(batch_request=batch_request,
    expectation_suite=suite)

# Column expectations
validator.expect_column_to_exist("user_id")
validator.expect_column_values_to_not_be_null("user_id")
validator.expect_column_values_to_be_unique("user_id")
validator.expect_column_values_to_be_between("amount", min_value=0, max_value=100000)
validator.expect_column_values_to_be_in_set("currency", ["USD", "EUR", "GBP", "JPY"])
validator.expect_column_values_to_match_regex("email", r"^[^@]+@[^@]+\.[^@]+$")

# Distribution expectations
validator.expect_column_mean_to_be_between("amount", min_value=50, max_value=200)
validator.expect_column_values_to_be_in_type_list("amount", ["float64", "float32"])

# Save suite
validator.save_expectation_suite()
```

### Checkpoint (Run Validation in Pipeline)

```python
# Define a checkpoint
checkpoint = ctx.add_or_update_checkpoint(
    name="events_checkpoint",
    validations=[
        {"batch_request": batch_request, "expectation_suite_name": "events_suite"},
    ],
)

# Run in pipeline
result = checkpoint.run()
if not result.success:
    raise ValueError(f"Data validation failed: {result}")
```

### Common Expectations

| Expectation | Use |
|---|---|
| `expect_column_to_exist` | Schema check |
| `expect_column_values_to_not_be_null` | Required fields |
| `expect_column_values_to_be_unique` | Primary keys |
| `expect_column_values_to_be_in_set` | Enums / categories |
| `expect_column_values_to_be_between` | Numeric ranges |
| `expect_column_values_to_match_regex` | Format (email, phone) |
| `expect_column_values_to_be_in_type_list` | Dtype check |
| `expect_column_mean_to_be_between` | Distribution drift (CI) |
| `expect_column_row_count_to_be_between` | Volume sanity |
| `expect_column_pair_values_a_to_be_greater_than_b` | Cross-column |

### Inline Validation (no full GE setup)

For simpler pipelines, use `pandas` assertions or `pandera`:

```python
import pandera as pa
from pandera import Column, DataFrameSchema

schema = DataFrameSchema({
    "user_id": Column(str, nullable=False, unique=True),
    "amount": Column(float, checks=pa.Check.in_range(0, 100000)),
    "currency": Column(str, checks=pa.Check.isin(["USD","EUR","GBP"])),
    "timestamp": Column(pa.DateTime, nullable=False),
})

df = schema.validate(df)  # raises on violation
```

## Data Drift Detection

Track feature distributions over time; alert on drift:

```python
from scipy.stats import ks_2samp, chi2_contingency

def check_drift(train_col, prod_col, threshold=0.05):
    stat, p_value = ks_2samp(train_col, prod_col)
    if p_value < threshold:
        print(f"DRIFT: p={p_value:.4f} — distribution changed")
    return p_value

# For categoricals
def check_categorical_drift(train_col, prod_col):
    train_dist = train_col.value_counts(normalize=True)
    prod_dist = prod_col.value_counts(normalize=True)
    all_cats = sorted(set(train_dist.index) | set(prod_dist.index))
    train_freq = [train_dist.get(c, 0) for c in all_cats]
    prod_freq = [prod_dist.get(c, 0) for c in all_cats]
    chi2, p, _, _ = chi2_contingency([train_freq, prod_freq])
    return p
```

For production drift monitoring, use **Evidently** (Python lib) or **WhyLabs**.

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Train/serve skew | Model works offline, fails online | Use Feast feature store |
| Target encoding leakage | Inflated CV scores | Out-of-fold encoding |
| Random split on time series | Future leaks to past | Temporal split |
| Fitting scaler on full data | Test metrics inflated | Fit on train only |
| No data validation in pipeline | Silent corruption | Great Expectations / pandera gates |
| Feature schema drift | Model silently fails | Pin feature types; validate at serving |
| Online/offline feature mismatch | Train/serve skew | Single source of truth (Feast) |
| No drift monitoring | Slow model decay | Evidently / WhyLabs dashboards |
