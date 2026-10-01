# ML Pipelines & MLOps Reference

MLflow, model registry, training pipelines, model validation, A/B testing, and shadow deployment. Read alongside `feature-stores.md` (for input features) and `pipeline-orchestration.md` (for DAGs).

## MLOps Maturity Model

| Level | What | Tooling |
|---|---|---|
| 0 | Notebook → manual deploy | None |
| 1 | Tracked experiments; manual deploy | MLflow tracking |
| 2 | Automated training pipeline; CI for models | MLflow + Airflow/Prefect + registry |
| 3 | Automated retraining; canary + auto-rollback | Full MLOps stack + monitoring |

Aim for level 2 minimum. Level 3 only for high-stakes models.

## MLflow

### Tracking (log experiments)

```python
import mlflow
import mlflow.sklearn
from sklearn.ensemble import RandomForestClassifier
from sklearn.metrics import f1_score, roc_auc_score
import numpy as np

SEED = 42
np.random.seed(SEED)

mlflow.set_experiment("churn-classifier")
mlflow.autolog()  # auto-logs sklearn params/metrics

with mlflow.start_run(run_name="rf-v1") as run:
    params = {"n_estimators": 200, "max_depth": 8, "random_state": SEED}
    mlflow.log_params(params)

    model = RandomForestClassifier(**params).fit(X_train, y_train)
    preds = model.predict(X_test)

    mlflow.log_metric("f1", f1_score(y_test, preds))
    mlflow.log_metric("roc_auc", roc_auc_score(y_test, model.predict_proba(X_test)[:, 1]))
    mlflow.log_artifact("feature_importance.png")

    # Register in model registry
    mlflow.sklearn.log_model(model, artifact_path="model",
        registered_model_name="churn-classifier",
        input_example=X_train.head(),
        signature=mlflow.models.infer_signature(X_train, model.predict(X_train)))
```

### Model Registry

```python
client = mlflow.tracking.MlflowClient()

# List versions
versions = client.search_model_versions("name='churn-classifier'")
for v in versions:
    print(v.version, v.current_stage, v.status)

# Transition stages (None → Staging → Production → Archived)
client.transition_model_version_stage(
    name="churn-classifier",
    version=5,
    stage="Staging",
    archive_existing_versions=False,  # set True for "one prod version"
)

# Get the production model for serving
prod = client.get_latest_versions("churn-classifier", stages=["Production"])[0]
model = mlflow.sklearn.load_model(prod.source)
```

### Registry Stages

| Stage | Use |
|---|---|
| None | Just registered; not yet evaluated |
| Staging | Evaluated; ready for canary |
| Production | Live; serving traffic |
| Archived | Superseded; keep for rollback |

### Model Serving

```bash
# Serve a model with MLflow's built-in server
mlflow models serve -m models:/churn-classifier/Production -p 1234

# Or with scoring endpoint
curl -X POST http://localhost:1234/invocations \
  -H "Content-Type: application/json" \
  -d '{"dataframe_split": {"columns":["f1","f2"],"data":[[1.0,2.0]]}}'
```

For production serving, prefer:
- **BentoML** — model serving with proper API
- **Seldon Core** — K8s-native model serving
- **KServe** — serverless model serving on K8s
- **Ray Serve** — for scaling ML inference

## Training Pipeline (KFP / Airflow + MLflow)

```python
# kubeflow_pipelines/training.py
from kfp.v2 import dsl
from kfp.v2.dsl import component, Input, Output, Dataset, Model, Metrics

@component(base_image="python:3.11",
           packages_to_install=["scikit-learn==1.4.0", "mlflow==2.13.0", "pandas==2.2.0"])
def train_model(
    train_data: Input[Dataset],
    val_data: Input[Dataset],
    model_output: Output[Model],
    metrics_output: Output[Metrics],
    n_estimators: int = 200,
    max_depth: int = 8,
):
    import pandas as pd
    import mlflow, mlflow.sklearn
    from sklearn.ensemble import RandomForestClassifier
    from sklearn.metrics import f1_score, roc_auc_score
    import pickle, json

    train = pd.read_parquet(train_data.path)
    val = pd.read_parquet(val_data.path)
    X_train, y_train = train.drop("label", axis=1), train["label"]
    X_val, y_val = val.drop("label", axis=1), val["label"]

    mlflow.set_experiment("churn-classifier")
    with mlflow.start_run() as run:
        mlflow.log_params({"n_estimators": n_estimators, "max_depth": max_depth})
        model = RandomForestClassifier(n_estimators=n_estimators,
                                       max_depth=max_depth, random_state=42)
        model.fit(X_train, y_train)

        preds = model.predict(X_val)
        f1 = f1_score(y_val, preds)
        auc = roc_auc_score(y_val, model.predict_proba(X_val)[:, 1])
        mlflow.log_metric("val_f1", f1)
        mlflow.log_metric("val_roc_auc", auc)

        # Gate: only register if meets bar
        if f1 < 0.80:
            raise ValueError(f"Model below bar: f1={f1:.3f} < 0.80")

        mlflow.sklearn.log_model(model, "model",
            registered_model_name="churn-classifier")

    with open(model_output.path, "wb") as f:
        pickle.dump(model, f)
    metrics_output.log_metric("val_f1", f1)
    metrics_output.log_metric("val_roc_auc", auc)

@dsl.pipeline(name="training-pipeline")
def training_pipeline(
    train_path: str,
    val_path: str,
    n_estimators: int = 200,
):
    train_step = train_model(
        train_data=...,
        val_data=...,
        n_estimators=n_estimators,
    )
```

## Model Validation Gates

Before promoting a model:

```python
def validate_model(model, X_val, y_val, X_test, y_test, baseline_f1=None):
    """Run all validation gates. Raise on failure."""
    val_preds = model.predict(X_val)
    test_preds = model.predict(X_test)

    val_f1 = f1_score(y_val, val_preds)
    test_f1 = f1_score(y_test, test_preds)

    # Gate 1: meets minimum bar
    assert test_f1 >= 0.80, f"Below minimum: test_f1={test_f1:.3f}"

    # Gate 2: val/test consistency (no overfit to val)
    assert abs(val_f1 - test_f1) < 0.05, f"Val/test drift: {val_f1:.3f} vs {test_f1:.3f}"

    # Gate 3: beats baseline (if provided)
    if baseline_f1:
        assert test_f1 > baseline_f1, f"Regression: {test_f1:.3f} <= baseline {baseline_f1:.3f}"

    # Gate 4: per-class performance (no silent minority-class failure)
    from sklearn.metrics import classification_report
    report = classification_report(y_test, test_preds, output_dict=True)
    for cls in set(y_test):
        assert report[str(cls)]["f1-score"] >= 0.50, f"Class {cls} F1 too low"

    # Gate 5: calibration (for probabilistic models)
    from sklearn.calibration import calibration_curve
    prob_true, prob_pred = calibration_curve(y_test,
        model.predict_proba(X_test)[:, 1], n_bins=10)
    assert max(abs(prob_true - prob_pred)) < 0.15, "Calibration error too high"

    return {"val_f1": val_f1, "test_f1": test_f1}
```

## Bias & Fairness Checks

```python
from fairlearn.metrics import demographic_parity_difference, equalized_odds_difference

def fairness_check(model, X_test, y_test, sensitive_features):
    preds = model.predict(X_test)

    dp = demographic_parity_difference(y_test, preds,
        sensitive_features=sensitive_features)
    eo = equalized_odds_difference(y_test, preds,
        sensitive_features=sensitive_features)

    # Gate: fairness thresholds
    assert dp < 0.1, f"Demographic parity violation: {dp:.3f}"
    assert eo < 0.1, f"Equalized odds violation: {eo:.3f}"

    return {"demographic_parity": dp, "equalized_odds": eo}
```

## A/B Testing Models

```python
# Route a fraction of traffic to the new model
import random

def route_model(user_id: str, variant_b_pct: float = 0.1) -> str:
    # Deterministic hash so same user sees same variant
    bucket = hash(user_id) % 100 / 100.0
    return "B" if bucket < variant_b_pct else "A"

# In serving code:
variant = route_model(user_id)
model = load_model("churn-classifier", stage="Production" if variant == "A" else "Staging")
pred = model.predict(features)

# Log outcome for analysis
log_prediction(user_id, variant, pred, actual_outcome)
```

### Statistical Significance

Run until you have enough samples to detect the expected effect:

```python
from statsmodels.stats.proportion import proportions_ztest

# After collecting N samples per arm
control_conversions = 1000
control_n = 10000
treatment_conversions = 1150
treatment_n = 10000

z, p = proportions_ztest(
    [control_conversions, treatment_conversions],
    [control_n, treatment_n]
)
if p < 0.05:
    print(f"Significant: p={p:.4f} — promote treatment")
else:
    print(f"Not significant: p={p:.4f} — keep collecting data")
```

## Shadow Deployment

Run the new model alongside production; don't serve its outputs, just log them:

```python
def serve_shadow(features):
    # Production model — actually serves
    prod_pred = prod_model.predict(features)
    serve_to_user(prod_pred)

    # Shadow model — log only, don't serve
    shadow_pred = shadow_model.predict(features)
    log_to_shadow_store(features, prod_pred, shadow_pred)

    # Compare offline
    return prod_pred
```

Promote shadow to production only after offline metrics confirm it's better.

## Canary Deployment

```python
# Route 5% → 25% → 50% → 100% over time, watching metrics
canary_pct = 0.05  # start at 5%

if metric_alerts_during_canary():
    rollback_to_previous_version()
elif canary_stable_for(hours=24):
    canary_pct = 0.25  # increase
```

## Monitoring Deployed Models

| Signal | Alert | Action |
|---|---|---|
| Prediction distribution shift | KL divergence > 0.2 vs training | Investigate feature drift |
| Latency p95 > 2x baseline | Performance regression | Profile; check for cold start |
| Error rate > 1% | Infra bug or model edge case | Rollback; investigate |
| Feature drift (input) | PSI > 0.2 | Retrain; update features |
| Outcome drift (label, delayed) | Real-world metric drops | Trigger retraining pipeline |
| Calibration drift | Predicted probs no longer match outcomes | Recalibrate or retrain |

### Automated Retraining

```python
# Trigger: scheduled (weekly) + drift-triggered
@flow(name="retrain-on-drift")
def retrain_on_drift():
    if not drift_detected():
        return "No drift; skipping"

    # Run training pipeline
    new_version = train_pipeline(data_version=latest_data_version())

    # Validate
    if not validate_model(new_version):
        alert_team(f"Retrained model failed validation: {new_version}")
        return "Validation failed"

    # Shadow deploy
    deploy_to_shadow(new_version)
    if shadow_metrics_good_for(hours=48):
        promote_to_production(new_version)
    else:
        archive(new_version)
        alert_team("Shadow model underperformed; rolled back")
```

## Reproducibility Checklist

Before promoting any model:
- [ ] Random seed set (Python, NumPy, sklearn, PyTorch)
- [ ] Training data version pinned (DVC hash)
- [ ] Feature definitions versioned (Feast commit)
- [ ] Hyperparameters logged to MLflow
- [ ] Code commit hash recorded
- [ ] Docker image pinned (with all deps)
- [ ] Eval metrics recorded with eval set version
- [ ] Validation gates passed
- [ ] Model signed (hash) for tamper detection
- [ ] Rollback plan documented (previous version in registry)

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Train/test leakage | Inflated metrics | Use temporal split; out-of-fold encoding |
| No validation gate | Bad model reaches production | Assert min metrics + per-class F1 + calibration |
| No rollback plan | Bad model stuck in prod | Model registry + canary + auto-rollback |
| Drift undetected | Slow model decay | Monitor input + output distributions |
| Serving different code than training | Train/serve skew | Use MLflow model packaging |
| Hardcoded thresholds | Brittle to data changes | Move to config; alert on changes |
| No A/B test before promote | Vibe-based promotion | Always A/B or shadow before canary→prod |
| No per-class metrics | Silent minority-class failure | Always report classification_report per class |
| Retraining without validation | New model may be worse | Validation gate before any promotion |
