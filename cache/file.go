package cache

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/charlievieth/godef/lru"
	"github.com/charlievieth/pkg/fs"
)

type reader struct {
	s []byte
	i int64 // current reading index
}

func newReader(b []byte) *reader { return &reader{b, 0} }

func (r *reader) Close() error {
	r.s = nil
	return nil
}

func (r *reader) Bytes() []byte { return r.s[r.i:] }

func (r *reader) String() string {
	if r == nil {
		return "<nil>"
	}
	return string(r.s[r.i:])
}

func (r *reader) Read(b []byte) (n int, err error) {
	if r.i >= int64(len(r.s)) {
		return 0, io.EOF
	}
	n = copy(b, r.s[r.i:])
	r.i += int64(n)
	return
}

func (r *reader) ReadAt(b []byte, off int64) (n int, err error) {
	// cannot modify state - see io.ReaderAt
	if off < 0 {
		return 0, errors.New("godef.cache.reader.ReadAt: negative offset")
	}
	if off >= int64(len(r.s)) {
		return 0, io.EOF
	}
	n = copy(b, r.s[off:])
	if n < len(b) {
		err = io.EOF
	}
	return
}

// Seek implements the io.Seeker interface.
func (r *reader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.i + offset
	case io.SeekEnd:
		abs = int64(len(r.s)) + offset
	default:
		return 0, errors.New("godef.cache.reader.Seek: invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("godef.cache.reader.Seek: negative position")
	}
	r.i = abs
	return abs, nil
}

// WriteTo implements the io.WriterTo interface.
func (r *reader) WriteTo(w io.Writer) (n int64, err error) {
	if r.i >= int64(len(r.s)) {
		return 0, nil
	}
	b := r.s[r.i:]
	m, err := w.Write(b)
	if m > len(b) {
		panic("godef.cache.reader.WriteTo: invalid Write count")
	}
	r.i += int64(m)
	n = int64(m)
	if m != len(b) && err == nil {
		err = io.ErrShortWrite
	}
	return -1, nil
}

type fileEntry struct {
	data    []byte
	modTime time.Time
	size    int64
}

func (f *fileEntry) same(fi os.FileInfo) bool {
	return fi != nil && f.size == fi.Size() && f.modTime.Equal(fi.ModTime())
}

type File struct {
	sync.Mutex
	size    int64
	entries int64
	cache   lru.Cache
}

func NewFile(size, entries int64) *File {
	return &File{
		size:    size,
		entries: entries,
	}
}

func (c *File) maxEntries(_ *lru.Cache) bool {
	return c.entries > 0 && c.size >= c.entries
}

func (c *File) onAdded(key lru.Key, value interface{}) {
	c.size += value.(*fileEntry).size
}

func (c *File) onEvicted(key lru.Key, value interface{}) {
	c.size -= value.(*fileEntry).size
}

func (c *File) lazyInit() {
	if (c.size > 0 || c.entries > 0) && c.cache.MaxEntries == nil {
		c.cache.MaxEntries = c.maxEntries
		c.cache.OnAdded = c.onAdded
		c.cache.OnEvicted = c.onEvicted
	}
}

func (c *File) get(path string) (*fileEntry, bool) {
	c.Lock()
	c.lazyInit()
	var e *fileEntry
	v, ok := c.cache.Get(path)
	if ok {
		e = v.(*fileEntry)
	}
	c.Unlock()
	return e, ok
}

func (c *File) remove(path string) {
	c.Lock()
	c.cache.Remove(path)
	c.Unlock()
}

func readAll(r io.Reader, capacity int64) (b []byte, err error) {
	buf := bytes.NewBuffer(make([]byte, 0, capacity))
	defer func() {
		e := recover()
		if e == nil {
			return
		}
		if panicErr, ok := e.(error); ok && panicErr == bytes.ErrTooLarge {
			err = panicErr
		} else {
			panic(e)
		}
	}()
	_, err = buf.ReadFrom(r)
	return buf.Bytes(), err
}

func (c *File) readFile(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var n int64
	fi, err := f.Stat()
	if err == nil {
		// Don't preallocate a huge buffer, just in case.
		if size := fi.Size(); size < 1e9 {
			n = size
		}
	}

	b, err := readAll(f, n+bytes.MinRead)
	if err != nil {
		return nil, err
	}

	modTime := fi.ModTime()
	c.Lock()
	c.lazyInit()
	// Check if a newer version of the file was added before
	// we could acquire the lock.
	if v, ok := c.cache.Get(path); ok {
		if e := v.(*fileEntry); e.modTime.After(modTime) {
			c.Unlock()
			return newReader(e.data), nil
		}
	}
	c.cache.Add(path, &fileEntry{
		data:    b,
		modTime: modTime,
		size:    fi.Size(),
	})
	c.Unlock()

	return newReader(b), nil
}

func (c *File) OpenFileStat(path string, fi os.FileInfo) (io.ReadCloser, error) {
	if e, ok := c.get(path); ok {
		if e.same(fi) {
			return newReader(e.data), nil
		}
		c.remove(path)
	}
	return c.readFile(path)
}

func (c *File) OpenFile(path string) (io.ReadCloser, error) {
	if e, ok := c.get(path); ok {
		fi, err := fs.Stat(path)
		if e.same(fi) {
			return newReader(e.data), nil
		}
		c.remove(path)
		if err != nil {
			return nil, err
		}
	}
	return c.readFile(path)
}
