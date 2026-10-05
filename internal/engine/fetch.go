package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mtalavi/coolify-mirror/internal/archive"
	"github.com/mtalavi/coolify-mirror/internal/dbx"
	"github.com/mtalavi/coolify-mirror/internal/transfer"
)

// Fetched is a backup that is available locally, verified and inspected.
type Fetched struct {
	Path     string
	Key      string
	Manifest *Manifest
	Export   *dbx.Export // selective backups only
	Size     int64
	// Downloaded is true when the file was fetched from a link (and can be
	// deleted after a successful restore; the source still has the original).
	Downloaded bool
}

// SetDone sets the processed bytes of a step (for absolute progress sources).
func (s *Step) SetDone(n int64) {
	s.p.mu.Lock()
	s.Done = n
	s.p.mu.Unlock()
}

// Fetch downloads the backup (when source is a link), verifies it completely
// and reads its manifest. key may be empty when the link carries #key=.
func Fetch(ctx context.Context, source, key string, pr *Progress) (f *Fetched, err error) {
	loc, linkKey, isURL, err := transfer.ParseSource(source)
	if err != nil {
		return nil, err
	}
	if key == "" {
		key = linkKey
	}
	if key == "" && !isURL {
		if b, err := os.ReadFile(loc + ".key"); err == nil {
			key = strings.TrimSpace(string(b))
		}
	}
	if key == "" {
		return nil, errors.New("the link has no key - paste the complete link including the #key=… part")
	}
	local := loc
	if isURL {
		if err := os.MkdirAll(IncomingDir, 0o700); err != nil {
			return nil, err
		}
		u, _ := url.Parse(loc)
		token := path.Base(path.Dir(u.Path))
		if token == "" || token == "." || token == "/" {
			token = "download"
		}
		local = filepath.Join(IncomingDir, "backup-"+token+".cmb")
		st := pr.Add("Download backup", 0)
		st.Begin(u.Host)
		if _, err := os.Stat(local); err == nil {
			st.Finish("already downloaded")
		} else {
			if size, err := transfer.RemoteSize(ctx, loc); err == nil && size > 0 {
				st.SetWeight(size)
				if free := FreeSpace(IncomingDir); free >= 0 && free < size+(512<<20) {
					err := fmt.Errorf("not enough disk space: the backup is %s but only %s is free in %s", HumanBytes(size), HumanBytes(free), IncomingDir)
					st.Fail(err)
					return nil, err
				}
			}
			err := transfer.Download(ctx, loc, local, func(done, total int64) {
				if total > 0 {
					st.SetWeight(total)
				}
				st.SetDone(done)
			})
			if err != nil {
				st.Fail(err)
				return nil, err
			}
			fi, _ := os.Stat(local)
			st.Finish(HumanBytes(fi.Size()))
		}
	}
	fi, err := os.Stat(local)
	if err != nil {
		return nil, err
	}
	sv := pr.Add("Verify backup (encryption + checksums)", fi.Size())
	sv.Begin("")
	if err := VerifyFile(ctx, local, key, sv.Advance); err != nil {
		sv.Fail(err)
		if errors.Is(err, archive.ErrBadPassphrase) || errors.Is(err, archive.ErrNotBackup) {
			return nil, err
		}
		if isURL {
			_ = os.Remove(local) // corrupt download: fetch again next time
		}
		return nil, fmt.Errorf("the backup file is damaged or incomplete: %w", err)
	}
	sv.Finish("OK")
	man, ex, err := Inspect(local, key)
	if err != nil {
		return nil, err
	}
	return &Fetched{Path: local, Key: key, Manifest: man, Export: ex, Size: fi.Size(), Downloaded: isURL}, nil
}

// Inspect reads the manifest (and the selective export) from the start of a backup.
func Inspect(p, key string) (*Manifest, *dbx.Export, error) {
	r, err := archive.Open(p, key)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()
	var man *Manifest
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		switch h.Name {
		case entryManifest:
			b, err := r.ReadAllEntries(64 << 20)
			if err != nil {
				return nil, nil, err
			}
			man = &Manifest{}
			if err := json.Unmarshal(b, man); err != nil {
				return nil, nil, fmt.Errorf("bad manifest: %w", err)
			}
			if man.Format != FormatName {
				return nil, nil, fmt.Errorf("unsupported backup format %q (this tool reads %s)", man.Format, FormatName)
			}
			if man.Mode == ModeFull {
				return man, nil, nil
			}
		case entryExport:
			if man == nil {
				return nil, nil, errors.New("backup has no manifest")
			}
			b, err := r.ReadAllEntries(4 << 30)
			if err != nil {
				return nil, nil, err
			}
			ex := &dbx.Export{}
			if err := jsonNumberUnmarshal(b, ex); err != nil {
				return nil, nil, fmt.Errorf("bad database export: %w", err)
			}
			return man, ex, nil
		default:
			return nil, nil, fmt.Errorf("unexpected entry %q before the configuration", h.Name)
		}
	}
	return nil, nil, errors.New("backup is incomplete (no configuration found)")
}

func jsonNumberUnmarshal(b []byte, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	return dec.Decode(v)
}
