package debug

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync"
)

var dsrv struct { // debug server
	sync.Mutex
	srv    *Server
	exited bool // prevent from being hot started again
}

//----------

// Called by the generated config.
func StartServer() {
	hotStartServer()
}
func hotStartServer() {
	if dsrv.srv == nil {
		dsrv.Lock()
		if dsrv.srv == nil && !dsrv.exited {
			startServer2()
		}
		dsrv.Unlock()
	}
}
func startServer2() {
	srv, err := NewServer()
	if err != nil {
		fmt.Printf("error: godebug/debug: start server failed: %v\n", err)
		os.Exit(1)
	}
	dsrv.srv = srv
}

//----------

// Auto-inserted at main for a clean exit. Not to be used.
func ExitServer() {
	dsrv.Lock()
	if !dsrv.exited && dsrv.srv != nil {
		dsrv.srv.Close()
	}
	dsrv.exited = true
	dsrv.Unlock()

	if !hasSrcLines {
		if r := recover(); r != nil {
			// use std msg format
			println(fmt.Sprintf("panic: %v\n", r))

			println("GODEBUG WARNING: code not compiled with src lines references. Trace locations refer to annotated files. Consider using -srclines flag.\n")

			println(string(debug.Stack()))

			os.Exit(2) // default panic seems to exit with code 2 as well
		}
	}
}

// Auto-inserted in annotated files to replace os.Exit calls. Not to be used.
func Exit(code int) {
	ExitServer()
	os.Exit(code)
}

//----------

// Auto-inserted at annotations. Not to be used.
func Line(fileIndex, debugIndex, offset int, item Item) {
	hotStartServer()
	lmsg := &LineMsg{FileIndex: fileIndex, DebugIndex: debugIndex, Offset: offset, Item: item}
	dsrv.srv.Send(lmsg)
}
