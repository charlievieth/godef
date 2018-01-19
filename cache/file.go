package cache

import (
	"bytes"
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

func (r *reader) Read(b []byte) (n int, err error) {
	if r.i >= int64(len(r.s)) {
		return 0, io.EOF
	}
	n = copy(b, r.s[r.i:])
	r.i += int64(n)
	return
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
	maxSize int64
	cache   lru.Cache
}

func (c *File) maxEntries(_ *lru.Cache) bool {
	return c.maxSize > 0 && c.size >= c.maxSize
}

func (c *File) onAdded(key lru.Key, value interface{}) {
	c.size += value.(*fileEntry).size
}

func (c *File) onEvicted(key lru.Key, value interface{}) {
	c.size -= value.(*fileEntry).size
}

func (c *File) lazyInit() {
	if c.maxSize > 0 && c.cache.MaxEntries == nil {
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
