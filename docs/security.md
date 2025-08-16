# Security Overview

## Security Architecture

The Distributed Build Cache System implements enterprise-grade security controls across all layers of the infrastructure.

## Authentication & Authorization

### Workload Identity
```yaml
# Kubernetes Service Account binding to Google Cloud Service Account
apiVersion: v1
kind: ServiceAccount
metadata:
  name: build-cache-server
  namespace: build-cache
  annotations:
    iam.gke.io/gcp-service-account: build-cache-server@PROJECT_ID.iam.gserviceaccount.com
```

Benefits:
- No service account keys to manage
- Automatic credential rotation
- Fine-grained IAM permissions
- Audit trail for all access

### IAM Policies

#### Principle of Least Privilege
```hcl
# Service account permissions
resource "google_project_iam_member" "build_cache_storage" {
  project = var.project_id
  role    = "roles/storage.objectAdmin"
  member  = "serviceAccount:${google_service_account.build_cache_server.email}"
  
  condition {
    title       = "Cache bucket only"
    description = "Access limited to cache bucket"
    expression  = "resource.name.startsWith('projects/_/buckets/${var.project_id}-build-cache')"
  }
}
```

#### Role-Based Access Control (RBAC)
```yaml
# Kubernetes RBAC
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  namespace: build-cache
  name: cache-server-role
rules:
- apiGroups: [""]
  resources: ["configmaps", "secrets"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
```

## Network Security

### Private GKE Cluster
```hcl
resource "google_container_cluster" "primary" {
  private_cluster_config {
    enable_private_nodes    = true
    enable_private_endpoint = false
    master_ipv4_cidr_block  = "172.16.0.0/28"
  }
  
  master_authorized_networks_config {
    cidr_blocks {
      cidr_block   = "10.0.0.0/8"  # Corporate network only
      display_name = "Corporate VPN"
    }
  }
}
```

### Service Mesh Security (Istio)
```yaml
# Mutual TLS enforcement
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: build-cache
spec:
  mtls:
    mode: STRICT

# Authorization policy
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: cache-access-policy
  namespace: build-cache
spec:
  selector:
    matchLabels:
      app: build-cache-server
  rules:
  - when:
    - key: source.certificate_fingerprint
      values: ["allowed-client-cert-fingerprint"]
```

### Network Policies
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: cache-server-netpol
  namespace: build-cache
spec:
  podSelector:
    matchLabels:
      app: build-cache-server
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: istio-system
    ports:
    - protocol: TCP
      port: 8080
  egress:
  - to: []
    ports:
    - protocol: TCP
      port: 443  # HTTPS to Cloud Storage
```

## Data Protection

### Encryption at Rest
```hcl
# Cloud Storage bucket with Customer-Managed Encryption Keys (CMEK)
resource "google_storage_bucket" "cache_bucket" {
  name     = "${var.project_id}-build-cache"
  location = var.region
  
  encryption {
    default_kms_key_name = google_kms_crypto_key.cache_key.id
  }
}

# KMS key with automatic rotation
resource "google_kms_crypto_key" "cache_key" {
  name     = "build-cache-key"
  key_ring = google_kms_key_ring.cache_keyring.id
  
  rotation_period = "7776000s"  # 90 days
  
  lifecycle {
    prevent_destroy = true
  }
}
```

### Encryption in Transit
- TLS 1.3 for all client connections
- mTLS between services via Istio
- gRPC with TLS for cache operations

### Data Classification
```go
// Sensitive data handling
type CacheEntry struct {
    Key          string    `json:"key"`
    Size         int64     `json:"size"`
    LastAccessed time.Time `json:"last_accessed"`
    ContentType  string    `json:"content_type"`
    // PII fields marked for special handling
    UserID       string    `json:"user_id,omitempty" pii:"true"`
}
```

## Container Security

### Base Image Security
```dockerfile
# Use distroless base image
FROM gcr.io/distroless/static-debian11:nonroot

# Run as non-root user
USER nonroot:nonroot
```

### Security Context
```yaml
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 65534
        fsGroup: 65534
        seccompProfile:
          type: RuntimeDefault
      containers:
      - name: cache-server
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop:
            - ALL
```

### Image Scanning
```yaml
# Trivy security scanning in CI/CD
- name: Run Trivy vulnerability scanner
  uses: aquasecurity/trivy-action@master
  with:
    image-ref: ${{ env.REGISTRY }}/${{ env.PROJECT_ID }}/${{ env.IMAGE_NAME }}:${{ github.sha }}
    format: 'sarif'
    output: 'trivy-results.sarif'
```

## Secrets Management

### Google Secret Manager Integration
```go
// Secure secret retrieval
func getSecret(ctx context.Context, secretName string) (string, error) {
    client, err := secretmanager.NewClient(ctx)
    if err != nil {
        return "", err
    }
    defer client.Close()

    req := &secretmanagerpb.AccessSecretVersionRequest{
        Name: fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretName),
    }

    result, err := client.AccessSecretVersion(ctx, req)
    if err != nil {
        return "", err
    }

    return string(result.Payload.Data), nil
}
```

### Secret Rotation
```yaml
# Automatic secret rotation
apiVersion: v1
kind: CronJob
metadata:
  name: secret-rotator
  namespace: build-cache
spec:
  schedule: "0 2 * * 0"  # Weekly rotation
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: rotator
            image: gcr.io/PROJECT_ID/secret-rotator:latest
            env:
            - name: SECRETS_TO_ROTATE
              value: "tls-cert,api-key"
```

## Monitoring & Alerting

### Security Metrics
```go
// Security-related metrics
var (
    authFailures = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "auth_failures_total",
            Help: "Total authentication failures",
        },
        []string{"source", "reason"},
    )
    
    unauthorizedAccess = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "unauthorized_access_attempts_total",
            Help: "Total unauthorized access attempts",
        },
        []string{"source", "resource"},
    )
)
```

### Security Alerts
```yaml
# Prometheus alerting rules
groups:
- name: security
  rules:
  - alert: HighAuthFailureRate
    expr: rate(auth_failures_total[5m]) > 10
    for: 1m
    labels:
      severity: critical
    annotations:
      summary: "High authentication failure rate detected"
      
  - alert: UnauthorizedAccess
    expr: rate(unauthorized_access_attempts_total[5m]) > 5
    for: 1m
    labels:
      severity: warning
    annotations:
      summary: "Unauthorized access attempts detected"
```

## Compliance & Auditing

### Audit Logging
```yaml
# GKE audit policy
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
- level: RequestResponse
  namespaces: ["build-cache"]
  resources:
  - group: ""
    resources: ["secrets", "configmaps"]
  - group: "apps"
    resources: ["deployments"]
```

### Cloud Audit Logs
```hcl
# Enable audit logs for Cloud Storage
resource "google_project_iam_audit_config" "storage_audit" {
  project = var.project_id
  service = "storage.googleapis.com"
  
  audit_log_config {
    log_type = "ADMIN_READ"
  }
  
  audit_log_config {
    log_type = "DATA_READ"
  }
  
  audit_log_config {
    log_type = "DATA_WRITE"
  }
}
```

## Incident Response

### Security Incident Playbook

#### 1. Detection
- Automated alerting via Prometheus
- SIEM integration with Cloud Security Command Center
- Manual reporting channels

#### 2. Containment
```bash
# Emergency procedures
kubectl patch deployment build-cache-server -n build-cache -p '{"spec":{"replicas":0}}'
gcloud compute firewall-rules create emergency-block --action=DENY --source-ranges=MALICIOUS_IP
```

#### 3. Investigation
- Audit log analysis
- Container forensics
- Network traffic analysis

#### 4. Recovery
- Restore from known good state
- Apply security patches
- Validate system integrity

### Security Testing

#### Penetration Testing
```yaml
# Automated security testing
apiVersion: batch/v1
kind: Job
metadata:
  name: security-scan
  namespace: build-cache
spec:
  template:
    spec:
      containers:
      - name: scanner
        image: owasp/zap2docker-stable
        command: ["zap-baseline.py"]
        args: ["-t", "http://build-cache-server.build-cache.svc.cluster.local:8080"]
```

#### Vulnerability Management
- Regular dependency scanning
- CVE monitoring and patching
- Security advisory notifications

## Security Governance

### Security Policies
1. **Access Control Policy**: Multi-factor authentication required
2. **Data Classification Policy**: Sensitive data handling procedures
3. **Incident Response Policy**: 24/7 security team contact
4. **Vulnerability Management Policy**: 30-day patching SLA

### Training & Awareness
- Annual security training for all developers
- Security champions program
- Regular security reviews and assessments

### Compliance Framework
- SOC 2 Type II compliance
- ISO 27001 certification path
- GDPR compliance for EU operations
- Regular third-party security audits
