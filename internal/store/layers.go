package store

import (
	"io/fs"
	"sort"
)

// layered presents several flat migration directories as one, so the shared
// `common` migrations and one dialect's own migrations form a single goose
// source. The layers hold files only, and a name may appear in one layer only.
type layered []fs.FS

func (l layered) Open(name string) (fs.File, error) {
	for _, layer := range l {
		if f, err := layer.Open(name); err == nil {
			return f, nil
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (l layered) ReadFile(name string) ([]byte, error) {
	for _, layer := range l {
		if b, err := fs.ReadFile(layer, name); err == nil {
			return b, nil
		}
	}
	return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
}

func (l layered) ReadDir(name string) ([]fs.DirEntry, error) {
	var all []fs.DirEntry
	for _, layer := range l {
		entries, err := fs.ReadDir(layer, name)
		if err != nil {
			return nil, err
		}
		all = append(all, entries...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name() < all[j].Name() })
	return all, nil
}
