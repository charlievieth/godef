package godef

import (
	"go/build"
	"io/ioutil"
	"path/filepath"
	"testing"
)

// func (c *Config) Define(filename string, cursor int, src interface{}) (*Position, []byte, error) {

var defineTests = []struct {
	filename  string
	offset    int
	expLine   int
	expColumn int
	exp       Position
}{
	{
		filename: "testdata/parser/parser.go",
		offset:   61592,
		exp: Position{
			Filename: "parser.go",
			Line:     2317,
			Column:   18,
		},
	},
	{
		filename: "testdata/parser/parser.go",
		offset:   62214,
		exp: Position{
			Filename: "token.go",
			Line:     114,
			Column:   2,
		},
	},
	{
		filename: "testdata/parser/parser.go",
		offset:   63357,
		exp: Position{
			Filename: "ast.go",
			Line:     240,
			Column:   3,
		},
	},
	{
		filename: "testdata/parser/parser.go",
		offset:   62874,
		exp: Position{
			Filename: "interface.go",
			Line:     57,
			Column:   2,
		},
	},
	{
		filename: "testdata/parser/interface.go",
		offset:   6609,
		exp: Position{
			Filename: "errors.go",
			Line:     105,
			Column:   20,
		},
	},
	{
		filename: "testdata/os/path.go",
		offset:   2181,
		exp: Position{
			Filename: "error.go",
			Line:     56,
			Column:   6,
		},
	},
}

func TestDefine(t *testing.T) {
	conf := Config{Context: build.Default}
	for _, x := range defineTests {
		pos, _, err := conf.Define(x.filename, x.offset, nil)
		if err != nil {
			t.Errorf("(%+v): %#v\n", x, err)
			continue
		}
		name := filepath.Base(pos.Filename)
		if name != x.exp.Filename {
			t.Errorf("Filename (%+v): exp %s got %s\n", x, x.exp.Filename, pos.Filename)
		}
		if pos.Line != x.exp.Line {
			t.Errorf("Line (%+v): exp %d got %d\n", x, x.exp.Line, pos.Line)
		}
		if pos.Column != x.exp.Column {
			t.Errorf("Column (%+v): exp %d got %d\n", x, x.exp.Column, pos.Column)
		}
	}
}

func BenchmarkDefine(b *testing.B) {
	const filename = "testdata/os/path.go"
	src, err := ioutil.ReadFile(filename)
	if err != nil {
		b.Fatal(err)
	}
	conf := Config{Context: build.Default}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := conf.Define(filename, 2181, src); err != nil {
			b.Fatal(err)
		}
	}
}
