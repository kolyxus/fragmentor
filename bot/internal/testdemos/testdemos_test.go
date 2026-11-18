package testdemos

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// write makes the file of size bytes under dir, with its folders.
func write(t *testing.T, dir, name string, size int) {
	t.Helper()
	writeText(t, dir, name, strings.Repeat("x", size))
}

// writeText makes the file with the text under dir, with its folders.
func writeText(t *testing.T, dir, name, text string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// rel returns the files as paths in testdata, with slashes.
func rel(t *testing.T, testdata string, files []string) []string {
	t.Helper()
	var out []string
	for _, f := range files {
		r, err := filepath.Rel(testdata, f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(r))
	}
	return out
}

func TestFind(t *testing.T) {
	testdata := filepath.Join(t.TempDir(), "testdata")
	write(t, testdata, "b.zst", 30)
	write(t, testdata, "a-1-1.dem.zst", 50)
	write(t, testdata, "README.md", 1)
	write(t, testdata, "scope.json", 1)
	write(t, testdata, "x/c.zst", 10)
	write(t, testdata, "7-8lvl/e.zst", 20)
	write(t, testdata, "7-8lvl/f.zst", 20)
	write(t, testdata, "7-8lvl/notes.txt", 1)
	// The demos of other maps, for the dictionary of places only, and a
	// demo two folders deep: neither is for the tests.
	write(t, testdata, "maps/d.zst", 1)
	write(t, testdata, "deep/er/g.zst", 1)

	files, err := find(testdata)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"7-8lvl/e.zst", "7-8lvl/f.zst", "a-1-1.dem.zst", "b.zst", "x/c.zst"}
	if got := rel(t, testdata, files); !slices.Equal(got, want) {
		t.Errorf("find() = %q, want %q", got, want)
	}

	// The smallest, by size and then by path, in the order of their paths.
	for _, tt := range []struct {
		n    int
		want []string
	}{
		{1, []string{"x/c.zst"}},
		{2, []string{"7-8lvl/e.zst", "x/c.zst"}},
		{3, []string{"7-8lvl/e.zst", "7-8lvl/f.zst", "x/c.zst"}},
		{4, []string{"7-8lvl/e.zst", "7-8lvl/f.zst", "b.zst", "x/c.zst"}},
		{9, want},
	} {
		got, err := smallest(files, tt.n)
		if err != nil {
			t.Fatal(err)
		}
		if r := rel(t, testdata, got); !slices.Equal(r, tt.want) {
			t.Errorf("smallest(%d) = %q, want %q", tt.n, r, tt.want)
		}
	}
	if got := rel(t, testdata, files); !slices.Equal(got, want) {
		t.Errorf("smallest() changed the files: %q", got)
	}

	// No demos: nothing, and no error.
	if files, err := find(filepath.Join(t.TempDir(), "testdata")); err != nil || len(files) != 0 {
		t.Errorf("find() of an empty folder = %q, %v", files, err)
	}
}

func TestModuleRoot(t *testing.T) {
	root := t.TempDir()
	writeText(t, root, "go.mod", "// The bot.\nmodule "+module+" // the module\n\ngo 1.27.0\n")
	// A module of its own between the package and the module of the bot
	// does not stop the search.
	writeText(t, root, "internal/go.mod", "module example.com/other\n")
	writeText(t, root, "internal/pkg/pkg_test.go", "package pkg\n")
	pkg := filepath.Join(root, "internal", "pkg")
	for _, dir := range []string{root, pkg} {
		if got, err := moduleRoot(dir); err != nil || got != root {
			t.Errorf("moduleRoot(%s) = %q, %v; want %q", dir, got, err, root)
		}
	}
	if got, err := moduleRoot(t.TempDir()); err == nil {
		t.Errorf("moduleRoot() of a folder outside the module = %q, want an error", got)
	}
}

func TestModulePath(t *testing.T) {
	for in, want := range map[string]string{
		"module " + module + "\n":                   module,
		"// comment\nmodule\t" + module + " // x\n": module,
		`module "` + module + `"`:                   module,
		"modules are not here\n":                    "",
		"go 1.27.0\n":                               "",
		"":                                          "",
	} {
		if got := modulePath([]byte(in)); got != want {
			t.Errorf("modulePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestName(t *testing.T) {
	const demo = "1-00000000-0000-4000-8000-0000000000a1-1-1.dem.zst"
	testdata := filepath.Join("bot", "testdata")
	for file, want := range map[string]string{
		filepath.Join(testdata, demo):           "1-00000000",
		filepath.Join(testdata, "7-8lvl", demo): "7-8lvl/1-00000000",
		filepath.Join(testdata, "a.zst"):        "a.zst",
	} {
		if got := Name(file); got != want {
			t.Errorf("Name(%s) = %q, want %q", file, got, want)
		}
	}
}

// TestFiles checks the demos the tests of the module get: those of
// bot/testdata, none from maps/, and with -race no more than RaceDemos.
func TestFiles(t *testing.T) {
	files := Files(t)
	if race && len(files) > RaceDemos {
		t.Errorf("-race: %d demos, want %d at most", len(files), RaceDemos)
	}
	for _, f := range files {
		if filepath.Base(filepath.Dir(f)) == mapsDir {
			t.Errorf("a demo of maps/: %s", f)
		}
	}
	if !slices.IsSortedFunc(files, byPath) {
		t.Errorf("the demos are not in the order of their paths: %q", files)
	}
	if s := Smallest(t); !slices.Contains(files, s) {
		t.Errorf("the smallest demo %s is not among the demos", s)
	}
}
