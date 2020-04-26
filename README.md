# kubexit
Command supervisor for coordinated process termination.

## Tombstones

kubexit carves a tombstone at `${KUBEXIT_GRAVEYARD}/${KUBEXIT_NAME}` to mark the birth and death of the process it supervises:

1. When a wrapped app starts, kubexit will write a tombstone with a `Born` timestamp.
1. When a wrapped app exits, kubexit will update the tombstone with a `Died` timestamp and the `ExitCode`.

## Use Case: Romeo and Juliet

With kubexit, you can define **death dependencies** between processes that are wrapped with kubexit and configured with the same `KUBEXIT_GRAVEYARD`.

```
KUBEXIT_NAME=app1 \
KUBEXIT_GRAVEYARD=/lifecycle/ \
kubexit app1 &

KUBEXIT_NAME=app2 \
KUBEXIT_GRAVEYARD=/lifecycle/ \
KUBEXIT_DEATH_DEPS=app1
kubexit app2 &
```

If `app1` exits before `app2` does, kubexit will detect the tombstone update and send the `TERM` signal to `app2`.

## Use Case: Hercules and Iphicles

**TODO**: Birth Dependencies and Readiness Probes are not yet implemented.

With kubexit, you can define **birth dependencies** between processes that are wrapped with kubexit and configured with the same `KUBEXIT_PATH`.

```
KUBEXIT_NAME=app1 \
KUBEXIT_GRAVEYARD=/lifecycle/ \
kubexit app1 &

KUBEXIT_NAME=app2 \
KUBEXIT_GRAVEYARD=/lifecycle/ \
KUBEXIT_BIRTH_DEPS=app1
kubexit app2 &
```

If `kubexit app2` starts before `app1` is ready, kubexit will block the starting of `app2` until `app1` is ready.

## Install

**TODO**: Alpine & Ubuntu Docker images for multi-stage builds.

Build from source:

```
go get github.com/karlkfi/kubexit/cmd/kubexit
```

## Kubernetes

One reason to use `kubexit` is that Kuberntes Jobs continue to run as long as any of the containers are running.

If you've ever tried to use [cloudsql-proxy](https://github.com/GoogleCloudPlatform/cloudsql-proxy) as a sidecar in a Job, you may be familiar with this problem.

With the following Job manifest, the `main` container will sleep for a minute and then exit, leaving a tombstone in the graveyard.

The `kubexit` in the `cloudsql-proxy` should then see the `/graveyard/main` tombstone update, see the death timestamp, and trigger graceful shutdown of `cloudsql-proxy` with SIGTERM.

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
      - image: alpine
        name: main
        command: ['kubexit', 'sleep', '60']
        env:
        - name: KUBEXIT_NAME
          value: main
        - name: KUBEXIT_GRAVEYARD
          value: /graveyard
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
          value: project:region:instance=tcp:0.0.0.0:5432
        - name: GOOGLE_APPLICATION_CREDENTIALS
          value: /credentials/credentials.json
        - name: KUBEXIT_NAME
          value: cloudsql-proxy
        - name: KUBEXIT_GRAVEYARD
          value: /graveyard
        - name: KUBEXIT_DEATH_DEPS
          value: main
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
```
