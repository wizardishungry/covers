package covers

import (
	"fmt"
	"go/ast"
	"strings"
	"testing"
	_ "unsafe"

	"golang.org/x/tools/go/packages"
)

//go:linkname cover testing.cover
var cover testing.Cover

const TagName = "//MustCover:"

func May(t testing.TB) *Covers {
	c, _ := Setup(t)
	return c
}

func Should(t testing.TB) *Covers {
	c, err := Setup(t)
	if err != nil {
		t.Logf("problem setting up coverage testing %v", err)
	}
	return c
}

func Must(t testing.TB) *Covers {
	c, err := Setup(t)
	if err != nil {
		t.Fatalf("problem setting up coverage testing %v", err)
	}
	return c
}

func Setup(t testing.TB) (*Covers, error) {
	saved := clone(cover)

	c := &Covers{
		before: saved,
		t:      t,
	}
	t.Cleanup(func() {
		m := c.done(t)
		t.Logf("map is %+v", m)
		for _, f := range c.cbs {
			f(m)
		}
	})
	return c, nil
}

type Covers struct {
	before testing.Cover
	t      testing.TB
	cbs    []func(map[string]uint32)
}

func (c *Covers) Key(key string, f func(uint32)) {
	c.ForKeys(func(m map[string]uint32) {
		f(m[key])
	})
}

func (c *Covers) ForKeys(f func(map[string]uint32)) {
	c.cbs = append(c.cbs, f)
}
func (c *Covers) HasKey(key string) {
	c.Key(key, func(u uint32) {
		if u < 1 {
			c.t.Errorf("Covers.HasKey failed %s=%d", key, u)
		}
	})
}
func (c *Covers) NoHasKey(key string) {
	c.Key(key, func(u uint32) {
		if u > 0 {
			c.t.Errorf("Covers.NoHasKey failed %s=%d", key, u)
		}
	})
}

func (c *Covers) done(t testing.TB) map[string]uint32 {
	out := make(map[string]uint32)
	after := clone(cover)
	// printFiles(t, "cached", c.before)
	// printFiles(t, "after", after)
	delta := diff(c.before, after)
	// printFiles(t, "delta", delta)

	cfg := &packages.Config{
		Mode: packages.NeedSyntax |
			packages.NeedModule |
			packages.NeedCompiledGoFiles |
			packages.NeedFiles |
			packages.NeedTypes,
		// Logf:  t.Logf,
		Tests: true,
	}

	pkgs, err := packages.Load(cfg, "./...") // can likely sync.Once this
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	// t.Logf(after.CoveredPackages)

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
		// ^ all of this can be done once per test run

		for file, blocks := range delta.Blocks {
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
					ctr := delta.Counters[file][i]
					if ctr == 0 {
						continue
					}
					t.Logf("comment %+v matched block %+v; ctr %d", commentPos, block, ctr)
					fmt.Println(comment.Text)
					target, ok := targetMap[comment]
					if !ok {
						t.Fatalf("target not found for comment!")
					}
					out[target] = ctr
				}
			}
		}
	}
	return out
}

func printFiles(t testing.TB, label string, c testing.Cover) {
	t.Logf("== %s ==", label)
	for file, blocks := range c.Blocks {
		t.Logf("%s: %d", file, len(blocks))
		for i, b := range blocks {
			v := c.Counters[file][i] // could be atomic
			if v == 0 {
				continue
			}
			t.Logf("%s#L%dC%d-L%dC%d: %+v", file, b.Line0, b.Col0, b.Line1, b.Col1, v)
		}
	}
}

func clone(in testing.Cover) testing.Cover {
	out := zero(in)

	for k := range in.Counters {
		out.Counters[k] = make([]uint32, len(in.Counters[k]))
		copy(out.Counters[k], in.Counters[k])
	}

	for k := range in.Blocks {
		out.Blocks[k] = make([]testing.CoverBlock, len(in.Blocks[k]))
		copy(out.Blocks[k], in.Blocks[k])
	}

	return out
}

func zero(in testing.Cover) testing.Cover {
	return testing.Cover{
		Mode:            in.Mode,
		Counters:        make(map[string][]uint32, len(in.Counters)),
		Blocks:          make(map[string][]testing.CoverBlock, len(in.Blocks)),
		CoveredPackages: in.CoveredPackages,
	}
}

func diff(before, after testing.Cover) testing.Cover {
	out := clone(after)
	for file, perFileCounters := range before.Counters {
		for i, v := range perFileCounters {
			out.Counters[file][i] -= v // could be atomic
		}
	}
	return out
}
