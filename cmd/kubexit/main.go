package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/karlkfi/kubexit/pkg/kubernetes"
	"github.com/karlkfi/kubexit/pkg/log"
	"github.com/karlkfi/kubexit/pkg/supervisor"
	"github.com/karlkfi/kubexit/pkg/tombstone"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/util/retry"
)

func main() {
	var err error

	args := os.Args[1:]
	if len(args) == 0 {
		log.Error(errors.New("no arguments found"), "Error: no arguments found")
		os.Exit(2)
	}

	name := os.Getenv("KUBEXIT_NAME")
	if name == "" {
		log.Error(errors.New("missing env var: KUBEXIT_NAME"), "Error: missing env var: KUBEXIT_NAME")
		os.Exit(2)
	}
	log.Info("Name:", "name", name)

	graveyard := os.Getenv("KUBEXIT_GRAVEYARD")
	if graveyard == "" {
		graveyard = "/graveyard"
	} else {
		graveyard = strings.TrimRight(graveyard, "/")
		graveyard = filepath.Clean(graveyard)
	}
	log.Info("Graveyard:", "graveyard", graveyard)

	ts := &tombstone.Tombstone{
		Graveyard: graveyard,
		Name:      name,
	}
	log.Info("Tombstone:", "tombstone", ts.Path())

	birthDepsStr := os.Getenv("KUBEXIT_BIRTH_DEPS")
	var birthDeps []string
	if birthDepsStr == "" {
		log.Info("Birth Deps: N/A")
	} else {
		birthDeps = strings.Split(birthDepsStr, ",")
		log.Info("Birth Deps:", "birth deps", strings.Join(birthDeps, ","))
	}

	deathDepsStr := os.Getenv("KUBEXIT_DEATH_DEPS")
	var deathDeps []string
	if deathDepsStr == "" {
		log.Info("Death Deps: N/A")
	} else {
		deathDeps = strings.Split(deathDepsStr, ",")
		log.Info("Death Deps:", "death deps", strings.Join(deathDeps, ","))
	}

	birthTimeout := 30 * time.Second
	birthTimeoutStr := os.Getenv("KUBEXIT_BIRTH_TIMEOUT")
	if birthTimeoutStr != "" {
		birthTimeout, err = time.ParseDuration(birthTimeoutStr)
		if err != nil {
			log.Error(err, "Error: failed to parse birth timeout")
			os.Exit(2)
		}
	}
	log.Info("Birth Timeout:", "birth timeout", birthTimeout)

	gracePeriod := 30 * time.Second
	gracePeriodStr := os.Getenv("KUBEXIT_GRACE_PERIOD")
	if gracePeriodStr != "" {
		gracePeriod, err = time.ParseDuration(gracePeriodStr)
		if err != nil {
			log.Error(err, "Error: failed to parse grace period")
			os.Exit(2)
		}
	}
	log.Info("Grace Period:", "grace period", gracePeriod)

	podName := os.Getenv("KUBEXIT_POD_NAME")
	if podName == "" {
		if len(birthDeps) > 0 {
			log.Error(errors.New("missing env var: KUBEXIT_POD_NAME"), "missing env var", "var_name", "KUBEXIT_POD_NAME")
			os.Exit(2)
		}
		log.Info("Pod Name: N/A")
	} else {
		log.Info("Pod Name:", "pod name", podName)
	}

	namespace := os.Getenv("KUBEXIT_NAMESPACE")
	if namespace == "" {
		if len(birthDeps) > 0 {
			log.Error(errors.New("missing env var: KUBEXIT_NAMESPACE"), "Error: missing env var: KUBEXIT_NAMESPACE")
			os.Exit(2)
		}
		log.Info("Namespace: N/A")
	} else {
		log.Info("Namespace:", "namespace", namespace)
	}

	child := supervisor.New(args[0], args[1:]...)

	// watch for death deps early, so they can interrupt waiting for birth deps
	if len(deathDeps) > 0 {
		ctx, stopGraveyardWatcher := context.WithCancel(context.Background())
		// stop graveyard watchers on exit, if not sooner
		defer stopGraveyardWatcher()

		log.Info("Watching graveyard...")
		err = tombstone.Watch(ctx, graveyard, onDeathOfAny(deathDeps, func() {
			stopGraveyardWatcher()
			// trigger graceful shutdown
			// Skipped if not started.
			err := child.ShutdownWithTimeout(gracePeriod)
			// ShutdownWithTimeout doesn't block until timeout
			if err != nil {
				log.Error(err, "Error: failed to shutdown")
			}
		}))
		if err != nil {
			fatalf(child, ts, err, "Error: failed to watch graveyard")
		}
	}

	if len(birthDeps) > 0 {
		err = waitForBirthDeps(birthDeps, namespace, podName, birthTimeout)
		if err != nil {
			fatalf(child, ts, err, "Error: failed waiting for birth deps")
		}
	}

	err = child.Start()
	if err != nil {
		fatalf(child, ts, err, "Error: failed starting child")
	}

	err = ts.RecordBirth()
	if err != nil {
		fatalf(child, ts, err, "Error: failed recording birth")
	}

	code := waitForChildExit(child)

	err = ts.RecordDeath(code)
	if err != nil {
		log.Error(err, "Error: failed to record death")
		os.Exit(1)
	}

	os.Exit(code)
}

func waitForBirthDeps(birthDeps []string, namespace, podName string, timeout time.Duration) error {
	// Cancel context on SIGTERM to trigger graceful exit
	ctx := withCancelOnSignal(context.Background(), syscall.SIGTERM)

	ctx, stopPodWatcher := context.WithTimeout(ctx, timeout)
	// Stop pod watcher on exit, if not sooner
	defer stopPodWatcher()

	log.Info("Watching pod updates...")
	err := retry.OnError(
		retry.DefaultBackoff,
		isRetryableError,
		func() error {
			watchErr := kubernetes.WatchPod(ctx, namespace, podName,
				onReadyOfAll(birthDeps, stopPodWatcher),
			)
			if watchErr != nil {
				return fmt.Errorf("failed to watch pod: %v", watchErr)
			}
			return nil
		},
	)
	if err != nil {
		return fmt.Errorf("retry watching pods failed: %v", err)
	}

	// Block until all birth deps are ready
	<-ctx.Done()
	err = ctx.Err()
	if err == context.DeadlineExceeded {
		return fmt.Errorf("timed out waiting for birth deps to be ready: %s", timeout)
	} else if err != nil && err != context.Canceled {
		// ignore canceled. shouldn't be other errors, but just in case...
		return fmt.Errorf("waiting for birth deps to be ready: %v", err)
	}

	log.Info("All birth deps ready:", "birth deps", strings.Join(birthDeps, ", "))
	return nil
}

func isRetryableError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

// withCancelOnSignal calls cancel when one of the specified signals is recieved.
func withCancelOnSignal(ctx context.Context, signals ...os.Signal) context.Context {
	ctx, cancel := context.WithCancel(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, signals...)

	// Trigger context cancel on SIGTERM
	go func() {
		for {
			select {
			case s, ok := <-sigCh:
				if !ok {
					return
				}
				log.Info("Received shutdown signal:", "signal", s)
				cancel()
			case <-ctx.Done():
				signal.Reset()
				close(sigCh)
			}
		}
	}()

	return ctx
}

// wait for the child to exit and return the exit code
func waitForChildExit(child *supervisor.Supervisor) int {
	var code int
	err := child.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ProcessState.ExitCode()
		} else {
			code = -1
		}
		log.Info("Child Exited:", "code", code, "err", err)
	} else {
		code = 0
		log.Info("Child Exited(0)")
	}
	return code
}

// fatalf is for terminal errors.
// The child process may or may not be running.
func fatalf(child *supervisor.Supervisor, ts *tombstone.Tombstone, fatalErr error, msg string, args ...interface{}) {
	log.Error(fatalErr, msg, args...)

	// Skipped if not started.
	err := child.ShutdownNow()
	if err != nil {
		log.Error(err, "Error: failed to shutdown child process")
		os.Exit(1)
	}

	// Wait for shutdown...
	//TODO: timout in case the process is zombie?
	code := waitForChildExit(child)

	// Attempt to record death, if possible.
	// Another process may be waiting for it.
	err = ts.RecordDeath(code)
	if err != nil {
		log.Error(err, "Error: failed to record death")
		os.Exit(1)
	}

	os.Exit(1)
}

// onReadyOfAll returns an EventHandler that executes the callback when all of
// the birthDeps containers are ready.
func onReadyOfAll(birthDeps []string, callback func()) kubernetes.EventHandler {
	birthDepSet := map[string]struct{}{}
	for _, depName := range birthDeps {
		birthDepSet[depName] = struct{}{}
	}

	return func(event watch.Event) {
		log.Info("Event Type", "eventType", event.Type)
		// ignore Deleted (Watch will auto-stop on delete)
		if event.Type == watch.Deleted {
			return
		}

		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			log.Error(fmt.Errorf("unexpected non-pod object type: %+v", event.Object), "Error: unexpected non-pod object type")
			return
		}

		// Convert ContainerStatuses list to map of ready container names
		readyContainers := map[string]struct{}{}
		for _, status := range pod.Status.ContainerStatuses {
			if status.Ready {
				readyContainers[status.Name] = struct{}{}
			}
		}

		// Check if all birth deps are ready
		for _, name := range birthDeps {
			if _, ok := readyContainers[name]; !ok {
				// at least one birth dep is not ready
				return
			}
		}

		callback()
	}
}

// onDeathOfAny returns an EventHandler that executes the callback when any of
// the deathDeps processes have died.
func onDeathOfAny(deathDeps []string, callback func()) tombstone.EventHandler {
	deathDepSet := map[string]struct{}{}
	for _, depName := range deathDeps {
		deathDepSet[depName] = struct{}{}
	}

	return func(graveyard string, name string, op fsnotify.Op) {
		if op != 0 && op&fsnotify.Create != fsnotify.Create && op&fsnotify.Write != fsnotify.Write {
			// ignore events other than initial, create and write
			return
		}

		log.Info("Tombstone modified:", "name", name)
		if _, ok := deathDepSet[name]; !ok {
			// ignore other tombstones
			return
		}

		log.Info("Reading tombstone:", "name", name)
		ts, err := tombstone.Read(graveyard, name)
		if err != nil {
			log.Error(err, "Error: failed to read tombstone")
			return
		}

		if ts.Died == nil {
			// still alive
			return
		}
		log.Info("New death:", "name", name)
		log.Info("Tombstone:", "name", name, "tombstone", ts)

		callback()
	}
}
