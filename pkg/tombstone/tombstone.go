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

	"github.com/fsnotify/fsnotify"
	"github.com/karlkfi/kubexit/pkg/log"
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

func (t *Tombstone) RecordBirth(ctx context.Context) error {
	born := time.Now()
	t.Born = &born

	log.G(ctx).
		WithField("tombstone", t.Path()).
		Info("creating tombstone...")
	err := t.Write()
	if err != nil {
		return fmt.Errorf("failed to create tombstone: %v", err)
	}
	return nil
}

func (t *Tombstone) RecordDeath(ctx context.Context, exitCode int) error {
	code := exitCode
	died := time.Now()
	t.Died = &died
	t.ExitCode = &code

	log.G(ctx).
		WithField("tombstone", t.Path()).
		Info("updating tombstone...")
	err := t.Write()
	if err != nil {
		return fmt.Errorf("failed to update tombstone: %v", err)
	}
	return nil
}

func (t *Tombstone) String() string {
	inline, err := json.Marshal(t)
	if err != nil {
		log.L.Errorf("failed to marshal tombstone as json: %v", err)
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

type EventHandler func(context.Context, fsnotify.Event) error

// LoggingEventHandler is an example EventHandler that logs fsnotify events
func LoggingEventHandler(ctx context.Context, event fsnotify.Event) error {
	log.G(ctx).WithField("event_name", event.Name).Info("recieved tombstone watch event")
	return nil
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
				log.G(ctx).
					WithField("graveyard", graveyard).
					Info("tombstone watcher stopped")
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				err := eventHandler(ctx, event)
				if err != nil {
					log.G(ctx).
						WithField("event_name", event.Name).
						WithField("graveyard", graveyard).
						Warn(fmt.Errorf("error handling file system event: %v", err))
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.G(ctx).
					WithField("graveyard", graveyard).
					Warn(fmt.Errorf("error handling file system event: %v", err))
				// TODO: wrap ctx with WithCancel and cancel on terminal errors, if any
			}
		}
	}()

	err = watcher.Add(graveyard)
	if err != nil {
		return fmt.Errorf("failed to add watcher: %v", err)
	}

	files, err := ioutil.ReadDir(graveyard)
	if err != nil {
		return fmt.Errorf("failed to read graveyard dir: %v", err)
	}

	for _, f := range files {
		event := fsnotify.Event{
			Name: filepath.Join(graveyard, f.Name()),
			Op:   fsnotify.Create,
		}
		err = eventHandler(ctx, event)
		if err != nil {
			return fmt.Errorf("failed handling existing tombstone: %v", err)
		}
	}

	return nil
}
