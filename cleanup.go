// Package hook provides a mechanism for registering and running functions
// concurrently with a context, typically used for lifecycle management or
// event-driven hooks.
package hook

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// HookFunc is a function that performs an operation with a context and may return an error.
type HookFunc func(context.Context) error

// Registry manages a collection of HookFunc instances that can be executed concurrently.
type Registry struct {
	mu    sync.Mutex
	funcs []HookFunc
}

var (
	defaultOnce     sync.Once
	defaultRegistry *Registry
)

// New creates a new Registry for managing hook functions.
// The registry is initialized with a pre-allocated slice to optimize memory usage.
func New() *Registry {
	return &Registry{
		funcs: make([]HookFunc, 0, 10),
	}
}

// Default returns a singleton Registry instance, creating it if necessary.
// It is safe for concurrent use.
func Default() *Registry {
	defaultOnce.Do(func() {
		defaultRegistry = New()
	})
	return defaultRegistry
}

// Add registers one or more hook functions to the Registry.
// The functions are appended to the internal list and will be executed in reverse order during Run.
func (r *Registry) Add(funcs ...HookFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.funcs = append(r.funcs, funcs...)
}

// Run executes all registered hook functions concurrently with the provided context.
// It clears the registry before execution, checks for context cancellation, and collects any errors.
// If any functions panic or return errors, they are combined and returned as a single error.
// The functions are executed in reverse order of registration to support proper resource cleanup,
// as is common in lifecycle management (e.g., closing resources in the opposite order of initialization).
func (r *Registry) Run(ctx context.Context) error {
	r.mu.Lock()
	funcs := r.funcs
	r.funcs = r.funcs[:0]
	r.mu.Unlock()

	if len(funcs) == 0 {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	var (
		wg      sync.WaitGroup
		errorCh = make(chan error, len(funcs))
	)

	for i := len(funcs) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			errorCh <- err
			break
		}
		fn := funcs[i]
		wg.Add(1)
		go func(fn HookFunc) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCh <- fmt.Errorf("hook function panic: %v", r)
				}
			}()
			if err := fn(ctx); err != nil {
				errorCh <- err
			}
		}(fn)
	}

	wg.Wait()
	close(errorCh)

	var hookErrs []error
	for err := range errorCh {
		if err != nil {
			hookErrs = append(hookErrs, err)
		}
	}

	if len(hookErrs) == 0 {
		return nil
	}
	return errors.Join(hookErrs...)
}
