# AntCD

[![Go Version](https://img.shields.io/github/go-mod/go-version/ynotnauk/antcd)](https://golang.org)
[![License: MPL 2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](https://opensource.org/licenses/MPL-2.0)
[![Release Status](https://img.shields.io/github/actions/workflow/status/ynotnauk/antcd/release.yml?branch=main)](https://github.com/ynotnauk/antcd/actions)

AntCD is a lightweight, GitOps-driven Continuous Delivery (CD) operator for Kubernetes written in Go. 

It periodically polls one or more Git repositories for commit changes, pulls plain Kubernetes manifests or renders Helm charts, sorts the result by dependency order, and applies it directly to your cluster using Kubernetes Server-Side Apply. Resources removed from Git are automatically pruned.

It is deliberately small: a single static binary and a single Deployment, with no CRDs and no extra controllers to install.

## Features

- **Lightweight:** Single static Go binary on a distroless base image (see [Size](#size)).
- **Multiple Repositories:** Each repo has its own poll interval and one or more targets.
- **Plain Manifests and Helm:** Targets are either a directory of manifests or a Helm chart, rendered in-process (no `helm` binary needed).
- **Fast Git Polling:** Checks remote branch hashes without full clones.
- **Dependency Sorting:** Automatically applies Namespaces, CRDs, and Configs before workloads.
- **Automated Pruning:** Safely deletes Kubernetes resources removed from the Git repository, scoped per repo and target.
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

Edit ```config.yaml``` with your repositories and targets. The config is required; there are no defaults for `repos`.

```yaml
server:
  port: 8080
  webhookSecret: "supersecret"
repos:
  - name: platform
    url: "https://github.com/your-org/platform.git"
    branch: "main"          # default: main
    pollInterval: "10s"     # default: 30s
    tokenEnv: "PLATFORM_GIT_TOKEN"   # optional: env var holding a token for private repos
    targets:
      - name: infra
        type: manifests     # default
        path: ./manifests
      - name: myapp
        type: helm
        path: ./charts/myapp
        releaseName: myapp  # default: target name
        valuesFiles: [values-prod.yaml]
        values:
          replicas: 3
  - name: tools
    url: "https://github.com/your-org/tools.git"
    pollInterval: "5m"
    targets:
      - name: tools
        path: .
```

Each repo is polled independently, so a slow or failing repo never blocks the others. Names must be valid Kubernetes label values, and paths must be relative and stay inside the repository.

#### Targets

| Field | Applies to | Description |
|---|---|---|
| `name` | all | Unique within the repo. Used to scope pruning. |
| `type` | all | `manifests` (default) or `helm`. Always explicit, nothing is auto-detected. |
| `path` | all | Directory in the repo. For `helm` it must contain `Chart.yaml`. |
| `releaseName` | helm | Exposed as `.Release.Name`. Defaults to the target name. |
| `valuesFiles` | helm | Relative to the chart directory, applied in order over the chart defaults. |
| `values` | helm | Inline values, applied last. Nested maps merge. |

**Namespaces:** AntCD has no namespace setting. A resource's namespace comes from its manifest or chart; namespaced resources that don't declare one are applied to `default`.

**Labels and pruning:** every applied resource is labelled `app.kubernetes.io/managed-by=antcd`, `antcd.io/repo=<repo>` and `antcd.io/target=<target>`. Pruning only deletes resources carrying the matching repo and target labels. Removing a target from the config does *not* delete what it previously deployed.

#### Helm support

Charts are rendered client-side and applied as plain manifests; AntCD never creates Helm release objects, so `helm list` won't show them. Limitations:

- Hooks (`helm.sh/hook`) are skipped with a warning.
- `lookup` and cluster-dependent `.Capabilities` are not supported.
- Chart dependencies must be vendored into `charts/` and committed (`helm dependency build`).
- Charts in the Git repository only; remote chart repositories and OCI charts are not supported.

See [examples/helm-hello](examples/helm-hello) for a minimal chart.

### 3. Run AntCD

```bash
go run ./cmd/antcd/main.go --config config.yaml
```

---

## Production Installation (via Helm)

Deploy AntCD into any Kubernetes cluster directly from the GitHub Container Registry. Repos and targets are set in a values file:

```yaml
# antcd-values.yaml
server:
  webhookSecret: "change-me"
repos:
  - name: platform
    url: https://github.com/your-org/platform.git
    pollInterval: 30s
    targets:
      - name: infra
        path: ./manifests
      - name: myapp
        type: helm
        path: ./charts/myapp
        valuesFiles: [values-prod.yaml]
```

```bash
helm install antcd oci://ghcr.io/ynotnauk/charts/antcd \
  --namespace antcd-system \
  --create-namespace \
  -f antcd-values.yaml
```

The install fails if `repos` is empty.

### Private Git Repositories

Give a repo a token inline (stored in a chart-managed Secret) or reference a key in an existing Secret:

```yaml
repos:
  - name: private
    url: https://github.com/your-org/private-manifests.git
    token: "ghp_yourPersonalAccessToken"   # or:
    # tokenSecret:
    #   name: private-git-token
    #   key: token
    targets:
      - name: private
        path: .
```

Prefer `tokenSecret` so the token isn't kept in your values file. When running the binary directly, use `tokenEnv` in the config instead.

## Size

Measured on linux/amd64 with `CGO_ENABLED=0 go build -ldflags="-w -s"`:

| Artifact | Size |
|---|---|
| `antcd` binary | ~42 MB |
| Container image (uncompressed, distroless base) | ~65 MB |

Most of the binary is the Kubernetes and Helm client libraries. These figures will change between releases.

### RBAC

By default the chart creates a ClusterRole with `*` on all resources and verbs bound to AntCD's ServiceAccount (`<release>-sa`), because AntCD can apply any kind and must list and delete them to prune. To narrow it, override `rbac.rules`, for example:

```yaml
rbac:
  rules:
    - apiGroups: ["", "apps"]
      resources: ["namespaces", "configmaps", "services", "deployments"]
      verbs: ["get", "list", "create", "update", "patch", "delete"]
```

Set `rbac.create: false` to skip the ClusterRole and ClusterRoleBinding and bind the ServiceAccount yourself. AntCD also needs `get` and `list` on API discovery, which every authenticated user has by default.

## Triggering Syncs via Webhook

Trigger an immediate sync without waiting for the polling timer:

```bash
curl -i -X POST http://<antcd-host>:8080/api/v1/sync \
  -H "Authorization: Bearer <your-webhook-secret>"
```

## Releasing

Every push to `main` publishes the image (tagged with the version, `latest` and the short SHA) and the Helm chart. Before merging, bump the version in `chart/Chart.yaml` (`version` and `appVersion`) and `chart/values.yaml` (`image.tag`) to the same value. The release workflow fails if these disagree or if that version has already been published. Released versions are never deleted.

## License

This project is licensed under the [Mozilla Public License 2.0](https://ghcr.io/ynotnauk/antcd).
