package shutdown

import (
	"context"
	"errors"
	"sync"
)

type ShutdownFunc func(context.Context) error

type Shutdowner interface {
	Add(funcs ...ShutdownFunc)
	Len() int
	Clear()
	Shutdown(context.Context) error
}

type shutdowner struct {
	mu    sync.Mutex
	funcs []ShutdownFunc
}

var (
	once sync.Once
	inst *shutdowner
)

func New() Shutdowner {
	once.Do(func() {
		inst = &shutdowner{}
	})

	return inst
}

func (s *shutdowner) Add(funcs ...ShutdownFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.funcs = append(s.funcs, funcs...)
}

func (s *shutdowner) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.funcs)
}

func (s *shutdowner) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.funcs = nil
}

func (s *shutdowner) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.funcs) == 0 {
		return nil
	}

	var (
		wg   sync.WaitGroup
		errs = make(chan error, len(s.funcs))
	)

	for i := len(s.funcs) - 1; i >= 0; i-- {
		wg.Add(1)
		go func(fn ShutdownFunc) {
			defer wg.Done()
			if err := fn(ctx); err != nil {
				errs <- err
			}
		}(s.funcs[i])
	}

	go func() {
		wg.Wait()
		close(errs)
	}()

	var shutdownErrs []error
	for {
		select {
		case err, ok := <-errs:
			if !ok {
				s.funcs = nil
				return errors.Join(shutdownErrs...)
			}
			if err != nil {
				shutdownErrs = append(shutdownErrs, err)
			}
		case <-ctx.Done():
			return errors.Join(append(shutdownErrs, ctx.Err())...)
		}
	}
}
