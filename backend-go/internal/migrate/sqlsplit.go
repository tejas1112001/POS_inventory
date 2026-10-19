package migrate

import (
	"bufio"
	"errors"
	"strings"
)

// parsedSQL is the Up section of one goose SQL file.
type parsedSQL struct {
	Statements    []string
	NoTransaction bool
}

// splitUp parses the "-- +goose Up" section of a migration written to our
// conventions: statements end with ';' at the end of a line, plpgsql bodies
// sit between StatementBegin/StatementEnd. Used only by --dry-run; real runs
// go through goose itself.
func splitUp(src string) (parsedSQL, error) {
	var (
		out     parsedSQL
		inUp    bool
		inBlock bool
		buf     strings.Builder
	)
	flush := func() {
		if s := strings.TrimSpace(buf.String()); s != "" {
			out.Statements = append(out.Statements, s)
		}
		buf.Reset()
	}
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "-- +goose") {
			switch directive := strings.TrimSpace(strings.TrimPrefix(trim, "-- +goose")); directive {
			case "Up":
				inUp = true
			case "Down":
				if inUp {
					flush()
					return out, sc.Err()
				}
			case "StatementBegin":
				inBlock = true
			case "StatementEnd":
				inBlock = false
				if inUp {
					flush()
				}
			case "NO TRANSACTION":
				out.NoTransaction = true
			}
			continue
		}
		if !inUp {
			continue
		}
		if !inBlock && (trim == "" || strings.HasPrefix(trim, "--")) && buf.Len() == 0 {
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
		if !inBlock && strings.HasSuffix(trim, ";") {
			flush()
		}
	}
	if inBlock {
		return out, errors.New("migrate: unterminated StatementBegin")
	}
	flush()
	return out, sc.Err()
}
