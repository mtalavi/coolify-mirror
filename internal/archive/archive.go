// Package archive implements the backup file format:
//
//	"COOLIFY-MIRROR/1\n" + age(key)( zstd( tar ) )
//
// The key of a backup is a random scrypt passphrase, or - when the server has
// a recovery key - a random X25519 identity ("AGE-SECRET-KEY-1..."): the file
// is then encrypted to that identity and to the public recovery key too, so
// it opens with either (the recovery key never leaves the person's hands).
//
// The tar stream holds a manifest, the database export and every file of the
// saved volumes, directories and images, each under its own path prefix, with
// ownership, permissions, timestamps, symlinks, hardlinks and xattrs preserved.
// Everything is streamed: no temporary copies are needed on either side.
package archive

import (
	"archive/tar"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"
)

// Magic is the plaintext first line of every backup file.
const Magic = "COOLIFY-MIRROR/1\n"

// scryptWorkFactor: the passphrase is 130 random bits, so the KDF does not
// need to be slow.
const scryptWorkFactor = 15

// Writer builds a backup file.
type Writer struct {
	f     *os.File
	bw    *bufio.Writer
	out   *countWriter
	enc   io.WriteCloser
	zw    *zstd.Encoder
	tw    *tar.Writer
	raw   atomic.Int64 // uncompressed bytes of file data written
	close bool
}

type countWriter struct {
	w io.Writer
	n atomic.Int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	return n, err
}

// IdentityPrefix starts a key that is an age X25519 identity, not a passphrase.
const IdentityPrefix = "AGE-SECRET-KEY-1"

// Create starts a new backup file at path, encrypted with key. With
// recovery recipients (age1... public keys) key must be an X25519 identity and
// the file opens with that identity or with any recovery key.
func Create(path, key string, recovery ...string) (*Writer, error) {
	recipients, err := recipientsFor(key, recovery)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	w := &Writer{f: f}
	w.out = &countWriter{w: f}
	w.bw = bufio.NewWriterSize(w.out, 4<<20)
	if _, err := w.bw.WriteString(Magic); err != nil {
		f.Close()
		return nil, err
	}
	w.enc, err = age.Encrypt(w.bw, recipients...)
	if err != nil {
		f.Close()
		return nil, err
	}
	w.zw, err = zstd.NewWriter(w.enc,
		zstd.WithEncoderLevel(zstd.SpeedDefault),
		zstd.WithEncoderConcurrency(runtime.GOMAXPROCS(0)),
		zstd.WithWindowSize(8<<20))
	if err != nil {
		f.Close()
		return nil, err
	}
	w.tw = tar.NewWriter(w.zw)
	return w, nil
}

// Written returns the number of (compressed, encrypted) bytes on disk so far.
func (w *Writer) Written() int64 { return w.out.n.Load() }

// RawWritten returns the number of uncompressed data bytes added so far.
func (w *Writer) RawWritten() int64 { return w.raw.Load() }

// AddBytes stores a small in-memory file.
func (w *Writer) AddBytes(name string, data []byte) error {
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: time.Now(),
		Typeflag: tar.TypeReg, Format: tar.FormatPAX}
	if err := w.tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := w.tw.Write(data)
	w.raw.Add(int64(len(data)))
	return err
}

// AddStream stores size bytes read from r under name.
func (w *Writer) AddStream(name string, size int64, r io.Reader, progress func(int64)) error {
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: size, ModTime: time.Now(),
		Typeflag: tar.TypeReg, Format: tar.FormatPAX}
	if err := w.tw.WriteHeader(hdr); err != nil {
		return err
	}
	n, err := io.CopyBuffer(w.tw, &progressReader{r: io.LimitReader(r, size), fn: progress, w: w}, make([]byte, 1<<20))
	if err == nil && n != size {
		err = fmt.Errorf("%s: short read (%d of %d bytes)", name, n, size)
	}
	return err
}

// AddTarStream copies every entry of a tar stream (e.g. `docker save`) under prefix/.
func (w *Writer) AddTarStream(prefix string, r io.Reader, progress func(int64)) error {
	tr := tar.NewReader(r)
	buf := make([]byte, 1<<20)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		h := *hdr
		h.Name = prefix + "/" + hdr.Name
		if h.Typeflag == tar.TypeLink {
			h.Linkname = prefix + "/" + hdr.Linkname
		}
		h.Format = tar.FormatPAX
		if err := w.tw.WriteHeader(&h); err != nil {
			return err
		}
		if _, err := io.CopyBuffer(w.tw, &progressReader{r: tr, fn: progress, w: w}, buf); err != nil {
			return err
		}
	}
}

// Close flushes all layers and closes the file.
func (w *Writer) Close() error {
	if w.close {
		return nil
	}
	w.close = true
	errs := []error{w.tw.Close(), w.zw.Close(), w.enc.Close(), w.bw.Flush(), w.f.Sync(), w.f.Close()}
	return errors.Join(errs...)
}

// Abort closes the file without finalizing it.
func (w *Writer) Abort() {
	if !w.close {
		w.close = true
		w.f.Close()
	}
}

type progressReader struct {
	r  io.Reader
	fn func(int64)
	w  *Writer
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		if p.w != nil {
			p.w.raw.Add(int64(n))
		}
		if p.fn != nil {
			p.fn(int64(n))
		}
	}
	return n, err
}

// Reader reads a backup file sequentially.
type Reader struct {
	f   *os.File
	in  *countReader
	zr  *zstd.Decoder
	tr  *tar.Reader
	cur *tar.Header
}

type countReader struct {
	r io.Reader
	n atomic.Int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// ErrBadPassphrase is returned when the key does not open the backup.
var ErrBadPassphrase = errors.New("the key does not match this backup (check the #key part of the link)")

// ErrNotBackup is returned for files that are not coolify-mirror backups.
var ErrNotBackup = errors.New("this file is not a coolify-mirror backup")

// Open opens a backup file for reading.
func Open(path, passphrase string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r := &Reader{f: f}
	r.in = &countReader{r: bufio.NewReaderSize(f, 4<<20)}
	magic := make([]byte, len(Magic))
	if _, err := io.ReadFull(r.in, magic); err != nil || string(magic) != Magic {
		f.Close()
		return nil, ErrNotBackup
	}
	id, err := identityFor(passphrase)
	if err != nil {
		f.Close()
		return nil, err
	}
	dec, err := age.Decrypt(r.in, id)
	if err != nil {
		f.Close()
		var nm *age.NoIdentityMatchError
		if errors.As(err, &nm) {
			return nil, ErrBadPassphrase
		}
		return nil, fmt.Errorf("open backup: %w", err)
	}
	r.zr, err = zstd.NewReader(dec, zstd.WithDecoderConcurrency(runtime.GOMAXPROCS(0)), zstd.WithDecoderMaxWindow(64<<20))
	if err != nil {
		f.Close()
		return nil, err
	}
	r.tr = tar.NewReader(r.zr)
	return r, nil
}

// Next advances to the next entry (io.EOF at the end).
func (r *Reader) Next() (*tar.Header, error) {
	h, err := r.tr.Next()
	r.cur = h
	return h, err
}

// Read reads the data of the current entry.
func (r *Reader) Read(p []byte) (int, error) { return r.tr.Read(p) }

// Consumed returns how many bytes of the file were read so far.
func (r *Reader) Consumed() int64 { return r.in.n.Load() }

// Close releases the file.
func (r *Reader) Close() error {
	r.zr.Close()
	return r.f.Close()
}

// ReadAllEntries is a helper that fully reads the current entry (small files only).
func (r *Reader) ReadAllEntries(max int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.tr, max))
}

// recipientsFor turns a backup key (and recovery public keys) into age recipients.
func recipientsFor(key string, recovery []string) ([]age.Recipient, error) {
	if !strings.HasPrefix(key, IdentityPrefix) {
		if len(recovery) > 0 {
			return nil, errors.New("a backup with a recovery key needs an X25519 backup key")
		}
		r, err := age.NewScryptRecipient(key)
		if err != nil {
			return nil, err
		}
		r.SetWorkFactor(scryptWorkFactor)
		return []age.Recipient{r}, nil
	}
	id, err := age.ParseX25519Identity(key)
	if err != nil {
		return nil, fmt.Errorf("backup key: %w", err)
	}
	out := []age.Recipient{id.Recipient()}
	for _, rk := range recovery {
		r, err := age.ParseX25519Recipient(strings.TrimSpace(rk))
		if err != nil {
			return nil, fmt.Errorf("recovery key %q: %w", rk, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// identityFor turns a backup key or a recovery key into an age identity.
func identityFor(key string) (age.Identity, error) {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, IdentityPrefix) {
		return age.ParseX25519Identity(key)
	}
	return age.NewScryptIdentity(key)
}

// NewIdentityKey returns a fresh X25519 identity: the key of a backup that is
// also encrypted to recovery keys, or a new recovery key.
func NewIdentityKey() (secret, public string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", err
	}
	return id.String(), id.Recipient().String(), nil
}

// PublicKeyOf returns the age1... public key of an identity key.
func PublicKeyOf(secret string) (string, error) {
	id, err := age.ParseX25519Identity(strings.TrimSpace(secret))
	if err != nil {
		return "", err
	}
	return id.Recipient().String(), nil
}

// ValidPublicKey reports whether s is an age X25519 public key (age1...).
func ValidPublicKey(s string) error {
	_, err := age.ParseX25519Recipient(strings.TrimSpace(s))
	return err
}
