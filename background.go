package main

import (
	"sync"

	log "github.com/sirupsen/logrus"
)

type BackgroundHub struct {
	channels   map[string]chan interface{}
	completeWG sync.WaitGroup
}

type BackgroundFunction func(messages chan interface{})

type stopMessage struct{}

func newNotifyHub() *BackgroundHub {
	return &BackgroundHub{
		channels: make(map[string]chan interface{}),
	}
}

func (h *BackgroundHub) StopAll() {
	for key, ch := range h.channels {
		log.Debug("stopping " + key + " ...")
		ch <- struct{}{}
	}
}

func (h *BackgroundHub) WaitAll() {
	if len(h.channels) == 0 {
		return
	}
	h.completeWG.Wait()
	for k := range h.channels {
		close(h.channels[k])
		delete(h.channels, k)
	}
}

func (h *BackgroundHub) Run(fn BackgroundFunction) {

}
