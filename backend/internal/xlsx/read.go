package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// Read limits: uploads are untrusted, so every part is read through a cap
// (a small file can unzip to gigabytes).
const (
	maxSharedStrings = 1_000_000
	maxColumns       = 1000
)

// maxPartBytes caps the decompressed bytes of each XML part (a variable so
// tests can lower it).
var maxPartBytes int64 = 64 << 20

// ErrTooManyRows is returned when a sheet has a value below the row limit.
var ErrTooManyRows = errors.New("xlsx: too many rows")

// ErrNotWorkbook is returned for a file that is not an .xlsx workbook.
var ErrNotWorkbook = errors.New("xlsx: not an .xlsx workbook")

// Read returns the text of every cell of the workbook's first sheet, row
// by row: shared and inline strings as text, numbers as stored (8901234567890,
// 499.5), booleans TRUE/FALSE. Missing rows and cells read as empty.
func Read(data []byte) ([][]string, error) { return ReadLimit(data, 1<<20) }

// ReadLimit is Read for untrusted files: a value in a row after maxRows is
// ErrTooManyRows.
func ReadLimit(data []byte, maxRows int) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrNotWorkbook
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[strings.TrimPrefix(f.Name, "/")] = f
	}
	sheet := firstSheet(files)
	if files[sheet] == nil {
		return nil, ErrNotWorkbook
	}
	shared, err := sharedStrings(files)
	if err != nil {
		return nil, err
	}
	return readSheet(files[sheet], shared, maxRows)
}

func open(f *zip.File) (io.ReadCloser, error) {
	if f.UncompressedSize64 > uint64(maxPartBytes) { //nolint:gosec // G115: a positive constant
		return nil, fmt.Errorf("xlsx: %s is too large", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, ErrNotWorkbook
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(rc, maxPartBytes), rc}, nil
}

// decode streams the XML tokens of a part to fn.
func decode(f *zip.File, fn func(xml.Token) error) error {
	rc, err := open(f)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	d := xml.NewDecoder(rc)
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return ErrNotWorkbook
		}
		if err := fn(tok); err != nil {
			return err
		}
	}
}

func attr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// firstSheet finds the first sheet's part through the workbook and its
// relationships; xl/worksheets/sheet1.xml when they say nothing.
func firstSheet(files map[string]*zip.File) string {
	const fallback = "xl/worksheets/sheet1.xml"
	wb, rels := files["xl/workbook.xml"], files["xl/_rels/workbook.xml.rels"]
	if wb == nil || rels == nil {
		return fallback
	}
	rid := ""
	_ = decode(wb, func(tok xml.Token) error {
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sheet" && rid == "" {
			for _, a := range se.Attr {
				if a.Name.Local == "id" && a.Name.Space != "" {
					rid = a.Value
				}
			}
		}
		return nil
	})
	target := ""
	_ = decode(rels, func(tok xml.Token) error {
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" && attr(se, "Id") == rid && rid != "" {
			target = attr(se, "Target")
		}
		return nil
	})
	switch {
	case target == "":
		return fallback
	case strings.HasPrefix(target, "/"):
		return path.Clean(strings.TrimPrefix(target, "/"))
	default:
		return path.Clean("xl/" + target)
	}
}

// sharedStrings reads xl/sharedStrings.xml (absent in workbooks that use
// inline strings only). Rich text runs are joined; phonetic runs dropped.
func sharedStrings(files map[string]*zip.File) ([]string, error) {
	f := files["xl/sharedStrings.xml"]
	if f == nil {
		return nil, nil
	}
	var (
		out       []string
		cur       strings.Builder
		inSI, inT bool
		phonetic  int
	)
	err := decode(f, func(tok xml.Token) error {
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "rPh":
				phonetic++
			case "t":
				inT = inSI && phonetic == 0
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				if len(out) >= maxSharedStrings {
					return fmt.Errorf("xlsx: more than %d shared strings", maxSharedStrings)
				}
				out = append(out, cur.String())
				inSI = false
			case "rPh":
				phonetic--
			case "t":
				inT = false
			}
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		}
		return nil
	})
	return out, err
}

// colIndex is the 0-based column of a cell reference ("C7" → 2).
func colIndex(ref string) (int, bool) {
	n := 0
	for i := 0; i < len(ref); i++ {
		ch := ref[i]
		if ch >= 'a' && ch <= 'z' {
			ch -= 'a' - 'A'
		}
		if ch < 'A' || ch > 'Z' {
			if i == 0 {
				return 0, false
			}
			break
		}
		n = n*26 + int(ch-'A'+1)
		if n > maxColumns {
			return maxColumns, true // refused by the caller
		}
	}
	return n - 1, n > 0
}

func readSheet(f *zip.File, shared []string, maxRows int) ([][]string, error) {
	var (
		out                []string
		rows               [][]string
		rowNum, col        int
		cellType           string
		val, inline        strings.Builder
		inV, inIS, inT     bool
		phonetic           int
		hasValue, haveCell bool
	)
	err := decode(f, func(tok xml.Token) error {
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				if r, err := strconv.Atoi(attr(t, "r")); err == nil && r > rowNum {
					rowNum = r
				} else {
					rowNum++
				}
				out, hasValue = nil, false
				col = -1
			case "c":
				if c, ok := colIndex(attr(t, "r")); ok {
					col = c
				} else {
					col++
				}
				if col >= maxColumns {
					return fmt.Errorf("xlsx: more than %d columns", maxColumns)
				}
				cellType = attr(t, "t")
				val.Reset()
				inline.Reset()
				haveCell = true
			case "v":
				inV = haveCell
			case "is":
				inIS = haveCell
			case "rPh":
				phonetic++
			case "t":
				inT = inIS && phonetic == 0
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "is":
				inIS = false
			case "rPh":
				phonetic--
			case "t":
				inT = false
			case "c":
				text := cellText(cellType, val.String(), inline.String(), shared)
				haveCell = false
				if text == "" {
					return nil
				}
				for len(out) <= col {
					out = append(out, "")
				}
				out[col] = text
				hasValue = true
			case "row":
				if !hasValue {
					return nil
				}
				if rowNum > maxRows {
					return ErrTooManyRows
				}
				for len(rows) < rowNum-1 {
					rows = append(rows, nil)
				}
				rows = append(rows, out)
			}
		case xml.CharData:
			switch {
			case inV:
				val.Write(t)
			case inT:
				inline.Write(t)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func cellText(typ, v, inline string, shared []string) string {
	switch typ {
	case "s":
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || i < 0 || i >= len(shared) {
			return ""
		}
		return shared[i]
	case "inlineStr":
		return inline
	case "b":
		if strings.TrimSpace(v) == "1" {
			return "TRUE"
		}
		return "FALSE"
	default: // n, str, e, d
		return v
	}
}
