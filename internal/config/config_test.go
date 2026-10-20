package config

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func readFile(t *testing.T) string {
	t.Helper()
	p, _ := path()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMissingFileIsEmpty(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	if v, err := Get("A"); v != "" || err != nil {
		t.Fatalf("Get = %q, %v", v, err)
	}
}

func TestSetCreatesFileWithHeader(t *testing.T) {
	dir := t.TempDir() + "/nested/greyman"
	t.Setenv("GREYMAN_CONFIG_DIR", dir)
	if err := Set("A", "1"); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t), header+"\nA=1\n"; got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
	if v, _ := Get("A"); v != "1" {
		t.Fatalf("Get = %q", v)
	}
}

func TestSetReplacesInPlaceAndKeepsComments(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	p, _ := path()
	if err := os.WriteFile(p, []byte("# mine\nA=1\n\nB = 2\n# tail\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"B", "3"}, {"C", "4"}} {
		if err := Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := readFile(t), "# mine\nA=1\n\nB=3\n# tail\nC=4\n"; got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
}

func TestLoadParsing(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	p, _ := path()
	body := "# c\n\nA=1\n  B = x=y \nnoequals\nA=2\n#C=3\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || m["A"] != "1" || m["B"] != "x=y" {
		t.Fatalf("Load = %v", m)
	}
}

func TestSetRejectsBadEntries(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	for _, kv := range [][2]string{{"", "v"}, {"a=b", "v"}, {"a\nb", "v"}, {"a", "v\nw"}} {
		if err := Set(kv[0], kv[1]); err == nil {
			t.Errorf("Set(%q, %q) succeeded", kv[0], kv[1])
		}
	}
}

func TestEmptyFileGetsHeader(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	p, _ := path()
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Set("A", "1"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(readFile(t), header+"\n") {
		t.Fatalf("file = %q", readFile(t))
	}
}

func TestUpdateSeesOldValueAndKeepsOrder(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	var seen []string
	for _, add := range []string{"a", "b"} {
		if err := Update("LIST", func(old string) string {
			seen = append(seen, old)
			return strings.Trim(old+","+add, ",")
		}); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(seen, "|") != "|a" {
		t.Errorf("old values = %q", seen)
	}
	if v, _ := Get("LIST"); v != "a,b" {
		t.Errorf("LIST = %q", v)
	}
	if err := Update("LIST", func(string) string { return "x\ny" }); err == nil {
		t.Error("newline in value accepted")
	}
	if v, _ := Get("LIST"); v != "a,b" {
		t.Errorf("rejected update changed LIST to %q", v)
	}
}

func TestConcurrentUpdatesDoNotLoseWrites(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	const n = 25
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Update("LIST", func(old string) string { return old + "x" }); err != nil {
				t.Error(err)
			}
			if err := Set("K"+strconv.Itoa(i), "v"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if v, _ := Get("LIST"); v != strings.Repeat("x", n) {
		t.Errorf("LIST = %q, want %d x's", v, n)
	}
	m, _ := Load()
	if len(m) != n+1 {
		t.Errorf("%d keys, want %d", len(m), n+1)
	}
}

func TestDeleteRemovesEveryLineForTheKey(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	p, _ := path()
	if err := os.WriteFile(p, []byte("# mine\nA=1\nB=2\nA = 3\n# tail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Delete("A"); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t), "# mine\nB=2\n# tail\n"; got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
	if v, _ := Get("A"); v != "" {
		t.Fatalf("Get(A) = %q after Delete", v)
	}
}

func TestDeleteOfAnUnsetKeyLeavesTheFileAlone(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	if err := Delete("A"); err != nil {
		t.Fatal(err)
	}
	if p, _ := path(); fileExists(p) {
		t.Fatal("Delete created options.cfg")
	}
	if err := Set("B", "1"); err != nil {
		t.Fatal(err)
	}
	before := readFile(t)
	if err := Delete("A"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t); got != before {
		t.Fatalf("file = %q, want %q", got, before)
	}
	if err := Delete("a=b"); err == nil {
		t.Error("Delete accepted an invalid key")
	}
}

func TestDeletingTheLastLineLeavesAFileThatGetsItsHeaderBack(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	p, _ := path()
	if err := os.WriteFile(p, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Delete("A"); err != nil {
		t.Fatal(err)
	}
	if err := Set("B", "2"); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t), header+"\nB=2\n"; got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
}

func TestFileIsPrivateAndAnOldWorldReadableFileIsTightened(t *testing.T) {
	t.Setenv("GREYMAN_CONFIG_DIR", t.TempDir())
	p, _ := path()
	if err := os.WriteFile(p, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, write := range []func() error{
		func() error { return Set("B", "2") },
		func() error { return Delete("B") },
	} {
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := write(); err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, %v; want 0600", fi.Mode().Perm(), err)
		}
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
