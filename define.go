package godef

import (
	"errors"
	"fmt"
	"go/build"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	util "github.com/charlievieth/buildutil"
)

var knownOS = make(map[string]bool)
var knownArch = make(map[string]bool)

func init() {
	for _, s := range util.KnownOSList() {
		knownOS[s] = true
	}
	for _, s := range util.KnownArchList() {
		knownArch[s] = true
	}
}

type Position struct {
	Filename string // filename, if any
	Offset   int    // offset, starting at 0
	Line     int    // line number, starting at 1
	Column   int    // column number, starting at 1 (character count)
}

func newPosition(tp token.Position) *Position {
	p := Position(tp)
	return &p
}

func (p Position) IsValid() bool { return p.Line > 0 }

func (p Position) String() string {
	s := p.Filename
	if p.IsValid() {
		if s != "" {
			s += ":"
		}
		s += fmt.Sprintf("%d:%d", p.Line, p.Column)
	}
	if s == "" {
		s = "-"
	}
	return s
}

type Config struct {
	UseOffset bool
	Context   build.Context
}

func updateGOPATH(ctxt *build.Context, filename string) string {
	_, _, err := guessImportPath(filename, ctxt)
	if err == nil {
		return ctxt.GOPATH
	}
	if e, ok := err.(*PathError); ok && strings.Contains(e.Dir, "src") {
		dirs := segments(e.Dir)
		for i := len(dirs) - 1; i > 0; i-- {
			if dirs[i] == "src" {
				return strings.Join(dirs[:i], string(filepath.Separator)) +
					string(os.PathListSeparator) + ctxt.GOPATH
			}
		}
	}
	return ctxt.GOPATH
}

func updateContextForFile(ctxt *build.Context, filename string, src []byte) *build.Context {
	tags := make(map[string]bool)
	if !util.GoodOSArchFile(ctxt, filename, tags) || !util.ShouldBuild(ctxt, src, tags) {
		for tag, ok := range tags {
			switch {
			case knownOS[tag]:
				switch {
				case ok && ctxt.GOOS != tag:
					ctxt.GOOS = tag
				case !ok && ctxt.GOOS == tag && runtime.GOOS != ctxt.GOOS:
					if tags[runtime.GOOS] {
						ctxt.GOOS = runtime.GOOS
					}
				}
			case knownArch[tag]:
				switch {
				case ok && ctxt.GOARCH != tag:
					ctxt.GOARCH = tag
				case !ok && ctxt.GOARCH == tag && runtime.GOARCH != ctxt.GOARCH:
					if tags[runtime.GOARCH] {
						ctxt.GOARCH = runtime.GOARCH
					}
				}
			}
		}

	}
	ctxt.GOPATH = updateGOPATH(ctxt, filename)
	return ctxt
}

func (c *Config) Define(filename string, cursor int, src interface{}) (*Position, []byte, error) {
	filename = filepath.Clean(filename)
	body, off, err := readSourceOffset(filename, cursor, src)
	if err != nil {
		return nil, nil, err
	}
	modified := map[string][]byte{
		filename: body,
	}
	ctxt := useModifiedFiles(&c.Context, modified)
	ctxt = updateContextForFile(ctxt, filename, body)
	query := &Query{
		Mode:  "definition",
		Pos:   fmt.Sprintf("%s:#%d", filename, off),
		Build: ctxt,
	}
	if err := definition(query); err != nil {
		return nil, nil, err
	}
	pos := query.Fset.Position(query.result.pos)
	b, err := ioutil.ReadFile(pos.Filename)
	if err != nil {
		return nil, nil, err
	}
	return newPosition(pos), b, nil
}

func readSourceOffset(filename string, cursor int, src interface{}) ([]byte, int, error) {
	if cursor < 0 {
		return nil, -1, errors.New("non-positive offset")
	}
	var (
		b   []byte
		n   int
		err error
	)
	switch s := src.(type) {
	case []byte:
		b = s
	case string:
		if cursor < len(s) {
			n = stringOffset(s, cursor)
			b = []byte(s)
		}
	case nil:
		b, err = ioutil.ReadFile(filename)
	default:
		err = errors.New("invalid source")
	}
	if err == nil && n == 0 {
		if cursor < len(b) {
			n = byteOffset(b, cursor)
		} else {
			err = errors.New("offset out of range")
		}
	}
	return b, n, err
}

func stringOffset(src string, off int) int {
	for i := range src {
		if off == 0 {
			return i
		}
		off--
	}
	return -1
}

func byteOffset(src []byte, off int) int {
	// TODO: This needs to tested.
	var i int
	for len(src) != 0 {
		if off == 0 {
			return i
		}
		_, n := utf8.DecodeRune(src)
		src = src[n:]
		i += n
		off--
	}
	return -1
}

func readSource(filename string, src interface{}) ([]byte, error) {
	switch s := src.(type) {
	case nil:
		return ioutil.ReadFile(filename)
	case string:
		return []byte(s), nil
	case []byte:
		return s, nil
	}
	return nil, errors.New("invalid source")
}
