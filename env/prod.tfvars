project_id   = "YOUR_GCP_PROJECT_ID"
region       = "us-central1"
cluster_name = "build-cache-prod"
network_name = "vpc-build-cache-prod"
gcs_bucket   = "bazel-cache-YOUR_PROJECT_ID-prod"
domain_name  = "cache.example.com" # your production domain

# Environment-specific settings
environment = "production"
min_replicas = 5
max_replicas = 50
node_count = 10
machine_type = "e2-standard-8"

# Cache settings
max_cache_size_gb = 3000
pruning_interval_hours = 12
retention_days = 14

# Security settings
enable_private_nodes = true
authorized_networks = ["10.0.0.0/8"] # Corporate network only
