package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


func TestAuditService(t *testing.T) {
	t.Run("create service", func(t *testing.T) {
		service := NewAuditService()
		assert.NotNil(t, service)
		assert.Empty(t, service.observers)
	})

	t.Run("attach and notify", func(t *testing.T) {
		service := NewAuditService()
		observer := &mockObserver{}

		service.Attach(observer)
		assert.Len(t, service.observers, 1)

		event := AuditEvent{
			TS:     time.Now().Unix(),
			Action: "test",
			UserID: "user",
			URL:    "url",
		}

		service.Notify(event)
		assert.True(t, observer.called)
		assert.Equal(t, event, observer.lastEvent)
	})

	t.Run("concurrent access", func(t *testing.T) {
		service := NewAuditService()
		var wg sync.WaitGroup

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				service.Attach(&mockObserver{})
			}()
		}

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				service.Notify(AuditEvent{})
			}()
		}

		wg.Wait()
		assert.Len(t, service.observers, 10)
	})
}


func TestFileObserver(t *testing.T) {
	t.Run("create with empty path", func(t *testing.T) {
		obs, err := NewFileObserver("")
		assert.NoError(t, err)
		assert.Nil(t, obs)
	})

	t.Run("create with valid path", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "audit.log")
		obs, err := NewFileObserver(tmpFile)
		require.NoError(t, err)
		require.NotNil(t, obs)
		defer obs.file.Close()

		_, err = os.Stat(tmpFile)
		assert.NoError(t, err)
	})

	t.Run("write event", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "audit.log")
		obs, err := NewFileObserver(tmpFile)
		require.NoError(t, err)
		defer obs.file.Close()

		event := AuditEvent{
			TS:     123456789,
			Action: "shorten",
			UserID: "user123",
			URL:    "https://example.com",
		}

		obs.OnAudit(event)

		content, err := os.ReadFile(tmpFile)
		require.NoError(t, err)

		var writtenEvent AuditEvent
		err = json.Unmarshal(content, &writtenEvent)
		require.NoError(t, err)

		assert.Equal(t, event, writtenEvent)
	})

	t.Run("concurrent writes", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "audit.log")
		obs, err := NewFileObserver(tmpFile)
		require.NoError(t, err)
		defer obs.file.Close()

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				obs.OnAudit(AuditEvent{TS: int64(i)})
			}(i)
		}
		wg.Wait()

		content, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		lines := bytes.Split(bytes.TrimSpace(content), []byte{'\n'})
		assert.Len(t, lines, 10)
	})

	t.Run("nil observer", func(t *testing.T) {
		var nilObs *FileObserver
		assert.NotPanics(t, func() {
			nilObs.OnAudit(AuditEvent{})
		})
	})
}


func TestAPIObserver(t *testing.T) {
	t.Run("create with empty url", func(t *testing.T) {
		obs, err := NewAPIObserver("")
		assert.NoError(t, err)
		assert.Nil(t, obs)
	})

	t.Run("create with url", func(t *testing.T) {
		obs, err := NewAPIObserver("http://example.com")
		assert.NoError(t, err)
		assert.NotNil(t, obs)
		assert.Equal(t, "http://example.com", obs.url)
	})

	t.Run("successful post", func(t *testing.T) {
		received := make(chan AuditEvent, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var event AuditEvent
			json.NewDecoder(r.Body).Decode(&event)
			received <- event
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		obs, err := NewAPIObserver(server.URL)
		require.NoError(t, err)

		event := AuditEvent{
			TS:     123,
			Action: "test",
			UserID: "user",
			URL:    "url",
		}

		obs.OnAudit(event)

		select {
		case receivedEvent := <-received:
			assert.Equal(t, event, receivedEvent)
		case <-time.After(time.Second):
			t.Fatal("event not received")
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		obs, err := NewAPIObserver(server.URL)
		require.NoError(t, err)

		assert.NotPanics(t, func() {
			obs.OnAudit(AuditEvent{})
		})
	})

	t.Run("unavailable server", func(t *testing.T) {
		obs, err := NewAPIObserver("http://localhost:65432")
		require.NoError(t, err)

		assert.NotPanics(t, func() {
			obs.OnAudit(AuditEvent{})
		})
	})

	t.Run("nil observer", func(t *testing.T) {
		var nilObs *APIObserver
		assert.NotPanics(t, func() {
			nilObs.OnAudit(AuditEvent{})
		})
	})
}


func TestIntegration(t *testing.T) {
	service := NewAuditService()

	tmpFile := filepath.Join(t.TempDir(), "audit.log")
	fileObs, err := NewFileObserver(tmpFile)
	require.NoError(t, err)

	received := make(chan AuditEvent, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event AuditEvent
		json.NewDecoder(r.Body).Decode(&event)
		received <- event
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	apiObs, err := NewAPIObserver(server.URL)
	require.NoError(t, err)

	service.Attach(fileObs)
	service.Attach(apiObs)

	event := AuditEvent{
		TS:     123,
		Action: "integrate",
		UserID: "user",
		URL:    "url",
	}

	service.Notify(event)

	select {
	case receivedEvent := <-received:
		assert.Equal(t, event, receivedEvent)
	case <-time.After(time.Second):
		t.Fatal("API not received")
	}

	time.Sleep(100 * time.Millisecond)
	content, err := os.ReadFile(tmpFile)
	require.NoError(t, err)

	var fileEvent AuditEvent
	err = json.Unmarshal(bytes.TrimSpace(content), &fileEvent)
	require.NoError(t, err)
	assert.Equal(t, event, fileEvent)
}


type mockObserver struct {
	called    bool
	lastEvent AuditEvent
}

func (m *mockObserver) OnAudit(event AuditEvent) {
	m.called = true
	m.lastEvent = event
}
