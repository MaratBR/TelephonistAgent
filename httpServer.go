package main

import (
	"encoding/json"
	"net/http"
	"sync"
)

type Server struct {
	scheduler  *ApplicationScheduler
	isRunning  bool
	mut        sync.Mutex
	httpServer *http.Server
}

type ServerOptions struct {
	Scheduler *ApplicationScheduler
}

func NewServer(opts *ServerOptions) *Server {
	return &Server{scheduler: opts.Scheduler}
}

func (s *Server) Start() {
	s.mut.Lock()
	defer s.mut.Unlock()
	if s.isRunning {
		return
	}
	s.isRunning = true
	s.httpServer = &http.Server{Addr: "127.0.0.1:25864", Handler: s}
	go s.httpServer.ListenAndServe()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	w.Header().Add("content-type", "application/json")

	clientInfo := map[string]interface{}{}

	if s.scheduler.wsc == nil {
		clientInfo["HasWSC"] = false
	} else {
		clientInfo["HasWSC"] = true
		clientInfo["ConnectionID"] = s.scheduler.wsc.ConnectionID
		clientInfo["InstanceID"] = s.scheduler.InstanceID
		clientInfo["IsConnected"] = s.scheduler.wsc.IsConnected()
		clientInfo["IsStarted"] = s.scheduler.wsc.IsStarted()
		lastError := s.scheduler.wsc.GetLastError()
		if lastError == nil {
			clientInfo["WSC_LastError"] = lastError
		} else {
			clientInfo["WSC_LastError"] = lastError.Error()
		}
	}

	data := map[string]interface{}{
		"Config": map[string]interface{}{
			"Path":  s.scheduler.config.file.filepath,
			"Value": s.scheduler.config.Value,
		},
		"Executor":      s.scheduler.executor.Explain(),
		"TaskScheduler": s.scheduler.taskScheduler.Explain(),
		"Client":        clientInfo,
	}

	err := encoder.Encode(data)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
	}
}

func (s *Server) Stop() {
	if s.httpServer != nil {
		s.httpServer.Close()
		s.httpServer = nil
	}
}
