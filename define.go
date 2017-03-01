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

func updateGOOS(ctxt *build.Context, tags map[string]bool) string {
	if tags[ctxt.GOOS] {
		return ctxt.GOOS
	}
	for tag, ok := range tags {
		if !knownOS[tag] {
			continue
		}
		if ok && ctxt.GOOS != tag {
			return tag
		}
		if !ok && ctxt.GOOS == tag && runtime.GOOS != ctxt.GOOS {
			if tags[runtime.GOOS] {
				return runtime.GOOS
			}
		}
	}
	return ctxt.GOOS
}

func updateGOARCH(ctxt *build.Context, tags map[string]bool) string {
	if tags[ctxt.GOARCH] {
		return ctxt.GOARCH
	}
	for tag, ok := range tags {
		if !knownArch[tag] {
			continue
		}
		if ok && ctxt.GOARCH != tag {
			return tag
		}
		if !ok && ctxt.GOARCH == tag && runtime.GOARCH != ctxt.GOARCH {
			if tags[runtime.GOARCH] {
				return runtime.GOARCH
			}
		}
	}
	return ctxt.GOARCH
}

func updateContextForFile(ctxt *build.Context, filename string, src []byte) *build.Context {
	tags := make(map[string]bool)
	if !util.GoodOSArchFile(ctxt, filename, tags) || !util.ShouldBuild(ctxt, src, tags) {
		ctxt.GOOS = updateGOOS(ctxt, tags)
		ctxt.GOARCH = updateGOARCH(ctxt, tags)
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

func stringOffset(s string, off int) int {
	i := 0
	var n int
	for n = 0; i < len(s) && n < off; n++ {
		if s[i] < utf8.RuneSelf {
			i++
		} else {
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
		}
	}
	if n == off && i < len(s) {
		return i
	}
	return -1
}

func byteOffset(s []byte, off int) int {
	i := 0
	var n int
	for n = 0; i < len(s) && n < off; n++ {
		if s[i] < utf8.RuneSelf {
			i++
		} else {
			_, size := utf8.DecodeRune(s[i:])
			i += size
		}
	}
	if n == off && i < len(s) {
		return i
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
