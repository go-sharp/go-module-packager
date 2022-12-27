// Copyright © 2022 The Go-Sharp Authors
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package cache

import (
	"context"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type EventType int

const (
	ModAddedEvent EventType = iota
	ModRemovedEvent
)

type ModInfo struct {
	Name    string
	Version string
}

type ModEvent struct {
	Info  ModInfo
	Event EventType
}

// NewModulManager creates a new ModulManager instance with given option parameters.
func NewModulManager(options ...ModMgrOption) (m *ModulManager, err error) {
	panic("not implemented yet")
}

// ModMgrOption configures the ModulManager instance.
type ModMgrOption func(m *ModulManager) error

func WithCachePathOption(p string) ModMgrOption {
	return func(m *ModulManager) error {
		if err := verifyCachePath(p); err != nil {
			return err
		}

		m.cachePath = p
		return nil
	}
}

// ModulManager is responsible to download and packaging modules.
type ModulManager struct {
	mux        *sync.RWMutex
	cachePath  string
	cachedMods map[string]map[ModInfo]struct{}

	watcher        fsnotify.Watcher
	cancelIndexing func()
	modCh          chan ModEvent

	doneCtx context.Context
}

func (m *ModulManager) ReIndex() {
	m.mux.Lock()
	defer m.mux.Unlock()

	if m.cancelIndexing != nil {
		return
	}

	m.cachedMods = map[string]map[ModInfo]struct{}{}

	var ctx context.Context
	ctx, m.cancelIndexing = context.WithCancel(context.Background())

	go indexCache(ctx, m.cachePath, m.modCh)
}

func (m *ModulManager) AbortIndexing() {
	m.mux.Lock()
	defer m.mux.Unlock()

	if m.cancelIndexing != nil {
		m.cancelIndexing()
		m.cancelIndexing = nil
	}
}

func (m *ModulManager) worker() {

}
