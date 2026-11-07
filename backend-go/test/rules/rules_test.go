// Package rules enforces repository-wide rules that a linter cannot express
// (TENANCY §5 CI rule).
package rules

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	// Session-level SET/RESET of role or search_path. SET LOCAL and
	// set_config(..., true) are the only allowed forms on the request path.
	sessionSet = regexp.MustCompile(`\b(SET\s+(SESSION\s+)?(ROLE|search_path)|RESET\s+(ROLE|search_path|ALL|SESSION))\b`)
	// set_config('search_path'|'role', ..., false) is a session-level SET.
	setConfigSession = regexp.MustCompile(`(?is)set_config\(\s*'(search_path|role)'\s*,[^)]*,\s*false\s*\)`)
	// "ALTER ROLE x SET search_path" sets a role default, not the session.
	alterRoleSet = regexp.MustCompile(`(?i)ALTER\s+ROLE\s+\S+\s+$`)
	// A function-level SET clause (CREATE FUNCTION ... SET search_path = ...)
	// pins the function's own path; it is the recommended hardening.
	functionSet  = regexp.MustCompile(`(?i)(LANGUAGE\s+\w+|SECURITY\s+DEFINER|STABLE|IMMUTABLE|VOLATILE)\s+$`)
	runtimeParam = regexp.MustCompile(`RuntimeParams\[\s*"(role|search_path)"\s*\]`)
	simpleProto  = regexp.MustCompile(`QueryExecModeSimpleProtocol|QueryExecModeCacheStatement|QueryExecModeCacheDescribe`)
)

const allowedRuntimeParamsFile = "internal/migrate/conn.go"

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func walk(t *testing.T, fn func(rel, src string)) {
	root := moduleRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "bin", "testdata", ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".sql") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if rel == "test/rules/rules_test.go" {
			return nil
		}
		b, err := os.ReadFile(path) // #nosec G304 -- walking our own module
		if err != nil {
			return err
		}
		fn(rel, string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoSessionLevelSet(t *testing.T) {
	walk(t, func(rel, src string) {
		for _, loc := range sessionSet.FindAllStringIndex(src, -1) {
			before := src[max(0, loc[0]-60):loc[0]]
			if alterRoleSet.MatchString(before) || functionSet.MatchString(before) {
				continue
			}
			lineStart := strings.LastIndexByte(src[:loc[0]], '\n') + 1
			line := strings.TrimSpace(src[lineStart:loc[0]])
			if strings.Contains(line, "--") || (strings.HasSuffix(rel, ".go") && strings.Contains(line, "//")) {
				continue // SQL or Go comment
			}
			t.Errorf("%s: session-level %q; use SET LOCAL / set_config(..., true) or startup parameters", rel, src[loc[0]:loc[1]])
		}
		if m := setConfigSession.FindString(src); m != "" {
			t.Errorf("%s: session-level %q", rel, m)
		}
	})
}

func TestRuntimeParamsOnlyInConnGo(t *testing.T) {
	walk(t, func(rel, src string) {
		if rel == allowedRuntimeParamsFile || strings.HasSuffix(rel, "_test.go") {
			return
		}
		if m := runtimeParam.FindString(src); m != "" {
			t.Errorf("%s: %s is only allowed in %s", rel, m, allowedRuntimeParamsFile)
		}
	})
}

func TestNoUnsafeExecModes(t *testing.T) {
	walk(t, func(rel, src string) {
		if strings.HasSuffix(rel, "_test.go") || strings.HasSuffix(rel, ".golangci.yml") {
			return // tests reproduce the cache failure on purpose
		}
		if m := simpleProto.FindString(src); m != "" {
			t.Errorf("%s: %s is banned (TENANCY §5.2)", rel, m)
		}
	})
}
