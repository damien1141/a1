# Pipeline Orchestration Reference

Airflow, Prefect, DVC, and DAG design patterns for production data pipelines. Read alongside `ml-pipelines.md` for ML-specific orchestration.

## Orchestrator Selection

| Tool | Strengths | Use When |
|---|---|---|
| **Airflow** | Mature; huge ecosystem; batch-centric | Traditional ETL; enterprise; existing Airflow stack |
| **Prefect** | Pythonic; dynamic DAGs; great DX | Modern data apps; dynamic workflows; ML |
| **Dagster** | Asset-centric; software-defined assets | Lakehouse; dbt integration; data mesh |
| **DVC** | Data + pipeline versioning; not an orchestrator | Reproducible ML; pair with Airflow/Prefect |
| **Mage** | Visual + code; built-in transformers | Lightweight ETL; small teams |
| **Kubeflow Pipelines** | K8s-native; ML-focused | K8s shops; ML at scale |

For most teams: **Prefect** (modern, Pythonic) or **Airflow** (mature, ecosystem). Pair with **DVC** for data versioning.

## Airflow

### DAG Structure

```python
# dags/daily_etl.py
from airflow import DAG
from airflow.operators.python import PythonOperator
from airflow.operators.bash import BashOperator
from airflow.sensors.external_task import ExternalTaskSensor
from datetime import datetime, timedelta
import pendulum

default_args = {
    "owner": "data-team",
    "depends_on_past": False,
    "retries": 2,
    "retry_delay": timedelta(minutes=5),
    "email_on_failure": True,
    "email": ["data-oncall@example.com"],
}

with DAG(
    dag_id="daily_etl",
    default_args=default_args,
    description="Daily ETL from S3 to warehouse",
    start_date=pendulum.datetime(2024, 1, 1, tz="UTC"),
    schedule="0 2 * * *",          # 2 AM UTC daily
    catchup=False,                 # don't backfill on deploy
    max_active_runs=1,             # prevent overlap
    tags=["etl", "daily"],
) as dag:

    def extract(**context):
        ds = context["ds"]  # execution date as YYYY-MM-DD
        # ... extract logic
        return "/tmp/extracted.parquet"

    def validate(**context):
        path = context["ti"].xcom_pull(task_ids="extract")
        # ... validation
        return path

    extract_task = PythonOperator(task_id="extract", python_callable=extract)
    validate_task = PythonOperator(task_id="validate", python_callable=validate)
    transform_task = PythonOperator(task_id="transform", python_callable=transform)
    load_task = PythonOperator(task_id="load", python_callable=load)

    extract_task >> validate_task >> transform_task >> load_task
```

### TaskFlow API (cleaner; Python-native)

```python
from airflow.decorators import task, dag
import pendulum

@dag(schedule="@daily", start_date=pendulum.datetime(2024, 1, 1), catchup=False)
def daily_etl():

    @task
    def extract(date: str) -> str:
        # XCom handled automatically (return value = next task's input)
        return extract_from_s3(date)

    @task
    def validate(path: str) -> str:
        validate_data(path)
        return path

    @task
    def transform(path: str) -> str:
        return transform_data(path)

    @task
    def load(path: str):
        load_to_warehouse(path)

    load(transform(validate(extract("{{ ds }}"))))

daily_etl()
```

### XCom (cross-task data)

```python
# Push small data (paths, configs) via XCom
@task
def extract():
    return {"path": "/tmp/data.parquet", "rows": 12345}

@task
def load(**context):
    meta = context["ti"].xcom_pull(task_ids="extract")
    # meta = {"path": ..., "rows": ...}
```

**XCom limits:** ~48MB by default (Airflow 2.5+ with custom backend). Use XCom for paths/metadata only; pass actual data via S3/GCS.

### Sensors (wait for external events)

```python
from airflow.sensors.filesystem import FileSensor
from airflow.sensors.s3 import S3KeySensor
from airflow.sensors.external_task import ExternalTaskSensor

# Wait for upstream file
wait_for_file = S3KeySensor(
    task_id="wait_for_file",
    bucket_key="data/{{ ds }}/events.parquet",
    bucket_name="my-bucket",
    aws_conn_id="aws_default",
    poke_interval=300,  # 5 min
    timeout=60 * 60 * 24,  # 24h
    mode="reschedule",  # don't hold a worker slot while waiting
)

# Wait for another DAG to finish
wait_for_upstream = ExternalTaskSensor(
    task_id="wait_for_upstream",
    external_dag_id="upstream_dag",
    external_task_id="final_task",
    mode="reschedule",
)
```

### Connections + Hooks

```python
from airflow.providers.snowflake.hooks.snowflake import SnowflakeHook

@task
def load_to_snowflake(records):
    hook = SnowflakeHook(snowflake_conn_id="snowflake_default")
    hook.run("INSERT INTO events VALUES (%s, %s, %s)", parameters=records)
```

Store connection details in Airflow UI → Admin → Connections (or via Secrets backend — AWS Secrets Manager, GCP Secret Manager, HashiCorp Vault).

## Prefect

### Flow + Task

```python
# flows/daily_etl.py
from prefect import flow, task
import pendulum

@task(retries=3, retry_delay_seconds=60)
def extract(date: str) -> str:
    # ... extract logic
    return "/tmp/extracted.parquet"

@task
def validate(path: str) -> str:
    # ... validation
    return path

@task
def load(path: str):
    # ... load
    pass

@flow(name="daily-etl")
def daily_etl(date: str = None):
    date = date or pendulum.today().to_date_string()
    path = extract(date)
    validated = validate(path)
    load(validated)

if __name__ == "__main__":
    daily_etl.serve(name="daily-deployment",
                    cron="0 2 * * *",
                    tags=["etl", "daily"])
```

### Why Prefect

- **Dynamic DAGs** — flow can branch based on runtime data (Airflow DAGs are static)
- **Native Python** — no templating; just Python functions
- **Better DX** — type hints, IDE autocomplete, less boilerplate
- **Deployments** — `flow.serve()` runs the flow as a long-running process; no Airflow scheduler needed
- **Work pools** — push-based (no worker on your infra) or process-based

### Prefect Deployments

```python
# Serve as a long-running process (Prefect Cloud / self-hosted)
daily_etl.serve(
    name="daily-etl-prod",
    cron="0 2 * * *",
    timezone="UTC",
    tags=["etl", "daily"],
    description="Daily ETL from S3 to warehouse",
)

# Or: deploy to a work pool
from prefect import deploy
deploy(daily_etl.to_deployment(name="daily-etl", cron="0 2 * * *"),
       work_pool_name="my-pool")
```

## DVC (Data Version Control)

DVC versions large datasets and pipelines alongside Git. Git tracks code + DVC meta; DVC tracks the actual data (in object storage).

### Setup

```bash
git init
dvc init
git commit -m "init dvc"
dvc remote add -d myremote s3://my-bucket/dvc-storage
```

### Version Data

```bash
# Add data (creates data.csv.dvc — checked into Git)
dvc add data/raw.csv
git add data/raw.csv.dvc data/.gitignore
git commit -m "add raw data v1"

# Update data, version again
cp new_data.csv data/raw.csv
dvc add data/raw.csv
git commit -am "update raw data v2"

# Restore v1
git checkout HEAD~1 -- data/raw.csv.dvc
dvc pull
```

### DVC Pipeline (reproducible stages)

```bash
# dvc.yaml — declarative pipeline
stages:
  extract:
    cmd: python src/extract.py --out data/extracted.parquet
    deps:
      - src/extract.py
    outs:
      - data/extracted.parquet
  transform:
    cmd: python src/transform.py --in data/extracted.parquet --out data/transformed.parquet
    deps:
      - data/extracted.parquet
      - src/transform.py
    outs:
      - data/transformed.parquet
  train:
    cmd: python src/train.py --in data/transformed.parquet --out models/model.pkl
    deps:
      - data/transformed.parquet
      - src/train.py
    outs:
      - models/model.pkl
    metrics:
      - metrics.json:
          cache: false
    plots:
      - plots/confusion_matrix.png
```

```bash
dvc repro                  # runs only changed stages (like make)
dvc repro --force          # force rerun all
dvc push                   # push data to remote
dvc pull                   # pull data from remote
dvc dag                    # visualize pipeline
dvc metrics show           # show metrics across versions
dvc metrics diff HEAD~1    # diff metrics vs last commit
```

### Why DVC + Git Together

| Tool | Tracks |
|---|---|
| Git | Code, configs, `dvc.yaml`, `*.dvc` files, `metrics.json` |
| DVC | Actual data files (parquet, model.pkl), stored in S3/GCS/etc. |

Result: `git clone` + `dvc pull` reproduces the entire pipeline state.

## DAG Design Patterns

### Idempotent Tasks

Every task must be safe to rerun. If `load` writes `events_2024-01-15.parquet`, rerunning for the same date overwrites — no duplicates.

```python
@task
def load_to_warehouse(path: str, date: str):
    # DELETE-then-INSERT pattern (idempotent)
    hook.run(f"DELETE FROM events WHERE date = '{date}'")
    hook.run(f"INSERT INTO events SELECT * FROM parquet_scan('{path}')")
```

### Failure Isolation

If a task fails, only that task's downstream is blocked. Don't put unrelated work in the same task.

```
# Bad: one task does extract + transform + load (one fails → all rerun)
# Good: separate tasks, separate retries
extract >> validate >> transform >> load
                >> alert_team    # validate can also branch to alert
```

### Backfill Strategy

- `catchup=False` on new DAGs to avoid running months of historical data on first deploy
- For real backfills: use `airflow dags backfill -s 2024-01-01 -e 2024-02-01 daily_etl`
- Make tasks parameterized by `ds` (execution date) so backfill reruns are isolated per-date

### SLA + Alerts

```python
@dag(
    sla_miss_callback=alert_sla_miss,
    default_args={"sla": timedelta(hours=3)},
    on_failure_callback=alert_failure,
    on_success_callback=notify_success,
)
def daily_etl():
    ...
```

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Non-idempotent tasks | Duplicates on rerun | DELETE-INSERT or upsert |
| XCom for large data | Worker OOM | Pass paths via XCom; data via S3 |
| Catchup=True on deploy | Backfill storm | Set `catchup=False` |
| Hardcoded paths | Breaks across envs | Use Airflow Variables / connections |
| No retries | Transient failures kill run | `retries=2, retry_delay=5min` |
| Long-running sensor in `poke` mode | Holds worker slot | Use `mode="reschedule"` |
| Secrets in DAG code | Security risk | Use Connections / Secrets backend |
| No `max_active_runs` | Concurrent runs overlap | Set `max_active_runs=1` for daily DAGs |
| DVC not pushed | `dvc pull` fails on other machine | Run `dvc push` after `git push` |
| Pipeline not reproducible | "Works on my machine" | Use DVC `dvc repro` for ML pipelines |
