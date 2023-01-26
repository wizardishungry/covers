package covers

import (
	"errors"
	"go/ast"
	"strings"
	"sync/atomic"
	"testing"
	_ "unsafe"

	"golang.org/x/tools/go/packages"
)

const TagName = "//covers:"

var (
	ErrNoCoverage = errors.New("coverage not enabled (-cover)")
	ErrWrongMode  = errors.New("coverage mode not supported for operation")
)

//go:linkname cover testing.cover
var cover testing.Cover

func May(t testing.TB) *Covers {
	t.Helper()

	c, _ := Setup(t)
	return c
}

func Should(t testing.TB) *Covers {
	t.Helper()

	c, err := Setup(t)
	if err != nil {
		t.Logf("problem setting up coverage testing %v", err)
	}
	return c
}

func Must(t testing.TB) *Covers {
	t.Helper()

	c, err := Setup(t)
	if err != nil {
		t.Fatalf("problem setting up coverage testing %v", err)
	}
	return c
}

func Setup(t testing.TB) (*Covers, error) {
	t.Helper()

	c := &Covers{
		t: t,
	}

	if testing.CoverMode() == "" {
		return nil, ErrNoCoverage
	}

	c.init(t)
	t.Cleanup(func() {
		t.Helper()
		c.done(t)
	})
	return c, nil
}

type Covers struct {
	finishedCB
	before testing.Cover
	t      testing.TB
	values map[string]*uint32
}

func (c *Covers) Tag(tag string) *Counter {
	ctr, ok := c.values[tag]
	if !ok {
		c.t.Fatalf("tag not found: %s", tag)
	}
	counter := &Counter{
		old:  atomic.LoadUint32(ctr),
		ctr:  ctr,
		name: tag,
	}
	c.cbs = append(c.cbs, counter.done)
	return counter
}

func (c *Covers) init(t testing.TB) {
	t.Helper()

	// the code to build tag to counter map ("values") really should be package scope and sync.Once

	cfg := &packages.Config{
		Mode: packages.NeedSyntax |
			packages.NeedModule |
			packages.NeedCompiledGoFiles |
			packages.NeedFiles |
			packages.NeedTypes,
		// Logf:  t.Logf,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}

	values := make(map[string]*uint32) // tag key -> output values
	for _, pkg := range pkgs {
		commentMap := make(map[string][]*ast.Comment) // maps a file to the list of its tagged comments
		targetMap := make(map[*ast.Comment]string)    // which output registers get incremented by a comment
		dir := pkg.Module.Dir
		path := pkg.Module.Path
		for i, f := range pkg.CompiledGoFiles {
			if strings.HasPrefix(f, dir) {
				pathWithModule := strings.Replace(f, dir, path, 1)
				syntax := pkg.Syntax[i]
				commentMapEntry := commentMap[pathWithModule]
				for _, commentGroup := range syntax.Comments {
					for _, c := range commentGroup.List {
						if strings.HasPrefix(c.Text, TagName) {
							commentMapEntry = append(commentMapEntry, c)
							target := strings.TrimPrefix(c.Text, TagName)
							targetMap[c] = target
						}
					}
				}
				commentMap[pathWithModule] = commentMapEntry
			}
		}

		for file, blocks := range cover.Blocks {
			commentMapEntry, ok := commentMap[file]
			if !ok {
				// t.Logf("no comment map for %s", file)
				continue
			}
			for i, block := range blocks {
				for _, comment := range commentMapEntry {
					commentPos := pkg.Fset.Position(comment.Pos())
					if commentPos.Line < int(block.Line0) {
						continue
					}
					if commentPos.Line > int(block.Line1) {
						break // went far enough
					}
					if commentPos.Line == int(block.Line0) &&
						commentPos.Column < int(block.Col0) {
						continue
					}
					if commentPos.Line == int(block.Line1) &&
						commentPos.Column > int(block.Col1) {
						continue
					}
					ctr := &cover.Counters[file][i]
					target, ok := targetMap[comment]
					if !ok {
						t.Fatalf("target not found for comment!")
					}
					if _, ok := values[target]; ok {
						t.Fatalf("duplicated tag %s", comment.Text)
					}
					values[target] = ctr
					// t.Logf("comment %+v matched block %+v; tag %s", commentPos, block, target)
				}
			}
		}
	}
	c.values = values
}

type finishedCB struct {
	cbs []func(testing.TB)
}

func (d *finishedCB) done(t testing.TB) {
	t.Helper()
	for _, f := range d.cbs {
		f(t)
	}
}

type Counter struct {
	name string
	finishedCB
	old uint32
	ctr *uint32
}

func (c *Counter) Run(f func(delta uint32)) {
	c.run(func(t testing.TB, delta uint32) {
		t.Helper()
		switch cm := testing.CoverMode(); cm {
		case "count", "atomic":
		case "set":
			fallthrough
		default:
			t.Fatalf("%v; was %s. Try -covermode atomic|count", ErrWrongMode, cm)
		}
		f(delta)
	})
}

func (c *Counter) run(f func(t testing.TB, delta uint32)) {
	c.cbs = append(c.cbs, func(t testing.TB) {
		t.Helper()
		new := atomic.LoadUint32(c.ctr)
		f(t, new-c.old)
	})
}

func (c *Counter) IsZero() {
	c.run(func(t testing.TB, delta uint32) {
		t.Helper()
		if delta != 0 {
			t.Errorf("%s IsZero failed; was %d", c.name, delta)
		}
	})
}

func (c *Counter) IsNotZero() {
	c.run(func(t testing.TB, delta uint32) {
		t.Helper()

		if delta == 0 {
			t.Errorf("%s IsNotZero failed", c.name)
		}

	})
}
