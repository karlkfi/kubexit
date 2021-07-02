package main

import (
	"context"
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
)

func main() {
	var err error

	ctx := log.WithLogger(context.Background(), log.L)

	args := os.Args[1:]
	if len(args) == 0 {
		log.G(ctx).Error("no arguments found")
		os.Exit(2)
	}

	name := os.Getenv("KUBEXIT_NAME")
	if name == "" {
		log.G(ctx).Error("missing env var: KUBEXIT_NAME")
		os.Exit(2)
	}

	// add field to the context logger to differentiate when pod container logs are intermingled
	ctx = log.WithLogger(ctx, log.G(ctx).WithField("container_name", name))
	log.G(ctx).Info("KUBEXIT_NAME parsed")

	graveyard := os.Getenv("KUBEXIT_GRAVEYARD")
	if graveyard == "" {
		graveyard = "/graveyard"
	} else {
		graveyard = strings.TrimRight(graveyard, "/")
		graveyard = filepath.Clean(graveyard)
	}
	ts := &tombstone.Tombstone{
		Graveyard: graveyard,
		Name:      name,
	}
	log.G(ctx).
		WithField("graveyard", graveyard).
		WithField("tombstone", ts.Path()).
		Info("KUBEXIT_GRAVEYARD parsed")

	birthDepsStr := os.Getenv("KUBEXIT_BIRTH_DEPS")
	var birthDeps []string
	if birthDepsStr != "" {
		birthDeps = strings.Split(birthDepsStr, ",")
	}
	log.G(ctx).WithField("birth_deps", birthDeps).Info("KUBEXIT_BIRTH_DEPS parsed")

	deathDepsStr := os.Getenv("KUBEXIT_DEATH_DEPS")
	var deathDeps []string
	if deathDepsStr != "" {
		deathDeps = strings.Split(deathDepsStr, ",")
	}
	log.G(ctx).WithField("death_deps", deathDeps).Info("KUBEXIT_DEATH_DEPS parsed")

	birthTimeout := 30 * time.Second
	birthTimeoutStr := os.Getenv("KUBEXIT_BIRTH_TIMEOUT")
	if birthTimeoutStr != "" {
		birthTimeout, err = time.ParseDuration(birthTimeoutStr)
		if err != nil {
			log.G(ctx).Errorf("failed to parse birth timeout: %v", err)
			os.Exit(2)
		}
	}
	log.G(ctx).WithField("birth_timeout", birthTimeout).Info("KUBEXIT_BIRTH_TIMEOUT parsed")

	gracePeriod := 30 * time.Second
	gracePeriodStr := os.Getenv("KUBEXIT_GRACE_PERIOD")
	if gracePeriodStr != "" {
		gracePeriod, err = time.ParseDuration(gracePeriodStr)
		if err != nil {
			log.G(ctx).Errorf("failed to parse grace period: %v", err)
			os.Exit(2)
		}
	}
	log.G(ctx).WithField("grace_period", gracePeriod).Info("KUBEXIT_GRACE_PERIOD parsed")

	podName := os.Getenv("KUBEXIT_POD_NAME")
	if podName == "" {
		if len(birthDeps) > 0 {
			log.G(ctx).Error("missing env var: KUBEXIT_POD_NAME")
			os.Exit(2)
		}
	}
	log.G(ctx).WithField("pod_name", podName).Info("KUBEXIT_POD_NAME parsed")

	namespace := os.Getenv("KUBEXIT_NAMESPACE")
	if namespace == "" {
		if len(birthDeps) > 0 {
			log.G(ctx).Error("missing env var: KUBEXIT_NAMESPACE")
			os.Exit(2)
		}
	}
	log.G(ctx).WithField("namespace", namespace).Info("KUBEXIT_POD_NAME parsed")

	child := supervisor.New(ctx, args[0], args[1:]...)

	// watch for death deps early, so they can interrupt waiting for birth deps
	if len(deathDeps) > 0 {
		ctx, stopGraveyardWatcher := context.WithCancel(ctx)
		// stop graveyard watchers on exit, if not sooner
		defer stopGraveyardWatcher()

		log.G(ctx).Info("Watching graveyard...")
		err = tombstone.Watch(ctx, graveyard, onDeathOfAny(deathDeps, func() error {
			stopGraveyardWatcher()
			// trigger graceful shutdown
			// Error & exit if not started.
			// ShutdownWithTimeout doesn't block until timeout
			err := child.ShutdownWithTimeout(gracePeriod)
			if err != nil {
				return fmt.Errorf("failed to shutdown: %v", err)
			}
			return nil
		}))
		if err != nil {
			fatal(ctx, child, ts, fmt.Errorf("failed to watch graveyard: %v", err))
		}
	}

	if len(birthDeps) > 0 {
		err = waitForBirthDeps(ctx, birthDeps, namespace, podName, birthTimeout)
		if err != nil {
			fatal(ctx, child, ts, err)
		}
	}

	err = child.Start()
	if err != nil {
		fatal(ctx, child, ts, err)
	}

	err = ts.RecordBirth(ctx)
	if err != nil {
		fatal(ctx, child, ts, err)
	}

	code := waitForChildExit(ctx, child)

	err = ts.RecordDeath(ctx, code)
	if err != nil {
		log.G(ctx).Error(err)
		os.Exit(1)
	}

	os.Exit(code)
}

func waitForBirthDeps(ctx context.Context, birthDeps []string, namespace, podName string, timeout time.Duration) error {
	// Cancel context on SIGTERM to trigger graceful exit
	ctx = withCancelOnSignal(ctx, syscall.SIGTERM)

	ctx, stopPodWatcher := context.WithTimeout(ctx, timeout)
	// Stop pod watcher on exit, if not sooner
	defer stopPodWatcher()

	log.G(ctx).Info("watching pod updates...")
	err := kubernetes.WatchPod(ctx, namespace, podName,
		onReadyOfAll(ctx, birthDeps, stopPodWatcher),
	)
	if err != nil {
		return fmt.Errorf("failed to watch pod: %v", err)
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

	log.G(ctx).WithField("birth_deps", birthDeps).Info("all birth deps ready")
	return nil
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
				log.G(ctx).WithField("signal", s).Info("received shutdown signal")
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
func waitForChildExit(ctx context.Context, child *supervisor.Supervisor) int {
	var code int
	err := child.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ProcessState.ExitCode()
		} else {
			code = -1
		}
	} else {
		code = 0
	}
	log.G(ctx).
		WithField("exit_code", code).
		WithField("error", err).
		Info("child exited")
	return code
}

// fatal logs a terminal error and exits.
// The child process may or may not be running.
func fatal(ctx context.Context, child *supervisor.Supervisor, ts *tombstone.Tombstone, err error) {
	log.G(ctx).Error(err)

	// Skipped if not started.
	err = child.ShutdownNow()
	if err != nil {
		log.G(ctx).Errorf("failed to shutdown child process: %v", err)
		os.Exit(1)
	}

	// Wait for shutdown...
	//TODO: timout in case the process is zombie?
	code := waitForChildExit(ctx, child)

	// Attempt to record death, if possible.
	// Another process may be waiting for it.
	err = ts.RecordDeath(ctx, code)
	if err != nil {
		log.G(ctx).Errorf("failed to record death of child process: %v", err)
		os.Exit(1)
	}

	os.Exit(1)
}

// onReadyOfAll returns an EventHandler that executes the callback when all of
// the birthDeps containers are ready.
func onReadyOfAll(ctx context.Context, birthDeps []string, callback func()) kubernetes.EventHandler {
	birthDepSet := map[string]struct{}{}
	for _, depName := range birthDeps {
		birthDepSet[depName] = struct{}{}
	}

	return func(event watch.Event) {
		log.G(ctx).WithField("event_type", event.Type).Info("recieved pod watch event")
		// ignore Deleted (Watch will auto-stop on delete)
		if event.Type == watch.Deleted {
			return
		}

		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			log.G(ctx).WithField("object", event.Object).Warn("recieved unexpected non-pod object type")
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
func onDeathOfAny(deathDeps []string, callback func() error) tombstone.EventHandler {
	deathDepSet := map[string]struct{}{}
	for _, depName := range deathDeps {
		deathDepSet[depName] = struct{}{}
	}

	return func(ctx context.Context, event fsnotify.Event) error {
		if event.Op&fsnotify.Create != fsnotify.Create && event.Op&fsnotify.Write != fsnotify.Write {
			// ignore other events
			return nil
		}
		graveyard := filepath.Dir(event.Name)
		name := filepath.Base(event.Name)

		log.G(ctx).WithField("tombstone", name).Info("recieved tombstone watch event")
		if _, ok := deathDepSet[name]; !ok {
			// ignore other tombstones
			return nil
		}

		ts, err := tombstone.Read(graveyard, name)
		if err != nil {
			log.G(ctx).WithField("tombstone", name).Errorf("failed to read tombstone: %v", err)
			return nil
		}

		if ts.Died == nil {
			// still alive
			return nil
		}
		log.G(ctx).
			WithField("tombstone", name).
			WithField("tombstone_content", ts).
			Errorf("recieved new death event")

		return callback()
	}
}
