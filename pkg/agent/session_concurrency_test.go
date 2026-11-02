package agent

import (
	"sync"
	"testing"
	"time"
)

type countingSessionStorage struct {
	mu        sync.Mutex
	loads     int
	loadDelay time.Duration
	session   *Session
}

func (s *countingSessionStorage) SaveSession(session *Session) error { return nil }
func (s *countingSessionStorage) LoadSession(id string) (*Session, error) {
	time.Sleep(s.loadDelay)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	return s.session, nil
}
func (s *countingSessionStorage) ListSessions() ([]string, error) { return nil, nil }
func (s *countingSessionStorage) ListSessionInfos() ([]SessionInfo, error) {
	return nil, nil
}
func (s *countingSessionStorage) DeleteSession(id string) error { return nil }

func TestSessionManagerGetSessionSingleflightLoad(t *testing.T) {
	storage := &countingSessionStorage{
		loadDelay: 20 * time.Millisecond,
		session:   NewSessionWithID("singleflight"),
	}
	sm := NewSessionManager(WithSessionStorage(storage))
	defer sm.Shutdown()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session, ok := sm.GetSession("singleflight")
			if !ok || session == nil {
				t.Errorf("expected loaded session")
			}
		}()
	}
	wg.Wait()

	storage.mu.Lock()
	defer storage.mu.Unlock()
	if storage.loads != 1 {
		t.Fatalf("expected exactly one storage load, got %d", storage.loads)
	}
}
