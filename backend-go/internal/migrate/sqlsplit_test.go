package migrate

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/db"
)

func TestSplitUp(t *testing.T) {
	src := `-- header comment
-- +goose Up
CREATE TABLE a (x int);
-- a comment between statements
CREATE TABLE b (
  y int
);
-- +goose StatementBegin
CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$
BEGIN
  RETURN 1;
END $$;
-- +goose StatementEnd
INSERT INTO a VALUES (1);

-- +goose Down
DROP TABLE a;
`
	p, err := splitUp(src)
	require.NoError(t, err)
	require.False(t, p.NoTransaction)
	require.Len(t, p.Statements, 4)
	require.Contains(t, p.Statements[2], "RETURN 1;")
	require.NotContains(t, p.Statements[3], "DROP")

	p, err = splitUp("-- +goose NO TRANSACTION\n-- +goose Up\nCREATE INDEX CONCURRENTLY i ON a (x);\n")
	require.NoError(t, err)
	require.True(t, p.NoTransaction)
	require.Len(t, p.Statements, 1)

	_, err = splitUp("-- +goose Up\n-- +goose StatementBegin\nSELECT 1;\n")
	require.Error(t, err)
}

func TestSplitUpParsesEveryEmbeddedMigration(t *testing.T) {
	for _, tree := range []struct {
		fsys fs.FS
		dir  string
	}{{db.PlatformMigrations, "migrations/platform"}, {db.TenantMigrations, "migrations/tenant"}} {
		files, err := fs.Glob(tree.fsys, tree.dir+"/*.sql")
		require.NoError(t, err)
		require.NotEmpty(t, files)
		for _, f := range files {
			b, err := fs.ReadFile(tree.fsys, f)
			require.NoError(t, err)
			p, err := splitUp(string(b))
			require.NoError(t, err, f)
			require.NotEmpty(t, p.Statements, f)
		}
	}
}
