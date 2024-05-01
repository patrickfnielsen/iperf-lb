package session

import (
	"os/exec"
	"sync"
)

type Session struct {
	Client      string
	IperfPort   int
	IperfCookie string
	Iperf       *exec.Cmd
}

func (session *Session) containsClient(cookie string) bool {
	return session.IperfCookie == cookie
}

type Sessions struct {
	mu       sync.Mutex
	sessions []Session
}

func (s *Sessions) GetNextPort() int {
	newPort := 5202
	for _, s := range s.sessions {
		if s.IperfPort >= newPort {
			newPort = s.IperfPort + 1
		}
	}

	return newPort
}

func (s *Sessions) Get(cookie string) (Session, bool) {
	for _, s := range s.sessions {
		if s.containsClient(cookie) {
			return s, true
		}
	}

	return Session{}, false
}

func (s *Sessions) Count() int {
	return len(s.sessions)
}

func (s *Sessions) Remove(session Session) {
	s.mu.Lock()
	filteredSessions := s.sessions[:0]
	for _, s := range s.sessions {
		if s != session {
			filteredSessions = append(filteredSessions, s)
		}
	}

	s.sessions = filteredSessions
	s.mu.Unlock()
}

func (s *Sessions) Add(session Session) {
	s.mu.Lock()
	s.sessions = append(s.sessions, session)
	s.mu.Unlock()
}
