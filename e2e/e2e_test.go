package e2e_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	// `go test ./e2e` runs with cwd = <repo>/e2e
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"test.lua", "clua.go", "main.cpp"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("expected to run from the cLua repo (missing %s under %s); build with `make` from the repository root", name, root)
		}
	}
	return root
}

func mustExist(t *testing.T, path, hint string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("missing %s: %v\n%s", path, err, hint)
	}
	if info.Size() == 0 {
		t.Fatalf("%s exists but is empty", path)
	}
}

func findLua(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("LUA"); v != "" {
		if _, err := exec.LookPath(v); err == nil {
			return v
		}
		if _, err := os.Stat(v); err == nil {
			return v
		}
		t.Fatalf("LUA=%s is not executable", v)
	}
	for _, name := range []string{"lua", "lua5.4", "lua5.3"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Fatal("lua interpreter not found; install Lua 5.3+ or set LUA")
	return ""
}

func run(t *testing.T, dir string, env []string, name string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("command failed: %s %s\nerr: %v\nstdout:\n%s\nstderr:\n%s",
			name, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func TestCollectParseAndLcov(t *testing.T) {
	root := repoRoot(t)
	lib := filepath.Join(root, "libclua.so")
	clua := filepath.Join(root, "clua")
	src := filepath.Join(root, "test.lua")
	hint := "build first from the repository root: make"
	mustExist(t, lib, hint)
	mustExist(t, clua, hint)
	mustExist(t, src, hint)

	work := t.TempDir()
	for _, name := range []string{"libclua.so", "test.lua"} {
		in, err := os.Open(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(filepath.Join(work, name))
		if err != nil {
			in.Close()
			t.Fatal(err)
		}
		if _, err := io.Copy(out, in); err != nil {
			in.Close()
			out.Close()
			t.Fatal(err)
		}
		in.Close()
		if err := out.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(work, "libclua.so"), 0755); err != nil {
		t.Fatal(err)
	}

	lua := findLua(t)
	env := append(os.Environ(), "LUA_CPATH="+filepath.Join(work, "?.so")+";;")
	run(t, work, env, lua, "test.lua")

	cov := filepath.Join(work, "test.cov")
	mustExist(t, cov, "libclua should write test.cov after cl.stop()")

	stdout, _ := run(t, work, nil, clua, "-i", "test.cov", "-lcov", "test.info", "-path", ".")

	assertContains := func(s, want string) {
		t.Helper()
		if !strings.Contains(s, want) {
			t.Errorf("clua output missing %q\nfull output:\n%s", want, stdout)
		}
	}
	assertContains(stdout, "total coverage 78% 22/28")
	assertContains(stdout, "function coverage [function test1(i)] 66% 2/3")
	assertContains(stdout, "function coverage [function test2(i)] 100% 3/3")
	assertContains(stdout, "function coverage [function test3(i)] 66% 2/3")
	assertContains(stdout, "function coverage [test4 = function(i)] 75% 3/4")
	assertContains(stdout, "function coverage [local function test5(i)] 100% 1/1")

	infoPath := filepath.Join(work, "test.info")
	mustExist(t, infoPath, "clua -lcov should write test.info")
	data, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatal(err)
	}
	info := string(data)
	if !strings.HasPrefix(info, "TN:\n") {
		t.Errorf("LCOV should start with TN:, got prefix %q", firstLine(info))
	}
	if !strings.Contains(info, "end_of_record\n") {
		t.Error("LCOV missing end_of_record")
	}

	var sf string
	da := map[int]int{}
	var lf, lh, fnf, fnh int
	sawLF, sawLH, sawFNF, sawFNH := false, false, false, false
	reDA := regexp.MustCompile(`^DA:(\d+),(\d+)$`)
	for _, line := range strings.Split(info, "\n") {
		switch {
		case strings.HasPrefix(line, "SF:"):
			sf = strings.TrimSpace(strings.TrimPrefix(line, "SF:"))
		case strings.HasPrefix(line, "LF:"):
			lf, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "LF:")))
			sawLF = true
		case strings.HasPrefix(line, "LH:"):
			lh, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "LH:")))
			sawLH = true
		case strings.HasPrefix(line, "FNF:"):
			fnf, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "FNF:")))
			sawFNF = true
		case strings.HasPrefix(line, "FNH:"):
			fnh, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "FNH:")))
			sawFNH = true
		default:
			if m := reDA.FindStringSubmatch(line); m != nil {
				lineno, _ := strconv.Atoi(m[1])
				count, _ := strconv.Atoi(m[2])
				da[lineno] = count
			}
		}
	}
	if !strings.HasSuffix(filepath.ToSlash(sf), "test.lua") {
		t.Errorf("LCOV SF: want path ending in test.lua, got %q", sf)
	}
	if !sawLF || !sawLH || !sawFNF || !sawFNH {
		t.Errorf("LCOV missing summary records (LF=%v LH=%v FNF=%v FNH=%v)\n%s", sawLF, sawLH, sawFNF, sawFNH, info)
	}
	if lf != 28 || lh != 22 {
		t.Errorf("LCOV line coverage LF/LH = %d/%d, want 28/22", lf, lh)
	}
	if fnf != 5 || fnh != 5 {
		t.Errorf("LCOV function coverage FNF/FNH = %d/%d, want 5/5", fnf, fnh)
	}
	if len(da) == 0 {
		t.Fatal("LCOV has no DA:line,count records")
	}
	var hits, misses int
	for _, c := range da {
		if c > 0 {
			hits++
		} else {
			misses++
		}
	}
	if hits == 0 {
		t.Error("LCOV DA records have no executed lines")
	}
	if misses == 0 {
		t.Error("LCOV DA records have no unexecuted lines; test.lua leaves some branches uncovered")
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
