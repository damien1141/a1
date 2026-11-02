---
name: data-engineering
description: Builds production data and ML pipelines — pandas 2.x (Arrow-backed DataFrames, indexing, groupby, window functions, memory), Apache Spark 3.5 (DataFrame API, Spark SQL, structured streaming, partitioning, broadcast joins, Catalyst optimizer), and ML pipelines (DVC for data versioning, Airflow/Prefect for orchestration, Feast feature stores, Great Expectations for validation, MLflow model registry). Use when transforming datasets at any scale, building ETL/ELT pipelines, training and serving ML models with reproducibility, or orchestrating multi-stage data workflows.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: data
  triggers: pandas, DataFrame, PySpark, Apache Spark, Spark SQL, structured streaming, ETL, ELT, data pipeline, ML pipeline, MLflow, Airflow, Prefect, DVC, Feast, Great Expectations, feature store, model registry, data validation, partitioning, broadcast join
  role: specialist
  scope: implementation
  output-format: code
  related-skills: python-pro, database-pro, cloud-native, testing-master
---

# Data Engineering

One skill that spans the three layers of modern data/ML work: **single-node transformation** (pandas 2.x with Arrow backend), **distributed processing** (Spark 3.5 with Catalyst + structured streaming), and **pipeline orchestration** (DVC for data, Airflow/Prefect for DAGs, Feast for features, Great Expectations for validation, MLflow for models). Pick the right tool for the scale; gate every stage with validation.

## When to Use

- Transforming, cleaning, or analyzing tabular data (single-node: pandas; cluster-scale: Spark)
- Building ETL/ELT pipelines (batch or streaming)
- Training ML models with reproducible pipelines and tracked experiments
- Managing features across training and serving (feature store)
- Validating data quality at pipeline boundaries
- Orchestrating multi-stage workflows with retries, alerts, and SLAs
- Versioning datasets and models for reproducibility

## Operating Loop

1. **Profile** — Examine schema, dtypes, memory, row counts, null distribution. **Gate:** profile recorded; expected vs actual row count asserted.
2. **Choose scale** — Single-node (pandas) if data fits in ~25% of RAM; Spark if it doesn't or you need streaming. **Gate:** choice justified by data volume + latency target.
3. **Validate inputs** — Schema checks (Great Expectations) before any transformation. **Gate:** validation raises on schema drift; never silently accepts bad data.
4. **Transform** — Vectorized operations (pandas) or DataFrame API (Spark); avoid UDFs/loops. **Gate:** no row-by-row iteration; no `collect()` on large datasets.
5. **Validate outputs** — Schema + distribution checks again. **Gate:** row counts match expectations (within tolerance); no unexpected nulls.
6. **Persist + version** — Write to parquet/Iceberg; version with DVC; log to MLflow. **Gate:** dataset is reproducible from version pin.
7. **Orchestrate** — Wrap in Airflow DAG or Prefect flow with retries, SLAs, alerts. **Gate:** DAG runs end-to-end in CI; failure paths tested.

## Tool Selection (Single-Node vs Distributed)

| Signal | Tool |
|---|---|
| Data fits in 25% of RAM (rough: <5GB on 16GB machine) | **pandas 2.x** (Arrow-backed) |
| Need streaming SQL over Kafka/Kinesis | **Spark Structured Streaming** |
| Data > RAM but <100GB | **Polars** or **DuckDB** (single-node, fast) |
| Data > 100GB or needs cluster | **Spark 3.5** on a cluster |
| Need OLAP queries on lakehouse | **Spark SQL** or **DuckDB** |
| Need ML feature consistency train↔serve | **Feast** feature store |
| Need reproducible data + model versioning | **DVC** (data) + **MLflow** (models) |
| Need DAG orchestration with retries | **Airflow** or **Prefect** |
| Need strict data quality gates | **Great Expectations** |

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| pandas 2.x | `references/pandas.md` | DataFrame, indexing, groupby, window functions, Arrow backend, memory optimization |
| Spark 3.5 | `references/spark.md` | DataFrame API, Spark SQL, structured streaming, partitioning, broadcast joins, Catalyst |
| Feature stores | `references/feature-stores.md` | Feast, feature engineering, online/offline consistency, data validation (Great Expectations) |
| Pipeline orchestration | `references/pipeline-orchestration.md` | Airflow, Prefect, DAG design, DVC data versioning, retries, SLAs |
| ML pipelines & MLOps | `references/ml-pipelines.md` | MLflow, model registry, training pipelines, A/B testing, shadow deployment |

## Code Examples

### pandas 2.x — vectorized transform with validation

```python
import pandas as pd

df = pd.read_parquet("s3://bucket/events/")  # Arrow-backed by default in 2.x

# Profile
assert df.shape[0] > 0, "Empty dataset"
assert df["user_id"].isna().sum() == 0, "user_id has nulls"

# Vectorized transform (no iterrows)
summary = (df
    .groupby(["region", "category"], observed=True)
    .agg(total=("revenue", "sum"),
         avg_price=("price", "mean"),
         orders=("order_id", "nunique"))
    .reset_index())

# Memory: downcast numerics, categorize low-cardinality strings
summary["region"] = summary["region"].astype("category")
summary["total"] = pd.to_numeric(summary["total"], downcast="float")

# Validate output
assert summary.isna().sum().sum() == 0, "Unexpected nulls"
```

### Spark 3.5 — explicit schema + broadcast join

```python
from pyspark.sql import SparkSession, functions as F
from pyspark.sql.types import StructType, StructField, StringType, DoubleType, LongType

spark = (SparkSession.builder.appName("etl")
    .config("spark.sql.adaptive.enabled", "true")
    .config("spark.sql.shuffle.partitions", "400")
    .getOrCreate())

schema = StructType([
    StructField("user_id", StringType(), False),
    StructField("event_ts", LongType(), False),
    StructField("amount", DoubleType(), True),
])

events = spark.read.schema(schema).parquet("s3://bucket/events/")
enriched = events.join(F.broadcast(dim_df), on="user_id", how="left")  # dim < 200MB

result = (enriched.filter(F.col("amount").isNotNull())
    .groupBy("user_id")
    .agg(F.sum("amount").alias("total"), F.count("*").alias("events")))

result.write.mode("overwrite").parquet("s3://bucket/output/")
```

### Great Expectations — validation gate

```python
import great_expectations as gx

def validate(df, suite_name="events"):
    ctx = gx.get_context()
    source = ctx.data_sources.add_pandas("events")
    batch = source.get_asset("").build_batch_request(dataframe=df)
    result = ctx.get_validator(batch_request=batch).validate(suite_name)
    if not result.success:
        raise ValueError(f"Validation failed: {result.results}")
    return df

df = validate(raw_df)  # raises on schema drift before training
```

### MLflow — track experiment + register model

```python
import mlflow, mlflow.sklearn
from sklearn.ensemble import RandomForestClassifier

mlflow.set_experiment("churn-classifier")
with mlflow.start_run():
    params = {"n_estimators": 200, "max_depth": 8, "random_state": 42}
    mlflow.log_params(params)

    model = RandomForestClassifier(**params).fit(X_train, y_train)
    mlflow.log_metric("f1", f1_score(y_test, model.predict(X_test)))

    mlflow.sklearn.log_model(model, artifact_path="model",
        registered_model_name="churn-classifier")
```

### Airflow DAG skeleton

```python
from airflow import DAG
from airflow.operators.python import PythonOperator
from datetime import datetime, timedelta

with DAG("daily_etl", start_date=datetime(2024, 1, 1),
         schedule="@daily", retry_delay=timedelta(minutes=5),
         retries=2, catchup=False) as dag:

    extract = PythonOperator(task_id="extract", python_callable=extract)
    validate = PythonOperator(task_id="validate", python_callable=validate)
    transform = PythonOperator(task_id="transform", python_callable=transform)
    load = PythonOperator(task_id="load", python_callable=load)

    extract >> validate >> transform >> load
```

## Constraints

### MUST DO
- Profile data before transforming (dtypes, nulls, memory, row counts)
- Validate inputs AND outputs (Great Expectations or equivalent); raise on drift
- Use vectorized operations (pandas) / DataFrame API (Spark); avoid `iterrows` and UDFs
- Define explicit schemas in Spark production pipelines (no inference)
- Set `spark.sql.shuffle.partitions` appropriately (default 200 is usually wrong)
- Use broadcast joins for small dimension tables (<200MB)
- Cache intermediate DataFrames only when reused multiple times; unpersist after
- Version data (DVC) and models (MLflow registry); pin seeds and dependencies
- Log all hyperparameters, metrics, and artifacts to experiment tracking
- Use parameterized queries (never string interpolation) for DB writes
- Write parquet/Iceberg with partitioning that matches query patterns
- Test DAGs end-to-end in CI; test failure paths, not just happy paths
- Set retries + SLAs + alerts on production DAGs
- Separate training and inference code clearly

### MUST NOT DO
- `collect()` large Spark datasets to driver (OOM)
- Use `iterrows()` / itertuples() for vectorizable operations
- Cache every DataFrame without measuring benefit
- Skip schema definition in Spark production code
- Use UDFs when built-in functions exist (10–100x slower)
- Hardcode credentials in pipeline code — use secrets managers / env vars
- Deploy a model without recorded validation metrics
- Run training without experiment tracking
- Mix train and test data (leakage)
- Use `.append()` (deprecated) — use `pd.concat()`
- Use chained indexing (`df['A']['B'] = 1`) — use `.loc[]` or `.copy()`
- Ignore data skew (check Spark UI for stragglers)
- Assume pandas and Spark semantics match (null handling, joins, types differ)

## Output Template

1. **Profile** — dataset shape, dtypes, memory, null distribution, key stats
2. **Tool choice** — pandas / Spark / Polars / DuckDB with justification
3. **Validation plan** — Great Expectations suite (input + output gates)
4. **Transformation code** — vectorized / DataFrame API; no row-by-row loops
5. **Persistence** — parquet/Iceberg with partitioning strategy
6. **Orchestration** — Airflow DAG or Prefect flow with retries, SLAs, alerts
7. **Reproducibility** — DVC data version, MLflow model version, pinned deps, seed
8. **Monitoring** — schema drift alerts, data quality dashboard, pipeline SLAs

## Knowledge Reference

**pandas 2.x**: Arrow-backed DataFrames (PyArrow dtype support), indexing (`.loc`/`.iloc`/query), groupby + window functions, categorical dtypes, memory optimization (downcasting), method chaining, `pd.concat` (not deprecated `.append`), nullable types. **Spark 3.5**: DataFrame API, Spark SQL, Catalyst optimizer, Tungsten execution, AQE (Adaptive Query Execution), structured streaming (watermarks, stateful ops), partitioning strategies, broadcast joins, salting for skew, accumulators, Spark UI analysis. **Feature stores**: Feast (offline/online serving, point-in-time correctness), feature views, entity keys, materialization. **Orchestration**: Airflow (DAGs, XCom, operators, sensors, retries), Prefect (flows, tasks, deployments), DVC (data versioning, pipelines, reproducibility). **ML pipelines**: MLflow (experiments, model registry, model serving), Great Expectations (data validation, suites, checkpoints), model validation gates, A/B testing, shadow deployment, model rollback.
