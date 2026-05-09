# Contributing to Kubexit

Thank you for your interest in contributing! Below are guidelines to help you get started and ensure a smooth review process.

## Table of Contents

- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Coding Guidelines](#coding-guidelines)
- [Pull Requests](#pull-requests)
- [Testing](#testing)
- [Building](#building)
- [Docker Images](#docker-images)
- [Reporting Bugs](#reporting-bugs)
- [Asking Questions](#asking-questions)

## Getting Started

1. **Fork** the repository on GitHub.
2. **Clone** your fork locally:
   ```bash
   git clone https://github.com/<your-username>/kubexit.git
   cd kubexit
   ```
3. **Add the upstream remote** to stay in sync:
   ```bash
   git remote add upstream https://github.com/karlkfi/kubexit.git
   ```
4. **Sync the workspace**:
   ```bash
   go work sync && go mod download
   ```
   This project uses a Go workspace (`go.work`) with three modules: root (`pkg/`), `cmd/kubexit`, and `cmd/test-server`. The `go work sync` command keeps replace directives in sync across all modules.

## Development Workflow

1. **Create a feature branch** from `master`:
   ```bash
   git fetch upstream
   git checkout -b feature/your-feature-name upstream/master
   ```
2. **Make your changes** following the coding guidelines below.
3. **Run the linters and tests** before committing:
   ```bash
   make lint
   make test
   ```
   Both commands run across all modules in the workspace via `./...`.
4. **Commit** with a clear, descriptive message (see [Commit Messages](#commit-messages)).
5. **Push** and open a pull request against `upstream/master`.

## Coding Guidelines

### Go

- Format all code with `gofmt` (run `make fix` for auto-formatting including `goimports`).
- Run `go vet ./...` to catch common issues.
- Write a doc comment on every exported function, type, and package.
- Keep comments on private functions when the intent is non-obvious.
- Add tests for new functionality and updates when behavior changes. See [Testing](#testing).
- Don't add new dependencies unless necessary. If you must, prefer direct dependencies over transitive ones and note the rationale in the PR description.

### Commit Messages

- Use the imperative mood: "Add logging" not "Added logging".
- Keep the subject line under 72 characters.
- Include a body paragraph for non-trivial changes that explains the _why_, not the _what_.
- Reference related issues using `#<number>` when applicable.

### Pull Requests

- Keep PRs focused — one feature or fix per PR.
- Include a clear description of the problem and the approach.
- Reference any related issues.
- Ensure CI passes (unit tests, linting, and e2e tests) before requesting review.
- Respond to review comments promptly; push follow-up commits instead of amending (preserves review history).

## Testing

### Unit Tests

```bash
make test
```

Unit tests run via Go's built-in test framework and are executed on every push and pull request through CI.

### End-to-End Tests

E2E tests deploy kubexit in a real Kubernetes cluster to verify behavior against the live Kubernetes API. They run against a [Kind](https://kind.sigs.k8s.io/) cluster in CI.

To run them locally (requires Docker, `kind`, and `kubectl` installed):

```bash
# Start a Kind cluster
kind create cluster --name kubexit

# Build the kubexit image and load it into the cluster
docker build -t kubexit:latest .
kind load docker-image kubexit:latest --name kubexit

# Run the tests
bash ci/e2e-test/client-server/apply-job.sh
bash ci/e2e-test/client-server/await-job.sh
bash ci/e2e-test/client-server/delete-job.sh

# Tear down the cluster
kind delete cluster --name kubexit
```

See [ci/e2e-test/README.md](ci/e2e-test/README.md) for details.

### Linting

```bash
make lint
```

This runs `golangci-lint` (requires [golangci-lint](https://golangci-lint.run/) installed locally; CI handles this automatically).

### Building

```bash
make bin
```

### Docker Images

There are two Docker images in the project:

| Image | Dockerfile | Purpose |
|-------|------------|---------|
| `kubexit` | `Dockerfile` | Main application binary |
| `kubexit/test-server` | `cmd/test-server/Dockerfile` | Test HTTP server for integration tests |

Both use a multi-stage build: a `golang:1.26-alpine` builder stage compiles the binary, and an `alpine:3.23` runtime stage ships the final artifact.

Build locally with:

```bash
# Main image
docker build -t kubexit:latest .

# Test server image
docker build -f cmd/test-server/Dockerfile -t kubexit/test-server:latest .
```

To load images into a Kind cluster for local testing:

```bash
kind load docker-image kubexit:latest --name kubexit
kind load docker-image kubexit/test-server:latest --name kubexit
```

## Reporting Bugs

Open an issue on GitHub with:

1. A clear title and description of the expected vs. actual behavior.
2. Steps to reproduce.
3. The kubexit version and Kubernetes cluster version.
4. Any relevant logs or configuration.

## Asking Questions

- Open a [GitHub Issue](https://github.com/karlkfi/kubexit/issues) for general questions or feature ideas.
- For proposed changes, sketch the approach in an issue before implementing to avoid wasted effort.
