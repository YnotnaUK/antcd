# AntCD

[![Go Version](https://img.shields.io/github/go-mod/go-version/ynotnauk/antcd)](https://golang.org)
[![License: MPL 2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](https://opensource.org/licenses/MPL-2.0)
[![CI](https://img.shields.io/github/actions/workflow/status/ynotnauk/antcd/ci.yaml?branch=main&label=CI)](https://github.com/ynotnauk/antcd/actions/workflows/ci.yaml)
[![Release Status](https://img.shields.io/github/actions/workflow/status/ynotnauk/antcd/release.yaml?branch=main)](https://github.com/ynotnauk/antcd/actions)
[![Latest Release](https://img.shields.io/github/v/release/ynotnauk/antcd)](https://github.com/ynotnauk/antcd/releases)

AntCD is a lightweight GitOps continuous delivery operator for Kubernetes, written in Go.

It polls Git repositories, reads plain manifests or renders Helm charts, sorts the result by dependency order and applies it with Server-Side Apply. Resources removed from Git are pruned.

It is deliberately small: one static binary, one Deployment, no CRDs and no extra controllers.

## Features

- **Lightweight:** Single static binary on a distroless image (see [Size](#size)).
- **Multiple Repositories:** Each repo has its own poll interval and targets.
- **Plain Manifests and Helm:** Targets are either a directory of manifests or a Helm chart, rendered in-process (no `helm` binary required).
- **Fast Git Polling:** Checks remote branch hashes without cloning.
- **Dependency Sorting:** Applies Namespaces, CRDs and configs before workloads.
- **Pruning:** Deletes resources removed from Git, scoped per repo and target.
- **Secure Webhook:** Instant sync via `/api/v1/sync` secured with Bearer token authentication.
- **Health Probes:** Unauthenticated `/healthz` endpoint for liveness and readiness checks.

---

## Prerequisites

- **Go** (for local development)
- **Docker & Docker Compose** (for container builds and local cluster)
- **kubectl** (to inspect the cluster)
- **Helm 3+** (to install AntCD in production)

---

## Local Development & Testing

A K3s cluster is provided via Docker Compose, so no external cluster is needed.

### 1. Start the Local Kubernetes Cluster

```bash
docker compose up -d
```

Wait a few seconds for the credentials to be generated, then configure `kubectl` (or run `make kubeconfig`):

```bash
mkdir -p ~/.kube
cp ./k3s-data/kubeconfig.yaml ~/.kube/config
sed -i 's/127.0.0.1/localhost/g' ~/.kube/config
```

Check the node is ready:

```bash
kubectl get nodes
```

### 2. Configure AntCD

Edit `config.yaml` with your repositories and targets. `repos` has no default and is required.

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

The bundled `config.yaml` and `manifests/` are for local development only. Always set your own `webhookSecret` outside local use.

Repos are polled independently, so a slow or failing repo never blocks the others. Names must be valid Kubernetes label values, and paths must be relative and stay inside the repository.

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

Charts are rendered client-side and applied as plain manifests. No Helm release objects are created, so `helm list` shows nothing. Limitations:

- Hooks (`helm.sh/hook`) are skipped with a warning.
- `lookup` and cluster-dependent `.Capabilities` are not supported.
- Chart dependencies must be vendored into `charts/` and committed (`helm dependency build`).
- Charts must be in the Git repository; remote and OCI charts are not supported.

See [examples/helm-hello](examples/helm-hello) for a minimal chart.

### 3. Run AntCD

```bash
go run ./cmd/antcd --config config.yaml
```

---

## Make Targets

Run `make help` to list all targets.

| Target | Description |
|---|---|
| `make build` | Build a static `antcd` binary |
| `make run` | Run AntCD (override with `CONFIG=other.yaml`) |
| `make test` | Run unit tests |
| `make lint` | Check `gofmt` and run `go vet` |
| `make fmt` / `make tidy` | Format code / tidy `go.mod` |
| `make docker` | Build the image (override with `IMAGE=` and `VERSION=`) |
| `make lint-chart` | Lint the Helm chart |
| `make cluster-up` / `make cluster-down` | Start / stop the local K3s cluster |
| `make kubeconfig` | Install the local K3s kubeconfig to `~/.kube/config` |

## Production Installation (via Helm)

Install from the GitHub Container Registry, setting repos and targets in a values file:

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

Set a token inline (stored in a chart-managed Secret) or reference an existing Secret:

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

Prefer `tokenSecret` to keep the token out of your values file. When running the binary directly, use `tokenEnv`.

### RBAC

By default the chart binds a ClusterRole with `*` on all resources and verbs to the ServiceAccount (`<release>-sa`), as AntCD can apply any kind and must list and delete them to prune. To narrow it, override `rbac.rules`:

```yaml
rbac:
  rules:
    - apiGroups: ["", "apps"]
      resources: ["namespaces", "configmaps", "services", "deployments"]
      verbs: ["get", "list", "create", "update", "patch", "delete"]
```

Set `rbac.create: false` to bind the ServiceAccount yourself. AntCD also needs `get` and `list` on API discovery, which authenticated users have by default.

## Size

Measured on linux/amd64 with `CGO_ENABLED=0 go build -ldflags="-w -s"`:

| Artifact | Size |
|---|---|
| `antcd` binary | ~42 MB |
| Container image (uncompressed, distroless base) | ~65 MB |

Most of the binary is the Kubernetes and Helm client libraries. Figures vary between releases.

## Triggering Syncs via Webhook

Trigger an immediate sync:

```bash
curl -i -X POST http://<antcd-host>:8080/api/v1/sync \
  -H "Authorization: Bearer <your-webhook-secret>"
```

## Releasing

- Pull requests run CI: lint, tests, build, chart lint and an image build.
- Every push to `main` re-runs CI. If it passes, the image (tagged with the version, `latest` and the short SHA, with provenance and SBOM) and the Helm chart are published, and a `v<version>` GitHub Release is created with the chart attached.
- Before merging, set the same version in `chart/Chart.yaml` (`version` and `appVersion`) and `chart/values.yaml` (`image.tag`). The release fails if they disagree or the version is already published. Released versions are never deleted.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities as described in [SECURITY.md](SECURITY.md).

## License

This project is licensed under the [Mozilla Public License 2.0](LICENSE).
