package bucket

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// Folders do not exist in object storage: they are key prefixes ending in
// "/". An empty folder is a marker object with an empty body and the key
// "path/".

// Dir is one folder level.
type Dir struct {
	Prefix  string
	Folders []string // full prefixes ("docs/sub/"), sorted, never nil
	Objects []Object // objects of this level only, never nil
}

// DirLister is implemented by stores that list one level server-side.
type DirLister interface {
	ListDir(ctx context.Context, prefix string) (Dir, error)
}

// ListDir uses DirLister when s implements it; otherwise it walks All(prefix)
// and groups keys by the next "/". The folder's own marker (key == prefix)
// is omitted. prefix is normally "" or ends with "/".
func ListDir(ctx context.Context, s Store, prefix string) (Dir, error) {
	if l, ok := s.(DirLister); ok {
		return l.ListDir(ctx, prefix)
	}
	d := Dir{Prefix: prefix, Folders: make([]string, 0), Objects: make([]Object, 0)}
	seen := map[string]bool{}
	for o, err := range All(ctx, s, prefix) {
		if err != nil {
			return Dir{}, err
		}
		rest := strings.TrimPrefix(o.Key, prefix)
		if rest == "" {
			continue
		}
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			if f := prefix + rest[:i+1]; !seen[f] {
				seen[f] = true
				d.Folders = append(d.Folders, f)
			}
			continue
		}
		d.Objects = append(d.Objects, o)
	}
	slices.Sort(d.Folders)
	slices.SortFunc(d.Objects, func(a, b Object) int { return strings.Compare(a.Key, b.Key) })
	return d, nil
}

// BatchDeleter is implemented by stores that delete many keys per call.
// Missing keys are not an error, as with Delete.
type BatchDeleter interface {
	DeleteMany(ctx context.Context, keys []string) error
}

// deleteBatch is the most keys DeletePrefix hands DeleteMany at once: the S3
// DeleteObjects limit.
const deleteBatch = 1000

// DeletePrefix deletes every object under prefix ("" = the whole bucket)
// and returns how many were deleted. It uses BatchDeleter when available,
// Delete one by one otherwise, and stops at the first error.
//
// Keys are deleted deepest first, together with the implied folder markers
// of the subtree (deleting a missing key is a no-op), so stores that model
// folders natively (e.g. a filesystem) end up without the subtree too. The
// count only includes the objects the listing returned.
func DeletePrefix(ctx context.Context, s Store, prefix string) (int, error) {
	listed := map[string]bool{}
	for o, err := range All(ctx, s, prefix) {
		if err != nil {
			return 0, err
		}
		listed[o.Key] = true
	}
	keys := make([]string, 0, len(listed))
	implied := map[string]bool{}
	for k := range listed {
		keys = append(keys, k)
		// Every "/" from the end of prefix on closes a folder of the subtree,
		// prefix itself included when it ends with "/".
		for i := max(len(prefix)-1, 0); i < len(k)-1; i++ {
			if f := k[:i+1]; k[i] == '/' && !listed[f] && !implied[f] {
				implied[f] = true
				keys = append(keys, f)
			}
		}
	}
	// Reverse lexical order puts every key after its descendants.
	slices.Sort(keys)
	slices.Reverse(keys)

	n := 0
	if b, ok := s.(BatchDeleter); ok {
		for chunk := range slices.Chunk(keys, deleteBatch) {
			if err := b.DeleteMany(ctx, chunk); err != nil {
				return n, err
			}
			for _, k := range chunk {
				if listed[k] {
					n++
				}
			}
		}
		return n, nil
	}
	for _, k := range keys {
		if err := s.Delete(ctx, k); err != nil {
			return n, err
		}
		if listed[k] {
			n++
		}
	}
	return n, nil
}

// FolderMaker is implemented by stores where an empty "path/" object is not
// the native way to create a folder (e.g. a filesystem).
type FolderMaker interface {
	CreateFolder(ctx context.Context, path string) error
}

// CreateFolder makes path ("a/b" or "a/b/") show up in ListDir while empty:
// FolderMaker when available, otherwise Put of an empty "path/" marker.
// FolderMaker receives the normalized "a/b/" form. An empty path returns
// ErrInvalidConfig.
func CreateFolder(ctx context.Context, s Store, path string) error {
	p := strings.Trim(path, "/")
	if p == "" {
		return fmt.Errorf("%w: folder path is required", ErrInvalidConfig)
	}
	p += "/"
	if m, ok := s.(FolderMaker); ok {
		return m.CreateFolder(ctx, p)
	}
	return s.Put(ctx, PutInput{Key: p, Body: strings.NewReader(""), Size: 0})
}
