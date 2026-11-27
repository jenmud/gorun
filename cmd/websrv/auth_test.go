package websrv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jenmud/gorun/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// guarded is an app whose guard sits in front of a handler that answers 200, so a test sees only
// what the guard lets through.
func guarded(t *testing.T, password, admins string, st *store.Store) (*app, http.Handler) {
	t.Helper()
	a := &app{store: st, auth: newAuth(context.Background(), password, admins, st)}
	return a, a.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
}

// testStore is an empty SQLite history database.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "gorun.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// addUser keeps a user with a cheap hash, as the tests don't need a slow one.
func addUser(t *testing.T, st *store.Store, name, password string, admin bool) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err := st.AddUser(context.Background(), store.User{Name: name, PasswordHash: string(hash), Admin: admin, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func status(h http.Handler, r *http.Request) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code
}

// signInCookie signs in through the login handler and returns its session cookie.
func signInCookie(t *testing.T, a *app, name, password string) *http.Cookie {
	t.Helper()
	form := url.Values{"name": {name}, "password": {password}, "next": {"/"}}
	r := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.loginHandler(rec, r)
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatalf("signing in as %s: %d, no session", name, rec.Code)
	return nil
}

func TestGuardLetsAnyoneLookButOnlyUsersChange(t *testing.T) {
	st := testStore(t)
	addUser(t, st, "alice", "alice-pass", true)
	addUser(t, st, "bob", "bob-pass1", false)
	a, h := guarded(t, "", "", st)
	alice, bob := signInCookie(t, a, "alice", "alice-pass"), signInCookie(t, a, "bob", "bob-pass1")

	for _, c := range []struct {
		method, path        string
		anyone, user, admin int
	}{
		{"GET", "/", 200, 200, 200},
		{"GET", "/history", 200, 200, 200},
		{"POST", "/assistant/chat", 200, 200, 200}, // asking is looking
		{"GET", "/assistant/models", 200, 200, 200},
		{"POST", "/run", 403, 200, 200},
		{"POST", "/config/save", 403, 200, 200},
		{"POST", "/assistant/models/use", 403, 403, 200},
		{"POST", "/assistant/hub/download", 403, 403, 200},
		{"GET", "/assistant/hub/search", 403, 403, 200}, // asks Hugging Face for this machine
		{"GET", "/users", 403, 403, 200},
		{"GET", "/assistant/captures", 403, 403, 200},
		{"GET", "/assistant/captures/export", 403, 403, 200},
		{"POST", "/assistant/feedback", 200, 200, 200}, // anyone who may ask may rate
		{"POST", "/users/add", 403, 403, 200},
	} {
		for _, who := range []struct {
			cookie *http.Cookie
			want   int
		}{{nil, c.anyone}, {bob, c.user}, {alice, c.admin}} {
			r := httptest.NewRequest(c.method, c.path, nil)
			name := "anyone"
			if who.cookie != nil {
				r.AddCookie(who.cookie)
				name = who.cookie.Value[:6]
			}
			if got := status(h, r); got != who.want {
				t.Errorf("%s %s as %s = %d, want %d", c.method, c.path, name, got, who.want)
			}
		}
	}
}

func TestSessionsEndWhenTheUserChanges(t *testing.T) {
	st := testStore(t)
	addUser(t, st, "alice", "alice-pass", true)
	addUser(t, st, "bob", "bob-pass1", false)
	a, h := guarded(t, "", "", st)
	bob := signInCookie(t, a, "bob", "bob-pass1")
	try := func() int {
		r := httptest.NewRequest("POST", "/run", nil)
		r.AddCookie(bob)
		return status(h, r)
	}
	if got := try(); got != 200 {
		t.Fatalf("bob signed in = %d", got)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("new-pass1"), bcrypt.MinCost)
	_ = st.SetUserPassword(context.Background(), "bob", string(hash))
	if got := try(); got != 403 {
		t.Errorf("after bob's password changed = %d, want signed out", got)
	}
	bob = signInCookie(t, a, "bob", "new-pass1")
	_ = st.DeleteUser(context.Background(), "bob")
	if got := try(); got != 403 {
		t.Errorf("after bob was deleted = %d, want signed out", got)
	}
}

func TestAdminPasswordSignsInAsAdmin(t *testing.T) {
	a, h := guarded(t, "s3cret-pass", "", nil)
	r := httptest.NewRequest("POST", "/assistant/models/use", nil)
	r.AddCookie(signInCookie(t, a, adminName, "s3cret-pass"))
	if got := status(h, r); got != 200 {
		t.Errorf("admin = %d", got)
	}
	// a session from before the password changed isn't accepted
	b, h2 := guarded(t, "other-pass", "", nil)
	_ = b
	if got := status(h2, r); got != 403 {
		t.Errorf("an old password's session = %d", got)
	}
}

func TestGuardAnswersEachRequestInItsShape(t *testing.T) {
	_, h := guarded(t, "s3cret", "", nil)
	r := httptest.NewRequest("POST", "/assistant/add", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("a JSON post = %d %s", rec.Code, rec.Body)
	}

	r = httptest.NewRequest("POST", "/run", nil)
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	r.Header.Set("Referer", "http://gorun.test/run?x=1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	want := "/login?" + url.Values{"next": {"/run?x=1"}}.Encode()
	if loc := rec.Header().Get("Location"); rec.Code != 303 || loc != want {
		t.Errorf("a form post = %d to %q", rec.Code, loc)
	}
}

func TestNoLoginLeavesEverythingOpen(t *testing.T) {
	_, h := guarded(t, "", "", testStore(t))
	for _, p := range []string{"/run", "/users/add", "/assistant/models/use"} {
		if got := status(h, httptest.NewRequest("POST", p, nil)); got != 200 {
			t.Errorf("POST %s with no login = %d", p, got)
		}
	}
}

func TestFirstUserIsAnAdminAndSignedIn(t *testing.T) {
	st := testStore(t)
	a, _ := guarded(t, "", "", st)
	post := func(form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/users/add", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		a.guard(a.routes()).ServeHTTP(rec, r)
		return rec
	}
	rec := post(url.Values{"name": {"alice"}, "password": {"alice-pass"}}, nil)
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	if u, ok, _ := st.User(context.Background(), "alice"); !ok || !u.Admin || session == nil || !a.auth.on() {
		t.Fatalf("first user = %+v (found %v), session %v, login on %v", u, ok, session, a.auth.on())
	}
	if !strings.HasPrefix(u(t, st, "alice").PasswordHash, "$2") {
		t.Error("the password isn't kept as a bcrypt hash")
	}
	if rec := post(url.Values{"name": {"bob"}, "password": {"bob-pass1"}}, nil); rec.Code != 403 {
		t.Errorf("a second user added without signing in = %d", rec.Code)
	}
	post(url.Values{"name": {"bob"}, "password": {"bob-pass1"}}, session)
	if got := u(t, st, "bob"); got.Name != "bob" || got.Admin {
		t.Errorf("bob = %+v, want a user who isn't an admin", got)
	}
	for _, bad := range []url.Values{
		{"name": {"carol"}, "password": {"short"}},
		{"name": {"../x"}, "password": {"long-enough"}},
		{"name": {"dave"}, "password": {strings.Repeat("x", 73)}},
	} {
		post(bad, session)
		if _, ok, _ := st.User(context.Background(), bad.Get("name")); ok {
			t.Errorf("added %v", bad)
		}
	}
}

func u(t *testing.T, st *store.Store, name string) store.User {
	t.Helper()
	user, _, err := st.User(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func TestSessionCookieIsCheckedAndExpires(t *testing.T) {
	au := newAuth(context.Background(), "s3cret", "", nil)
	now := time.Now()
	good := au.sign(adminName, "s3cret", now.Add(time.Hour))
	if name, _, ok := au.verify(good, now); !ok || name != adminName {
		t.Fatalf("verify(good) = %q, %v", name, ok)
	}
	if _, _, ok := au.verify(good, now.Add(2*time.Hour)); ok {
		t.Error("an expired session was accepted")
	}
	body, mac, _ := strings.Cut(good, ".")
	if _, _, ok := au.verify(body+"x."+mac, now); ok {
		t.Error("a changed session was accepted")
	}
	if _, _, ok := newAuth(context.Background(), "other", "", nil).verify(good, now); ok {
		t.Error("a session signed with another key was accepted")
	}
}

func TestProxyUserMustBeAnAdmin(t *testing.T) {
	_, h := guarded(t, "", "alice, bob", nil)
	r := httptest.NewRequest("POST", "/users/add", nil)
	r.Header.Set("X-Forwarded-User", "bob")
	if got := status(h, r); got != 200 {
		t.Errorf("an admin through the proxy = %d", got)
	}
	r.Header.Set("X-Forwarded-User", "mallory")
	if got := status(h, r); got != 403 {
		t.Errorf("someone else through the proxy = %d", got)
	}
}

func TestLoginStaysOnThisSite(t *testing.T) {
	a := &app{auth: newAuth(context.Background(), "s3cret", "", nil)}
	post := func(name, password, next string) *httptest.ResponseRecorder {
		form := url.Values{"name": {name}, "password": {password}, "next": {next}}
		r := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		a.loginHandler(rec, r)
		return rec
	}
	rec := post(adminName, "s3cret", "//evil.example/")
	if rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Errorf("signing in went to %q", rec.Header().Get("Location"))
	}
	if c := rec.Result().Cookies(); len(c) != 1 || c[0].Name != sessionCookie || !c[0].HttpOnly {
		t.Errorf("cookies = %+v", c)
	}
	if testing.Short() {
		return
	}
	if rec := post(adminName, "wrong", "/run"); rec.Code != 401 || len(rec.Result().Cookies()) != 0 {
		t.Errorf("a wrong password = %d with %d cookies", rec.Code, len(rec.Result().Cookies()))
	}
}

func TestClaimRefusesWhileTheModelIsInUse(t *testing.T) {
	var q modelQueue
	release, err := q.claim(time.Minute)
	if err != nil {
		t.Fatalf("an unused model: %v", err)
	}
	if _, err := q.claim(time.Minute); err == nil {
		t.Error("claimed while claimed")
	}
	release()

	answer, err := q.acquire(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.claim(time.Minute); err == nil || !strings.Contains(err.Error(), "answering") {
		t.Errorf("while answering: %v", err)
	}
	answer()
	if _, err := q.claim(time.Minute); err == nil || !strings.Contains(err.Error(), "ago") {
		t.Errorf("just after an answer: %v", err)
	}
	release, err = q.claim(0)
	if err != nil {
		t.Fatalf("after the quiet time: %v", err)
	}
	release()
}
