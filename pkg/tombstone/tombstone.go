package tombstone

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/karlkfi/kubexit/pkg/log"

	"github.com/fsnotify/fsnotify"
	"sigs.k8s.io/yaml"
)

type Tombstone struct {
	Born     *time.Time `json:",omitempty"`
	Died     *time.Time `json:",omitempty"`
	ExitCode *int       `json:",omitempty"`

	Graveyard string `json:"-"`
	Name      string `json:"-"`

	fileLock sync.Mutex
}

func (t *Tombstone) Path() string {
	return filepath.Join(t.Graveyard, t.Name)
}

// Write a tombstone file, truncating before writing.
// If the FilePath directories do not exist, they will be created.
func (t *Tombstone) Write() error {
	// one write at a time
	t.fileLock.Lock()
	defer t.fileLock.Unlock()

	err := os.MkdirAll(t.Graveyard, os.ModePerm)
	if err != nil {
		return err
	}

	// does not exit
	file, err := os.Create(t.Path())
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

func (t *Tombstone) RecordBirth() error {
	born := time.Now()
	t.Born = &born

	log.Info("Creating tombstone:", "path", t.Path())
	err := t.Write()
	if err != nil {
		return fmt.Errorf("failed to create tombstone: %v", err)
	}
	return nil
}

func (t *Tombstone) RecordDeath(exitCode int) error {
	code := exitCode
	died := time.Now()
	t.Died = &died
	t.ExitCode = &code

	log.Info("Updating tombstone:", "path", t.Path())
	err := t.Write()
	if err != nil {
		return fmt.Errorf("failed to update tombstone: %v", err)
	}
	return nil
}

func (t *Tombstone) String() string {
	inline, err := json.Marshal(t)
	if err != nil {
		log.Error(err, "Error: failed to marshal tombstone as json")
		return "{}"
	}
	return string(inline)
}

// Read a tombstone from a graveyard.
func Read(graveyard, name string) (*Tombstone, error) {
	t := Tombstone{
		Graveyard: graveyard,
		Name:      name,
	}

	bytes, err := ioutil.ReadFile(t.Path())
	if err != nil {
		return nil, fmt.Errorf("failed to read tombstone file: %v", err)
	}

	err = yaml.Unmarshal(bytes, &t)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal tombstone yaml: %v", err)
	}

	return &t, nil
}

type EventHandler func(string, string, fsnotify.Op)

// LoggingEventHandler is an example EventHandler that logs fsnotify events
func LoggingEventHandler(graveyard string, tombstone string, op fsnotify.Op) {
	if op&fsnotify.Create == fsnotify.Create {
		log.Info("Tombstone Watch: file created:", "graveyard", graveyard, "tombstone", tombstone)
	}
	if op&fsnotify.Remove == fsnotify.Remove {
		log.Info("Tombstone Watch: file removed:", "graveyard", graveyard, "tombstone", tombstone)
	}
	if op&fsnotify.Write == fsnotify.Write {
		log.Info("Tombstone Watch: file modified:", "graveyard", graveyard, "tombstone", tombstone)
	}
	if op&fsnotify.Rename == fsnotify.Rename {
		log.Info("Tombstone Watch: file renamed:", "graveyard", graveyard, "tombstone", tombstone)
	}
	if op&fsnotify.Chmod == fsnotify.Chmod {
		log.Info("Tombstone Watch: file chmoded:", "graveyard", graveyard, "tombstone", tombstone)
	}
}

// Watch a graveyard and call the eventHandler (asyncronously) when an
// event happens. When the supplied context is canceled, watching will stop.
func Watch(ctx context.Context, graveyard string, eventHandler EventHandler) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %v", err)
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case <-ctx.Done():
				log.Info("Tombstone Watch: done", "graveyard", graveyard)
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				graveyard := filepath.Dir(event.Name)
				tombstone := filepath.Base(event.Name)
				eventHandler(graveyard, tombstone, event.Op)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error(err, "Tombstone Watch: error", "graveyard", graveyard)
				// TODO: wrap ctx with WithCancel and cancel on terminal errors, if any
			}
		}
	}()

	err = watcher.Add(graveyard)
	if err != nil {
		return fmt.Errorf("failed to add watcher: %v", err)
	}

	// fire initial events after we started watching, this way no events are ever missed
	f, err := os.Open(graveyard)
	if err != nil {
		return fmt.Errorf("failed to watch graveyard: %v", err)
	}

	files, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		return fmt.Errorf("failed to watch for initial tombstones: %v", err)
	}

	for _, file := range files {
		eventHandler(graveyard, file.Name(), 0)
	}
	return nil
}
