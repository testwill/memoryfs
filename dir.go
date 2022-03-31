package memoryfs

import (
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

var separator = "/"

type dir struct {
	sync.RWMutex
	info  fileinfo
	dirs  map[string]*dir
	files map[string]*file
}

func (d *dir) Open(name string) (fs.File, error) {

	if name == "" || name == "." {
		return d, nil
	}

	if f, err := d.getFile(name); err == nil {
		return f.openR(), nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if f, err := d.getDir(name); err == nil {
		return f, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	return nil, fmt.Errorf("no such file or directory: %s: %w", name, fs.ErrNotExist)
}

func (d *dir) Stat() (fs.FileInfo, error) {
	d.RLock()
	defer d.RUnlock()
	return d.info, nil
}

func (d *dir) getFile(name string) (*file, error) {

	parts := strings.Split(name, separator)
	if len(parts) == 1 {
		d.RLock()
		f, ok := d.files[name]
		d.RUnlock()
		if ok {
			return f, nil
		}
		return nil, fs.ErrNotExist
	}

	sub, err := d.getDir(parts[0])
	if err != nil {
		return nil, err
	}

	return sub.getFile(strings.Join(parts[1:], separator))
}

func (d *dir) getDir(name string) (*dir, error) {

	if name == "" {
		return d, nil
	}

	parts := strings.Split(name, separator)

	d.RLock()
	f, ok := d.dirs[parts[0]]
	d.RUnlock()
	if ok {
		return f.getDir(strings.Join(parts[1:], separator))
	}

	return nil, fs.ErrNotExist
}

func (d *dir) ReadDir(name string) ([]fs.DirEntry, error) {

	if name == "" {
		var entries []fs.DirEntry
		d.RLock()
		for _, file := range d.files {
			stat, _ := file.openR().Stat()
			entries = append(entries, stat.(fs.DirEntry))
		}
		for _, dir := range d.dirs {
			stat, _ := dir.Stat()
			entries = append(entries, stat.(fs.DirEntry))
		}
		d.RUnlock()
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		return entries, nil
	}

	parts := strings.Split(name, separator)

	d.RLock()
	dir, ok := d.dirs[parts[0]]
	d.RUnlock()
	if !ok {
		return nil, fs.ErrNotExist
	}
	return dir.ReadDir(strings.Join(parts[1:], separator))
}

func (f *dir) Read(_ []byte) (int, error) {
	return 0, fs.ErrInvalid
}

func (f *dir) Close() error {
	return nil
}

func (f *dir) MkdirAll(path string, perm fs.FileMode) error {
	parts := strings.Split(path, separator)

	f.RLock()
	_, ok := f.files[parts[0]]
	f.RUnlock()
	if ok {
		return fs.ErrExist
	}

	f.Lock()
	if _, ok := f.dirs[parts[0]]; !ok {
		f.dirs[parts[0]] = &dir{
			info: fileinfo{
				name:     parts[0],
				size:     0x100,
				modified: time.Now(),
				isDir:    true,
				mode:     perm,
			},
			dirs:  map[string]*dir{},
			files: map[string]*file{},
		}
	}
	f.info.modified = time.Now()
	f.Unlock()

	if len(parts) == 1 {
		return nil
	}

	f.RLock()
	defer f.RUnlock()
	return f.dirs[parts[0]].MkdirAll(strings.Join(parts[1:], separator), perm)
}

func (f *dir) WriteFile(path string, data []byte, perm fs.FileMode) error {
	parts := strings.Split(path, separator)

	if len(parts) == 1 {
		max := bufferSize
		if len(data) > max {
			max = len(data)
		}
		buffer := make([]byte, len(data), max)
		copy(buffer, data)
		f.Lock()
		defer f.Unlock()
		if existing, ok := f.files[parts[0]]; ok {
			if err := existing.overwrite(buffer, perm); err != nil {
				return err
			}
		} else {
			f.files[parts[0]] = &file{
				info: fileinfo{
					name:     parts[0],
					size:     int64(len(buffer)),
					modified: time.Now(),
					isDir:    false,
					mode:     perm,
				},
				content: buffer,
			}
		}
		return nil
	}

	f.RLock()
	_, ok := f.dirs[parts[0]]
	f.RUnlock()
	if !ok {
		return fs.ErrNotExist
	}

	f.RLock()
	defer f.RUnlock()
	return f.dirs[parts[0]].WriteFile(strings.Join(parts[1:], separator), data, perm)
}
