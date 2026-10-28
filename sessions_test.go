package main

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSessionsSurviveRestart(t *testing.T) {
	a, pw := testAccount(t)
	token, _, e := a.login("127.0.0.1", "admin", pw, "")
	if e != nil {
		t.Fatal(e)
	}
	gone, _, _ := a.login("127.0.0.1", "admin", pw, "")
	a.logout(gone)
	a.saveSessions()
	raw, e := os.ReadFile(a.sessionsPath())
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("the saved file holds a raw token")
	}
	if st, _ := os.Stat(a.sessionsPath()); runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}

	b, e := newAuth(a.Path, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(b.sessionsPath()); !os.IsNotExist(e) {
		t.Fatal("the file must be removed once read")
	}
	if _, e = b.get(token, true); e != nil {
		t.Fatalf("carried session refused: %v", e)
	}
	if _, e = b.get(gone, false); e == nil {
		t.Fatal("a signed-out session came back")
	}
	if _, e = b.get("", false); e == nil {
		t.Fatal("empty token accepted")
	}
}

func TestSessionsCarryOverExpires(t *testing.T) {
	a, pw := testAccount(t)
	token, _, e := a.login("127.0.0.1", "admin", pw, "")
	if e != nil {
		t.Fatal(e)
	}
	a.saveSessions()
	old := time.Now().Add(-sessionCarryOver - time.Minute)
	raw, _ := os.ReadFile(a.sessionsPath())
	stale := strings.Replace(string(raw), `"saved": "`, `"saved": "`+old.Format(time.RFC3339Nano)+`","was": "`, 1)
	if e = os.WriteFile(a.sessionsPath(), []byte(stale), 0600); e != nil {
		t.Fatal(e)
	}
	b, e := newAuth(a.Path, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.get(token, false); e == nil {
		t.Fatal("a stale carry-over was accepted")
	}
}
