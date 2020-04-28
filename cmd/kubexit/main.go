package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	if len(birthDeps) > 0 {
		// TODO: max start delay timeout
		ctx, stopPodWatcher := context.WithCancel(context.Background())
		// stop pod watcher on exit, if not sooner
		defer stopPodWatcher()

		log.Println("Watching pod updates...")
		err = kubernetes.WatchPod(ctx, namespace, podName,
			newPodEventHandler(birthDeps, stopPodWatcher),
		)
		if err != nil {
			log.Printf("Error: failed to watch pod: %v\n", err)
			os.Exit(1)
		}

		// Block until all birth deps are ready
		// TODO: start delay timeout?
		<-ctx.Done()
		log.Printf("All birth deps ready: %v\n", strings.Join(birthDeps, ", "))
	}

	child := supervisor.New(args[0], args[1:]...)

	log.Printf("Exec: %s\n", child)
	err = child.Start()
	if err != nil {
		log.Printf("Error: failed to start child process: %v\n", err)
		os.Exit(1)
	}

	born := time.Now()
	ts.Born = &born

	log.Printf("Creating tombstone: %s\n", ts.Path())
	err = ts.Write()
	if err != nil {
		fatalf(child, "Error: failed to create tombstone: %v\n", err)
	}

	if len(deathDeps) > 0 {
		ctx, stopGraveyardWatcher := context.WithCancel(context.Background())
		// stop graveyard watchers on exit, if not sooner
		defer stopGraveyardWatcher()

		log.Println("Watching graveyard...")
		err = tombstone.Watch(ctx, graveyard,
			newFSEventHandler(deathDeps, child, stopGraveyardWatcher, gracePeriod),
		)
		if err != nil {
			fatalf(child, "Error: failed to watch graveyard: %v\n", err)
		}
	}

	code := wait(child)

	died := time.Now()
	ts.Died = &died
	ts.ExitCode = &code

	log.Printf("Updating tombstone: %s\n", ts.Path())
	err = ts.Write()
	if err != nil {
		log.Printf("Error: failed to update tombstone: %v\n", err)
		os.Exit(1)
	}

	os.Exit(code)
}

// wait for the child to exit and return the exit code
func wait(child *supervisor.Supervisor) int {
	var code int
	err := child.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ProcessState.ExitCode()
		} else {
			code = -1
		}
		log.Printf("Exit(%d): %v\n", code, err)
	} else {
		code = 0
		log.Println("Exit(0)")
	}
	return code
}

// fatalf is for terminal errors while the child process is running.
func fatalf(child *supervisor.Supervisor, msg string, args ...interface{}) {
	log.Printf(msg, args...)
	err := child.ShutdownNow()
	if err != nil {
		log.Printf("Error: failed to shutdown child process: %v", err)
		os.Exit(1)
	}

	wait(child)

	os.Exit(1)
}

func newPodEventHandler(birthDeps []string, stopPodWatcher context.CancelFunc) kubernetes.EventHandler {
	birthDepSet := map[string]struct{}{}
	for _, depName := range birthDeps {
		birthDepSet[depName] = struct{}{}
	}

	return func(event watch.Event) {
		fmt.Printf("Event Type: %v\n", event.Type)
		// ignore Added & Deleted (Watch will auto-stop on delete)
		if event.Type != watch.Modified {
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

		// stop watching and unblock delayed start
		stopPodWatcher()
	}
}

// newFSEventHandler returns an EventHandler that shuts down the child process,
// if the specified tombstone has a Died timestamp.
func newFSEventHandler(deathDeps []string, child *supervisor.Supervisor, stopGraveyardWatcher context.CancelFunc, gracePeriod time.Duration) tombstone.EventHandler {
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

		// TODO: handle multiple deathDeps atomically

		stopGraveyardWatcher()
		// trigger graceful shutdown
		err = child.ShutdownWithTimeout(gracePeriod)
		// ShutdownWithTimeout doesn't block until timeout
		if err != nil {
			log.Printf("Error: failed to shutdown: %v\n", err)
			return
		}
	}
}
