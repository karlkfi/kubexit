package tombstone

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"sigs.k8s.io/yaml"
)

type Tombstone struct {
	Born     *time.Time `json:",omitempty"`
	Died     *time.Time `json:",omitempty"`
	Ready    bool
	ExitCode *int `json:",omitempty"`
	fileLock sync.Mutex
}

// Write a tombstone file, truncating before writing.
// If the path directories do not exist, they will be created.
func (t *Tombstone) Write(path string) error {
	// one write at a time
	t.fileLock.Lock()
	defer t.fileLock.Unlock()

	err := os.MkdirAll(filepath.Dir(path), os.ModePerm)
	if err != nil {
		return err
	}

	// does not exit
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create tombstone file: %v", err)
	}
	defer file.Close()

	pretty, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("failed to marshal tombstone yaml: %v", err)
	}
	file.Write(pretty)
	return nil
}

// Read a tombstone file.
func Read(path string) (*Tombstone, error) {
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read tombstone file: %v", err)
	}

	t := Tombstone{}
	err = yaml.Unmarshal(bytes, &t)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal tombstone yaml: %v", err)
	}

	return &t, nil
}

type EventHandler func(fsnotify.Event)

// LoggingEventHandler is an example EventHandler that logs fsnotify events
func LoggingEventHandler(event fsnotify.Event) {
	if event.Op&fsnotify.Create == fsnotify.Create {
		log.Printf("Watch: file created: %s\n", event.Name)
	}
	if event.Op&fsnotify.Remove == fsnotify.Remove {
		log.Printf("Watch: file removed: %s\n", event.Name)
	}
	if event.Op&fsnotify.Write == fsnotify.Write {
		log.Printf("Watch: file modified: %s\n", event.Name)
	}
	if event.Op&fsnotify.Rename == fsnotify.Rename {
		log.Printf("Watch: file renamed: %s\n", event.Name)
	}
	if event.Op&fsnotify.Chmod == fsnotify.Chmod {
		log.Printf("Watch: file chmoded: %s\n", event.Name)
	}
}

// Watch a filesystem path and call the eventHandler (asyncronously) when an
// event happens. When the supplied context is canceled, watching will stop.
// If the path is a directory, events will also trigger for immediate children.
func Watch(ctx context.Context, path string, eventHandler EventHandler) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %v", err)
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case <-ctx.Done():
				log.Printf("Watch(%s): done\n", path)
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// TODO: Do these need to be asyncronous?
				go eventHandler(event)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Watch(%s): error: %v\n", path, err)
				// TODO: wrap ctx with WithCancel and cancel on terminal errors, if any
			}
		}
	}()

	err = watcher.Add(path)
	if err != nil {
		return fmt.Errorf("failed to add watcher: %v", err)
	}
	return nil
}
