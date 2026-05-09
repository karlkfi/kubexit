# GitHub Actions Workflows

This directory contains the GitHub Actions workflow definitions used for continuous integration and testing of Kubexit.

## Workflows

### Unit Tests (`unit-test.yaml`)
- **Triggers**: Pushes to `master` branch and all pull requests.
- **Description**: Executes unit tests using `make test-unit`. This ensures that the core logic remains correct with every change.
- **Environment**: `ubuntu-latest`, Go 1.22.0.

### Integration Tests (`integration-test.yaml`)
- **Triggers**: Pushes to `master` branch and pull requests targeting `master`.
- **Description**: Executes integration tests using `make test-integration`. These tests use a `kind` cluster to verify the interaction between kubexit and Kubernetes components in a real environment.
- **Environment**: `ubuntu-latest`, Go 1.22.0, Docker, `kind`.

### End-to-End Tests (`e2e-test.yaml`)
- **Triggers**: Pull requests targeting `master`.
- **Description**: Performs a more comprehensive test suite including:
    - Linting the Go codebase via `make lint`.
    - Building the Kubexit Docker image.
    - Setting up a `kind` cluster.
    - Running end-to-end test scripts located in `ci/e2e-test/`.
- **Environment**: `ubuntu-latest`, Docker, `kind`, `kubectl`, `kustomize`, `jq`.

## CI/CD Pipeline Overview

The CI pipeline is designed to catch regressions early by running a layered testing strategy:
1.  **Unit Tests**: Fast feedback on logic changes.
2.  **Integration Tests**: Verifies behavior within a Kubernetes-like environment (`kind`).
3.  **E2E Tests**: Validates the full deployment and execution flow, including Docker image compatibility and cluster interaction.
