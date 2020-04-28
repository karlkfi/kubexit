# kubexit
Command supervisor for coordinated process termination.

![kubexit dockerhub stars](https://img.shields.io/docker/stars/karlkfi/kubexit.svg?link=https://hub.docker.com/repository/docker/karlkfi/kubexit)
![kubexit dockerhub pulls](https://img.shields.io/docker/pulls/karlkfi/kubexit.svg?link=https://hub.docker.com/repository/docker/karlkfi/kubexit)
![kubexit license](https://img.shields.io/github/license/karlkfi/kubexit.svg?link=https://github.com/karlkfi/kubexit/blob/master/LICENSE)
![kubexit latest release](https://img.shields.io/github/v/release/karlkfi/kubexit.svg?include_prereleases&sort=semver&link=https://github.com/karlkfi/kubexit/releases)

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

Build from source:

```
go get github.com/karlkfi/kubexit/cmd/kubexit
```

Copy from pre-built Alpine-based container image in a multi-stage build:

```
FROM karlkfi/kubexit:latest AS kubexit

FROM alpine:3.11
RUN apk --no-cache add ca-certificates tzdata
COPY --from=kubexit /bin/kubexit /bin/
ENTRYPOINT ["kubexit"]
```

Copy from init container to ephemeral volume:

```
volumes:
- name: kubexit
  emptyDir: {}

initContainers:
- name: kubexit
  image: karlkfi/kubexit:latest
  command: ['cp', '/bin/kubexit', '/kubexit/kubexit']
  volumeMounts:
  - mountPath: /kubexit
    name: kubexit
```

## Examples

- [Client Server Job](examples/client-server-job/)
- [CloudSQL Proxy Job](examples/cloudsql-proxy-job/)
