package websrv

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jenmud/gorun/cmd/web/ui"
	"github.com/jenmud/gorun/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie = "gorun_session"
	sessionLength = 7 * 24 * time.Hour
	// adminName is who GORUN_ADMIN_PASSWORD signs in as; no stored user may take it then.
	adminName = "admin"
	// usersRecheck is how often gorun looks for users while it has none, in case another gorun on
	// the same database added one.
	usersRecheck = 5 * time.Second
)

// proxyUserHeaders are the headers auth proxies name the signed-in user in.
var proxyUserHeaders = []string{"X-Forwarded-User", "X-Auth-Request-User", "Remote-User"}

// dummyHash is compared against when there is no such user, so a wrong name takes as long as a
// wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("gorun has no such user"), bcrypt.DefaultCost)

// who is someone signed in.
type who struct {
	Name  string
	Admin bool // may manage users and the assistant's models
	Proxy bool // signed in by an auth proxy, so there is no sign out here
}

// auth decides who may change things. Anyone may look. Signed-in users may start and cancel
// runs and edit the Gorunfile; admins may also manage users and the assistant's models. With no
// admin password, proxy admins or users, everyone may do everything, as before there was a login.
type auth struct {
	password string       // GORUN_ADMIN_PASSWORD, which signs in as adminName
	admins   []string     // GORUN_ADMINS: users an auth proxy in front of gorun may sign in
	st       *store.Store // the users, when the history database is open
	key      []byte       // signs session cookies

	mu        sync.Mutex
	hasUsers  bool
	checkedAt time.Time
}

func newAuth(ctx context.Context, password, admins string, st *store.Store) *auth {
	a := &auth{password: password, st: st}
	for _, u := range strings.Split(admins, ",") {
		if u = strings.TrimSpace(u); u != "" {
			a.admins = append(a.admins, u)
		}
	}
	if st != nil {
		// kept in the database, so every gorun on it accepts the same sessions
		key, err := st.SessionKey(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "web sessions: read the session key", "error", err)
		}
		a.key = key
	}
	if a.key == nil && password != "" {
		m := hmac.New(sha256.New, []byte(password))
		m.Write([]byte("gorun session v1"))
		a.key = m.Sum(nil)
	}
	return a
}

// on reports whether changes need signing in.
func (au *auth) on() bool {
	if au == nil {
		return false
	}
	return au.password != "" || len(au.admins) > 0 || au.usersExist()
}

// usersExist reports whether the database has users, asking it again now and then while it has none.
func (au *auth) usersExist() bool {
	if au.st == nil || au.key == nil {
		return false
	}
	au.mu.Lock()
	defer au.mu.Unlock()
	if au.hasUsers || time.Since(au.checkedAt) < usersRecheck {
		return au.hasUsers
	}
	n, _, err := au.st.CountUsers(context.Background())
	if err != nil {
		slog.Error("web sign in: count users", "error", err)
	}
	au.hasUsers, au.checkedAt = n > 0, time.Now()
	return au.hasUsers
}

// usersChanged makes the next on ask the database again.
func (au *auth) usersChanged() {
	au.mu.Lock()
	au.hasUsers, au.checkedAt = false, time.Time{}
	au.mu.Unlock()
}

// describe is the startup message's login line.
func (au *auth) describe() string {
	if !au.on() {
		return "none: anyone who can open the web UI may run tasks, edit the Gorunfile and download models (add a user on the Users page, or set GORUN_ADMIN_PASSWORD)"
	}
	var ways []string
	if au.password != "" {
		ways = append(ways, "admin password")
	}
	if len(au.admins) > 0 {
		ways = append(ways, "proxy users in GORUN_ADMINS")
	}
	if au.usersExist() {
		ways = append(ways, "users kept in the history database")
	}
	return strings.Join(ways, ", ") + " to change things; anyone may look"
}

// user is who r comes from; its Name is "" when r isn't signed in.
func (au *auth) user(r *http.Request) who {
	if !au.on() {
		return who{}
	}
	for _, h := range proxyUserHeaders {
		if u := r.Header.Get(h); u != "" && slices.Contains(au.admins, u) {
			return who{Name: u, Admin: true, Proxy: true}
		}
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return who{}
	}
	name, fp, ok := au.verify(c.Value, time.Now())
	if !ok {
		return who{}
	}
	if name == adminName && au.password != "" {
		if fp == fingerprint(au.password) {
			return who{Name: adminName, Admin: true}
		}
		return who{}
	}
	if au.st == nil {
		return who{}
	}
	// a deleted user, or one whose password changed, is signed out
	u, found, err := au.st.User(r.Context(), name)
	if err != nil || !found || fp != fingerprint(u.PasswordHash) {
		return who{}
	}
	return who{Name: u.Name, Admin: u.Admin}
}

// fingerprint ties a session to the password it was signed in with, so changing the password
// ends it.
func fingerprint(secret string) string {
	sum := sha256.Sum256([]byte("gorun session|" + secret))
	return base64.RawURLEncoding.EncodeToString(sum[:9])
}

// sign makes a session cookie value for name, signed in with secret, that lasts until expires.
func (au *auth) sign(name, secret string, expires time.Time) string {
	payload := name + "|" + strconv.FormatInt(expires.Unix(), 10) + "|" + fingerprint(secret)
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	m := hmac.New(sha256.New, au.key)
	m.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// verify checks a session cookie value, and returns its name and password fingerprint.
func (au *auth) verify(value string, now time.Time) (name, fp string, ok bool) {
	body, mac, found := strings.Cut(value, ".")
	if !found || au.key == nil {
		return "", "", false
	}
	m := hmac.New(sha256.New, au.key)
	m.Write([]byte(body))
	got, err := base64.RawURLEncoding.DecodeString(mac)
	if err != nil || !hmac.Equal(got, m.Sum(nil)) {
		return "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return "", "", false
	}
	unix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || now.Unix() >= unix {
		return "", "", false
	}
	return parts[0], parts[2], true
}

// signIn checks a name and password, and returns who they are and the secret the session is tied to.
func (au *auth) signIn(ctx context.Context, name, password string) (who, string, bool) {
	if name == adminName && au.password != "" {
		a, b := sha256.Sum256([]byte(password)), sha256.Sum256([]byte(au.password))
		return who{Name: adminName, Admin: true}, au.password, subtle.ConstantTimeCompare(a[:], b[:]) == 1
	}
	var u store.User
	found := false
	if au.st != nil {
		var err error
		if u, found, err = au.st.User(ctx, name); err != nil {
			slog.ErrorContext(ctx, "web sign in: read the user", "error", err)
		}
	}
	if !found {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return who{}, "", false
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return who{}, "", false
	}
	return who{Name: u.Name, Admin: u.Admin}, u.PasswordHash, true
}

// startSession signs the browser in as w.
func (au *auth) startSession(rw http.ResponseWriter, r *http.Request, name, secret string) {
	expires := time.Now().Add(sessionLength)
	http.SetCookie(rw, &http.Cookie{
		Name: sessionCookie, Value: au.sign(name, secret, expires), Path: "/", Expires: expires,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure(r),
	})
}

type whoKey struct{}

// signedIn is the user guard let r through for, or "".
func signedIn(r *http.Request) string {
	u, _ := r.Context().Value(whoKey{}).(who)
	return u.Name
}

// openPosts are the posts anyone may make: asking the assistant is looking, not changing.
var openPosts = []string{"/assistant/chat", "/login", "/logout"}

// adminOnly is whether path is for admins alone: managing users, and the assistant's models,
// which includes asking Hugging Face for this machine.
func adminOnly(path string) bool {
	for _, p := range []string{"/users", "/assistant/models/", "/assistant/hub/"} {
		if path == p || strings.HasPrefix(path, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	return false
}

// guard lets anyone look, signed-in users change things, and only admins manage users and models.
func (a *app) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		read := r.Method == http.MethodGet || r.Method == http.MethodHead
		admin := adminOnly(r.URL.Path)
		if !a.auth.on() || (read && !admin) || (!read && slices.Contains(openPosts, r.URL.Path)) {
			next.ServeHTTP(w, r)
			return
		}
		u := a.auth.user(r)
		switch {
		case u.Name == "":
			deny(w, r, "Sign in to do this.")
		case admin && !u.Admin:
			deny(w, r, "Only an admin can do this.")
		default:
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), whoKey{}, u)))
		}
	})
}

// deny answers each kind of request in the shape its page reads: JSON, a form's page, or HTML
// the page puts in place.
func deny(w http.ResponseWriter, r *http.Request, msg string) {
	switch {
	case strings.HasPrefix(r.Header.Get("Content-Type"), "application/json"):
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
	case r.Header.Get("Sec-Fetch-Mode") == "navigate":
		back := "/"
		if ref, err := url.Parse(r.Referer()); err == nil && ref.Path != "" {
			back = ref.RequestURI()
		}
		if r.Method == http.MethodGet {
			back = r.URL.RequestURI()
		}
		http.Redirect(w, r, "/login?"+url.Values{"next": {back}}.Encode(), http.StatusSeeOther)
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<p class="rounded border border-red-900/60 bg-red-950/30 px-2 py-1 text-red-300">` + msg + ` <a href="/login" class="underline">Sign in</a></p>`))
	}
}

// localPath is next when it is a page on this site, else "/", so signing in can't send the
// browser elsewhere.
func localPath(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	return next
}

func (a *app) loginPage(w http.ResponseWriter, r *http.Request) {
	if !a.auth.on() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.page(w, r, "Sign in", "", ui.LoginPage(a.loginData(localPath(r.URL.Query().Get("next")), "")))
}

func (a *app) loginData(next, errText string) ui.LoginPageData {
	return ui.LoginPageData{Next: next, Error: errText, Form: a.auth.password != "" || a.auth.usersExist(), AdminPassword: a.auth.password != ""}
}

func (a *app) loginHandler(w http.ResponseWriter, r *http.Request) {
	if !a.auth.on() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	next := localPath(r.PostFormValue("next"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	u, secret, ok := a.auth.signIn(r.Context(), name, r.PostFormValue("password"))
	if !ok {
		slog.WarnContext(r.Context(), "web sign in failed", "user", name, "client", r.RemoteAddr)
		time.Sleep(time.Second) // slows guessing
		w.WriteHeader(http.StatusUnauthorized)
		a.page(w, r, "Sign in", "", ui.LoginPage(a.loginData(next, "That name and password don't match.")))
		return
	}
	a.auth.startSession(w, r, u.Name, secret)
	slog.InfoContext(r.Context(), "web sign in", "user", u.Name, "client", r.RemoteAddr)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *app) logoutHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure(r)})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// secure reports whether the browser reached gorun over HTTPS, directly or through a proxy.
func secure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// pageCtx is renderCtx with who is looking, for pages drawn for one request.
func (a *app) pageCtx(r *http.Request) context.Context {
	u := a.auth.user(r)
	on := a.auth.on()
	return ui.WithViewer(a.renderCtx(), ui.Viewer{
		LoginOn: on, User: u.Name, Admin: !on || u.Admin, Proxy: u.Proxy, Path: r.URL.RequestURI(),
		Users: a.store != nil,
	})
}
