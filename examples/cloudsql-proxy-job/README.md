# CloudSQL Proxy Sidecar

One reason to use `kubexit` is that Kuberntes Jobs continue to run as long as any of the containers are running.

If you've ever tried to use [cloudsql-proxy](https://github.com/GoogleCloudPlatform/cloudsql-proxy) as a sidecar in a Kubernetes Job, you may be familiar with these problems:
1. Jobs are only complete when all the containers in the pod have exited. Long-running sidecars need to exit gracefully when a short-running container exits, otherwise the Job pod will stay running forever.
2. When a container depends on another container, the dependency needs to come up first, otherwise the primary will crash on start. The two usual workarounds are to either retry without crashing or crash and let Kubernetes restart the container. One requires modifying the app, and the other can cause slow startup and crash loop backoffs.

With the following Job manifest, `kubexit` can solve both these problems. The `psql` command will wait until the `cloudsql-proxy` command is ready according to its readiness probe. Then the `psql` command will execute and complete, printing its result to the log. When the `psql` command exits, the `cloudsql-proxy` will be gracefully terminated. When both commands are exited, the job will be complete.

See [job.yaml](job.yaml)

**Note**: This example job manifest does not work as-is. You will need to configure a CloudSQL instance, database, credentials, and secrets. The container images specified are also not real images. You'll need to build your own.
