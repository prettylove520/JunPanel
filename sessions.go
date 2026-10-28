package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Sign-ins survive a restart of the web service (an upgrade restarts it), so nobody is sent back to
// the sign-in page halfway through. On a clean stop the open sessions are written next to the
// account, keyed by a SHA-256 of their token rather than the token itself; the next start reads the
// file once, deletes it, and accepts a session from it only if the file is under ten minutes old.
// Every other rule still applies: the account revision, 30 minutes idle and 8 hours in all.

const sessionCarryOver = 10 * time.Minute

type savedSessions struct {
	Saved    time.Time               `json:"saved"`
	Sessions map[string]loginSession `json:"sessions"`
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (a *authStore) sessionsPath() string {
	return filepath.Join(filepath.Dir(a.Path), "sessions.json")
}

// saveSessions runs once the server has stopped taking requests.
func (a *authStore) saveSessions() {
	if a.Demo || a.Path == "" {
		return
	}
	a.Lock()
	out := savedSessions{Saved: time.Now(), Sessions: map[string]loginSession{}}
	for token, s := range a.Sessions {
		out.Sessions[tokenHash(token)] = s
	}
	for hash, s := range a.Restored {
		out.Sessions[hash] = s
	}
	a.Unlock()
	if len(out.Sessions) == 0 {
		return
	}
	if e := atomicJSON(a.sessionsPath(), out, 0600); e != nil {
		log.Printf("sessions: %v", e)
	}
}

// restoreSessions reads and removes the file a clean stop left behind.
func (a *authStore) restoreSessions() {
	a.Restored = map[string]loginSession{}
	if a.Demo || a.Path == "" {
		return
	}
	path := a.sessionsPath()
	b, e := os.ReadFile(path)
	if e != nil {
		return
	}
	os.Remove(path)
	var in savedSessions
	if json.Unmarshal(b, &in) != nil || time.Since(in.Saved) > sessionCarryOver || time.Since(in.Saved) < 0 {
		return
	}
	for hash, s := range in.Sessions {
		if len(hash) == 64 {
			a.Restored[hash] = s
		}
	}
}
