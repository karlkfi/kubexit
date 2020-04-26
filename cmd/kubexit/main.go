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

	// TODO: Use a single fsnotify watcher for all files?
	for _, depName := range deathDeps {
		depTombstonePath := filepath.Join(graveyard, depName)
		log.Printf("Watching tombstone: %s\n", depTombstonePath)
		err = tombstone.Watch(ctx, depTombstonePath,
			newEventHandler(depTombstonePath, child, gracePeriod, stopWatchers),
		)
		if err != nil {
			fatalf(child, "Failed to watch tombstone: %v\n", err)
		}
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
	err := child.ShutdownNow()
	if err != nil {
		log.Printf(msg, args...)
		os.Exit(1)
	}

	wait(child)

	os.Exit(1)
}

// newEventHandler returns an EventHandler that shuts down the child process,
// if the specified tombstone has a Died timestamp.
func newEventHandler(depTombstonePath string, child *supervisor.Supervisor, gracePeriod time.Duration, stopWatchers context.CancelFunc) tombstone.EventHandler {
	return func(event fsnotify.Event) {
		if event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write {
			log.Printf("File modified: %s\n", event.Name)
			depTombstone, err := tombstone.Read(depTombstonePath)
			if err != nil {
				log.Printf("Failed to read tombstone: %v\n", err)
				return
			}
			if depTombstone.Died == nil {
				// still alive
				return
			}
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
}
