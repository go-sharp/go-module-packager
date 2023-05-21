// Copyright © 2022 The Go-Sharp Authors
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.
package cache

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/go-sharp/go-module-packager/pkg/log"
)

const downladPath = "cache" + string(filepath.Separator) + "download"

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
	var modPath []byte
	if modPath, err = exec.Command("go", "env", "GOMODCACHE").Output(); err != nil {
		return nil, fmt.Errorf("%v: %w", pkgName, err)
	}

	m = &ModulManager{
		mux:        &sync.RWMutex{},
		cachePath:  string(modPath),
		cachedMods: map[string]map[ModInfo]struct{}{},
		cacheCount: 0,
		modCh:      make(chan ModEvent, 10),
		listeners:  nil,
	}
	m.doneCtx, m.cancelFunc = context.WithCancel(context.Background())

	// Setup file monitor
	monitorPath := filepath.Join(m.cachePath, downladPath)
	_ = os.MkdirAll(monitorPath, 0770)

	if m.watcher, err = fsnotify.NewWatcher(); err != nil {
		return nil, fmt.Errorf("%v: failed to create file watcher: %w", pkgName, err)
	}

	if err = m.watcher.Add(monitorPath); err != nil {
		return nil, fmt.Errorf("%v: failed to monitor cache directory: %w", pkgName, err)
	}

	// Setup file manager worker loop
	go func() {
		log.Debug().Println(logPrefix, "starting file monitor")
		defer func() { log.Debug().Println(logPrefix, "file monitor stopped") }()

		for {
			select {
			case e := <-m.watcher.Events:
				// Only interested in zip and module files
				if !(strings.EqualFold(filepath.Ext(e.Name), ".mod") || strings.EqualFold(filepath.Ext(e.Name), ".zip")) {
					continue
				}
				log.Debug().Println(logPrefix, "cache files changed:", e)

				var name, version string
				var ok bool
				if name, version, ok = getModuleNameAndVersion(e.Name, monitorPath); !ok {
					log.Error().Println(logPrefix, "failed to get name and version")
					continue
				}

				event := ModEvent{
					Info: ModInfo{
						Name:    name,
						Version: version,
					},
					Event: ModAddedEvent,
				}

				if e.Has(fsnotify.Remove) {
					event.Event = ModRemovedEvent
				}

				m.modCh <- event
			case e := <-m.watcher.Errors:
				log.Error().Println(logPrefix, "file monitor error:", e)
			case <-m.doneCtx.Done():
				return
			}
		}

	}()

	// Start worker and index process
	go m.worker()
	go m.ReIndex()

	return m, nil
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

	watcher        *fsnotify.Watcher
	cancelIndexing func()
	modCh          chan ModEvent
	listeners      []ModEventListener

	doneCtx    context.Context
	cancelFunc context.CancelFunc
}

// Close dispose this [ModulManager] instance and closes all registered listeners.
func (m *ModulManager) Close() {
	if m == nil || isCtxDone(m.doneCtx) {
		return
	}
	m.mux.Lock()
	defer m.mux.Unlock()

	_ = m.watcher.Close()

	for i := range m.listeners {
		close(m.listeners[i])
	}

	m.cancelFunc()
}

// AddListeners adds a listener which will receive events from the cache.
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

// AbortIndexing
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
	log.Debug().Println(logPrefix, "starting cache worker")
	defer func() { log.Debug().Println(logPrefix, "cache worker stopped") }()

LOOP:
	for {
		select {
		case <-m.doneCtx.Done():
			break LOOP
		case evt := <-m.modCh:
			m.mux.Lock()
			log.Debug().Println(logPrefix, "received event", evt)
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

	m.mux.Lock()
	defer m.mux.Unlock()

	m.cachedMods = map[string]map[ModInfo]struct{}{}
	m.cacheCount = 0
}

func (m ModulManager) publishEvent(evt ModEvent) {
	log.Debug().Println(logPrefix, "publishing event to listeners", evt)
	for i := range m.listeners {
		select {
		case m.listeners[i] <- evt:
		default:
		}
	}
}
