package reslocparser

import (
	"sync"

	"github.com/jmigpin/editor/util/parseutil"
)

type ResLocParser struct {
	parseMu sync.Mutex // allow .Parse() to be used concurrently

	Escape        rune
	PathSeparator rune
	ParseVolume   bool

	sc *parseutil.Scanner
	fn struct {
		location ScFn
		reverse  ScFn
	}
	vk struct {
		scheme *parseutil.ScValueKeeper
		volume *parseutil.ScValueKeeper
		path   *parseutil.ScValueKeeper
		line   *parseutil.ScValueKeeper
		column *parseutil.ScValueKeeper
	}
}

func NewResLocParser() *ResLocParser {
	p := &ResLocParser{}
	p.sc = parseutil.NewScanner()

	p.Escape = '\\'
	p.PathSeparator = '/'
	p.ParseVolume = false

	return p
}
func (p *ResLocParser) Init() {
	sc := p.sc

	p.vk.scheme = sc.NewValueKeeper()
	p.vk.volume = sc.NewValueKeeper()
	p.vk.path = sc.NewValueKeeper()
	p.vk.line = sc.NewValueKeeper()
	p.vk.column = sc.NewValueKeeper()
	resetVks := func() error {
		p.vk.scheme.Reset()
		p.vk.volume.Reset()
		p.vk.path.Reset()
		p.vk.line.Reset()
		p.vk.column.Reset()
		return nil
	}

	//----------

	nameSyms := func(except ...rune) ScFn {
		rs := nameRunes(except...)
		return sc.P.RuneAny(rs)
	}

	volume := func(pathSepFn ScFn) ScFn {
		if p.ParseVolume {
			return sc.P.And(
				p.vk.volume.KeepBytes(sc.P.And(
					sc.M.Letter, sc.P.Rune(':'),
				)),
				pathSepFn,
			)
		} else {
			return nil
		}
	}

	//----------

	// ex: "/a/b.txt"
	// ex: "/a/b.txt:12:3"
	cEscRu := p.Escape
	cPathSepRu := p.PathSeparator
	cPathSep := sc.P.Rune(cPathSepRu)
	cName := sc.P.Or(
		sc.P.EscapeAny(cEscRu),
		sc.M.Digit,
		sc.M.Letter,
		nameSyms(cPathSepRu, cEscRu),
	)
	cNames := sc.P.Loop2(sc.P.Or(
		cName,
		cPathSep,
	))
	cPath := sc.P.And(
		sc.P.Optional(volume(cPathSep)),
		cNames,
	)
	cLineCol := sc.P.And(
		sc.P.Rune(':'),
		p.vk.line.KeepBytes(sc.M.Digits), // line
		sc.P.Optional(sc.P.And(
			sc.P.Rune(':'),
			p.vk.column.KeepBytes(sc.M.Digits), // column
		)),
	)
	cFile := sc.P.And(
		p.vk.path.KeepBytes(cPath),
		sc.P.Optional(cLineCol),
	)

	//----------

	// ex: "file:///a/b.txt:12"
	// no escape sequence for scheme, used to be '\\' but better to avoid conflicts with platforms that use '\\' as escape; could always use encoding (ex: %20 for ' ')
	schEscRu := '\\'    // fixed
	schPathSepRu := '/' // fixed
	schPathSep := sc.P.Rune(schPathSepRu)
	schName := sc.P.Or(
		sc.P.EscapeAny(schEscRu),
		sc.M.Digit,
		sc.M.Letter,
		nameSyms(schPathSepRu, schEscRu),
	)
	schNames := sc.P.Loop2(sc.P.Or(
		schName,
		schPathSep,
	))
	schPath := sc.P.And(
		schPathSep,
		sc.P.Optional(volume(schPathSep)),
		schNames,
	)
	schFileTagS := "file://"
	schFile := sc.P.And(
		p.vk.scheme.KeepBytes(sc.P.Sequence(schFileTagS)),
		p.vk.path.KeepBytes(schPath),
		sc.P.Optional(cLineCol),
	)

	//----------

	// ex: "\"/a/b.txt\""
	dquote := sc.P.Rune('"') // double quote
	dquotedFile := sc.P.And(
		dquote,
		p.vk.path.KeepBytes(cPath),
		dquote,
	)

	//----------

	// ex: "\"/a/b.txt\", line 23"
	pyLineTagS := ", line "
	pyFile := sc.P.And(
		dquotedFile,
		sc.P.And(
			sc.P.Sequence(pyLineTagS),
			p.vk.line.KeepBytes(sc.M.Digits),
		),
	)

	//----------

	// ex: "/a/b.txt: line 23"
	shellLineTagS := ": line "
	shellFile := sc.P.And(
		p.vk.path.KeepBytes(cPath),
		sc.P.And(
			sc.P.Sequence(shellLineTagS),
			p.vk.line.KeepBytes(sc.M.Digits),
		),
	)

	//----------

	p.fn.location = sc.P.Or(
		// ensure values are reset at each attempt
		sc.P.And(resetVks, schFile),
		sc.P.And(resetVks, pyFile),
		sc.P.And(resetVks, dquotedFile),
		sc.P.And(resetVks, shellFile),
		sc.P.And(resetVks, cFile),
	)

	//----------
	//----------

	revNames := sc.P.Loop2(
		sc.P.Or(
			cName,
			//schName, // can't reverse, contains fixed '\\' escape that can conflit with platform not considering it an escape
			sc.P.Rune(cEscRu),
			sc.P.Rune(schEscRu),
			cPathSep,
			schPathSep,
		),
	)
	p.fn.reverse = sc.P.And(
		sc.P.Optional(dquote),
		//sc.P.Optional(cVolume),
		//sc.P.Optional(schVolume),
		sc.P.Optional(sc.P.SequenceMid(schFileTagS)),
		sc.P.Optional(sc.P.Loop2(sc.P.Or(
			cPathSep,
			schPathSep,
		))),
		sc.P.Optional(sc.P.And(
			sc.M.Letter, sc.P.Rune(':'), // volume
			//sc.P.Optional(sc.
		)),
		sc.P.Optional(revNames),
		sc.P.Optional(dquote),
		sc.P.Optional(sc.P.SequenceMid(pyLineTagS)),
		sc.P.Optional(sc.P.SequenceMid(shellLineTagS)),
		// c line column
		sc.P.Optional(sc.P.Loop2(
			sc.P.Or(sc.P.Rune(':'), sc.M.Digit),
		)),
	)
}
func (p *ResLocParser) Parse(src []byte, index int) (*ResLoc, error) {
	// only one instance of this parser can parse at each time
	p.parseMu.Lock()
	defer p.parseMu.Unlock()

	p.sc.SetSrc(src)
	p.sc.Pos = index

	//fmt.Printf("start pos=%v\n", p.sc.Pos)

	p.sc.Reverse = true
	_ = p.fn.reverse() // best effort
	p.sc.Reverse = false
	_ = p.sc.Pos

	//fmt.Printf("reverse pos=%v\n", p.sc.Pos)

	//p.sc.Debug = true
	pos0 := p.sc.KeepPos()
	if err := p.fn.location(); err != nil {
		return nil, err
	}
	//fmt.Printf("location pos=%v\n", p.sc.Pos)

	rl := &ResLoc{}
	rl.Scheme = string(p.vk.scheme.BytesOrNil())
	rl.Volume = string(p.vk.volume.BytesOrNil())
	rl.Path = string(p.vk.path.BytesOrNil())
	rl.Line = p.vk.line.IntOrZero()
	rl.Column = p.vk.column.IntOrZero()
	rl.Escape = p.Escape
	rl.PathSep = p.PathSeparator
	rl.Pos = pos0.Pos
	rl.End = p.sc.Pos

	return rl, nil
}

//----------
//----------
//----------

type ScFn = parseutil.ScFn

//----------
//----------
//----------

// all syms except letters and digits
var syms = "_-~.%@&?!=#+:^(){}[]<>\\/ "

// name separator symbols
var nameSepSyms = "" +
	" " + // word separator
	"=" + // usually around filenames (ex: -arg=/a/b.txt)
	"(){}[]<>" + // usually used around filenames in various outputs
	":" + // usually separating lines/cols from filenames
	""

func nameRunes(except ...rune) []rune {
	out := nameSepSyms
	for _, ru := range except {
		if ru != 0 {
			out += string(ru)
		}
	}
	s := parseutil.RunesExcept(syms, out)
	return []rune(s)
}
