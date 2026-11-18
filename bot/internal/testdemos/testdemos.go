// Package testdemos picks the real demos the tests parse: those of
// internal/demo, internal/metrics, internal/findings, internal/review and
// internal/analysis. Only tests import it, the bot does not.
//
// The demos lie in bot/testdata and are not in git (bot/testdata/README.md):
// without them, and with -short, as each takes seconds, the tests skip. They
// are bot/testdata/*.zst and bot/testdata/*/*.zst, the folders of other
// levels like 7-8lvl/ among them, but not maps/: the demos of other maps lie
// there for the dictionary of places only, which
// go run ./cmd/parsedemo -places checks. With -race, which parses 10 to 20
// times slower, the tests take the RaceDemos smallest demos only.
package testdemos

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// RaceDemos is how many demos the tests parse with -race: the smallest.
const RaceDemos = 3

const (
	// module is the path of the module of the bot, whose folder holds
	// testdata.
	module = "github.com/kolyxus/fragmentor/bot"
	// mapsDir is the folder of testdata whose demos are for the dictionary
	// of places only.
	mapsDir = "maps"
)

// Files returns the demos for the tests in the order of their paths in
// bot/testdata: all of them, or the RaceDemos smallest with -race. It skips
// the test with -short and without demos.
func Files(t testing.TB) []string {
	t.Helper()
	files, err := find(testdata(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no demos in bot/testdata")
	}
	if race && len(files) > RaceDemos {
		t.Logf("-race: the %d smallest of %d demos", RaceDemos, len(files))
		if files, err = smallest(files, RaceDemos); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

// Smallest returns the smallest demo for the tests, the one that parses
// fastest. It skips the test with -short and without demos.
func Smallest(t testing.TB) string {
	t.Helper()
	files, err := find(testdata(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no demos in bot/testdata")
	}
	if files, err = smallest(files, 1); err != nil {
		t.Fatal(err)
	}
	return files[0]
}

// Name names the subtest of a demo: its folder in testdata, if any, and the
// start of its name, like "7-8lvl/1-b569b123".
func Name(file string) string {
	name := filepath.Base(file)
	if len(name) > 10 {
		name = name[:10]
	}
	if dir := filepath.Base(filepath.Dir(file)); dir != "testdata" {
		name = dir + "/" + name
	}
	return name
}

// testdata returns the folder bot/testdata, found from the working
// directory of the test, the folder of its package, up to the folder of the
// module of the bot. With -short it skips the test.
func testdata(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("parsing real demos takes seconds")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := moduleRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "testdata")
}

// moduleRoot returns the folder of the module of the bot: dir or the first
// folder above it whose go.mod is of the module.
func moduleRoot(dir string) (string, error) {
	for d := dir; ; {
		b, err := os.ReadFile(filepath.Join(d, "go.mod"))
		switch {
		case err == nil && modulePath(b) == module:
			return d, nil
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return "", err
		}
		up := filepath.Dir(d)
		if up == d {
			return "", fmt.Errorf("testdemos: no go.mod of %s in %s or above", module, dir)
		}
		d = up
	}
}

// modulePath returns the path of the module a go.mod declares, "" if none.
func modulePath(gomod []byte) string {
	s := bufio.NewScanner(bytes.NewReader(gomod))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		rest, ok := strings.CutPrefix(line, "module")
		if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		rest, _, _ = strings.Cut(rest, "//")
		rest = strings.TrimSpace(rest)
		if p, err := strconv.Unquote(rest); err == nil {
			return p
		}
		return rest
	}
	return ""
}

// find returns the demos for the tests in the folder testdata, *.zst in it
// and in its folders but maps/, in the order of their paths.
func find(testdata string) ([]string, error) {
	var files []string
	for _, pattern := range []string{"*.zst", filepath.Join("*", "*.zst")} {
		found, err := filepath.Glob(filepath.Join(testdata, pattern))
		if err != nil {
			return nil, err
		}
		for _, f := range found {
			// The demos of maps/ are for the dictionary of places only.
			if filepath.Base(filepath.Dir(f)) != mapsDir {
				files = append(files, f)
			}
		}
	}
	slices.SortFunc(files, byPath)
	return files, nil
}

// smallest returns the n smallest of the files, by size and then by path,
// in the order of their paths.
func smallest(files []string, n int) ([]string, error) {
	sizes := make(map[string]int64, len(files))
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			return nil, err
		}
		sizes[f] = info.Size()
	}
	out := slices.Clone(files)
	slices.SortFunc(out, func(a, b string) int { return cmp.Or(cmp.Compare(sizes[a], sizes[b]), byPath(a, b)) })
	out = out[:min(n, len(out))]
	slices.SortFunc(out, byPath)
	return out, nil
}

// byPath orders files by their paths, with the same separator on any
// system.
func byPath(a, b string) int {
	return strings.Compare(filepath.ToSlash(a), filepath.ToSlash(b))
}
