package kubernetes

import (
	"context"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func TestWatchPod_Signature(t *testing.T) {
	t.Skip("requires Kubernetes cluster")

	handler := func(event watch.Event) {
		if pod, ok := event.Object.(*v1.Pod); ok {
			_ = pod.Status.Phase
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := WatchPod(ctx, "default", "nonexistent", handler)
	if err != nil {
		t.Fatalf("WatchPod failed to start: %v", err)
	}

	// Block until context times out — no pod should exist, so no events fired
	<-ctx.Done()
}
