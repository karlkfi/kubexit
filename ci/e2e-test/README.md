# E2E Tests

End-to-end tests that deploy kubexit in a real Kubernetes cluster to verify
behavior against the live Kubernetes API.

## Client-Server Test (`client-server/`)

Deploys a two-container Job to validate the core kubexit dependency-wiring:

* **Server** (nginx): Runs kubexit watching the client container (`DEATH_DEPS`).
  If the client exits, kubexit terminates nginx.
* **Client** (curl): Runs kubexit waiting for the server to be ready (`BIRTH_DEPS`),
  then curls `localhost:80`.

Both containers share a graveyard volume so kubexit can write tombstones.

### Running

```bash
cd client-server
./apply-job.sh      # Deploy the job
./await-job.sh      # Wait for completion and show logs
./delete-job.sh     # Clean up
```

The job uses kustomize to manage the kubexit image tag. RBAC files
(`role.yaml`, `role-binding.yaml`, `service-account.yaml`) provide pod watch
permissions in the `default` namespace.
