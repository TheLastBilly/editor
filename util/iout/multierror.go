package iout

import (
	"fmt"
	"strings"
	"sync"
)

type MultiError struct {
	errors []error
	addMu  sync.Mutex
}

// Returns an error (MultiError) or nil if the errors added were all nil.
func MultiErrors(errs ...error) error {
	me := &MultiError{}
	for _, e := range errs {
		me.Add(e)
	}
	return me.Result()
}

// Returns itself, or nil if it has no errors.
func (me *MultiError) Result() error {
	if len(me.errors) == 0 {
		return nil
	}
	return me
}

// Can be used concurrently.
func (me *MultiError) Add(err error) {
	if err != nil {
		me.addMu.Lock()
		me.errors = append(me.errors, err)
		me.addMu.Unlock()
	}
}

func (me *MultiError) Error() string {
	if len(me.errors) == 1 {
		return me.errors[0].Error()
	}
	u := []string{}
	for i, e := range me.errors {
		u = append(u, fmt.Sprintf("err%d: %v", i+1, e.Error()))
	}
	return fmt.Sprintf("multierror(%d){%s}", len(me.errors), strings.Join(u, ", "))
}
