# AntCD

[![Go Version](https://img.shields.io/github/go-mod/go-version/ynotnauk/antcd)](https://golang.org)
[![License: MPL 2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](https://opensource.org/licenses/MPL-2.0)
[![Release Status](https://img.shields.io/github/actions/workflow/status/ynotnauk/antcd/release.yml?branch=main)](https://github.com/ynotnauk/antcd/actions)
[![Image Size](https://img.shields.io/badge/image_size-~14MB-blue)](https://ghcr.io/ynotnauk/antcd)

AntCD is a lightweight, GitOps-driven Continuous Delivery (CD) operator for Kubernetes written in Go. 

It periodically polls a Git repository for commit changes, pulls the manifests, sorts them by dependency order, and applies them directly to your cluster using Kubernetes Server-Side Apply. Resources removed from Git are automatically pruned.

## Features

- **Lightweight:** Single static Go binary, minimal container footprint (~14MB).
- **Fast Git Polling:** Checks remote branch hashes without full clones.
- **Dependency Sorting:** Automatically applies Namespaces, CRDs, and Configs before workloads.
- **Automated Pruning:** Safely deletes Kubernetes resources removed from the Git repository.
- **Secure Webhook:** Instant sync via `/api/v1/sync` secured with Bearer token authentication.
- **Health Probes:** Unauthenticated `/healthz` endpoint for Kubernetes liveness/readiness checks.

---

## Prerequisites

- **Go** (for local development)
- **Docker & Docker Compose** (for container builds and local cluster)
- **kubectl** (to inspect the cluster)
- **Helm 3+** (to install AntCD in production)

---

## Local Development & Testing

AntCD includes a lightweight **K3s** cluster via Docker Compose so you can develop and test locally without an external cluster.

### 1. Start the Local Kubernetes Cluster

```bash
docker compose up -d
```

Wait a few seconds for the cluster to generate the credentials, then configure ```kubectl```:

```bash
mkdir -p ~/.kube
cp ./k3s-data/kubeconfig.yaml ~/.kube/config
sed -i 's/127.0.0.1/localhost/g' ~/.kube/config
```

Verify the node is ready

```bash
kubectl get nodes
```

### 2. Configure AntCD

Edit ```config.yaml``` with your target repository and polling interval:

```yaml
git:
  repo: "https://github.com/ynotnauk/antcd.git"
  branch: "main"
  path: "./manifests"
  pollInterval: "10s"
server:
  port: 8080
  webhookSecret: "supersecret"
targetNamespace: "default"
```

### 3. Run AntCD

```bash
go run ./cmd/antcd/main.go --config config.yaml
```

---

## Production Installation (via Helm)

Deploy AntCD into any Kubernetes cluster directly from the GitHub Container Registry:

```bash
helm install antcd oci://ghcr.io/ynotnauk/charts/antcd \
  --namespace antcd-system \
  --create-namespace \
  --set git.repo="https://github.com/your-org/your-manifests.git" \
  --set git.branch="main" \
  --set git.path="./manifests" \
  --set git.pollInterval="30s"
```

### Private Git Repositories

Pass your token during install:

```bash
helm install antcd oci://ghcr.io/ynotnauk/charts/antcd \
  --namespace antcd-system \
  --create-namespace \
  --set git.repo="https://github.com/your-org/private-manifests.git" \
  --set git.token="ghp_yourPersonalAccessToken"
```

## Triggering Syncs via Webhook

Trigger an immediate sync without waiting for the polling timer:

```bash
curl -i -X POST http://<antcd-host>:8080/api/v1/sync \
  -H "Authorization: Bearer <your-webhook-secret>"
```

## License

This project is licensed under the [Mozilla Public License 2.0](https://ghcr.io/ynotnauk/antcd).
