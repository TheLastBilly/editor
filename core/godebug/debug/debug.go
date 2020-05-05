package debug

import (
	"fmt"
	"os"
	"sync"
)

var server *Server
var startServerMu sync.Mutex

// Called by the generated config.
func StartServer() {
	hotStartServer()
}

func hotStartServer() {
	if server == nil {
		startServerMu.Lock()
		if server == nil {
			startServer()
		}
		startServerMu.Unlock()
	}
}

func startServer() {
	srv, err := NewServer()
	if err != nil {
		fmt.Printf("error: godebug/debug: start server: %v\n", err)
		os.Exit(1)
	}
	server = srv
}

//----------

// Auto-inserted at main for a clean exit. Not to be used.
func ExitServer() {
	if server != nil {
		server.Close()
	}
}

//----------

// Auto-inserted at annotations. Not to be used.
func Line(fileIndex, debugIndex, offset int, item Item) {
	hotStartServer()
	lmsg := &LineMsg{FileIndex: fileIndex, DebugIndex: debugIndex, Offset: offset, Item: item}
	server.Send(lmsg)
}
