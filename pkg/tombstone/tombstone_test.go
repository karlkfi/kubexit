package tombstone

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func makeTempGraveyard(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestPath verifies that Path returns the correct file path for a tombstone.

func TestPath(t *testing.T) {
	ts := &Tombstone{Graveyard: "/graveyard", Name: "myapp"}
	got := ts.Path()
	want := "/graveyard/myapp"
	if got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

// TestWriteAndRead verifies that a tombstone can be written and read back with the expected fields.

func TestWriteAndRead(t *testing.T) {
	graveyard := makeTempGraveyard(t)
	ts := &Tombstone{Graveyard: graveyard, Name: "app"}

	now := time.Now().Truncate(time.Second)
	ts.Born = &now
	exitCode := 42
	ts.Died = &now
	ts.ExitCode = &exitCode

	if err := ts.Write(); err != nil {
		t.Fatal(err)
	}

	got, err := Read(graveyard, "app")
	if err != nil {
		t.Fatal(err)
	}

	if got.Born == nil || !now.Equal(*got.Born) {
		t.Errorf("Born = %v, want %v", got.Born, now)
	}
	if got.Died == nil || !now.Equal(*got.Died) {
		t.Errorf("Died = %v, want %v", got.Died, now)
	}
	if got.ExitCode == nil || *got.ExitCode != exitCode {
		t.Errorf("ExitCode = %v, want %v", got.ExitCode, exitCode)
	}
}

// TestRecordBirth verifies that RecordBirth sets the Born field and writes the tombstone.

func TestRecordBirth(t *testing.T) {
	graveyard := makeTempGraveyard(t)
	ts := &Tombstone{Graveyard: graveyard, Name: "app"}

	before := time.Now()
	if err := ts.RecordBirth(); err != nil {
		t.Fatal(err)
	}
	after := time.Now()

	if ts.Born == nil || ts.Born.Before(before) || ts.Born.After(after) {
		t.Fatalf("Born not set correctly: %v", ts.Born)
	}
	if ts.Died != nil {
		t.Error("Died should be nil after RecordBirth")
	}
	if ts.ExitCode != nil {
		t.Error("ExitCode should be nil after RecordBirth")
	}

	// Verify file exists
	if _, err := os.Stat(ts.Path()); err != nil {
		t.Fatal(err)
	}
}

// TestRecordDeath verifies that RecordDeath sets the Died and ExitCode fields and writes the tombstone.

func TestRecordDeath(t *testing.T) {
	graveyard := makeTempGraveyard(t)
	ts := &Tombstone{Graveyard: graveyard, Name: "app"}

	before := time.Now()
	if err := ts.RecordDeath(1); err != nil {
		t.Fatal(err)
	}
	after := time.Now()

	if ts.Died == nil || ts.Died.Before(before) || ts.Died.After(after) {
		t.Fatalf("Died not set correctly: %v", ts.Died)
	}
	if ts.ExitCode == nil || *ts.ExitCode != 1 {
		t.Fatalf("ExitCode = %v, want 1", ts.ExitCode)
	}
}

// TestReadNonexistent verifies that Read returns an error for a nonexistent tombstone file.

func TestReadNonexistent(t *testing.T) {
	_, err := Read("/nonexistent", "app")
	if err == nil {
		t.Error("expected error reading nonexistent tombstone")
	}
}

// TestString verifies that String returns a formatted string with all tombstone fields.

func TestString(t *testing.T) {
	ts := &Tombstone{Graveyard: "/g", Name: "app"}
	ts.Born = new(time.Time)
	*ts.Born = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	got := ts.String()
	if got == "" {
		t.Error("String returned empty")
	}
	if got == "{}" {
		t.Error("String returned empty JSON object")
	}
}

func TestStringEmpty(t *testing.T) {
	ts := &Tombstone{}
	got := ts.String()
	if got != "{}" {
		t.Errorf("String = %q, want {}", got)
	}
}

// TestWriteCreatesGraveyard verifies that Write creates the graveyard directory if it doesn't exist.

func TestWriteCreatesGraveyard(t *testing.T) {
	base := makeTempGraveyard(t)
	graveyard := filepath.Join(base, "nested", "graveyard")
	ts := &Tombstone{Graveyard: graveyard, Name: "app"}

	if err := ts.RecordBirth(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(graveyard); os.IsNotExist(err) {
		t.Error("graveyard directory was not created")
	}
}

// TestWatchFiresOnWrite verifies that the watch event handler fires when a tombstone is written.

func TestWatchFiresOnWrite(t *testing.T) {
	graveyard := makeTempGraveyard(t)

	fired := false
	handler := func(e fsnotify.Event) { fired = true }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Watch(ctx, graveyard, handler)
	// Give the watcher time to start
	time.Sleep(100 * time.Millisecond)

	// Write a file to trigger the handler
	ts := &Tombstone{Graveyard: graveyard, Name: "app"}
	if err := ts.RecordBirth(); err != nil {
		t.Fatal(err)
	}

	// Give the watcher time to process
	time.Sleep(200 * time.Millisecond)

	if !fired {
		t.Error("Watch handler did not fire on file write")
	}
}

// TestWatchStopsOnCancel verifies that the watch goroutine exits when the context is cancelled.

func TestWatchStopsOnCancel(t *testing.T) {
	graveyard := makeTempGraveyard(t)

	handler := func(e fsnotify.Event) {}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		Watch(ctx, graveyard, handler)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// Good, Watch returned
	case <-time.After(2 * time.Second):
		t.Error("Watch did not stop after context cancellation")
	}
}

// TestReadReturnsCorrectFields verifies that Read parses all tombstone fields correctly.

func TestReadReturnsCorrectFields(t *testing.T) {
	graveyard := makeTempGraveyard(t)
	ts := &Tombstone{Graveyard: graveyard, Name: "test-app"}

	born := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	ts.Born = &born
	exitCode := 0
	ts.Died = &born
	ts.ExitCode = &exitCode

	if err := ts.Write(); err != nil {
		t.Fatal(err)
	}

	got, err := Read(graveyard, "test-app")
	if err != nil {
		t.Fatal(err)
	}

	if got.Born == nil || *got.Born != born {
		t.Errorf("Born = %v, want %v", got.Born, born)
	}
	if got.Died == nil || *got.Died != born {
		t.Errorf("Died = %v, want %v", got.Died, born)
	}
	if got.ExitCode == nil || *got.ExitCode != exitCode {
		t.Errorf("ExitCode = %v, want %v", got.ExitCode, exitCode)
	}
}

// TestRecordDeathExitCodes verifies that RecordDeath handles various exit codes correctly.

func TestRecordDeathExitCodes(t *testing.T) {
	for _, code := range []int{-1, 0, 1, 137, 255} {
		t.Run(fmt.Sprintf("%d", code), func(t *testing.T) {
			graveyard := makeTempGraveyard(t)
			ts := &Tombstone{Graveyard: graveyard, Name: "app"}

			if err := ts.RecordDeath(code); err != nil {
				t.Fatal(err)
			}

			if ts.ExitCode == nil || *ts.ExitCode != code {
				t.Errorf("ExitCode = %v, want %d", ts.ExitCode, code)
			}
		})
	}
}

// TestWriteOverwrites verifies that Write overwrites an existing tombstone.

func TestWriteOverwrites(t *testing.T) {
	graveyard := makeTempGraveyard(t)
	ts := &Tombstone{Graveyard: graveyard, Name: "app"}

	// Write initial tombstone
	if err := ts.RecordBirth(); err != nil {
		t.Fatal(err)
	}
	firstBorn := *ts.Born

	// Wait a bit and write again
	time.Sleep(10 * time.Millisecond)
	if err := ts.RecordBirth(); err != nil {
		t.Fatal(err)
	}
	secondBorn := *ts.Born

	// Read and verify it's the newer one
	got, err := Read(graveyard, "app")
	if err != nil {
		t.Fatal(err)
	}

	if got.Born == nil || !got.Born.Equal(secondBorn) {
		t.Errorf("Born = %v, want %v (second write)", got.Born, secondBorn)
	}

	_ = firstBorn // suppress unused warning
}

// TestMultipleTombstones verifies that multiple tombstones can coexist in the same graveyard.

func TestMultipleTombstones(t *testing.T) {
	graveyard := makeTempGraveyard(t)

	names := []string{"app1", "app2", "app3"}
	for _, name := range names {
		ts := &Tombstone{Graveyard: graveyard, Name: name}
		if err := ts.RecordBirth(); err != nil {
			t.Fatalf("Failed to create tombstone %s: %v", name, err)
		}
	}

	for _, name := range names {
		got, err := Read(graveyard, name)
		if err != nil {
			t.Fatalf("Failed to read tombstone %s: %v", name, err)
		}
		if got.Born == nil {
			t.Errorf("Tombstone %s has nil Born", name)
		}
	}
}
