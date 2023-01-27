package covers

import (
	"errors"
	"fmt"
	"go/ast"
	"path"
	"strings"
	"sync"
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

func May(t testing.TB) *Counters {
	t.Helper()

	c, _ := Setup(t)
	return c
}

func Should(t testing.TB) *Counters {
	t.Helper()

	c, err := Setup(t)
	if err != nil {
		t.Logf("problem setting up coverage testing: %v", err)
	}
	return c
}

func Must(t testing.TB) *Counters {
	t.Helper()

	c, err := Setup(t)
	if err != nil {
		t.Fatalf("problem setting up coverage testing: %v", err)
	}
	return c
}

func Setup(t testing.TB) (*Counters, error) {
	t.Helper()

	c := &Counters{
		tb:       t,
		counters: map[string]*uint32{},
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

	c.counters = initCounters(t, c.isEnabled)
	c.Snapshot = c.NewSnapshot()

	if err != nil {
		return c, err
	}

	return c, nil
}

type Counters struct {
	before    testing.Cover
	tb        testing.TB
	counters  map[string]*uint32
	isEnabled bool
	Snapshot
}

// Snapshot represents the state of the counters at a point in time
type Snapshot struct {
	counters *Counters
	values   map[*uint32]uint32
}

func (c *Counters) NewSnapshot() Snapshot {
	c.tb.Helper()

	ss := make(map[*uint32]uint32, len(c.counters))
	for tag := range c.counters {
		addr := c.counters[tag]
		var val uint32
		if addr != nil {
			// This code path is for when coverage is off
			val = atomic.LoadUint32(addr)
		}
		ss[addr] = val
	}
	return Snapshot{
		counters: c,
		values:   ss,
	}
}

func (ss *Snapshot) Tag(tag string, f func(delta uint32)) {
	ss.counters.tb.Helper()

	addr, ok := ss.counters.counters[tag]
	if !ok {
		ss.counters.tb.Fatalf("tag not found in counters: %s", tag)
	}
	oldValue, ok := ss.values[addr]
	if !ok {
		ss.counters.tb.Fatalf("tag not found: %s", tag)
	}

	if addr == nil {
		// This code path is for when coverage is off
		return
	}
	value := atomic.LoadUint32(addr)
	delta := value - oldValue
	f(delta)
}

var (
	initCountersOnce  sync.Once
	initCountersValue map[string]*uint32
)

func initCounters(t testing.TB, coverageEnabled bool) map[string]*uint32 {
	t.Helper()
	initCountersOnce.Do(func() {
		t.Helper()

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
		initCountersValue = values
	})

	return initCountersValue
}
