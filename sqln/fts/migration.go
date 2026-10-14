package fts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// steps renders the DDL steps of the indexes in d, each index tagged with its Comment.
func steps(d driver.Dialect, indexes []*Index) ([]driver.TextStep, error) {
	fd, err := fullText(d)
	if err != nil {
		return nil, err
	}
	var out []driver.TextStep
	for _, idx := range indexes {
		if idx == nil {
			return nil, fmt.Errorf("%w: nil index", ErrInvalidIndex)
		}
		comment, err := idx.Comment(d)
		if err != nil {
			return nil, err
		}
		s, err := fd.TextIndexDDL(&idx.def, comment)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrInvalidIndex, idx.def.Name, err)
		}
		out = append(out, s...)
	}
	return out, nil
}

// Statements renders the statements creating (up) and dropping (down) the
// indexes in d, in order; down undoes up in reverse.
func Statements(d driver.Dialect, indexes ...*Index) (up, down []string, err error) {
	s, err := steps(d, indexes)
	if err != nil {
		return nil, nil, err
	}
	for _, st := range s {
		up = append(up, st.Up...)
	}
	for _, st := range slices.Backward(s) {
		down = append(down, st.Down...)
	}
	return up, down, nil
}

// File is the body of one up and one down migration file.
type File struct {
	Up, Down string
}

// Migrations renders the migration files of the indexes in d: consecutive
// statements share a file, and each statement that cannot run inside a
// transaction block (CREATE INDEX CONCURRENTLY) gets its own, since a
// migration file runs as one.
func Migrations(d driver.Dialect, indexes ...*Index) ([]File, error) {
	s, err := steps(d, indexes)
	if err != nil {
		return nil, err
	}
	var groups [][]driver.TextStep
	for i, st := range s {
		if st.Isolated || i == 0 || s[i-1].Isolated {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], st)
	}
	files := make([]File, len(groups))
	for i, g := range groups {
		var up, down []string
		for _, st := range g {
			up = append(up, st.Up...)
		}
		for _, st := range slices.Backward(g) {
			down = append(down, st.Down...)
		}
		files[i] = File{Up: joinStatements(up), Down: joinStatements(down)}
	}
	return files, nil
}

// errSeveralFiles reports indexes whose migration needs several files.
var errSeveralFiles = errors.New("fts: the migration needs several files (CREATE INDEX CONCURRENTLY runs alone); use Migrations or WriteMigration")

// Migration renders the indexes as the bodies of a single up and down
// migration file, run by sqln/migrate like any other (on MySQL, connection
// enables multiStatements for the migration pool). A Concurrently index
// needs several files: use Migrations or WriteMigration.
func Migration(d driver.Dialect, indexes ...*Index) (up, down string, err error) {
	files, err := Migrations(d, indexes...)
	if err != nil {
		return "", "", err
	}
	if len(files) != 1 {
		return "", "", errSeveralFiles
	}
	return files[0].Up, files[0].Down, nil
}

// joinStatements renders a file body; an empty one still runs (golang-migrate
// executes every file), so it holds a statement that changes nothing.
func joinStatements(s []string) string {
	if len(s) == 0 {
		return "SELECT 1;\n"
	}
	return strings.Join(s, ";\n\n") + ";\n"
}

var migrationNameRe = regexp.MustCompile(`^[a-z0-9_]+$`)

// WriteMigration writes <version>_<name>.up.sql and .down.sql into dir, the
// version zero-padded to six digits, in the golang-migrate layout read by
// sqln/migrate; when Migrations renders several files, the next ones take
// the following versions and the names <name>_2, <name>_3…. Call it from a
// generator (go:generate) or a test, and commit the files: the database gets
// the index through the normal migration path. Existing files are replaced.
func WriteMigration(dir string, version uint64, name string, d driver.Dialect, indexes ...*Index) error {
	if !migrationNameRe.MatchString(name) {
		return fmt.Errorf("fts: migration name %q must be lower-case letters, digits and '_'", name)
	}
	files, err := Migrations(d, indexes...)
	if err != nil {
		return err
	}
	for i, f := range files {
		n := name
		if i > 0 {
			n = fmt.Sprintf("%s_%d", name, i+1)
		}
		base := filepath.Join(dir, fmt.Sprintf("%06d_%s", version+uint64(i), n))
		// Migrations are committed source files, readable like the rest of the tree.
		if err := os.WriteFile(base+".up.sql", []byte(f.Up), 0o644); err != nil { // #nosec G306 -- source file, no secret
			return err
		}
		if err := os.WriteFile(base+".down.sql", []byte(f.Down), 0o644); err != nil { // #nosec G306 -- source file, no secret
			return err
		}
	}
	return nil
}
