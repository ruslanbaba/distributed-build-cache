project_id   = "YOUR_GCP_PROJECT_ID"
region       = "us-central1"
cluster_name = "build-cache-dev"
network_name = "vpc-build-cache"
gcs_bucket   = "bazel-cache-YOUR_PROJECT_ID-dev"
domain_name  = "" # leave empty to use public IP only

# Environment-specific settings
environment = "dev"
min_replicas = 2
max_replicas = 10
node_count = 3
machine_type = "e2-standard-4"

# Cache settings
max_cache_size_gb = 500
pruning_interval_hours = 24
retention_days = 7
