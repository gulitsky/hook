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

// HookFunc is a function that performs an operation with a context and may
// return an error.
type HookFunc func(context.Context) error

// Registry manages a collection of HookFunc instances that can be executed
// concurrently.
type Registry struct {
	mu    sync.Mutex
	hooks []HookFunc
}

var (
	defaultOnce     sync.Once
	defaultRegistry *Registry
)

// New creates a new Registry for managing hook functions.
// The registry is initialized with a pre-allocated slice to optimize memory
// usage.
func New() *Registry {
	return &Registry{
		hooks: make([]HookFunc, 0, 10),
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
func (r *Registry) Add(funcs ...HookFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, funcs...)
}

// Clear removes all registered hook functions from the Registry.
// It is safe for concurrent use.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = r.hooks[:0]
}

// Run executes all registered hook functions concurrently with the provided context.
// The hooks remain in the registry after execution, allowing for repeated runs.
//
// The functions are executed in reverse order of registration to support LIFO
// semantics, which is common for resource cleanup (e.g., closing resources
// in the opposite order of their creation).
//
// If the context is already canceled, Run returns the context's error immediately.
// Any errors or panics from the hook functions are collected and returned as a
// single error using errors.Join.
func (r *Registry) Run(ctx context.Context) error {
	r.mu.Lock()
	hooks := make([]HookFunc, len(r.hooks))
	copy(hooks, r.hooks)
	r.mu.Unlock()

	if len(hooks) == 0 {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	var (
		wg      sync.WaitGroup
		errChan = make(chan error, len(hooks))
	)

	wg.Add(len(hooks))

	for i := len(hooks) - 1; i >= 0; i-- {
		go func(fn HookFunc) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errChan <- fmt.Errorf("hook function panic: %v", r)
				}
			}()

			if err := fn(ctx); err != nil {
				errChan <- err
			}
		}(hooks[i])
	}

	wg.Wait()
	close(errChan)

	hookErrs := make([]error, 0, len(hooks))
	for err := range errChan {
		hookErrs = append(hookErrs, err)
	}

	return errors.Join(hookErrs...)
}

// Len returns the number of registered hook functions.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.hooks)
}

// IsEmpty returns true if no hooks are registered.
func (r *Registry) IsEmpty() bool {
	return r.Len() == 0
}
