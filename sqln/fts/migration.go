package fts

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// Statements renders the statements creating (up) and dropping (down) the
// indexes in d, each index tagged with its Comment.
func Statements(d driver.Dialect, indexes ...*Index) (up, down []string, err error) {
	fd, err := fullText(d)
	if err != nil {
		return nil, nil, err
	}
	for _, idx := range indexes {
		if idx == nil {
			return nil, nil, fmt.Errorf("%w: nil index", ErrInvalidIndex)
		}
		comment, err := idx.Comment(d)
		if err != nil {
			return nil, nil, err
		}
		u, dn := fd.TextIndexDDL(&idx.def, comment)
		up = append(up, u...)
		down = append(dn, down...) // drop in reverse creation order
	}
	return up, down, nil
}

// Migration renders Statements as the bodies of an up and a down migration
// file. On MySQL, golang-migrate needs multiStatements=true in the DSN to
// run a file with several statements.
func Migration(d driver.Dialect, indexes ...*Index) (up, down string, err error) {
	u, dn, err := Statements(d, indexes...)
	if err != nil {
		return "", "", err
	}
	return joinStatements(u), joinStatements(dn), nil
}

func joinStatements(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return strings.Join(s, ";\n\n") + ";\n"
}

var migrationNameRe = regexp.MustCompile(`^[a-z0-9_]+$`)

// WriteMigration writes <version>_<name>.up.sql and .down.sql into dir, the
// version zero-padded to six digits, in the golang-migrate layout read by
// sqln/migrate. Call it from a generator (go:generate) or a test, and commit
// the files: the database gets the index through the normal migration path.
// Existing files are replaced.
func WriteMigration(dir string, version uint64, name string, d driver.Dialect, indexes ...*Index) error {
	if !migrationNameRe.MatchString(name) {
		return fmt.Errorf("fts: migration name %q must be lower-case letters, digits and '_'", name)
	}
	up, down, err := Migration(d, indexes...)
	if err != nil {
		return err
	}
	base := filepath.Join(dir, fmt.Sprintf("%06d_%s", version, name))
	// Migrations are committed source files, readable like the rest of the tree.
	if err := os.WriteFile(base+".up.sql", []byte(up), 0o644); err != nil { // #nosec G306 -- source file, no secret
		return err
	}
	return os.WriteFile(base+".down.sql", []byte(down), 0o644) // #nosec G306 -- source file, no secret
}
