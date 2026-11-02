# Cloud Decision Matrix — AWS / Azure / GCP

## Service Selection by Category

### Compute (VMs)

| Need | AWS | Azure | GCP | Notes |
|---|---|---|---|---|
| General purpose | m5/m6g (Graviton) | D-series | n2/e2 | Graviton4 = 20% cheaper + better perf/W |
| Burstable (dev) | t3 | B-series | e2-medium | Cheap; credits |
| Compute-optimized | c6/c7g | F-series | c2 | High CPU |
| Memory-optimized | r6/r7 | M-series/E-series | m2/m3 | In-memory DBs |
| GPU (ML) | p4d/p5 | NC H-series | a2/a3 | A3 cheapest for H100 |
| Spot | Spot Instances | Spot VMs | Spot VMs | 60-90% off |

### Containers & Serverless

| Need | AWS | Azure | GCP | Default pick |
|---|---|---|---|---|
| Managed K8s | EKS | AKS | GKE / GKE Autopilot | GKE Autopilot (lowest ops) |
| Serverless containers | Fargate (ECS/Fargate) | Container Apps | Cloud Run | Cloud Run (dev), Fargate (AWS-locked) |
| Functions | Lambda | Functions | Cloud Functions | Match stack vendor |
| Service mesh | App Mesh | Open Service Mesh | Anthos Service Mesh | Use Istio/Linkerd OSS instead |

### Storage

| Need | AWS | Azure | GCP | Notes |
|---|---|---|---|---|
| Object storage | S3 | Blob Storage | Cloud Storage | All S3-compatible via endpoints |
| Block storage | EBS (gp3/io2) | Managed Disk | Persistent Disk | gp3 is 20% cheaper than gp2 |
| File storage (NFS) | EFS | Azure Files | Filestore | Multi-attach (RWX) |
| Archive | Glacier / Deep Archive | Archive Storage | Archive | Deep Archive cheapest long-term |
| CDN | CloudFront | Azure CDN | Cloud CDN | Match origin vendor |

### Databases

| Need | AWS | Azure | GCP | Notes |
|---|---|---|---|---|
| Relational (managed) | Aurora / RDS | Azure SQL / Postgres | Cloud SQL | Aurora fastest HA; Cloud SQL cheapest |
| Serverless SQL | Aurora Serverless v2 | Azure SQL Serverless | AlloyDB | Variable workloads |
| Key-value (NoSQL) | DynamoDB | Cosmos DB | Firestore / Datastore | Lock-in HIGH |
| Wide-column | DynamoDB | Cosmos DB (Cassandra API) | Bigtable | Bigtable for heavy write |
| Cache (Redis) | ElastiCache | Azure Cache | Memorystore | All managed Redis/Memcached |
| Graph | Neptune | Cosmos DB (Gremlin) | — | Niche |
| Time-series | Timestream | Azure Data Explorer | Bigtable | ADX most capable |
| Analytics DW | Redshift | Synapse | BigQuery | BigQuery (ad-hoc), Redshift (throughput) |

### Networking

| Need | AWS | Azure | GCP |
|---|---|---|---|
| VPC | VPC + subnets | VNet + subnets | VPC + subnets |
| Load balancer (L4) | NLB | Azure LB | Cloud LB (TCP) |
| Load balancer (L7) | ALB | Application Gateway | Cloud LB (HTTPS) |
| DNS | Route 53 | Azure DNS | Cloud DNS |
| CDN | CloudFront | Azure CDN | Cloud CDN |
| Private connectivity | PrivateLink | Private Link | Private Service Connect |
| Direct interconnect | Direct Connect | ExpressRoute | Cloud Interconnect |
| Mesh transit | Transit Gateway | Virtual WAN | Network Connectivity Center |

### Identity & Secrets

| Need | AWS | Azure | GCP |
|---|---|---|---|
| Identity | IAM | Entra ID (Azure AD) | Cloud IAM |
| Federation | IAM OIDC | Workload Identity | Workload Identity |
| Secrets | Secrets Manager + Parameter Store | Key Vault | Secret Manager |
| KMS | KMS | Key Vault | Cloud KMS |
| Certs | ACM | Key Vault | Certificate Manager |

### Observability

| Need | AWS | Azure | GCP |
|---|---|---|---|
| Metrics | CloudWatch | Azure Monitor | Cloud Monitoring |
| Logs | CloudWatch Logs | Log Analytics | Cloud Logging |
| Traces | X-Ray | Application Insights | Cloud Trace |
| Distributed | OpenTelemetry collector + backend (Jaeger/Tempo) | — | — |

**Recommendation**: Use OpenTelemetry + a vendor-neutral backend (Grafana stack, Datadog, Honeycomb). Avoid vendor lock-in on observability.

## Multi-Region Strategy

| Pattern | RPO | RTO | Cost | Complexity |
|---|---|---|---|---|
| Backup & restore | Hours | Hours | Lowest | Low |
| Pilot light | Minutes | Hours | Low | Medium |
| Warm standby | Seconds | Minutes | Medium | Medium |
| Multi-site active-active | Near-zero | Near-zero | Highest | Highest |

### Decision framework

- **RTO < 15 min AND RPO < 1 min** → multi-site active-active (e.g. Aurora Global Database, Cosmos multi-master, Spanner)
- **RTO < 1 hour AND RPO < 5 min** → warm standby in second region with async replication
- **RTO hours** → backup-restore (automated daily snapshots + cross-region copy)

### Implementation per service

| Service | Multi-region option |
|---|---|
| S3 / Blob / GCS | Cross-region replication (CRR) |
| DynamoDB | Global tables (multi-master) |
| Aurora | Aurora Global Database (1 replica region, <1s RPO) |
| Cosmos DB | Multi-region writes |
| Cloud SQL | Cross-region read replica + promote |
| Redis | ElastiCache Global Datastore / Memorystore cross-region |
| Route 53 / DNS | Health-checked failover routing |

## Cost Optimization

### Compute

| Strategy | Savings | Use When |
|---|---|---|
| Savings Plans / Committed Use Discount | 30-72% | Baseline always-on (commit 1-3yr) |
| Spot / Preemptible | 60-90% | Fault-tolerant, batch, CI/CD, K8s workers |
| Graviton / Arm64 | ~20% | Compatible workloads (no x86 binaries) |
| Right-size (VPA / Compute Optimizer) | 10-40% | All workloads, quarterly review |
| Fargate / Cloud Run (scale to zero) | High (variable) | Spiky workloads |

### Storage lifecycle

```
S3 Standard (0-30d) → S3 Standard-IA (30-90d) → Glacier IR (90-180d) → Glacier Deep Archive (>180d) → Delete (730d)
```

Set lifecycle rules on every bucket holding logs/backups. Saves 60-80% on log retention.

### Network (egress is the silent killer)

- Cross-AZ data transfer: $0.01/GB each way (AWS) — minimize by keeping services in same AZ where possible
- Cross-region: $0.02/GB
- Internet egress: $0.08-0.12/GB — use CDN (CloudFront caches at edge)
- Use VPC endpoints (S3, DynamoDB free) to avoid NAT gateway charges
- CloudFront is cheaper than direct S3 for cached content

### FinOps KPIs

| KPI | Target |
|---|---|
| Reservation coverage | >70% of baseline |
| Reservation utilization | >80% |
| Idle resource waste | <10% of total |
| Tagging coverage | >95% (for cost allocation) |
| Forecast accuracy | 90-110% |

### Tagging strategy (cloud-agnostic)

```yaml
# Required tags on every resource
Environment: production | staging | dev
CostCenter: engineering | marketing
Owner: platform-team
Project: payments
ManagedBy: terraform | manual
```

Enforce via SCP (AWS), Policy (Azure), Org Policy (GCP).

## Vendor Lock-In Mitigation

| Layer | Risk | Mitigation |
|---|---|---|
| Compute (VM) | Low | Standard OS images; IaC |
| Kubernetes | Low | Plain manifests; avoid proprietary add-ons |
| Object storage | Low | S3-compatible API (works on R2, MinIO) |
| SQL DB | Medium | Standard SQL; logical backups |
| NoSQL (DynamoDB) | High | Abstract via interface; or use Postgres+JSONB |
| Serverless functions | High | Container-image-based functions; avoid proprietary SDK |
| Managed AI/ML | High | ONNX export; open-source frameworks |
| Proprietary workflow (Step Functions) | High | Argo Workflows / Temporal as alternative |

## Multi-Cloud Patterns

**Valid multi-cloud reasons**: regulatory data residency, best-of-breed (BigQuery + AWS ML), DR across clouds, acquisition integration, negotiating leverage.

**Bad reasons**: "Avoid lock-in" without exit scenario, "spread risk" without architecture.

| Pattern | Complexity | When |
|---|---|---|
| Active-Active | Highest | Global latency optimization; needs robust data sync |
| Active-Passive DR | Medium | Cloud provider as failure domain |
| Segmented by workload | Lowest | Best-fit per workload (e.g. analytics on GCP, app on AWS) |

## Well-Architected Checklist

- [ ] Multi-AZ for tier-1 services (N+2 AZs)
- [ ] Multi-region DR plan with documented RTO/RPO
- [ ] IaC (Terraform) for everything
- [ ] Cost allocation tags enforced
- [ ] Encryption at rest + in transit
- [ ] Least-privilege IAM
- [ ] Observability (metrics + logs + traces) for every service
- [ ] Backups + restore drills (quarterly)
- [ ] Autoscaling + circuit breakers
- [ ] Capacity plan reviewed quarterly
