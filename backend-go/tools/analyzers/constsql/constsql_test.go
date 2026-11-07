package constsql_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tejas1112001/POS_inventory/backend-go/tools/analyzers/constsql"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), constsql.Analyzer, "a")
}
