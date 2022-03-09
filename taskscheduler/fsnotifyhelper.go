package taskscheduler

import "github.com/fsnotify/fsnotify"

type FSWatcher struct {
	watcher  *fsnotify.Watcher
	counters map[string]int
}

func newFSWatcher() (*FSWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &FSWatcher{
		watcher:  watcher,
		counters: make(map[string]int),
	}, nil
}

func (h *FSWatcher) Add(path string) error {
	if _, ok := h.counters[path]; ok {
		h.counters[path] += 1
	} else {
		err := h.watcher.Add(path)
		if err != nil {
			return err
		}
		h.counters[path] = 1
	}
	return nil
}

func (h *FSWatcher) Remove(path string) error {
	if _, ok := h.counters[path]; ok {
		if h.counters[path] == 1 {
			err := h.watcher.Remove(path)
			if err != nil {
				return err
			}
			delete(h.counters, path)
		} else {
			h.counters[path] -= 1
		}
	}
	return nil
}

func (h *FSWatcher) Events() chan fsnotify.Event {
	return h.watcher.Events
}

func (h *FSWatcher) Errors() chan error {
	return h.watcher.Errors
}
