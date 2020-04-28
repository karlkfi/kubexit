# kubexit
Command supervisor for coordinated process termination.

![kubexit Stars](https://img.shields.io/docker/stars/karlkfi/kubexit.svg)
![kubexit Pulls](https://img.shields.io/docker/pulls/karlkfi/kubexit.svg)
![kubexit Automated](https://img.shields.io/docker/automated/karlkfi/kubexit.svg)
![kubexit Build Status](https://img.shields.io/docker/status/karlkfi/kubexit.svg)

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

## Examples

- [Client Server Job](examples/client-server-job/)
- [CloudSQL Proxy Job](examples/cloudsql-proxy-job/)
