# test-server

Test HTTP server for validating kubexit container exit behavior.

## Purpose

A minimal HTTP server used to test how kubexit handles process signals and graceful shutdown. Run standalone while attached to a kubexit graveyard to verify death dependency handling.

## Endpoints

| Endpoint | Method | Behavior |
|---|---|---|
| `/health` | GET | Returns `200 OK` – readiness probe |
| `/exit` | POST | Returns `200 OK` then triggers clean shutdown (exit 0) |

## Usage

All commands run from the project root (the workspace includes `cmd/test-server` as a module via `go.work`):

```bash
# Run directly
go run ./cmd/test-server

# Build a binary
go build -o test-server ./cmd/test-server
./test-server
```

## Docker

Build the container from the project root:

```bash
docker build -f cmd/test-server/Dockerfile -t kubexit/test-server:latest .
```

Run with a custom port:

```bash
docker run -p 8080:8080 -e PORT=8080 kubexit/test-server:latest
```

## Signal Handling

The server responds to `SIGTERM` and `SIGINT` by initiating a graceful shutdown — draining connections and exiting with code 0. This allows it to be used as a drop-in primary container when testing kubexit death dependencies.

Send a POST to `/exit` to simulate the primary container completing its work and signaling kubexit to shut down.
