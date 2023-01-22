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

func (m ModInfo) ID() string {
	return m.Name + "@" + m.Version
}

type ModEvent struct {
	Info  ModInfo
	Event EventType
}

type ModEventListener chan<- ModEvent

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
	cacheCount int

	watcher        fsnotify.Watcher
	cancelIndexing func()
	modCh          chan ModEvent
	listeners      []ModEventListener

	doneCtx context.Context
}

func (m *ModulManager) AddListeners(listeners ...ModEventListener) {
	m.mux.Lock()
	defer m.mux.Unlock()

	m.listeners = append(m.listeners, listeners...)
}

func (m ModulManager) GetModuleInfos() (modInfos []ModInfo) {
	m.mux.RLock()
	defer m.mux.RUnlock()

	modInfos = make([]ModInfo, 0, m.cacheCount)
	for _, versions := range m.cachedMods {
		for v := range versions {
			modInfos = append(modInfos, v)
		}
	}

	return modInfos
}

// ReIndex clears current modul cache and starts the re-index process.
// This functions blocks until indexing is done. It returns an error
// if reindexing is already in progress or instance has been closed.
func (m *ModulManager) ReIndex() (err error) {
	m.mux.Lock()

	if m.cancelIndexing != nil {
		m.mux.Unlock()
		return errIndexIndexingRunning
	}

	if isCtxDone(m.doneCtx) {
		m.mux.Unlock()
		return errInstanceClosed
	}

	m.cachedMods = map[string]map[ModInfo]struct{}{}
	m.cacheCount = 0

	var ctx context.Context
	ctx, m.cancelIndexing = context.WithCancel(context.Background())

	cachePath, modCh := m.cachePath, m.modCh
	// End of protected part
	m.mux.Unlock()

	// Start actual indexing process
	indexCache(ctx, cachePath, modCh)

	m.mux.Lock()
	defer m.mux.Unlock()

	m.cancelIndexing = nil
	return ctx.Err()
}

func (m *ModulManager) AbortIndexing() {
	m.mux.RLock()
	defer m.mux.RUnlock()

	if m.cancelIndexing != nil {
		m.cancelIndexing()
	}
}

func (m ModulManager) IsIndexing() bool {
	m.mux.RLock()
	defer m.mux.RUnlock()

	return m.cancelIndexing != nil
}

func (m *ModulManager) worker() {
	var (
		ok     bool
		modMap map[ModInfo]struct{}
	)

LOOP:
	for {
		select {
		case <-m.doneCtx.Done():
			break LOOP
		case evt := <-m.modCh:
			m.mux.Lock()
			switch evt.Event {
			case ModAddedEvent:
				if modMap, ok = m.cachedMods[evt.Info.Name]; !ok {
					m.cachedMods[evt.Info.ID()] = map[ModInfo]struct{}{}
				}

				if _, ok = modMap[evt.Info]; !ok {
					modMap[evt.Info] = struct{}{}
					m.cacheCount++
					m.publishEvent(evt)
				}
			case ModRemovedEvent:
				if modMap, ok = m.cachedMods[evt.Info.Name]; !ok || len(modMap) == 0 {
					// No modul in cache so break select statement and wait for next event
					break // breaks select statement
				}

				if _, ok = modMap[evt.Info]; ok {
					m.cacheCount--
					delete(modMap, evt.Info)
					m.publishEvent(evt)
				}
			default:
			}
			m.mux.Unlock()
		}
	}
}

func (m ModulManager) publishEvent(evt ModEvent) {
	for i := range m.listeners {
		select {
		case m.listeners[i] <- evt:
		default:
		}
	}
}
