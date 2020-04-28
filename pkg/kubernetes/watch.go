package kubernetes

import (
	"context"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
)

type EventHandler func(watch.Event)

// Watch a pod and call the eventHandler (asyncronously) when an
// event happens. When the supplied context is canceled, watching will stop.
func WatchPod(ctx context.Context, namespace, podName string, eventHandler EventHandler) error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to configure kubernetes client: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %v", err)
	}

	// Watch doesn't take name matches, only selectors. So select on name.
	fieldSelector := fields.OneTermEqualSelector("metadata.name", podName).String()

	// UntilWithSync takes this crazy compound input to List and then Watch.
	// These functions add our FieldSelector to the requests.
	// UntilWithSync uses the List to get the current resource version, because
	// Watch requires an initial resource version to start at, and the resource
	// version needs to still be in the etcd event history cache.
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (object runtime.Object, e error) {
			options.FieldSelector = fieldSelector
			return clientset.CoreV1().Pods(namespace).List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (i watch.Interface, e error) {
			options.FieldSelector = fieldSelector
			return clientset.CoreV1().Pods(namespace).Watch(ctx, options)
		},
	}

	go func() {
		ctx, cancel := context.WithCancel(ctx)
		// cancel the provided context when done, so that caller can block on it
		defer cancel()

		// watch until deleted
		_, err := watchtools.UntilWithSync(ctx, lw, &corev1.Pod{}, nil, func(event watch.Event) (bool, error) {
			if event.Type == watch.Error {
				log.Printf("Pod Watch(%s): recoverable error: %+v\n", podName, event.Object)
				return false, nil
			}

			eventHandler(event)

			if event.Type == watch.Deleted {
				log.Printf("Pod Watch(%s): pod deleted\n", podName)
				return true, nil
			}
			return false, nil
		})
		// ErrWaitTimeout is returned when the context is canceled.
		// Since cancellation is the only way we exit, just ignore it.
		if err != nil && err != wait.ErrWaitTimeout {
			// TODO: should we do something about this??
			log.Printf("Pod Watch(%s): terminal error: %v\n", podName, err)
		}
		log.Printf("Pod Watch(%s): done\n", podName)
	}()

	return nil
}
