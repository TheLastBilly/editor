package contentcmds

import (
	"context"
	"strings"
	"unicode"

	"github.com/jmigpin/editor/core"
	"github.com/jmigpin/editor/util/iout/iorw"
	"github.com/jmigpin/editor/util/parseutil"
)

func OpenSession(ctx context.Context, erow *core.ERow, index int) (error, bool) {
	ta := erow.Row.TextArea

	// limit reading
	rd := iorw.NewLimitedReaderAtPad(ta.RW(), index, index, 1000)

	sname, err := sessionName(rd, index)
	if err != nil {
		return nil, false
	}

	erow.Ed.UI.RunOnUIGoRoutine(func() {
		core.OpenSessionFromString(erow.Ed, sname)
	})

	return nil, true
}

func sessionName(rd iorw.ReaderAt, index int) (string, error) {
	sc := parseutil.NewScanner()
	sc.SetSrc2(rd)
	sc.Pos = index

	// index at: "OpenSe|ssion sessionname"
	sc.Reverse = true
	_ = sc.M.LoopRuneFn(sessionNameRune)
	sc.Reverse = false

	// index at: "|OpenSession sessionname"
	sname, err := readCmdSessionName(sc)
	if err == nil {
		// found
		return sname, nil
	}

	// index at: "OpenSession |sessionname"
	sc.Reverse = true
	if err := sc.M.Rune(' '); err != nil {
		return "", sc.SrcErrorf("space")
	}
	_ = sc.M.LoopRuneFn(sessionNameRune)
	sc.Reverse = false

	// index at: "|OpenSession sessionname"
	sname, err = readCmdSessionName(sc)
	if err == nil {
		// found
		return sname, nil
	}

	return "", sc.SrcErrorf("not found")
}

func readCmdSessionName(sc *parseutil.Scanner) (string, error) {
	pos0 := sc.KeepPos()
	err := sc.M.RestorePosOnErr(func() error {
		cmd := "OpenSession"
		if err := sc.M.Sequence(cmd + " "); err != nil {
			return sc.SrcErrorf("cmd")
		}
		if err := sc.M.LoopRuneFn(sessionNameRune); err != nil {
			return sc.SrcErrorf("sessionname")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return string(pos0.Bytes()), nil
}

func sessionNameRune(ru rune) bool {
	return unicode.IsLetter(ru) ||
		unicode.IsDigit(ru) ||
		strings.ContainsRune("_-.", ru)
}
