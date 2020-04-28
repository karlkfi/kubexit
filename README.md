# kubexit
Command supervisor for coordinated process termination.

## Tombstones

kubexit carves a tombstone at `${KUBEXIT_GRAVEYARD}/${KUBEXIT_NAME}` to mark the birth and death of the process it supervises:

1. When a wrapped app starts, kubexit will write a tombstone with a `Born` timestamp.
1. When a wrapped app exits, kubexit will update the tombstone with a `Died` timestamp and the `ExitCode`.

## Use Case: Romeo and Juliet

With kubexit, you can define **death dependencies** between processes that are wrapped with kubexit and configured with the same graveyard.

```
KUBEXIT_NAME=app1 \
KUBEXIT_GRAVEYARD=/graveyard \
kubexit app1 &

KUBEXIT_NAME=app2 \
KUBEXIT_GRAVEYARD=/graveyard \
KUBEXIT_DEATH_DEPS=app1
kubexit app2 &
```

If `app1` exits before `app2` does, kubexit will detect the tombstone update and send the `TERM` signal to `app2`.

## Use Case: Hercules and Iphicles

With kubexit, you can define **birth dependencies** between processes that are wrapped with kubexit and configured with the same graveyard.

```
KUBEXIT_NAME=app1 \
KUBEXIT_GRAVEYARD=/graveyard \
kubexit app1 &

KUBEXIT_NAME=app2 \
KUBEXIT_GRAVEYARD=/graveyard \
KUBEXIT_BIRTH_DEPS=app1
KUBEXIT_POD_NAME=example-pod
KUBEXIT_NAMESPACE=example-namespace
kubexit app2 &
```

If `kubexit app2` starts before `app1` is ready (according to its Kubernetes readiness probe), kubexit will block the starting of `app2` until `app1` is ready.

## Config

kubexit is configured with environment variables only, to make it easy to configure in Kubernetes and minimize entrypoint/command changes.

Tombstone:
- `KUBEXIT_NAME` - The name of the tombstone file to use. Must match the name of the Kubernetes pod container, if using birth dependency.
- `KUBEXIT_GRAVEYARD` - The file path of the graveyard directory, where tombstones will be read and written.

Death Dependency:
- `KUBEXIT_DEATH_DEPS` - The name(s) of this process death dependencies, comma separated.
- `KUBEXIT_GRACE_PERIOD` - Duration to wait for this process to exit after a graceful termination, before being killed. Default: `30s`.

Birth Dependency:
- `KUBEXIT_BIRTH_DEPS` - The name(s) of this process birth dependencies, comma separated.
- `KUBEXIT_POD_NAME` - The name of the Kubernetes pod that this process and all its siblings are in.
- `KUBEXIT_NAMESPACE` - The name of the Kubernetes namespace that this pod is in.

## Install

**TODO**: Alpine & Ubuntu Docker images for multi-stage builds.

Build from source:

```
go get github.com/karlkfi/kubexit/cmd/kubexit
```

## Kubernetes

One reason to use `kubexit` is that Kuberntes Jobs continue to run as long as any of the containers are running.

If you've ever tried to use [cloudsql-proxy](https://github.com/GoogleCloudPlatform/cloudsql-proxy) as a sidecar in a Kubernetes Job, you may be familiar with these problems:
1. Jobs are only complete when all the containers in the pod have exited. Long-running sidecars need to exit gracefully when a short-running container exits, otherwise the Job pod will stay running forever.
2. When a container depends on another container, the dependency needs to come up first, otherwise the primary will crash on start. The two usual workarounds are to either retry without crashing or crash and let Kubernetes restart the container. One requires modifying the app, and the other can cause slow startup and crash loop backoffs.

With the following Job manifest, `kubexit` can solve both these problems. The `psql` command will wait until the `cloudsql-proxy` command is ready according to its readiness probe. Then the `psql` command will execute and complete, printing its result to the log. When the `psql` command exits, the `cloudsql-proxy` will be gracefully terminated. When both commands are exited, the job will be complete.

```
apiVersion: batch/v1
kind: Job
metadata:
  name: example
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: example
    spec:
      restartPolicy: Never
      volumes:
      - name: graveyard
        emptyDir:
          medium: Memory
      - name: cloudsql
        emptyDir: {}
      - name: service-account-token
        secret:
          secretName: service-account-token
      containers:
      - image: postgresql-client
        name: psql
        command: ['kubexit', 'psql']
        args: ['-c', 'SELECT * FROM foo;']
        env:
        - name: KUBEXIT_NAME
          value: psql
        - name: KUBEXIT_GRAVEYARD
          value: /graveyard
        - name: KUBEXIT_BIRTH_DEPS
          value: cloudsql-proxy
        - name: KUBEXIT_POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: KUBEXIT_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: PGDATABASE
          # ex: postgres://user:pass@localhost:5432/db?sslmode=disable
          valueFrom:
            secretKeyRef:
              name: postgres-database
              key: url
        volumeMounts:
        - mountPath: /graveyard
          name: graveyard
      - image: cloudsql-proxy-kubexit
        name: cloudsql-proxy
        command: ['kubexit', 'cloud_sql_proxy', '-term_timeout=10s']
        lifecycle:
          preStop:
            exec:
              command: ['sleep', '10']
        env:
        - name: INSTANCES
          value: project:region:instance=tcp:localhost:5432
        - name: GOOGLE_APPLICATION_CREDENTIALS
          value: /credentials/credentials.json
        - name: KUBEXIT_NAME
          value: cloudsql-proxy
        - name: KUBEXIT_GRAVEYARD
          value: /graveyard
        - name: KUBEXIT_DEATH_DEPS
          value: psql
        ports:
        - name: proxy
          containerPort: 5432
        volumeMounts:
        - mountPath: /cloudsql
          name: cloudsql
        - mountPath: /credentials
          name: service-account-token
        - mountPath: /graveyard
          name: graveyard
        livenessProbe:
          tcpSocket:
            port: 5432
          initialDelaySeconds: 30
          periodSeconds: 10
          timeoutSeconds: 5
          successThreshold: 1
          failureThreshold: 6
        readinessProbe:
          tcpSocket:
            port: 5432
          initialDelaySeconds: 5
          periodSeconds: 10
          timeoutSeconds: 5
          successThreshold: 1
          failureThreshold: 6
```
