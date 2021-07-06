# kubexit

Command supervisor for coordinated Kubernetes pod container termination.

![kubexit dockerhub stars](https://img.shields.io/docker/stars/karlkfi/kubexit.svg?link=https://hub.docker.com/repository/docker/karlkfi/kubexit)
![kubexit dockerhub pulls](https://img.shields.io/docker/pulls/karlkfi/kubexit.svg?link=https://hub.docker.com/repository/docker/karlkfi/kubexit)
![kubexit license](https://img.shields.io/github/license/karlkfi/kubexit.svg?link=https://github.com/karlkfi/kubexit/blob/master/LICENSE)
![kubexit latest release](https://img.shields.io/github/v/release/karlkfi/kubexit.svg?include_prereleases&sort=semver&link=https://github.com/karlkfi/kubexit/releases)

## Use Cases

Kubernetes supports multiple containers in a pod, but there is no current feature to manage dependency ordering, so all the containers (other than init containers) start at the same time. This can cause a number of issues with certain configurations, some of which kubexit is designed to mitigate.

1. Kubernetes jobs run until all containers have exited. If a sidecar container is supporting a primary container, the sidecar needs to be gracefully terminated after the primary container has exited, before the job will end. Kubexit mitigates this with death dependencies.
2. Sidecar proxies (e.g. Istio, CloudSQL Proxy) are often designed to handle network traffic to and from a pod's primary container. But if the primary container tries to make egress call or recieve ingress calls before the sidecar proxy is up and ready, those calls may fail. Kubexit mitigates this with birth dependencies.

## Tombstones

kubexit automatically carves (writes to disk) a tombstone (`${KUBEXIT_GRAVEYARD}/${KUBEXIT_NAME}`) to mark the birth and death of the process it supervises:

1. When a wrapped app starts, kubexit will write a tombstone with a `Born` timestamp.
1. When a wrapped app exits, kubexit will update the tombstone with a `Died` timestamp and the `ExitCode`.

These tombstones are written to the graveyard, a folder on the local file system. In Kubernetes, an in-memory volume can be used to share the graveyard between containers in a pod. By watching the file system inodes in the graveyard, kubexit will know when the other containers in the pod start and stop.

Tombstone Content:

```
Born: <timestamp>
Died: <timestamp>
ExitCode: <int>
```

## Birth Dependencies

With kubexit, you can define birth dependencies between processes that are wrapped with kubexit and configured with the same graveyard.

Unlike death dependencies, birth dependencies only work within a Kubernetes pod, because kubexit watches pod container readiness, rather than implementing its own readiness checks.

Kubexit will block the execution of the dependent container process (ex: a stateless webapp) until the dependency container (ex: a sidecar proxy) is ready.

The primary use case for this feature is Kubernetes sidecar proxies, where the proxy needs to come up before the primary container process, otherwise the primary process egress calls will fail unitl the proxy is up.

## Death Dependencies

With kubexit, you can define death dependencies between processes that are wrapped with kubexit and configured with the same graveyard.

If the dependency process (ex: a stateless webapp) exits before the dependent process (ex: a sidecar proxy), kubexit will detect the tombstone update (`Died: <timestamp>`) and send the `TERM` signal to the dependent process.

The primary use case for this feature is Kubernetes Jobs, where a sidecar container needs to be gracefully shutdown when the primary container exits, otherwise the Job will never complete.

## Config

kubexit is configured with environment variables only, to make it easy to configure in Kubernetes and minimize entrypoint/command changes.

General:
- `KUBEXIT_LOG_LEVEL` - The log level for logged messages. Default: `info`.

Tombstone:
- `KUBEXIT_NAME` - The name of the tombstone file to use. Must match the name of the Kubernetes pod container, if using birth dependency.
- `KUBEXIT_GRAVEYARD` - The file path of the graveyard directory, where tombstones will be read and written.

Death Dependency:
- `KUBEXIT_DEATH_DEPS` - The name(s) of this process death dependencies, comma separated.
- `KUBEXIT_GRACE_PERIOD` - Duration to wait for this process to exit after a graceful termination, before being killed. Default: `30s`.

Birth Dependency:
- `KUBEXIT_BIRTH_DEPS` - The name(s) of this process birth dependencies, comma separated.
- `KUBEXIT_BIRTH_TIMEOUT` - Duration to wait for all birth dependencies to be ready. Default: `30s`.
- `KUBEXIT_POD_NAME` - The name of the Kubernetes pod that this process and all its siblings are in.
- `KUBEXIT_NAMESPACE` - The name of the Kubernetes namespace that this pod is in.

## Install

While kubexit can easily be installed on your local machine, the primary use cases require execution within Kubernetes pod containers. So the recommended method of installation is to either side-load kubexit using a shared volume and an init container, or build kubexit into your own container images.

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
