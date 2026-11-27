package websrv

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jenmud/gorun/cmd/web/ui"
	"github.com/jenmud/gorun/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// userName is what a user may be called: short, and safe to show and log.
var userName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$`)

// checkPassword says what is wrong with a new password, if anything. bcrypt reads only the first
// 72 bytes, so a longer one is refused rather than cut short without a word.
func checkPassword(p string) error {
	switch {
	case len(p) < 8:
		return errors.New("A password needs 8 or more characters.")
	case len(p) > 72:
		return errors.New("A password can be at most 72 bytes.")
	}
	return nil
}

// usersPage lists the users, for an admin.
func (a *app) usersPage(w http.ResponseWriter, r *http.Request) {
	msg, bad := flashFrom(r)
	d := ui.UsersPageData{Flash: msg, FlashBad: bad, NoStore: a.store == nil, LoginOff: !a.auth.on(), AdminPassword: a.auth.password != ""}
	if a.store != nil {
		users, err := a.store.Users(r.Context())
		if err != nil {
			d.Flash, d.FlashBad = "Couldn't read the users: "+err.Error(), true
		}
		me := a.auth.user(r).Name
		for _, u := range users {
			d.Users = append(d.Users, ui.UserRow{Name: u.Name, Admin: u.Admin, Created: u.CreatedAt.Local().Format("2006-01-02 15:04"), You: u.Name == me})
		}
	}
	a.page(w, r, "Users", "users", ui.UsersPage(d))
}

// addUserHandler adds a user. With no login yet, the first one is an admin, and the browser
// that added them is signed in as them, so turning the login on doesn't lock them out.
func (a *app) addUserHandler(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		redirectWith(w, r, "/users", "The history database isn't open.", true)
		return
	}
	name, password := strings.TrimSpace(r.PostFormValue("name")), r.PostFormValue("password")
	first := !a.auth.on()
	switch {
	case !userName.MatchString(name):
		redirectWith(w, r, "/users", "A name is up to 64 letters, digits, . _ @ and -, starting with a letter or digit.", true)
		return
	case name == adminName && a.auth.password != "":
		redirectWith(w, r, "/users", "The name admin is GORUN_ADMIN_PASSWORD's; pick another.", true)
		return
	}
	if err := checkPassword(password); err != nil {
		redirectWith(w, r, "/users", err.Error(), true)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		redirectWith(w, r, "/users", "Couldn't hash the password: "+err.Error(), true)
		return
	}
	u := store.User{Name: name, PasswordHash: string(hash), Admin: first || r.PostFormValue("admin") == "on", CreatedAt: time.Now()}
	if err := a.store.AddUser(r.Context(), u); err != nil {
		msg := "Couldn't add the user: " + err.Error()
		if errors.Is(err, store.ErrUserExists) {
			msg = fmt.Sprintf("There is a user called %s already.", name)
		}
		redirectWith(w, r, "/users", msg, true)
		return
	}
	a.auth.usersChanged()
	slog.InfoContext(r.Context(), "web user added", "user", name, "admin", u.Admin, "by", signedIn(r))
	if first {
		a.auth.startSession(w, r, u.Name, u.PasswordHash)
		redirectWith(w, r, "/users", fmt.Sprintf("Added %s, an admin, and signed you in as them. Changes now need signing in.", name), false)
		return
	}
	redirectWith(w, r, "/users", "Added "+name+".", false)
}

// setPasswordHandler replaces a user's password, which signs them out everywhere.
func (a *app) setPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		redirectWith(w, r, "/users", "The history database isn't open.", true)
		return
	}
	name, password := r.PostFormValue("name"), r.PostFormValue("password")
	if err := checkPassword(password); err != nil {
		redirectWith(w, r, "/users", err.Error(), true)
		return
	}
	if _, ok, err := a.store.User(r.Context(), name); err != nil || !ok {
		redirectWith(w, r, "/users", "There is no user called "+name+".", true)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err == nil {
		err = a.store.SetUserPassword(r.Context(), name, string(hash))
	}
	if err != nil {
		redirectWith(w, r, "/users", "Couldn't set the password: "+err.Error(), true)
		return
	}
	slog.InfoContext(r.Context(), "web user password set", "user", name, "by", signedIn(r))
	if name == signedIn(r) {
		a.auth.startSession(w, r, name, string(hash)) // stay signed in here
	}
	redirectWith(w, r, "/users", "Set "+name+"'s password. They are signed out everywhere else.", false)
}

// deleteUserHandler removes a user, other than the one signed in, so an admin always remains.
func (a *app) deleteUserHandler(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		redirectWith(w, r, "/users", "The history database isn't open.", true)
		return
	}
	name := r.PostFormValue("name")
	if name == signedIn(r) {
		redirectWith(w, r, "/users", "You can't delete yourself.", true)
		return
	}
	if err := a.store.DeleteUser(r.Context(), name); err != nil {
		redirectWith(w, r, "/users", "Couldn't delete the user: "+err.Error(), true)
		return
	}
	a.auth.usersChanged()
	slog.InfoContext(r.Context(), "web user deleted", "user", name, "by", signedIn(r))
	redirectWith(w, r, "/users", "Deleted "+name+".", false)
}
