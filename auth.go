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
	"net"
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
type attempts struct {
	Failures    int
	Locked      time.Time
	First, Last time.Time
	Trusted     time.Time
}
type authStore struct {
	sync.Mutex
	Path     string
	Demo     bool
	Account  account
	Sessions map[string]loginSession
	Clients  map[string]*attempts
	Global   attempts
	OTPStep  int64
	OTPSetup map[string]string
}

const (
	clientFailures = 5
	globalFailures = 200
	lockWindow     = 10 * time.Minute
	maxClients     = 4096
	trustedFor     = 30 * 24 * time.Hour
)

func newAuth(path string, demo bool) (*authStore, error) {
	a := &authStore{Path: path, Demo: demo, Sessions: map[string]loginSession{}, Clients: map[string]*attempts{}, OTPSetup: map[string]string{}}
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
		a.OTPStep = 0
	}
	a.Account = v
	return nil
}

// Failures are counted per client address so one source cannot lock out the
// administrator. A high global ceiling bounds distributed guessing; addresses
// that signed in successfully recently are exempt from it.
func limitKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}
	if v4 := parsed.To4(); v4 != nil {
		return v4.String()
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
func (t *attempts) blocked(now time.Time) bool {
	if now.Before(t.Locked) {
		return true
	}
	if !t.Locked.IsZero() || now.Sub(t.First) >= lockWindow {
		t.Failures, t.Locked, t.First = 0, time.Time{}, time.Time{}
	}
	return false
}
func (t *attempts) fail(now time.Time, limit int) {
	if t.Failures == 0 {
		t.First = now
	}
	t.Failures++
	t.Last = now
	if t.Failures >= limit {
		t.Locked = now.Add(lockWindow)
	}
}
func (a *authStore) client(ip string, now time.Time) *attempts {
	key := limitKey(ip)
	if t, ok := a.Clients[key]; ok {
		return t
	}
	if len(a.Clients) >= maxClients {
		oldest := ""
		for k, t := range a.Clients {
			if !now.Before(t.Locked) && now.Sub(t.Last) >= lockWindow {
				delete(a.Clients, k)
			} else if oldest == "" || t.Last.Before(a.Clients[oldest].Last) {
				oldest = k
			}
		}
		if len(a.Clients) >= maxClients {
			delete(a.Clients, oldest)
		}
	}
	t := &attempts{}
	a.Clients[key] = t
	return t
}
func (a *authStore) throttled(ip string, now time.Time) (*attempts, error) {
	t := a.client(ip, now)
	global := a.Global.blocked(now) && now.Sub(t.Trusted) >= trustedFor
	if t.blocked(now) || global {
		return t, bad(429, "尝试过多，请 10 分钟后再试")
	}
	return t, nil
}
func (a *authStore) succeeded(t *attempts, now time.Time) {
	*t = attempts{Last: now, Trusted: now}
}
func (a *authStore) failed(t *attempts, now time.Time) {
	t.fail(now, clientFailures)
	a.Global.fail(now, globalFailures)
}

// Accept each time step at most once so an observed code cannot be replayed.
func (a *authStore) checkOTP(code string, now time.Time) (int64, bool) {
	step, ok := otpStep(a.Account.TOTP, code, now)
	return step, ok && step > a.OTPStep
}
func (a *authStore) login(ip, user, password, otp string) (string, loginSession, error) {
	a.Lock()
	defer a.Unlock()
	if e := a.reload(); e != nil {
		return "", loginSession{}, e
	}
	now := time.Now()
	limit, e := a.throttled(ip, now)
	if e != nil {
		return "", loginSession{}, e
	}
	if len(user) > 64 || len(password) > 1024 {
		return "", loginSession{}, bad(400, "账号或密码长度错误")
	}
	good := hmac.Equal([]byte(derive(password, a.Account.Salt)), []byte(a.Account.Hash))
	good = hmac.Equal([]byte(user), []byte(a.Account.User)) && good
	var step int64
	if a.Account.TOTP != "" {
		var fresh bool
		step, fresh = a.checkOTP(otp, now)
		good = fresh && good
	}
	if !good {
		a.failed(limit, now)
		return "", loginSession{}, bad(401, "账号、密码或动态码错误（每个动态码只能使用一次）")
	}
	a.succeeded(limit, now)
	if step > 0 {
		a.OTPStep = step
	}
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
	var step int64
	if a.Account.TOTP != "" {
		var fresh bool
		if step, fresh = a.checkOTP(otp, time.Now()); !fresh {
			return bad(403, "动态码错误或已使用，请等待下一个动态码")
		}
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
	if step > 0 {
		a.OTPStep = step
	}
	return nil
}
func validOTP(secret, code string, at time.Time) bool {
	_, ok := otpStep(secret, code, at)
	return ok
}

// otpStep reports the 30-second time step the code belongs to, allowing one step of clock drift.
func otpStep(secret, code string, at time.Time) (int64, bool) {
	key, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if e != nil || len(code) != 6 {
		return 0, false
	}
	for delta := -1; delta <= 1; delta++ {
		step := at.Unix()/30 + int64(delta)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(step))
		h := hmac.New(sha1.New, key)
		h.Write(b[:])
		sum := h.Sum(nil)
		off := sum[len(sum)-1] & 15
		n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
		if hmac.Equal([]byte(fmt.Sprintf("%06d", n%1000000)), []byte(code)) {
			return step, true
		}
	}
	return 0, false
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
	a.OTPStep = 0
	return nil
}
func (a *authStore) disableOTP(password, code string) error {
	a.Lock()
	defer a.Unlock()
	if e := a.reload(); e != nil {
		return e
	}
	if _, fresh := a.checkOTP(code, time.Now()); !hmac.Equal([]byte(derive(password, a.Account.Salt)), []byte(a.Account.Hash)) || a.Account.TOTP == "" || !fresh {
		return bad(403, "密码或动态码错误，动态码只能使用一次")
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
	a.OTPStep = 0
	return nil
}

func (a *authStore) info() (string, bool) {
	a.Lock()
	defer a.Unlock()
	return a.Account.User, a.Account.TOTP != ""
}

func (a *authStore) confirm(ip, password, otp string) error {
	a.Lock()
	defer a.Unlock()
	if e := a.reload(); e != nil {
		return e
	}
	now := time.Now()
	limit, e := a.throttled(ip, now)
	if e != nil {
		return e
	}
	var step int64
	fresh := true
	if a.Account.TOTP != "" {
		step, fresh = a.checkOTP(otp, now)
	}
	if len(password) > 1024 || !hmac.Equal([]byte(derive(password, a.Account.Salt)), []byte(a.Account.Hash)) || !fresh {
		a.failed(limit, now)
		return bad(403, "密码或动态码错误，动态码只能使用一次")
	}
	a.succeeded(limit, now)
	if step > 0 {
		a.OTPStep = step
	}
	return nil
}
