package edit

import (
	"flag"
	"fmt"
	"image"
	"jmigpin/editor/edit/toolbar"
	"jmigpin/editor/ui"
	"jmigpin/editor/xutil/dragndrop"
	"net/url"
	"strings"

	"github.com/BurntSushi/xgb/xproto"
	"github.com/fsnotify/fsnotify"
)

func Main() {
	_, err := NewEditor()
	if err != nil {
		fmt.Println(err)
	}
}

type Editor struct {
	ui *ui.UI
	fs *FilesState
}

func NewEditor() (*Editor, error) {
	ed := &Editor{}

	ui0, err := ui.NewUI()
	if err != nil {
		return nil, err
	}
	ed.ui = ui0
	ed.ui.OnEvent = ed.onUIEvent

	fs, err := NewFilesState()
	if err != nil {
		return nil, err
	}
	ed.fs = fs
	ed.fs.OnError = ed.Error
	ed.fs.OnEvent = ed.onFSEvent

	// set up layout toolbar
	ta := ed.ui.Layout.Toolbar
	ta.SetText("Exit | ListSessions | NewColumn | NewRow")

	// flags
	flag.Parse()
	args := flag.Args()
	if len(args) > 0 {
		col := ed.ActiveColumn()
		for _, s := range args {
			_, err := openPathAtCol(ed, s, col)
			if err != nil {
				ed.Error(err)
				continue
			}
		}
	}

	//initCatchSignals(ed.onSignal)
	go ed.fs.EventLoop()
	ed.ui.EventLoop()

	return ed, nil
}
func (ed *Editor) Close() {
	ed.fs.Close()
	ed.ui.Close()
}

//func (ed *Editor) onSignal(sig os.Signal) {
//fmt.Printf("signal: %v\n", sig)
//}
func (ed *Editor) Error(err error) {
	row := ed.getSpecialTagRow("Errors")
	ta := row.TextArea
	ta.SetText(ta.Text() + err.Error() + "\n") // append
}
func (ed *Editor) onUIEvent(ev ui.Event) {
	switch ev0 := ev.(type) {
	case error:
		ed.Error(ev0)
	case *ui.TextAreaCmdEvent:
		ed.onTextAreaCmd(ev0)
	case *ui.TextAreaSetTextEvent:
		ed.onTextAreaSetText(ev0)
	case *ui.RowKeyPressEvent:
		ed.onRowKeyPress(ev0)
	case *ui.RowCloseEvent:
		rowCtx.Cancel(ev0.Row)
		ed.updateFileStates()
	case *ui.ColumnDndPositionEvent:
		ed.onColumnDndPosition(ev0)
	case *ui.ColumnDndDropEvent:
		ed.onColumnDndDrop(ev0)
	default:
		fmt.Printf("editor unhandled event: %v\n", ev)
	}
}
func (ed *Editor) onTextAreaCmd(ev *ui.TextAreaCmdEvent) {
	ta := ev.TextArea
	switch t0 := ta.Data.(type) {
	case *ui.Layout:
		ToolbarCmdFromLayout(ed, ta)
	case *ui.Row:
		switch ta {
		case t0.TextArea:
			textCmd(ed, t0)
		case t0.Toolbar:
			ToolbarCmdFromRow(ed, t0)
		}
	}
}
func (ed *Editor) onTextAreaSetText(ev *ui.TextAreaSetTextEvent) {
	ta := ev.TextArea
	switch t0 := ta.Data.(type) {
	case *ui.Row:
		switch ta {
		case t0.TextArea:
			// set as dirty only if it has a filename
			tsd := toolbar.NewStringData(t0.Toolbar.Text())
			_, ok := tsd.FilenameTag()
			if ok {
				t0.Square.SetDirty(true)
			}
		case t0.Toolbar:
			tsd := toolbar.NewStringData(t0.Toolbar.Text())
			_, ok := tsd.FilenameTag()
			if ok {
				ed.updateFileStates()
			}
		}
	}
}
func (ed *Editor) onRowKeyPress(ev *ui.RowKeyPressEvent) {
	fks := ev.Key.FirstKeysym()
	m := ev.Key.Modifiers
	if m.Control() && fks == 's' {
		saveRowFile(ed, ev.Row)
		return
	}
	if m.Control() && m.Shift() && fks == 'f' {
		filemanagerShortcut(ev.Row)
		return
	}
	if m.Control() && fks == 'f' {
		quickFindShortcut(ev.Row)
		return
	}
}
func (ed *Editor) onColumnDndPosition(ev *ui.ColumnDndPositionEvent) {
	// supported types
	ok := false
	types := []xproto.Atom{dragndrop.DropTypeAtoms.TextURLList}
	for _, t := range types {
		if ev.Event.SupportsType(t) {
			ok = true
			break
		}
	}
	if ok {
		// TODO: if ctrl is pressed, set to XdndActionLink
		// reply accept with action
		action := dragndrop.DndAtoms.XdndActionCopy
		ev.Event.ReplyAccept(action)
	}
}
func (ed *Editor) onColumnDndDrop(ev *ui.ColumnDndDropEvent) {
	data, err := ev.Event.RequestData(dragndrop.DropTypeAtoms.TextURLList)
	if err != nil {
		ev.Event.ReplyDeny()
		ed.Error(err)
		return
	}
	urls, err := parseAsTextURLList(data)
	if err != nil {
		ev.Event.ReplyDeny()
		ed.Error(err)
		return
	}
	ed.handleColumnDroppedURLs(ev.Column, ev.Point, urls)
	ev.Event.ReplyAccepted()
}
func parseAsTextURLList(data []byte) ([]*url.URL, error) {
	s := string(data)
	entries := strings.Split(s, "\n")
	var urls []*url.URL
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		u, err := url.Parse(e)
		if err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	return urls, nil
}
func (ed *Editor) handleColumnDroppedURLs(col *ui.Column, p *image.Point, urls []*url.URL) {
	for _, u := range urls {
		if u.Scheme == "file" {
			row, err := openPathAtCol(ed, u.Path, col)
			if err != nil {
				ed.Error(err)
				continue
			}
			col.Cols.MoveRowToPoint(row, p)
		}
	}
}

func (ed *Editor) updateFileStates() {
	var u []string
	for _, c := range ed.ui.Layout.Cols.Cols {
		for _, r := range c.Rows {
			tsd := toolbar.NewStringData(r.Toolbar.Text())
			filename, ok := tsd.FilenameTag()
			if ok {
				u = append(u, filename)
			}
		}
	}
	ed.fs.SetFiles(u)
}
func (ed *Editor) onFSEvent(ev fsnotify.Event) {
	evs := fsnotify.Create | fsnotify.Write | fsnotify.Remove | fsnotify.Rename
	if ev.Op&evs > 0 {
		row, ok := ed.findFilenameRow(ev.Name)
		if ok {
			row.Square.SetCold(true)
		}
		return
	} else if ev.Op&fsnotify.Chmod > 0 {
		return
	}
	ed.Error(fmt.Errorf("unhandled fs event: %v", ev))
	// The window might not have focus, and no expose event will happen
	// Since the fsevents are async, a request is done to ensure a draw
	ed.ui.RequestTreePaint()
}

func (ed *Editor) ActiveRow() (*ui.Row, bool) {
	for _, c := range ed.ui.Layout.Cols.Cols {
		for _, r := range c.Rows {
			if r.Square.Active() {
				return r, true
			}
		}
	}
	return nil, false
}
func (ed *Editor) ActiveColumn() *ui.Column {
	row, ok := ed.ActiveRow()
	if ok {
		return row.Col
	}
	return ed.ui.Layout.Cols.LastColumnOrNew()
}

func (ed *Editor) findFilenameRow(name string) (*ui.Row, bool) {
	cols := ed.ui.Layout.Cols
	for _, c := range cols.Cols {
		for _, r := range c.Rows {
			tsd := toolbar.NewStringData(r.Toolbar.Text())
			s, ok := tsd.FilenameTag()
			if ok && s == name {
				return r, true
			}
		}
	}
	return nil, false
}
func (ed *Editor) findSpecialTagRow(name string) (*ui.Row, bool) {
	cols := ed.ui.Layout.Cols
	for _, c := range cols.Cols {
		for _, r := range c.Rows {
			tsd := toolbar.NewStringData(r.Toolbar.Text())
			s, ok := tsd.SpecialTag()
			if ok && s == name {
				return r, true
			}
		}
	}
	return nil, false
}

func (ed *Editor) getSpecialTagRow(name string) *ui.Row {
	row, ok := ed.findSpecialTagRow(name)
	if ok {
		return row
	}
	// new row
	col := ed.ActiveColumn()
	row = col.NewRow()
	row.Toolbar.SetText("s:" + name)
	return row
}
