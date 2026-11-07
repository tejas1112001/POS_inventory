// Command constsql runs the constsql analyzer, standalone or as
// `go vet -vettool=$(which constsql) ./...`.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/tejas1112001/POS_inventory/backend-go/tools/analyzers/constsql"
)

func main() { singlechecker.Main(constsql.Analyzer) }
