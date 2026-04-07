package socialpluginregistry

import (
	"sort"
	"sync"

	"qorvexus/internal/socialplugin"
)

type Factory func() socialplugin.Plugin

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

func Register(factory Factory) {
	if factory == nil {
		return
	}
	plugin := factory()
	if plugin == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	factories[plugin.Channel()] = factory
}

func NewManager() *socialplugin.Manager {
	manager := socialplugin.NewManager()
	for _, factory := range Registered() {
		manager.Register(factory())
	}
	return manager
}

func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func Registered() []Factory {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Factory, 0, len(names))
	for _, name := range names {
		out = append(out, factories[name])
	}
	return out
}
