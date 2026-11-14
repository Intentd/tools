package kube

import (
	"context"
	"errors"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/intentd/tools/logger"
)

// EventRecorder emits Kubernetes events. *Events implements it for a real
// cluster; DiscardEvents returns a silent one.
type EventRecorder interface {
	Eventf(obj runtime.Object, eventType, reason, messageFmt string, args ...interface{})
}

// DiscardEvents returns an EventRecorder that drops every event, for tests
// and simulation.
func DiscardEvents() EventRecorder { return discardEvents{} }

type discardEvents struct{}

func (discardEvents) Eventf(runtime.Object, string, string, string, ...interface{}) {}

// Events records Kubernetes events from component through a broadcaster it
// starts and shuts down. Eventf does not block; Shutdown waits for events in
// flight.
type Events struct {
	name        string
	cs          kubernetes.Interface
	scheme      *runtime.Scheme
	broadcaster record.EventBroadcaster
	recorder    record.EventRecorder
	component   string
	stopped     bool
	wg          sync.WaitGroup // events in flight
	mu          sync.Mutex     // guards stopped
	started     bool
}

// NewEvents returns Events for component (the events' source), writing
// through cs and naming objects by scheme.
func NewEvents(cs kubernetes.Interface, scheme *runtime.Scheme, component string) (*Events, error) {
	if scheme == nil {
		return nil, errors.New("scheme cannot be nil")
	}
	return &Events{
		name:      "event handler",
		component: component,
		cs:        cs,
		scheme:    scheme,
	}, nil
}

// Start starts the broadcaster and recorder.
func (e *Events) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.broadcaster = record.NewBroadcaster(record.WithContext(ctx))
	e.broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: e.cs.CoreV1().Events(""),
	})
	e.recorder = e.broadcaster.NewRecorder(e.scheme, corev1.EventSource{Component: e.component})
	e.started = true
	return nil
}

// Eventf records an event in the background; after Shutdown it is dropped.
func (e *Events) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	e.wg.Add(1)
	e.mu.Unlock()

	go func() {
		defer e.wg.Done()
		if e.recorder != nil {
			e.recorder.Eventf(object, eventtype, reason, messageFmt, args...)
		}
	}()
}

// Shutdown stops taking events, waits for those in flight until ctx is done,
// then shuts the broadcaster down.
func (e *Events) Shutdown(ctx context.Context) {
	logger.Info().Msgf("shutting down %s...", e.name)

	e.mu.Lock()
	e.stopped = true
	e.mu.Unlock()

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info().Msg("all events flushed")
	case <-ctx.Done():
		logger.Warn().Msg("timeout waiting for events to flush")
	}

	if e.broadcaster != nil {
		e.broadcaster.Shutdown()
	}
}

// Started reports whether Start has run.
func (e *Events) Started() bool { return e.started }

// Name names the component in logs.
func (e *Events) Name() string { return e.name }

// Broadcaster returns the broadcaster, nil before Start.
func (e *Events) Broadcaster() record.EventBroadcaster { return e.broadcaster }

// Recorder returns the client-go recorder, nil before Start.
func (e *Events) Recorder() record.EventRecorder { return e.recorder }
