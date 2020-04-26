package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/karlkfi/kubexit/pkg/supervisor"
	"github.com/karlkfi/kubexit/pkg/tombstone"
)

func main() {
	var err error

	// remove log timestamp
	log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))

	args := os.Args[1:]
	if len(args) == 0 {
		log.Println("No arguments found")
		os.Exit(2)
	}

	name := os.Getenv("KUBEXIT_NAME")
	if name == "" {
		log.Println("Missing env var: KUBEXIT_NAME")
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

	tombstonePath := filepath.Join(graveyard, name)
	log.Printf("Tombstone: %s\n", tombstonePath)

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
			log.Printf("Failed to parse grace period: %v\n", err)
			os.Exit(2)
		}
	}
	log.Printf("Grace Period: %s\n", gracePeriod)

	child := supervisor.New(args[0], args[1:]...)

	log.Printf("Executing: %s\n", child)
	err = child.Start()
	if err != nil {
		log.Printf("Failed to start child process: %v\n", err)
		os.Exit(1)
	}

	born := time.Now()
	ts := &tombstone.Tombstone{
		Born:  &born,
		Ready: false,
	}

	log.Printf("Creating tombstone: %s\n", tombstonePath)
	err = ts.Write(tombstonePath)
	if err != nil {
		fatalf(child, "Failed to create tombstone: %v\n", err)
	}

	// TODO: Update Tombstone Ready

	ctx, stopWatchers := context.WithCancel(context.Background())
	// stop all watchers on supervisor exit
	defer stopWatchers()

	log.Println("Watching graveyard...")
	err = tombstone.Watch(ctx, graveyard,
		newEventHandler(deathDeps, child, gracePeriod, stopWatchers),
	)
	if err != nil {
		fatalf(child, "Failed to watch graveyard: %v\n", err)
	}

	code := wait(child)

	died := time.Now()
	ts.Died = &died
	ts.Ready = false
	ts.ExitCode = &code

	log.Printf("Updating tombstone: %s\n", name)
	err = ts.Write(tombstonePath)
	if err != nil {
		log.Printf("Failed to update tombstone: %v\n", err)
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
		log.Printf("Failed to shutdown child process: %v", err)
		os.Exit(1)
	}

	wait(child)

	os.Exit(1)
}

// newEventHandler returns an EventHandler that shuts down the child process,
// if the specified tombstone has a Died timestamp.
func newEventHandler(deathDeps []string, child *supervisor.Supervisor, gracePeriod time.Duration, stopWatchers context.CancelFunc) tombstone.EventHandler {
	deathDepSet := map[string]struct{}{}
	for _, depName := range deathDeps {
		deathDepSet[depName] = struct{}{}
	}

	return func(event fsnotify.Event) {
		if event.Op&fsnotify.Create != fsnotify.Create && event.Op&fsnotify.Write != fsnotify.Write {
			// ignore other events
			return
		}
		basename := filepath.Base(event.Name)
		log.Printf("File modified: %s\n", basename)
		if _, ok := deathDepSet[basename]; !ok {
			// ignore other tombstones
			return
		}

		log.Printf("Reading tombstone: %s\n", event.Name)
		ts, err := tombstone.Read(event.Name)
		if err != nil {
			log.Printf("Failed to read tombstone: %v\n", err)
			return
		}

		if ts.Died == nil {
			// still alive
			return
		}
		log.Printf("Dead Death Dep: %s\n", basename)

		// stop all watchers
		stopWatchers()
		// trigger graceful shutdown
		err = child.ShutdownWithTimeout(gracePeriod)
		// ShutdownWithTimeout doesn't block until timeout
		if err != nil {
			log.Printf("Failed to shutdown: %v\n", err)
			return
		}
	}
}
