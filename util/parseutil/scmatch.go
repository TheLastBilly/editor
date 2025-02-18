package parseutil

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"unicode"
)

// scanner match utility funcs
type ScMatch struct {
	sc    *Scanner
	P     *ScParse
	cache struct {
		regexps map[string]*regexp.Regexp
	}
}

func (m *ScMatch) init(sc *Scanner) {
	m.sc = sc
	m.P = &sc.P
	m.cache.regexps = map[string]*regexp.Regexp{}
}

//----------

func (m *ScMatch) Eof() bool {
	pos0 := m.sc.KeepPos()
	_, err := m.sc.ReadRune()
	if err == nil {
		pos0.Restore()
		return false
	}
	return err == io.EOF
}

//----------

func (m *ScMatch) Rune(ru rune) error {
	return m.sc.RestorePosOnErr(func() error {
		ru2, err := m.sc.ReadRune()
		if err != nil {
			return err
		}
		if ru2 != ru {
			return NoMatchErr
		}
		return nil
	})
}
func (m *ScMatch) RuneAny(rs []rune) error { // "or", any of the runes
	return m.sc.RestorePosOnErr(func() error {
		ru, err := m.sc.ReadRune()
		if err != nil {
			return err
		}
		if !ContainsRune(rs, ru) {
			return NoMatchErr
		}
		return nil
	})
}
func (m *ScMatch) RuneAnyNot(rs []rune) error { // "or", any of the runes
	return m.sc.RestorePosOnErr(func() error {
		ru, err := m.sc.ReadRune()
		if err != nil {
			return err
		}
		if ContainsRune(rs, ru) {
			return NoMatchErr
		}
		return nil
	})
}
func (m *ScMatch) RuneSequence(seq []rune) error {
	return m.sc.RestorePosOnErr(func() error {
		for i, l := 0, len(seq); i < l; i++ {
			ru := seq[i]
			if m.sc.Reverse {
				ru = seq[l-1-i]
			}

			// NOTE: using spm.Rune() would call keeppos n times

			ru2, err := m.sc.ReadRune()
			if err != nil {
				return err
			}
			if ru2 != ru {
				return NoMatchErr
			}
		}
		return nil
	})
}
func (m *ScMatch) RuneSequenceMid(rs []rune) error {
	return m.sc.RestorePosOnErr(func() error {
		for k := 0; ; k++ {
			if err := m.RuneSequence(rs); err == nil {
				return nil // match
			}
			if k+1 >= len(rs) {
				break
			}
			// backup to previous rune to try to match again
			m.sc.Reverse = !m.sc.Reverse
			_, err := m.sc.ReadRune()
			m.sc.Reverse = !m.sc.Reverse
			if err != nil {
				return err
			}
		}
		return NoMatchErr
	})
}
func (m *ScMatch) RuneRange(rr RuneRange) error {
	return m.sc.RestorePosOnErr(func() error {
		ru, err := m.sc.ReadRune()
		if err != nil {
			return err
		}
		if !rr.HasRune(ru) {
			return NoMatchErr
		}
		return nil
	})
}
func (m *ScMatch) RuneRangeNot(rr RuneRange) error { // negation
	return m.sc.RestorePosOnErr(func() error {
		ru, err := m.sc.ReadRune()
		if err != nil {
			return err
		}
		if rr.HasRune(ru) {
			return NoMatchErr
		}
		return nil
	})
}
func (m *ScMatch) RunesAndRuneRanges(rs []rune, rrs RuneRanges) error { // negation
	return m.sc.RestorePosOnErr(func() error {
		ru, err := m.sc.ReadRune()
		if err != nil {
			return err
		}
		if !ContainsRune(rs, ru) && !rrs.HasRune(ru) {
			return NoMatchErr
		}
		return nil
	})
}
func (m *ScMatch) RunesAndRuneRangesNot(rs []rune, rrs RuneRanges) error {
	return m.sc.RestorePosOnErr(func() error {
		ru, err := m.sc.ReadRune()
		if err != nil {
			return err
		}
		if ContainsRune(rs, ru) || rrs.HasRune(ru) {
			return NoMatchErr
		}
		return nil
	})
}

//----------

func (m *ScMatch) RuneFn(fn func(rune) bool) error {
	pos0 := m.sc.KeepPos()
	ru, err := m.sc.ReadRune()
	if err == nil {
		if !fn(ru) {
			pos0.Restore()
			err = NoMatchErr
		}
	}
	return err
}

// one or more
func (m *ScMatch) RuneFnLoop(fn func(rune) bool) error {
	for first := true; ; first = false {
		if err := m.RuneFn(fn); err != nil {
			if first {
				return err
			}
			return nil
		}
	}
}

//func (m *SMatcher) RuneFnZeroOrMore(fn func(rune) bool) int {
//	for i := 0; ; i++ {
//		if err := m.RuneFn(fn); err != nil {
//			return i
//		}
//	}
//}
//func (m *SMatcher) RuneFnOneOrMore(fn func(rune) bool) error {
//	return m.LoopRuneFn(fn)

//	if err := m.RuneFn(fn); err != nil {
//		return err
//	}
//	_ = m.RuneFnZeroOrMore(fn)
//	return nil
//}

//----------

func (m *ScMatch) Sequence(seq string) error {
	return m.RuneSequence([]rune(seq))
}
func (m *ScMatch) SequenceMid(seq string) error {
	return m.RuneSequenceMid([]rune(seq))
}

//// same as rune sequence, but directly using strings comparison
//func (m *ScMatch) Sequence(seq string) error {
//	if m.sc.Reverse {
//		return m.RuneSequence([]rune(seq))
//	}
//	l := len(seq)
//	b := m.sc.Src[m.sc.Pos:]
//	if l > len(b) {
//		return NoMatchErr
//	}
//	if string(b[:l]) != seq {
//		return NoMatchErr
//	}
//	m.sc.Pos += l
//	return nil
//}

//----------

func (m *ScMatch) RegexpFromStartCached(res string, maxLen int) error {
	return m.RegexpFromStart(res, true, maxLen)
}
func (m *ScMatch) RegexpFromStart(res string, cache bool, maxLen int) error {
	// TODO: reverse

	res = "^(" + res + ")" // from start

	re := (*regexp.Regexp)(nil)
	if cache {
		re2, ok := m.cache.regexps[res]
		if ok {
			re = re2
		}
	}
	if re == nil {
		re3, err := regexp.Compile(res)
		if err != nil {
			return err
		}
		re = re3
		if cache {
			m.cache.regexps[res] = re
		}
	}

	// limit input to be read
	src := m.sc.Src[m.sc.Pos:]
	max := maxLen
	if max > len(src) {
		max = len(src)
	}
	src = m.sc.Src[m.sc.Pos : m.sc.Pos+max]

	locs := re.FindIndex(src)
	if len(locs) == 0 {
		return NoMatchErr
	}
	m.sc.Pos += locs[1]
	return nil
}

//----------

func (m *ScMatch) DoubleQuotedString(maxLen int) error {
	return m.StringSection("\"", '\\', true, maxLen, false)
}
func (m *ScMatch) QuotedString() error {
	//return m.QuotedString2('\\', 3000, 10)
	return m.QuotedString2('\\', 3000, 3000)
}

// allows escaped runes (if esc!=0)
func (m *ScMatch) QuotedString2(esc rune, maxLen1, maxLen2 int) error {
	// doublequote: fail on newline, eof doesn't close
	if err := m.StringSection("\"", esc, true, maxLen1, false); err == nil {
		return nil
	}
	// singlequote: fail on newline, eof doesn't close (usually a smaller maxlen)
	if err := m.StringSection("'", esc, true, maxLen2, false); err == nil {
		return nil
	}
	// backquote: can have newline, eof doesn't close
	if err := m.StringSection("`", esc, false, maxLen1, false); err == nil {
		return nil
	}
	return fmt.Errorf("not a quoted string")
}

func (m *ScMatch) StringSection(openclose string, esc rune, failOnNewline bool, maxLen int, eofClose bool) error {
	return m.Section(openclose, openclose, esc, failOnNewline, maxLen, eofClose)
}

// match opened/closed sections.
func (m *ScMatch) Section(open, close string, esc rune, failOnNewline bool, maxLen int, eofClose bool) error {
	pos0 := m.sc.Pos
	return m.sc.RestorePosOnErr(func() error {
		if err := m.Sequence(open); err != nil {
			return err
		}
		for {
			if esc != 0 && m.EscapeAny(esc) == nil {
				continue
			}
			if err := m.Sequence(close); err == nil {
				return nil // ok
			}
			// consume rune
			ru, err := m.sc.ReadRune()
			if err != nil {
				// extension: stop on eof
				if eofClose && err == io.EOF {
					return nil // ok
				}

				return err
			}
			// extension: stop after maxlength
			if maxLen > 0 {
				d := m.sc.Pos - pos0
				if d < 0 { // handle reverse
					d = -d
				}
				if d > maxLen {
					return fmt.Errorf("passed maxlen")
				}
			}
			// extension: newline
			if failOnNewline && ru == '\n' {
				return fmt.Errorf("found newline")
			}
		}
	})
}

//----------

func (m *ScMatch) EscapeAny(escape rune) error {
	return m.sc.RestorePosOnErr(func() error {
		if m.sc.Reverse {
			if err := m.NRunes(1); err != nil {
				return err
			}
		}
		if err := m.Rune(escape); err != nil {
			return err
		}
		if !m.sc.Reverse {
			return m.NRunes(1)
		}
		return nil
	})
}
func (m *ScMatch) NRunes(n int) error {
	pos0 := m.sc.KeepPos()
	for i := 0; i < n; i++ {
		_, err := m.sc.ReadRune()
		if err != nil {
			pos0.Restore()
			return err
		}
	}
	return nil
}

//----------

func (m *ScMatch) SpacesIncludingNL() bool {
	err := m.Spaces(true, 0)
	return err == nil
}
func (m *ScMatch) SpacesExcludingNL() bool {
	err := m.Spaces(false, 0)
	return err == nil
}
func (m *ScMatch) Spaces(includeNL bool, escape rune) error {
	for first := true; ; first = false {
		if escape != 0 {
			if err := m.EscapeAny(escape); err == nil {
				continue
			}
		}
		pos0 := m.sc.KeepPos()
		ru, err := m.sc.ReadRune()
		if err == nil {
			valid := unicode.IsSpace(ru) && (includeNL || ru != '\n')
			if !valid {
				err = NoMatchErr
			}
		}
		if err != nil {
			pos0.Restore()
			if first {
				return err
			}
			return nil
		}
	}
}

//----------

func (m *ScMatch) And(fns ...ScFn) error {
	return m.sc.RestorePosOnErr(func() error {
		if m.sc.Reverse {
			for i := len(fns) - 1; i >= 0; i-- {
				fn := fns[i]
				if fn == nil {
					continue
				}
				if err := fn(); err != nil {
					return err
				}
			}
		} else {
			for _, fn := range fns {
				if fn == nil {
					continue
				}
				if err := fn(); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func (m *ScMatch) Or(fns ...ScFn) error {
	//me := iout.MultiError{} // TODO: better then first error?
	firstErr := error(nil)
	for _, fn := range fns {
		if fn == nil {
			continue
		}
		pos0 := m.sc.KeepPos()
		if err := fn(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if IsScFatalError(err) {
				return err
			}
			pos0.Restore()
			continue
		}
		return nil
	}
	return firstErr
}
func (m *ScMatch) Optional(fn ScFn) error {
	if fn == nil {
		return nil
	}
	pos0 := m.sc.KeepPos()
	if err := fn(); err != nil {
		if IsScFatalError(err) {
			return err
		}
		pos0.Restore()
	}
	return nil
}

//----------

func (m *ScMatch) ToNLExcludeOrEnd(esc rune) int {
	pos0 := m.sc.KeepPos()
	valid := func(ru rune) bool { return ru != '\n' }
	for {
		if esc != 0 && m.EscapeAny(esc) == nil {
			continue
		}
		if err := m.RuneFn(valid); err == nil {
			continue
		}
		break
	}
	return pos0.Len()
}
func (m *ScMatch) ToNLIncludeOrEnd(esc rune) int {
	pos0 := m.sc.KeepPos()
	_ = m.ToNLExcludeOrEnd(esc)
	_ = m.Rune('\n')
	return pos0.Len()
}

//----------

func (m *ScMatch) Letter() error {
	return m.RuneFn(unicode.IsLetter)
}
func (m *ScMatch) Digit() error {
	return m.RuneFn(unicode.IsDigit)
}
func (m *ScMatch) Digits() error {
	return m.RuneFnLoop(unicode.IsDigit)
}

func (m *ScMatch) Integer() error {
	// TODO: reverse
	//u := "[+-]?[0-9]+"
	//return m.RegexpFromStartCached(u)

	return m.And(
		m.P.Optional(m.sign),
		m.Digits,
	)
}

func (m *ScMatch) Float() error {
	// TODO: reverse
	//u := "[+-]?([0-9]*[.])?[0-9]+"
	//u := "[+-]?(\\d+([.]\\d*)?([eE][+-]?\\d+)?|[.]\\d+([eE][+-]?\\d+)?)"
	//return m.RegexpFromStartCached(u, 100)

	return m.Or(
		// -1.2
		// -1.2e3
		m.P.And(
			m.Integer,
			m.fraction,
			m.P.Optional(m.exponent),
		),
		// .2
		// .2e3
		m.P.And(
			m.fraction,
			m.P.Optional(m.exponent),
		),
	)
}

func (m *ScMatch) sign() error {
	return m.sc.M.RuneAny([]rune("+-"))
}
func (m *ScMatch) fraction() error {
	return m.And(
		m.P.Rune('.'),
		m.Digits,
	)
}
func (m *ScMatch) exponent() error {
	return m.And(
		m.P.RuneAny([]rune("eE")),
		m.P.Optional(m.sign),
		m.Digits,
	)
}

//----------
//----------
//----------

type RuneRange [2]rune // assume [0]<[1]

func (rr RuneRange) HasRune(ru rune) bool {
	return ru >= rr[0] && ru <= rr[1]
}
func (rr RuneRange) IntersectsRange(rr2 RuneRange) bool {
	noIntersection := rr2[1] <= rr[0] || rr2[0] > rr[1]
	return !noIntersection
}
func (rr RuneRange) String() string {
	return fmt.Sprintf("%q-%q", rr[0], rr[1])
}

//----------
//----------
//----------

type RuneRanges []RuneRange

func (rrs RuneRanges) HasRune(ru rune) bool {
	for _, rr := range rrs {
		if rr.HasRune(ru) {
			return true
		}
	}
	return false
}

//----------
//----------
//----------

var NoMatchErr = errors.New("no match")
