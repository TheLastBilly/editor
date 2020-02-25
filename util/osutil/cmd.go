package osutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"sync"
)

type Cmd struct {
	*exec.Cmd
	ctx         context.Context
	cancelCtx   context.CancelFunc
	setupCalled bool

	PreOutputCallback func()

	NoEnsureStop bool
	ensureStop   struct {
		sync.Mutex
		off bool
	}

	copy struct {
		closers     map[io.Closer]*sync.Once
		closersWait sync.WaitGroup
		fns         []func()
	}
}

// If Start() is not called, Cancel() must be called to clear resources.
func NewCmd(ctx context.Context, args ...string) *Cmd {
	ctx2, cancel := context.WithCancel(ctx)
	c := exec.CommandContext(ctx2, args[0], args[1:]...) // panic on empty args
	cmd := &Cmd{Cmd: c, ctx: ctx2, cancelCtx: cancel}
	cmd.copy.closers = map[io.Closer]*sync.Once{}
	return cmd
}

//----------

// If Start() returns no error, Wait() must be called to clear resources.
func (cmd *Cmd) Start() error {
	if err := cmd.start2(); err != nil {
		cmd.Cancel()
		return err
	}
	return nil
}

func (cmd *Cmd) start2() error {
	if cmd.Cmd.SysProcAttr == nil {
		SetupExecCmdSysProcAttr(cmd.Cmd)
	}

	if err := cmd.Cmd.Start(); err != nil {
		return err
	}

	// Ensure callback is called before the first stdout/stderr write (works since the process takes longer to launch and write back, but in theory this could be called after some output comes out). Always works if stdin/out/err was setup with SetupStdInOutErr() since the copy loop starts after the callback.
	if cmd.PreOutputCallback != nil {
		cmd.PreOutputCallback()
	}

	cmd.runCopyFns()

	go func() {
		select {
		case <-cmd.ctx.Done():
			cmd.closeCopyClosers()
			cmd.ensureStopNow()
		}
	}()

	return nil
}

func (cmd *Cmd) Wait() error {
	// Explanations on possible hangs.
	// https://github.com/golang/go/issues/18874#issuecomment-277280139

	defer func() {
		cmd.disableEnsureStop() // no need to kill process anymore
		cmd.cancelCtx()
	}()
	cmd.copy.closersWait.Wait()
	return cmd.Cmd.Wait()
}

func (cmd *Cmd) Run() error {
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Wait()
}

func (cmd *Cmd) Cancel() {
	cmd.closeCopyClosers()
	cmd.cancelCtx()
}

//----------

func (cmd *Cmd) disableEnsureStop() {
	cmd.ensureStop.Lock()
	defer cmd.ensureStop.Unlock()
	cmd.ensureStop.off = true
}

func (cmd *Cmd) ensureStopNow() {
	cmd.ensureStop.Lock()
	defer cmd.ensureStop.Unlock()
	if !cmd.ensureStop.off {
		cmd.ensureStop.off = true
		if !cmd.NoEnsureStop {
			if err := KillExecCmd(cmd.Cmd); err != nil {
				// ignoring error: just best effort to stop process
			}
		}
	}
}

//----------

func (cmd *Cmd) SetupStdio(ir io.Reader, ow, ew io.Writer) error {
	err := cmd.setupStdio2(ir, ow, ew)
	if err != nil {
		cmd.closeCopyClosers()
	}
	return err
}

func (cmd *Cmd) setupStdio2(ir io.Reader, ow, ew io.Writer) error {
	// setup only once (don't allow f(w, nil) and later f(nil, w)
	if cmd.setupCalled {
		return fmt.Errorf("setup already called")
	}
	cmd.setupCalled = true

	// setup stdin
	if ir != nil {
		ipwc, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		cmd.addCopyCloser(ipwc)
		cmd.copy.fns = append(cmd.copy.fns, func() {
			defer cmd.closeCopyCloser(ipwc)
			io.Copy(ipwc, ir)
		})
	}

	// setup stdout
	if ow != nil {
		oprc, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		cmd.addCopyCloser(oprc)
		cmd.copy.fns = append(cmd.copy.fns, func() {
			defer cmd.closeCopyCloser(oprc)
			io.Copy(ow, oprc)
		})
		// setup stderr if the same
		//if ew == ow { // can panic
		//if interfaceEqual(ew, ow) { // can panic
		if reflect.DeepEqual(ew, ow) {
			cmd.Stderr = cmd.Stdout // set in StdoutPipe() call
			return nil              // early exit, stderr set
		}
	}

	// setup stderr
	if ew != nil {
		eprc, err := cmd.StderrPipe()
		if err != nil {
			return err
		}
		cmd.addCopyCloser(eprc)
		cmd.copy.fns = append(cmd.copy.fns, func() {
			defer cmd.closeCopyCloser(eprc)
			io.Copy(ew, eprc)
		})
	}
	return nil
}

//// from: os/exec/exec.go:218
//func interfaceEqual(a, b interface{}) bool {
//	defer func() {
//		recover()
//	}()
//	return a == b
//}

//----------

func (cmd *Cmd) runCopyFns() {
	for _, fn := range cmd.copy.fns {
		// go fn() // will call the same fn twice (loop var)
		go func(fn2 func()) {
			fn2()
		}(fn)
	}
}

//----------

func (cmd *Cmd) closeCopyClosers() {
	for c := range cmd.copy.closers {
		cmd.closeCopyCloser(c)
	}
}
func (cmd *Cmd) addCopyCloser(c io.Closer) {
	_, ok := cmd.copy.closers[c]
	if !ok {
		cmd.copy.closersWait.Add(1)
		cmd.copy.closers[c] = &sync.Once{}
	}
}
func (cmd *Cmd) closeCopyCloser(c io.Closer) {
	once, ok := cmd.copy.closers[c]
	if ok {
		once.Do(func() {
			defer cmd.copy.closersWait.Done()
			c.Close()
		})
	}
}

//----------

func RunCmdCombinedOutput(cmd *Cmd, rd io.Reader) ([]byte, error) {
	obuf := &bytes.Buffer{}
	if err := cmd.SetupStdio(rd, obuf, obuf); err != nil {
		return nil, err
	}
	err := cmd.Run()
	return obuf.Bytes(), err
}

func RunCmdOutputs(cmd *Cmd, rd io.Reader) (sout []byte, serr []byte, _ error) {
	obuf := &bytes.Buffer{}
	ebuf := &bytes.Buffer{}
	if err := cmd.SetupStdio(rd, obuf, ebuf); err != nil {
		return nil, nil, err
	}
	err := cmd.Run()
	return obuf.Bytes(), ebuf.Bytes(), err
}

// Adds stderr to err if it happens.
func RunCmdStdoutAndStderrInErr(cmd *Cmd, rd io.Reader) ([]byte, error) {
	bout, berr, err := RunCmdOutputs(cmd, rd)
	if err != nil {
		serr := strings.TrimSpace(string(berr))
		if serr != "" {
			err = fmt.Errorf("%w: stderr(%v)", err, serr)
		}
		return nil, err
	}
	return bout, nil
}

//----------
