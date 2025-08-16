terraform {
  required_version = ">= 1.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "~> 5.0"
    }
  }

  backend "gcs" {
    bucket = "terraform-state-build-cache"
    prefix = "terraform/state"
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

provider "google-beta" {
  project = var.project_id
  region  = var.region
}

# Variables
variable "project_id" {
  description = "GCP Project ID"
  type        = string
}

variable "region" {
  description = "GCP Region"
  type        = string
  default     = "us-central1"
}

variable "cluster_name" {
  description = "GKE Cluster name"
  type        = string
  default     = "build-cache-cluster"
}

variable "node_pool_machine_type" {
  description = "Machine type for GKE nodes"
  type        = string
  default     = "e2-standard-4"
}

variable "node_pool_disk_size" {
  description = "Disk size for GKE nodes in GB"
  type        = number
  default     = 100
}

variable "min_node_count" {
  description = "Minimum number of nodes in the node pool"
  type        = number
  default     = 1
}

variable "max_node_count" {
  description = "Maximum number of nodes in the node pool"
  type        = number
  default     = 10
}

variable "network_name" {
  description = "The name of the VPC network"
  type        = string
  default     = "vpc-build-cache"
}

variable "gcs_bucket" {
  description = "The name of the GCS bucket for cache storage"
  type        = string
}

variable "domain_name" {
  description = "The domain name for the ingress (leave empty for IP-only access)"
  type        = string
  default     = ""
}

variable "environment" {
  description = "The environment (dev, staging, production)"
  type        = string
  default     = "dev"
}

variable "enable_private_nodes" {
  description = "Enable private nodes for the cluster"
  type        = bool
  default     = false
}

variable "authorized_networks" {
  description = "List of authorized networks for cluster access"
  type        = list(string)
  default     = null
}

# Enable required APIs
resource "google_project_service" "required_apis" {
  for_each = toset([
    "container.googleapis.com",
    "compute.googleapis.com",
    "storage.googleapis.com",
    "iam.googleapis.com",
    "monitoring.googleapis.com",
    "logging.googleapis.com",
    "cloudresourcemanager.googleapis.com"
  ])

  service = each.value
  project = var.project_id

  disable_dependent_services = false
  disable_on_destroy         = false
}

# VPC Network
resource "google_compute_network" "vpc" {
  name                    = "${var.cluster_name}-vpc"
  auto_create_subnetworks = false
  depends_on              = [google_project_service.required_apis]
}

# Subnet
resource "google_compute_subnetwork" "subnet" {
  name          = "${var.cluster_name}-subnet"
  ip_cidr_range = "10.0.0.0/16"
  region        = var.region
  network       = google_compute_network.vpc.id

  secondary_ip_range {
    range_name    = "pod-range"
    ip_cidr_range = "10.1.0.0/16"
  }

  secondary_ip_range {
    range_name    = "service-range"
    ip_cidr_range = "10.2.0.0/16"
  }
}

# Cloud NAT for private cluster
resource "google_compute_router" "router" {
  name    = "${var.cluster_name}-router"
  region  = var.region
  network = google_compute_network.vpc.id
}

resource "google_compute_router_nat" "nat" {
  name                               = "${var.cluster_name}-nat"
  router                            = google_compute_router.router.name
  region                            = var.region
  nat_ip_allocate_option            = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"

  log_config {
    enable = true
    filter = "ERRORS_ONLY"
  }
}

# GKE Cluster
resource "google_container_cluster" "primary" {
  name     = var.cluster_name
  location = var.region

  # Enable Autopilot mode
  enable_autopilot = true

  # Networking configuration
  network    = google_compute_network.vpc.self_link
  subnetwork = google_compute_subnetwork.private.self_link

  # IP allocation policy for VPC-native cluster
  ip_allocation_policy {
    cluster_secondary_range_name  = "pod-range"
    services_secondary_range_name = "service-range"
  }

  # Private cluster configuration
  private_cluster_config {
    enable_private_nodes    = var.enable_private_nodes
    enable_private_endpoint = false
    master_ipv4_cidr_block  = "172.16.0.0/28"
  }

  # Master authorized networks
  dynamic "master_authorized_networks_config" {
    for_each = var.authorized_networks != null ? [1] : []
    content {
      dynamic "cidr_blocks" {
        for_each = var.authorized_networks
        content {
          cidr_block   = cidr_blocks.value
          display_name = "Authorized network"
        }
      }
    }
  }

  # Workload Identity
  workload_identity_config {
    workload_pool = "${var.project_id}.svc.id.goog"
  }

  # Release channel for automatic updates
  release_channel {
    channel = "REGULAR"
  }

  # Maintenance window
  maintenance_policy {
    daily_maintenance_window {
      start_time = "03:00"
    }
  }

  # Monitoring and logging
  monitoring_config {
    enable_components = ["SYSTEM_COMPONENTS", "WORKLOADS"]
    managed_prometheus {
      enabled = true
    }
  }

  logging_config {
    enable_components = ["SYSTEM_COMPONENTS", "WORKLOADS"]
  }

  # Enable binary authorization
  binary_authorization {
    evaluation_mode = "PROJECT_SINGLETON_POLICY_ENFORCE"
  }

  # Enable shielded nodes
  enable_shielded_nodes = true

  # Cluster autoscaling
  cluster_autoscaling {
    enabled = true
    auto_provisioning_defaults {
      oauth_scopes = [
        "https://www.googleapis.com/auth/cloud-platform",
      ]
    }
  }

  depends_on = [
    google_project_service.container,
    google_project_service.compute,
  ]
}

# Node pool for build cache workloads
resource "google_container_node_pool" "build_cache_nodes" {
  name       = "build-cache-pool"
  location   = var.region
  cluster    = google_container_cluster.primary.name
  node_count = var.min_node_count

  # Auto-scaling
  autoscaling {
    min_node_count = var.min_node_count
    max_node_count = var.max_node_count
  }

  # Node configuration
  node_config {
    preemptible  = false
    machine_type = var.node_pool_machine_type
    disk_size_gb = var.node_pool_disk_size
    disk_type    = "pd-ssd"

    # Google service account
    service_account = google_service_account.gke_nodes.email
    oauth_scopes = [
      "https://www.googleapis.com/auth/cloud-platform"
    ]

    # Labels and taints
    labels = {
      role = "build-cache"
    }

    taint {
      key    = "build-cache"
      value  = "true"
      effect = "NO_SCHEDULE"
    }

    # Workload Identity
    workload_metadata_config {
      mode = "GKE_METADATA"
    }

    # Security
    metadata = {
      disable-legacy-endpoints = "true"
    }
  }

  # Upgrade settings
  upgrade_settings {
    max_surge       = 1
    max_unavailable = 0
  }

  # Management
  management {
    auto_repair  = true
    auto_upgrade = true
  }
}

# Service accounts
resource "google_service_account" "gke_nodes" {
  account_id   = "gke-nodes-${var.cluster_name}"
  display_name = "GKE Nodes Service Account"
}

resource "google_service_account" "build_cache_server" {
  account_id   = "build-cache-server"
  display_name = "Build Cache Server Service Account"
}

# IAM bindings for node service account
resource "google_project_iam_member" "gke_nodes_roles" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
    "roles/monitoring.viewer",
    "roles/stackdriver.resourceMetadata.writer"
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.gke_nodes.email}"
}

# Cloud Storage bucket for cache
resource "google_storage_bucket" "cache_bucket" {
  name     = "${var.project_id}-build-cache"
  location = var.region

  # Versioning
  versioning {
    enabled = false
  }

  # Lifecycle management
  lifecycle_rule {
    condition {
      age = 90
    }
    action {
      type = "Delete"
    }
  }

  lifecycle_rule {
    condition {
      age = 30
    }
    action {
      type          = "SetStorageClass"
      storage_class = "NEARLINE"
    }
  }

  # Uniform bucket-level access
  uniform_bucket_level_access = true

  # Encryption
  encryption {
    default_kms_key_name = google_kms_crypto_key.cache_key.id
  }

  depends_on = [google_project_service.required_apis]
}

# KMS for encryption
resource "google_kms_key_ring" "cache_keyring" {
  name     = "build-cache-keyring"
  location = var.region
}

resource "google_kms_crypto_key" "cache_key" {
  name     = "build-cache-key"
  key_ring = google_kms_key_ring.cache_keyring.id

  lifecycle {
    prevent_destroy = true
  }
}

# IAM for build cache server
resource "google_storage_bucket_iam_member" "cache_bucket_admin" {
  bucket = google_storage_bucket.cache_bucket.name
  role   = "roles/storage.admin"
  member = "serviceAccount:${google_service_account.build_cache_server.email}"
}

resource "google_kms_crypto_key_iam_member" "cache_key_user" {
  crypto_key_id = google_kms_crypto_key.cache_key.id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = "serviceAccount:${google_service_account.build_cache_server.email}"
}

# Workload Identity binding
resource "google_service_account_iam_member" "workload_identity_binding" {
  service_account_id = google_service_account.build_cache_server.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[build-cache/build-cache-server]"
}

# Outputs
output "cluster_endpoint" {
  description = "GKE Cluster Endpoint"
  value       = google_container_cluster.primary.endpoint
  sensitive   = true
}

output "cluster_ca_certificate" {
  description = "GKE Cluster CA Certificate"
  value       = google_container_cluster.primary.master_auth[0].cluster_ca_certificate
  sensitive   = true
}

output "cache_bucket_name" {
  description = "Cloud Storage bucket name for cache"
  value       = google_storage_bucket.cache_bucket.name
}

output "service_account_email" {
  description = "Build cache server service account email"
  value       = google_service_account.build_cache_server.email
}
