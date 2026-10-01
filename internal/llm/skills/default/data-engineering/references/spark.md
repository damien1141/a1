# Spark 3.5 Reference

Production patterns for Apache Spark 3.5: DataFrame API, Spark SQL, Structured Streaming, partitioning, broadcast joins, Catalyst optimizer, and AQE.

## Session Setup

```python
from pyspark.sql import SparkSession, functions as F
from pyspark.sql.types import StructType, StructField, StringType, LongType, DoubleType, TimestampType

spark = (SparkSession.builder
    .appName("etl-pipeline")
    .config("spark.sql.adaptive.enabled", "true")              # AQE on
    .config("spark.sql.adaptive.coalescePartitions.enabled", "true")
    .config("spark.sql.adaptive.skewJoin.enabled", "true")    # skew handling
    .config("spark.sql.shuffle.partitions", "400")            # tune per workload
    .config("spark.sql.parquet.filterPushdown", "true")
    .config("spark.sql.parquet.compression.codec", "snappy")
    .config("spark.sql.execution.arrow.pyspark.enabled", "true")  # faster to_pandas
    .getOrCreate())

spark.sparkContext.setLogLevel("WARN")
```

### Key Configs

| Config | Default | Tune when |
|---|---|---|
| `spark.sql.shuffle.partitions` | 200 | Always (default rarely right) |
| `spark.sql.adaptive.enabled` | false | Always enable (AQE) |
| `spark.sql.adaptive.coalescePartitions.enabled` | true (if AQE on) | Leave on |
| `spark.sql.adaptive.skewJoin.enabled` | true (if AQE on) | Leave on |
| `spark.executor.memory` | 1g | Profile workload |
| `spark.executor.cores` | 1 | 4–5 per executor typical |
| `spark.sql.files.maxPartitionBytes` | 128MB | Small-file problem |

## Schemas (Always Explicit in Production)

```python
schema = StructType([
    StructField("user_id", StringType(), False),       # False = nullable=False
    StructField("event_ts", TimestampType(), False),
    StructField("amount", DoubleType(), True),
    StructField("currency", StringType(), True),
])

# Read with explicit schema (skips inference — faster + safer)
df = spark.read.schema(schema).parquet("s3://bucket/events/")

# Or use DDL string
df = spark.read.schema("user_id STRING NOT NULL, amount DOUBLE").parquet(path)
```

**Never rely on schema inference in production.** Inference scans data (slow) and can guess wrong types (silent bugs).

## DataFrame API

### Selecting & Filtering

```python
# Select + filter (lazy)
result = (df
    .select("user_id", "amount", "event_ts")
    .filter((F.col("amount").isNotNull()) & (F.col("amount") > 100))
    .filter(F.col("event_ts") >= F.lit("2024-01-01")))

# SQL expressions
df.filter("amount > 100 AND currency = 'USD'")

# Distinct
df.select("user_id").distinct()

# Drop columns
df.drop("internal_field", "debug_col")
```

### Adding & Transforming Columns

```python
# withColumn — adds or replaces
df = df.withColumn("amount_usd", F.col("amount") * F.col("fx_rate"))
df = df.withColumn("log_amount", F.log(F.col("amount") + 1))

# Multiple columns at once (faster than chained withColumn)
df = df.withColumns({
    "amount_usd": F.col("amount") * F.col("fx_rate"),
    "is_high_value": F.when(F.col("amount") > 1000, True).otherwise(False),
    "currency_upper": F.upper(F.col("currency")),
})

# Conditional logic
df = df.withColumn("tier",
    F.when(F.col("amount") > 1000, "gold")
     .when(F.col("amount") > 100, "silver")
     .otherwise("bronze"))

# Date/time
df = df.withColumn("date", F.to_date(F.col("event_ts")))
df = df.withColumn("hour", F.hour(F.col("event_ts")))
df = df.withColumn("day_of_week", F.date_format(F.col("event_ts"), "E"))
```

### Aggregations

```python
result = (df
    .groupBy("user_id", F.date_format("event_ts", "yyyy-MM").alias("month"))
    .agg(
        F.sum("amount").alias("total_amount"),
        F.count("*").alias("event_count"),
        F.avg("amount").alias("avg_amount"),
        F.max("event_ts").alias("last_seen"),
        F.countDistinct("currency").alias("currency_count"),
        F.expr("percentile(amount, 0.95)").alias("p95_amount"),
    )
    .filter(F.col("event_count") >= 5))
```

### Joins

```python
# Inner join
result = df.join(other_df, on="user_id", how="inner")

# Multiple keys
result = df.join(other_df, on=["user_id", "date"], how="left")

# Different column names
result = df.join(other_df, df.user_id == other_df.customer_id, how="left")

# Broadcast join — small dim table (<200MB)
result = df.join(F.broadcast(dim_df), on="user_id", how="left")

# Self-join (alias required)
df.alias("a").join(df.alias("b"),
    F.col("a.user_id") == F.col("b.referred_by"), how="left") \
    .select("a.user_id", "b.user_id.alias("referral_id"))
```

### When to Use Broadcast Join

| Right-side size | Strategy |
|---|---|
| < 100MB | Broadcast (auto if `spark.sql.autoBroadcastJoinThreshold=10MB`; bump threshold) |
| 100MB – 1GB | Broadcast explicitly with `F.broadcast()` |
| > 1GB | Shuffle join (default) |
| Skewed key | Salting (see below) |

```python
spark.conf.set("spark.sql.autoBroadcastJoinThreshold", 104857600)  # 100MB
```

## Spark SQL

```python
# Register a temp view, then use SQL
df.createOrReplaceTempView("events")

result = spark.sql("""
    SELECT user_id, SUM(amount) as total, COUNT(*) as cnt
    FROM events
    WHERE amount IS NOT NULL
    GROUP BY user_id
    HAVING COUNT(*) >= 5
    ORDER BY total DESC
    LIMIT 100
""")

# Window functions
result = spark.sql("""
    SELECT
        user_id, event_ts, amount,
        ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY event_ts DESC) as rn,
        SUM(amount) OVER (PARTITION BY user_id ORDER BY event_ts
                          ROWS BETWEEN 6 PRECEDING AND CURRENT ROW) as rolling_7d
    FROM events
""")
```

## Structured Streaming

```python
# Read from Kafka
stream = (spark
    .readStream
    .format("kafka")
    .option("kafka.bootstrap.servers", "broker:9092")
    .option("subscribe", "events")
    .option("startingOffsets", "latest")
    .load())

# Parse + transform
parsed = (stream
    .selectExpr("CAST(value AS STRING) as json_str")
    .selectExpr("json_str",
        "from_json(json_str, 'user_id STRING, amount DOUBLE, ts TIMESTAMP') as data")
    .select("data.*"))

# Windowed aggregation with watermark (handles late data)
agg = (parsed
    .withWatermark("ts", "10 minutes")  # drop data >10min late
    .groupBy(F.window("ts", "5 minutes"), "user_id")
    .agg(F.sum("amount").alias("total")))

# Write to console (dev) or parquet/kafka (prod)
query = (agg.writeStream
    .outputMode("append")              # append | complete | update
    .format("parquet")
    .option("path", "s3://bucket/streaming-output/")
    .option("checkpointLocation", "s3://bucket/checkpoints/agg-v1")
    .trigger(processingTime="1 minute")
    .start())

query.awaitTermination()
```

### Output Modes

| Mode | When to use |
|---|---|
| `append` | Only new rows (after watermark); default for sinks that can't update |
| `update` | Only changed rows since last trigger; good for sinks that support updates |
| `complete` | Full result every trigger; only for aggregations; use with console/Kafka |

### Watermarks

Watermarks tell Spark how long to wait for late data before finalizing a window. Set based on your data's real-world lateness distribution, not guess.

## Partitioning

### Read Partitioning

```python
# Read partitioned dataset
df = spark.read.parquet("s3://bucket/events/")  # auto-discovers partitions
df = spark.read.parquet("s3://bucket/events/date=2024-01-01/")

# Predicate pushdown (only reads matching files)
df = spark.read.parquet("s3://bucket/events/") \
    .filter((F.col("date") >= "2024-01-01") & (F.col("region") == "US"))
```

### Write Partitioning

```python
# Partition by columns used in WHERE clauses
df.write.mode("overwrite") \
    .partitionBy("date", "region") \
    .parquet("s3://bucket/output/")

# Pick partition columns by query patterns:
# - High cardinality (user_id): don't partition (too many small files)
# - Low cardinality (region): partition
# - Date: always partition (time-based queries are universal)
```

### Shuffle Partition Tuning

`spark.sql.shuffle.partitions` (default 200) controls post-shuffle partitions.

```python
# Rule of thumb: 200MB per partition post-shuffle
# If post-shuffle data is 10GB → 50 partitions
# Check actual: df.rdd.getNumPartitions() and partition sizes in Spark UI

spark.conf.set("spark.sql.shuffle.partitions", "400")  # for larger shuffles
```

AQE auto-coalesces small partitions post-shuffle, so the exact number matters less when AQE is on.

### Salting for Skew

When one key dominates (e.g., one user has 80% of events):

```python
SALT_BUCKETS = 50

# Add salt to skewed side
skewed_salted = (skewed_df
    .withColumn("salt", (F.rand() * SALT_BUCKETS).cast("int"))
    .withColumn("salted_key", F.concat(F.col("user_id"), F.lit("_"), F.col("salt"))))

# Explode salt on other side
other_salted = (other_df
    .withColumn("salt", F.explode(F.array([F.lit(i) for i in range(SALT_BUCKETS)])))
    .withColumn("salted_key", F.concat(F.col("user_id"), F.lit("_"), F.col("salt"))))

result = (skewed_salted.join(other_salted, on="salted_key", how="inner")
    .drop("salt", "salted_key"))
```

Or enable `spark.sql.adaptive.skewJoin.enabled=true` (AQE) — handles most skew automatically without code changes.

## Caching & Persistence

```python
# Cache ONLY when reused multiple times
df_cleaned = df.filter(...).withColumn(...).cache()
df_cleaned.count()  # materialize; check Spark UI for spill

# Use it twice
report_a = df_cleaned.groupBy("region").agg(...)
report_b = df_cleaned.groupBy("product").agg(...)

# Release when done
df_cleaned.unpersist()
```

### Storage Levels

| Level | Memory? | Disk? | Serialized? | Replication |
|---|---|---|---|---|
| `MEMORY_ONLY` | Yes | No | No | 1 |
| `MEMORY_AND_DISK` | Yes | Yes | No | 1 |
| `MEMORY_ONLY_SER` | Yes | No | Yes | 1 |
| `DISK_ONLY` | No | Yes | Yes | 1 |

Default `cache()` = `MEMORY_AND_DISK` (deserialized). For large DataFrames reused often, consider `MEMORY_AND_DISK_SER` (uses less memory).

## Performance Tuning

### Avoid UDFs (10–100x slower)

```python
# WRONG — UDF breaks Catalyst optimization
@F.udf("double")
def calc_tax(amount):
    return amount * 0.2 if amount > 100 else amount * 0.1

df = df.withColumn("tax", calc_tax(F.col("amount")))

# RIGHT — built-in functions, Catalyst-optimized
df = df.withColumn("tax",
    F.when(F.col("amount") > 100, F.col("amount") * 0.2)
     .otherwise(F.col("amount") * 0.1))
```

### Small File Problem

```python
# Coalesce small files before writing
df.coalesce(10).write.mode("overwrite").parquet(path)

# Or repartition to a target size (~128MB per file)
target_files = total_size_bytes // (128 * 1024 * 1024)
df.repartition(max(1, target_files)).write.mode("overwrite").parquet(path)
```

### Spark UI

Watch for:
- **Skew** — one task takes 10x longer than others (check task duration distribution)
- **Spill (memory)** — task spills to disk; increase executor memory or reduce partition size
- **Spill (disk)** — shuffle spilling; tune `shuffle.partitions`
- **GC time** — >10% of task time; reduce memory pressure
- **Scan time** — reading too much data; add partition filters or predicate pushdown

## Catalyst Optimizer

Catalyst transforms your DataFrame operations into an optimized physical plan. Phases:
1. **Analysis** — resolve column references against catalog
2. **Logical optimization** — predicate pushdown, projection pruning, constant folding
3. **Physical planning** — pick join strategies (broadcast vs shuffle), choose indexes
4. **Code generation** — Tungsten generates Java bytecode

To see the plan: `df.explain(True)` (full plan) or `df.explain("formatted")` (Spark 3+).

AQE (Adaptive Query Execution) re-optimizes at runtime based on actual statistics — handles skew and coalesces small partitions. Always enable in production.

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| `collect()` on large DF | OOM on driver | Use `.toPandas()` only after `.limit()` or aggregation |
| UDF for vectorizable op | 10–100x slower | Use built-in functions |
| Schema inference in prod | Wrong types, slow | Define explicit schemas |
| Default shuffle partitions | Too many small tasks | Tune `spark.sql.shuffle.partitions` |
| Cache without reuse | Wasted memory | Only cache when DF used 2+ times |
| Skewed join key | One task dominates | Salt or enable AQE skew join |
| Small file problem | Thousands of tiny parquet files | Coalesce before write |
| No watermark in streaming | Unbounded state | Set `withWatermark` for stateful ops |
| `count()` after `cache()` without action | Cache not materialized | Call `count()` to materialize |
| Filter after expensive op | Wasted computation | Push filters up early |
