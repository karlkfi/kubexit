# kubexit
Command wrapper for coordinated Kubernetes pod shutdown.

## Tombstones

kubexit carves a tombstone at `${KUBEXIT_PATH}/${KUBEXIT_NAME}` to mark the birth and death of the process it supervises:

1. When a wrapped app starts, kubexit will write a tombstone with a `Born` timestamp.
1. When a wrapped app exits, kubexit will update the tombstone with a `Died` timestamp and the `ExitCode`.

## Use Case: Romeo and Juliet

With kubexit, you can define **death dependencies** between processes that are wrapped with kubexit and configured with the same `KUBEXIT_PATH`.

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
