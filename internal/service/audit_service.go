package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
)


type AuditEvent struct {
	TS      int64  `json:"ts"`
	Action  string `json:"action"`
	UserID  string `json:"user_id"`
	URL     string `json:"url"`
}

type Observer interface {
	OnAudit(event AuditEvent)
}


// file
type FileObserver struct {
	file *os.File
	mu       sync.RWMutex
}

func NewFileObserver(path string) (*FileObserver, error) {
	if path == "" {
		return nil, nil
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open/create log file: %w", err)
	}

	return &FileObserver{file: file}, nil
}

func (f *FileObserver) OnAudit(event AuditEvent) {
	if f == nil || f.file == nil {
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("AUDIT FILE ERROR Failed to marshal: %v", err)
		return
	}

	data = append(data, '\n')
	if _, err := f.file.Write(data); err != nil {
		log.Printf("AUDIT FILE ERROR Failed to write: %v", err)
	}
}

// API
type APIObserver struct {
	url string
}

func NewAPIObserver(url string) (*APIObserver, error) {
	if url == "" {
		return nil, nil
	}

	return &APIObserver{url: url}, nil
}

func (h *APIObserver) OnAudit(event AuditEvent) {
	if h == nil || h.url == "" {
		return
	}

	jsonData, err := json.Marshal(event)
	if err != nil {
		log.Printf("AUDIT HTTP ERROR Marshal error: %v", err)
		return
	}

	resp, err := http.Post(h.url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("AUDIT HTTP ERROR Request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("AUDIT HTTP ERROR Remote server returned status: %d", resp.StatusCode)
	}
}

// service
type AuditService struct {
	observers []Observer
	mu        sync.RWMutex
}

func NewAuditService() *AuditService {
	return &AuditService{}
}

func (a *AuditService) Attach(obs Observer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.observers = append(a.observers, obs)
}

func (a *AuditService) Notify(event AuditEvent) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, obs := range a.observers {
		obs.OnAudit(event)
	}
}
