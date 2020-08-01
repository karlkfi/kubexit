package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/karlkfi/kubexit/pkg/kubernetes"
	"github.com/karlkfi/kubexit/pkg/supervisor"
	"github.com/karlkfi/kubexit/pkg/tombstone"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func main() {
	var err error

	// remove log timestamp
	log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))

	args := os.Args[1:]
	if len(args) == 0 {
		log.Println("Error: no arguments found")
		os.Exit(2)
	}

	name := os.Getenv("KUBEXIT_NAME")
	if name == "" {
		log.Println("Error: missing env var: KUBEXIT_NAME")
		os.Exit(2)
	}
	log.Printf("Name: %s\n", name)

	graveyard := os.Getenv("KUBEXIT_GRAVEYARD")
	if graveyard == "" {
		graveyard = "/graveyard"
	} else {
		graveyard = strings.TrimRight(graveyard, "/")
		graveyard = filepath.Clean(graveyard)
	}
	log.Printf("Graveyard: %s\n", graveyard)

	ts := &tombstone.Tombstone{
		Graveyard: graveyard,
		Name:      name,
	}
	log.Printf("Tombstone: %s\n", ts.Path())

	birthDepsStr := os.Getenv("KUBEXIT_BIRTH_DEPS")
	var birthDeps []string
	if birthDepsStr == "" {
		log.Println("Birth Deps: N/A")
	} else {
		birthDeps = strings.Split(birthDepsStr, ",")
		log.Printf("Birth Deps: %s\n", strings.Join(birthDeps, ","))
	}

	deathDepsStr := os.Getenv("KUBEXIT_DEATH_DEPS")
	var deathDeps []string
	if deathDepsStr == "" {
		log.Println("Death Deps: N/A")
	} else {
		deathDeps = strings.Split(deathDepsStr, ",")
		log.Printf("Death Deps: %s\n", strings.Join(deathDeps, ","))
	}

	birthTimeout := 30 * time.Second
	birthTimeoutStr := os.Getenv("KUBEXIT_BIRTH_TIMEOUT")
	if birthTimeoutStr != "" {
		birthTimeout, err = time.ParseDuration(birthTimeoutStr)
		if err != nil {
			log.Printf("Error: failed to parse birth timeout: %v\n", err)
			os.Exit(2)
		}
	}
	log.Printf("Birth Timeout: %s\n", birthTimeout)

	gracePeriod := 30 * time.Second
	gracePeriodStr := os.Getenv("KUBEXIT_GRACE_PERIOD")
	if gracePeriodStr != "" {
		gracePeriod, err = time.ParseDuration(gracePeriodStr)
		if err != nil {
			log.Printf("Error: failed to parse grace period: %v\n", err)
			os.Exit(2)
		}
	}
	log.Printf("Grace Period: %s\n", gracePeriod)

	podName := os.Getenv("KUBEXIT_POD_NAME")
	if podName == "" {
		if len(birthDeps) > 0 {
			log.Println("Error: missing env var: KUBEXIT_POD_NAME")
			os.Exit(2)
		}
		log.Println("Pod Name: N/A")
	} else {
		log.Printf("Pod Name: %s\n", podName)
	}

	namespace := os.Getenv("KUBEXIT_NAMESPACE")
	if namespace == "" {
		if len(birthDeps) > 0 {
			log.Println("Error: missing env var: KUBEXIT_NAMESPACE")
			os.Exit(2)
		}
		log.Println("Namespace: N/A")
	} else {
		log.Printf("Namespace: %s\n", namespace)
	}

	child := supervisor.New(args[0], args[1:]...)

	// watch for death deps early, so they can interrupt waiting for birth deps
	if len(deathDeps) > 0 {
		ctx, stopGraveyardWatcher := context.WithCancel(context.Background())
		// stop graveyard watchers on exit, if not sooner
		defer stopGraveyardWatcher()

		log.Println("Watching graveyard...")
		err = tombstone.Watch(ctx, graveyard, onDeathOfAny(deathDeps, func() {
			stopGraveyardWatcher()
			// trigger graceful shutdown
			// Skipped if not started.
			err := child.ShutdownWithTimeout(gracePeriod)
			// ShutdownWithTimeout doesn't block until timeout
			if err != nil {
				log.Printf("Error: failed to shutdown: %v\n", err)
			}
		}))
		if err != nil {
			fatalf(child, ts, "Error: failed to watch graveyard: %v\n", err)
		}
	}

	if len(birthDeps) > 0 {
		err = waitForBirthDeps(birthDeps, namespace, podName, birthTimeout)
		if err != nil {
			fatalf(child, ts, "Error: %v\n", err)
		}
	}

	err = child.Start()
	if err != nil {
		fatalf(child, ts, "Error: %v\n", err)
	}

	err = ts.RecordBirth()
	if err != nil {
		fatalf(child, ts, "Error: %v\n", err)
	}

	code := waitForChildExit(child)

	err = ts.RecordDeath(code)
	if err != nil {
		log.Printf("Error: %v\n", err)
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

	log.Println("Watching pod updates...")
	err := kubernetes.WatchPod(ctx, namespace, podName,
		onReadyOfAll(birthDeps, stopPodWatcher),
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

	log.Printf("All birth deps ready: %v\n", strings.Join(birthDeps, ", "))
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
				log.Printf("Received shutdown signal: %v", s)
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
		log.Printf("Child Exited(%d): %v\n", code, err)
	} else {
		code = 0
		log.Println("Child Exited(0)")
	}
	return code
}

// fatalf is for terminal errors.
// The child process may or may not be running.
func fatalf(child *supervisor.Supervisor, ts *tombstone.Tombstone, msg string, args ...interface{}) {
	log.Printf(msg, args...)

	// Skipped if not started.
	err := child.ShutdownNow()
	if err != nil {
		log.Printf("Error: failed to shutdown child process: %v", err)
		os.Exit(1)
	}

	// Wait for shutdown...
	//TODO: timout in case the process is zombie?
	code := waitForChildExit(child)

	// Attempt to record death, if possible.
	// Another process may be waiting for it.
	err = ts.RecordDeath(code)
	if err != nil {
		log.Printf("Error: %v\n", err)
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
		fmt.Printf("Event Type: %v\n", event.Type)
		// ignore Deleted (Watch will auto-stop on delete)
		if event.Type == watch.Deleted {
			return
		}

		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			log.Printf("Error: unexpected non-pod object type: %+v\n", event.Object)
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

	return func(event fsnotify.Event) {
		if event.Op&fsnotify.Create != fsnotify.Create && event.Op&fsnotify.Write != fsnotify.Write {
			// ignore other events
			return
		}
		graveyard := filepath.Dir(event.Name)
		name := filepath.Base(event.Name)

		log.Printf("Tombstone modified: %s\n", name)
		if _, ok := deathDepSet[name]; !ok {
			// ignore other tombstones
			return
		}

		log.Printf("Reading tombstone: %s\n", name)
		ts, err := tombstone.Read(graveyard, name)
		if err != nil {
			log.Printf("Error: failed to read tombstone: %v\n", err)
			return
		}

		if ts.Died == nil {
			// still alive
			return
		}
		log.Printf("New death: %s\n", name)
		log.Printf("Tombstone(%s): %s\n", name, ts)

		callback()
	}
}
