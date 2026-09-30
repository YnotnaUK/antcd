# AntCD

[![Go Version](https://img.shields.io/github/go-mod/go-version/ynotnauk/antcd)](https://golang.org)
[![License: MPL 2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](https://opensource.org/licenses/MPL-2.0)
[![Release Status](https://img.shields.io/github/actions/workflow/status/ynotnauk/antcd/release.yml?branch=main)](https://github.com/ynotnauk/antcd/actions)
[![Image Size](https://img.shields.io/badge/image_size-~14MB-blue)](https://ghcr.io/ynotnauk/antcd)

AntCD is a lightweight, GitOps-driven Continuous Delivery (CD) operator for Kubernetes written in Go. 

It periodically polls a Git repository for commit changes, pulls the latest manifests, and applies them directly to your cluster using Kubernetes Server-Side Apply.

## Features

- **Lightweight:** Single binary, minimal image footprint (~14MB).
- **Fast Git Polling:** Queries remote branch refs without performing heavy full clones.
- **Native Kubernetes Client:** Uses client-go dynamic engine to apply arbitrary YAML manifests.
- **Embedded Web Server:**
  - `GET /healthz` - Health and liveness probe endpoint.
  - `POST /sync` - Webhook endpoint to trigger an immediate sync without waiting for the poll interval.
- **Private Repo Support:** Easily pass access tokens via environment variables or Secrets.

## Installation

Install AntCD to your Kubernetes cluster using Helm directly from the GitHub Container Registry (OCI):

```bash
helm install antcd oci://ghcr.io/ynotnauk/charts/antcd \
  --namespace antcd-system \
  --create-namespace \
  --set git.repo="https://github.com/your-org/your-manifests.git" \
  --set git.branch="main" \
  --set git.path="./manifests" \
  --set git.pollInterval="30s"
```

### Private Repositories

If syncing a private repository, provide a Git token:

```bash
helm install antcd oci://ghcr.io/ynotnauk/charts/antcd
--namespace antcd-system
--create-namespace
--set git.repo="https://github.com/your-org/private-manifests.git"
--set git.token="ghp_yourPersonalAccessToken"
```

## Manual Sync Webhook
Trigger an instant sync on push via HTTP:

```bash
curl -X POST http://<antcd-service-ip>:8080/api/v1/sync \
  -H "Authorization: Bearer <your-webhook-secret>"
```

## Local Development
1. Clone the repository:
```bash
git clone https://github.com/ynotnauk/antcd.git
cd antcd
```
2. Run locally:
```bash
go run ./cmd/antcd/main.go --config config.yaml
```
3. Build Docker image:
```bash
docker build -t antcd:latest .
```
