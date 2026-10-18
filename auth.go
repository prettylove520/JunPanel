package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"
)

const rounds = 600000

type account struct {
	User     string `json:"user"`
	Salt     string `json:"salt"`
	Hash     string `json:"hash"`
	Revision string `json:"revision"`
	TOTP     string `json:"totp,omitempty"`
}

func derive(p, s string) string {
	salt, e := hex.DecodeString(s)
	if e != nil {
		return ""
	}
	b, e := pbkdf2.Key(sha256.New, p, salt, rounds, 32)
	if e != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
func newAccount(user, password string) account {
	salt := randomID(32)
	return account{User: user, Salt: salt, Hash: derive(password, salt), Revision: randomID(32)}
}
func readAccount(path string) (account, error) {
	var a account
	e := loadJSON(path, &a)
	if e == nil && (a.User == "" || len(a.Hash) != 64 || len(a.Salt) != 32 || len(a.Revision) < 32) {
		e = bad(500, "管理员配置损坏，拒绝重新初始化")
	}
	return a, e
}

type loginSession struct {
	CSRF       string
	Revision   string
	Born, Seen time.Time
}
type authStore struct {
	sync.Mutex
	Path     string
	Demo     bool
	Account  account
	Sessions map[string]loginSession
	Failures int
	Locked   time.Time
	OTPSetup map[string]string
}

func newAuth(path string, demo bool) (*authStore, error) {
	a := &authStore{Path: path, Demo: demo, Sessions: map[string]loginSession{}, OTPSetup: map[string]string{}}
	if demo {
		a.Account = newAccount("demo", "JunPanelPreview2026!")
		return a, nil
	}
	e := a.reload()
	return a, e
}
func (a *authStore) reload() error {
	if a.Demo {
		return nil
	}
	v, e := readAccount(a.Path)
	if e != nil {
		return e
	}
	if a.Account.Revision != "" && a.Account.Revision != v.Revision {
		a.Sessions = map[string]loginSession{}
		a.OTPSetup = map[string]string{}
	}
	a.Account = v
	return nil
}
func (a *authStore) login(user, password, otp string) (string, loginSession, error) {
	a.Lock()
	defer a.Unlock()
	if e := a.reload(); e != nil {
		return "", loginSession{}, e
	}
	now := time.Now()
	if now.Before(a.Locked) {
		return "", loginSession{}, bad(429, "尝试过多，请 10 分钟后再试")
	}
	if !a.Locked.IsZero() {
		a.Failures = 0
		a.Locked = time.Time{}
	}
	if len(user) > 64 || len(password) > 1024 {
		return "", loginSession{}, bad(400, "账号或密码长度错误")
	}
	good := hmac.Equal([]byte(derive(password, a.Account.Salt)), []byte(a.Account.Hash))
	good = hmac.Equal([]byte(user), []byte(a.Account.User)) && good
	if a.Account.TOTP != "" {
		good = validOTP(a.Account.TOTP, otp, now) && good
	}
	if !good {
		a.Failures++
		if a.Failures >= 5 {
			a.Locked = now.Add(10 * time.Minute)
		}
		return "", loginSession{}, bad(401, "账号、密码或动态码错误")
	}
	a.Failures = 0
	for key, s := range a.Sessions {
		if now.Sub(s.Seen) > 30*time.Minute || now.Sub(s.Born) > 8*time.Hour {
			delete(a.Sessions, key)
			delete(a.OTPSetup, key)
		}
	}
	if len(a.Sessions) >= 32 {
		for key := range a.Sessions {
			delete(a.Sessions, key)
			delete(a.OTPSetup, key)
			break
		}
	}
	token := randomID(64)
	s := loginSession{randomID(64), a.Account.Revision, now, now}
	a.Sessions[token] = s
	return token, s, nil
}
func (a *authStore) get(token string, touch bool) (loginSession, error) {
	a.Lock()
	defer a.Unlock()
	if e := a.reload(); e != nil {
		return loginSession{}, e
	}
	s, ok := a.Sessions[token]
	now := time.Now()
	if !ok || s.Revision != a.Account.Revision || now.Sub(s.Seen) >= 30*time.Minute || now.Sub(s.Born) >= 8*time.Hour {
		delete(a.Sessions, token)
		delete(a.OTPSetup, token)
		return s, bad(401, "会话已过期，请重新登录")
	}
	if touch {
		s.Seen = now
		a.Sessions[token] = s
	}
	return s, nil
}
func (a *authStore) logout(token string) {
	a.Lock()
	defer a.Unlock()
	delete(a.Sessions, token)
	delete(a.OTPSetup, token)
}
func (a *authStore) change(old, new, otp string) error {
	a.Lock()
	defer a.Unlock()
	if e := a.reload(); e != nil {
		return e
	}
	if !hmac.Equal([]byte(derive(old, a.Account.Salt)), []byte(a.Account.Hash)) {
		return bad(403, "当前密码错误")
	}
	if a.Account.TOTP != "" && !validOTP(a.Account.TOTP, otp, time.Now()) {
		return bad(403, "动态码错误")
	}
	if utf8.RuneCountInString(new) < 8 || utf8.RuneCountInString(new) > 256 {
		return bad(400, "新密码需为 8–256 字符")
	}
	v := newAccount(a.Account.User, new)
	v.TOTP = a.Account.TOTP
	if e := atomicJSON(a.Path, v, 0640); e != nil {
		return e
	}
	a.Account = v
	a.Sessions = map[string]loginSession{}
	a.OTPSetup = map[string]string{}
	return nil
}
func validOTP(secret, code string, at time.Time) bool {
	key, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if e != nil || len(code) != 6 {
		return false
	}
	for delta := -1; delta <= 1; delta++ {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(at.Unix()/30+int64(delta)))
		h := hmac.New(sha1.New, key)
		h.Write(b[:])
		sum := h.Sum(nil)
		off := sum[len(sum)-1] & 15
		n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
		if hmac.Equal([]byte(fmt.Sprintf("%06d", n%1000000)), []byte(code)) {
			return true
		}
	}
	return false
}
func (a *authStore) setupOTP(token, password string) (string, error) {
	a.Lock()
	defer a.Unlock()
	if e := a.reload(); e != nil {
		return "", e
	}
	if a.Account.TOTP != "" {
		return "", bad(409, "请先关闭现有两步验证")
	}
	if !hmac.Equal([]byte(derive(password, a.Account.Salt)), []byte(a.Account.Hash)) {
		return "", bad(403, "密码错误")
	}
	raw, _ := hex.DecodeString(randomID(40))
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	a.OTPSetup[token] = secret
	return secret, nil
}
func (a *authStore) enableOTP(token, code string) error {
	a.Lock()
	defer a.Unlock()
	if e := a.reload(); e != nil {
		return e
	}
	if s, ok := a.Sessions[token]; !ok || time.Since(s.Seen) >= 30*time.Minute || time.Since(s.Born) >= 8*time.Hour {
		return bad(401, "会话已过期")
	}
	secret := a.OTPSetup[token]
	if secret == "" || !validOTP(secret, code, time.Now()) {
		return bad(400, "动态码错误或设置已过期")
	}
	v := a.Account
	v.TOTP = secret
	v.Revision = randomID(32)
	if e := atomicJSON(a.Path, v, 0640); e != nil {
		return e
	}
	a.Account = v
	a.Sessions = map[string]loginSession{}
	a.OTPSetup = map[string]string{}
	return nil
}
func (a *authStore) disableOTP(password, code string) error {
	a.Lock()
	defer a.Unlock()
	if e := a.reload(); e != nil {
		return e
	}
	if !hmac.Equal([]byte(derive(password, a.Account.Salt)), []byte(a.Account.Hash)) || !validOTP(a.Account.TOTP, code, time.Now()) {
		return bad(403, "密码或动态码错误")
	}
	v := a.Account
	v.TOTP = ""
	v.Revision = randomID(32)
	if e := atomicJSON(a.Path, v, 0640); e != nil {
		return e
	}
	a.Account = v
	a.Sessions = map[string]loginSession{}
	a.OTPSetup = map[string]string{}
	return nil
}

func (a *authStore) info() (string, bool) {
	a.Lock()
	defer a.Unlock()
	return a.Account.User, a.Account.TOTP != ""
}

func (a *authStore) confirm(password, otp string) error {
	a.Lock()
	defer a.Unlock()
	if e := a.reload(); e != nil {
		return e
	}
	if time.Now().Before(a.Locked) {
		return bad(429, "尝试过多，请 10 分钟后再试")
	}
	if !a.Locked.IsZero() {
		a.Failures = 0
		a.Locked = time.Time{}
	}
	if len(password) > 1024 || !hmac.Equal([]byte(derive(password, a.Account.Salt)), []byte(a.Account.Hash)) || a.Account.TOTP != "" && !validOTP(a.Account.TOTP, otp, time.Now()) {
		a.Failures++
		if a.Failures >= 5 {
			a.Locked = time.Now().Add(10 * time.Minute)
		}
		return bad(403, "密码或动态码错误")
	}
	a.Failures = 0
	return nil
}
