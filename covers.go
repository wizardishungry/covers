package covers

import (
	"errors"
	"fmt"
	"go/ast"
	"path"
	"strings"
	"sync/atomic"
	"testing"
	_ "unsafe"

	"golang.org/x/tools/go/packages"
)

const TagName = "//covers:"

var (
	ErrNoCoverage = errors.New("coverage not enabled (-cover)")
	ErrWrongMode  = errors.New("mode not supported for operation")
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
		t.Logf("problem setting up coverage testing: %v", err)
	}
	return c
}

func Must(t testing.TB) *Covers {
	t.Helper()

	c, err := Setup(t)
	if err != nil {
		t.Fatalf("problem setting up coverage testing: %v", err)
	}
	return c
}

func Setup(t testing.TB) (*Covers, error) {
	t.Helper()

	c := &Covers{
		t:      t,
		values: map[string]*uint32{},
	}

	var err error
	switch cm := testing.CoverMode(); cm {
	case "count", "atomic":
		c.isEnabled = true
	case "":
		err = ErrNoCoverage
	case "set":
		fallthrough
	default:
		err = fmt.Errorf("%v; was \"%s\". Try -covermode atomic|count", ErrWrongMode, cm)
	}

	c.values = mustInit(t, c.isEnabled)
	if err != nil {
		return c, err
	}

	t.Cleanup(func() {
		t.Helper()
		c.done(t)
	})
	return c, nil
}

type Covers struct {
	finishedCB
	before    testing.Cover
	t         testing.TB
	values    map[string]*uint32
	isEnabled bool
}

func (c *Covers) Tag(tag string) *Counter {
	ctr, ok := c.values[tag]
	if !ok {
		c.t.Fatalf("covers tag not found: %s", tag)
	}
	var old uint32
	if ctr != nil {
		old = atomic.LoadUint32(ctr)
	}
	counter := &Counter{
		old:  old,
		ctr:  ctr,
		name: tag,
	}
	c.cbs = append(c.cbs, counter.done)
	return counter
}

var (
	mustInitOnce   atomic.Bool
	mustInitValues map[string]*uint32
)

func mustInit(t testing.TB, coverageEnabled bool) map[string]*uint32 {
	t.Helper()

	if mustInitOnce.Load() {
		return mustInitValues
	}
	defer func() { mustInitOnce.Store(true) }()

	pkgsForGettingModulePath, err := packages.Load(&packages.Config{
		Mode: packages.NeedModule,
	})
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	if len(pkgsForGettingModulePath) < 1 {
		t.Fatalf("packages.Load was empty")
	}
	modulePath := pkgsForGettingModulePath[0].Module.Path

	cfg := &packages.Config{
		Mode: packages.NeedSyntax |
			packages.NeedModule |
			packages.NeedCompiledGoFiles |
			packages.NeedFiles |
			packages.NeedTypes,
		// Logf:  t.Logf,
		Tests: true,
	}

	pkgs, err := packages.Load(cfg, path.Join(modulePath, "..."))
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
							if !coverageEnabled {
								// when in Should or May mode we still want to fail on missing tags
								values[target] = nil
							}
						}
					}
				}
				commentMap[pathWithModule] = commentMapEntry
			}
		}

		if !coverageEnabled {
			continue
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
					// In tests there are two pkgs for each pkg - with and without tests
					// We should probably only visit each file once!
					if otherCtr, ok := values[target]; ok && otherCtr != ctr {
						t.Fatalf("duplicated tag %s", comment.Text)
					}
					values[target] = ctr
					// t.Logf("comment %+v matched block %+v; tag %s", commentPos, block, target)
				}
			}
		}
	}
	mustInitValues = values
	return mustInitValues
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
			t.Errorf("IsZero(%s) failed; was %d", c.name, delta)
		}
	})
}

func (c *Counter) IsNotZero() {
	c.run(func(t testing.TB, delta uint32) {
		t.Helper()

		if delta == 0 {
			t.Errorf("IsNotZero(%s) failed", c.name)
		}

	})
}
