package session

import (
	"os/exec"
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

type Sessions []Session

func (sessions Sessions) GetNextPort() int {
	newPort := 5202
	for _, s := range sessions {
		if s.IperfPort >= newPort {
			newPort = s.IperfPort + 1
		}
	}

	return newPort
}

func (sessions Sessions) RemoveSession(session Session) *Sessions {
	filteredSessions := sessions[:0]
	for _, s := range sessions {
		if s != session {
			filteredSessions = append(filteredSessions, session)
		}
	}

	return &filteredSessions
}

func (sessions Sessions) GetSession(cookie string) (Session, bool) {
	for _, s := range sessions {
		if s.containsClient(cookie) {
			return s, true
		}
	}

	return Session{}, false
}
