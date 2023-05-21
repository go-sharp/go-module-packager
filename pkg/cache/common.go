// Copyright © 2022 The Go-Sharp Authors
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package cache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-sharp/go-module-packager/pkg/log"
	"golang.org/x/mod/modfile"
)

const pkgName = "pkg/cache"
const logPrefix = "[" + pkgName + "]"

var (
	errInvalidCacheDir      = errors.New(pkgName + ": cache path must be a writeable directory")
	errIndexIndexingRunning = errors.New(pkgName + ": indexing is already running")
	errInstanceClosed       = errors.New(pkgName + ": instance already closed")
)

func verifyCachePath(p string) (err error) {
	var fi fs.FileInfo
	if fi, err = os.Stat(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %v", errInvalidCacheDir, err)
	}

	// Create directory because it does not exist yet
	if fi == nil {
		if err = os.MkdirAll(p, 0770); err != nil {
			return err
		}

		if fi, err = os.Stat(p); err != nil {
			return fmt.Errorf("%w: %v", errInvalidCacheDir, err)
		}
	}

	if !fi.IsDir() {
		return errInvalidCacheDir
	}

	var (
		fp = filepath.Join(p, "cache-"+strconv.FormatInt(time.Now().Unix(), 10))
		f  *os.File
	)

	if f, err = os.OpenFile(fp, os.O_CREATE|os.O_WRONLY, 0600); err != nil {
		return fmt.Errorf("%w: %v", errInvalidCacheDir, err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(fp)
	}()

	return nil
}

func indexCache(ctx context.Context, path string, modCh chan<- ModEvent) {
	var (
		ok   bool
		modF *modfile.File
	)
	log.Info().Println(logPrefix, "indexing cache directory:", path)

	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		// Check if processing should be terminated
		select {
		case <-ctx.Done():
			log.Info().Println(logPrefix, "cache indexing aborted")
			return errors.New(pkgName + ": cache indexing aborted")
		default:
		}

		// Ignore all error and go on with indexing
		if err != nil {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if strings.ToLower(filepath.Ext(d.Name())) != ".mod" {
			return nil
		}

		evt := ModEvent{Event: ModAddedEvent}
		if evt.Info.Version, modF, ok = getModulFileAndVersion(path); !ok {
			// Parsing of mod file failed, therefore skip this module
			return nil
		}

		if modF.Module == nil {
			return nil
		}
		evt.Info.Name = modF.Module.Mod.Path

		modCh <- evt

		return nil
	})

	log.Info().Println(logPrefix, "indexing done for directory:", path)
}

func getModulFileAndVersion(path string) (version string, modF *modfile.File, ok bool) {
	fNameNoExt, ext := trimExt(path), filepath.Ext(path)
	if fNameNoExt == "" || (ext != ".mod" && ext != ".zip") {
		return version, nil, false
	}

	var (
		err     error
		modPath = path
		modData []byte
	)

	if ext == ".mod" {
		if _, err = os.Stat(fNameNoExt + ".zip"); err != nil {
			return version, nil, false
		}
	} else {
		modPath = fNameNoExt + ".mod"
		if _, err = os.Stat(modPath); err != nil {
			return version, nil, false
		}
	}

	if modData, err = os.ReadFile(modPath); err != nil {
		return version, nil, false
	}

	if modF, err = modfile.Parse("", modData, nil); err != nil || modF.Module == nil {
		return version, nil, false
	}

	return filepath.Base(fNameNoExt), modF, true
}

func trimExt(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path))
}

func isCtxDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}
