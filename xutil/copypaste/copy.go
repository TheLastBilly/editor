package copypaste

import (
	"bytes"
	"encoding/binary"
	"log"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/jmigpin/editor/xutil/xgbutil"
)

// NOTES on other applications
// chromium seems to send an abnormal number of selection requests (also target requests) just to finally settle on what it is being provided
// thunar (or the underlying framework) seems to request immediatly the selection as soon as the selection owner is set - without explicit paste

type Copy struct {
	conn  *xgb.Conn
	win   xproto.Window
	reply chan *xproto.SelectionNotifyEvent
	str   string
}

var CopyAtoms struct {
	UTF8_STRING xproto.Atom
	XSEL_DATA   xproto.Atom
	CLIPBOARD   xproto.Atom
	TARGETS     xproto.Atom
}

func NewCopy(conn *xgb.Conn, win xproto.Window) (*Copy, error) {
	c := &Copy{conn: conn, win: win}
	if err := xgbutil.LoadAtoms(conn, &CopyAtoms); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Copy) Set(str string) {
	c.str = str
	// set at clipboard
	xproto.SetSelectionOwner(
		c.conn,
		c.win,
		CopyAtoms.CLIPBOARD, // selection
		0)
	// set at primary
	xproto.SetSelectionOwner(
		c.conn,
		c.win,
		xproto.AtomPrimary, // selection
		0)
}

// Another application is asking for the data
func (c *Copy) OnSelectionRequest(ev *xproto.SelectionRequestEvent) bool {
	switch ev.Target {
	case CopyAtoms.UTF8_STRING:
		c.transferUTF8String(ev)
		return true
	case CopyAtoms.TARGETS:
		c.transferTargets(ev)
		return true
	default:
		// debug
		s, err := xgbutil.GetAtomName(c.conn, ev.Target)
		if err != nil {
			s = err.Error()
		}
		log.Printf("copy: ignored selection request: asking for type %v (%v)\n", ev.Target, s)
		return false
	}
}
func (c *Copy) transferUTF8String(ev *xproto.SelectionRequestEvent) {
	b := []byte(c.str)
	// change property on the requestor
	xproto.ChangeProperty(
		c.conn,
		xproto.PropModeReplace,
		ev.Requestor, // requestor window
		ev.Property,  // property
		ev.Target,    // type
		8,            // format
		uint32(len(b)),
		b)
	// notify the server
	sne := xproto.SelectionNotifyEvent{
		Requestor: ev.Requestor,
		Selection: ev.Selection,
		Target:    ev.Target, // type
		Property:  ev.Property,
	}
	buf := sne.Bytes()
	_ = xproto.SendEvent(c.conn,
		false,
		sne.Requestor,
		xproto.EventMaskNoEvent,
		string(buf))
}
func (c *Copy) transferTargets(ev *xproto.SelectionRequestEvent) {
	targets := []xproto.Atom{CopyAtoms.UTF8_STRING}

	tbuf := new(bytes.Buffer)
	for _, t := range targets {
		binary.Write(tbuf, binary.LittleEndian, t)
	}
	b := tbuf.Bytes()

	// change property on the requestor
	xproto.ChangeProperty(
		c.conn,
		xproto.PropModeReplace,
		ev.Requestor, // requestor window
		ev.Property,  // property
		ev.Target,    // type
		32,           // format
		uint32(len(targets)),
		b)
	// notify the server
	sne := xproto.SelectionNotifyEvent{
		Requestor: ev.Requestor,
		Selection: ev.Selection,
		Target:    ev.Target, // type
		Property:  ev.Property,
	}
	buf := sne.Bytes()
	_ = xproto.SendEvent(c.conn,
		false,
		sne.Requestor,
		xproto.EventMaskNoEvent,
		string(buf))
}

// Another application now owns the selection.
func (c *Copy) OnSelectionClear(ev *xproto.SelectionClearEvent) {
	c.str = ""
}

// event register support

func (c *Copy) SetupEventRegister(evReg *xgbutil.EventRegister) {
	evReg.Add(xproto.SelectionRequest,
		&xgbutil.ERCallback{func(ev0 xgbutil.EREvent) {
			ev := ev0.(xproto.SelectionRequestEvent)
			ok := c.OnSelectionRequest(&ev)
			_ = ok
		}})
	evReg.Add(xproto.SelectionClear,
		&xgbutil.ERCallback{func(ev0 xgbutil.EREvent) {
			ev := ev0.(xproto.SelectionClearEvent)
			c.OnSelectionClear(&ev)
		}})
}
