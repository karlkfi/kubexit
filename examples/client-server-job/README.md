# Example: Clinet Server Job

This example has 3 containers:
- `kubexit` - An init container that builds kubexit and stores it in an ephemeral volume for the other containers to use.
- `client` - An alpine container that installs curl, sleeps until the server is ready (birth dependency), curls the server, and then exits.
- `server` - An nginx container that sleeps (to exemplify slow startup), and then starts serving traffic. When the client exits, the server will exit (death dependency).

See [job.yaml](job.yaml)

```
kubectl apply -f examples/client-server-job/job.yaml

POD_NAME="$(kubectl get pod -l app=example-job --no-headers=true -o=custom-columns='DATA:metadata.name')"

kubectl logs "${POD_NAME}" -f -c client 2>&1 | tee examples/client-server-job/client.log
kubectl logs "${POD_NAME}" -f -c server 2>&1 | tee examples/client-server-job/server.log

kubectl delete -f examples/client-server-job/job.yaml
```
