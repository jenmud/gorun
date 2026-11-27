package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// User is someone who may sign in to the web UI. Their password is kept only as a bcrypt hash,
// which the caller makes: the store never sees a password.
type User struct {
	Name         string
	PasswordHash string
	Admin        bool // may manage users and the assistant's models
	CreatedAt    time.Time
}

// ErrUserExists is returned by AddUser when the name is taken.
var ErrUserExists = errors.New("a user with that name exists")

// Users lists the web UI's users by name.
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.query(ctx, `SELECT name, password_hash, admin, created_at FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// User returns the user called name, and false when there is none.
func (s *Store) User(ctx context.Context, name string) (User, bool, error) {
	u, err := scanUser(s.queryRow(ctx, `SELECT name, password_hash, admin, created_at FROM users WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	return u, err == nil, err
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var admin int
	var created string
	if err := row.Scan(&u.Name, &u.PasswordHash, &admin, &created); err != nil {
		return User{}, err
	}
	u.Admin = admin != 0
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return u, nil
}

// AddUser adds a user. It fails with ErrUserExists when the name is taken.
func (s *Store) AddUser(ctx context.Context, u User) error {
	if _, ok, err := s.User(ctx, u.Name); err != nil || ok {
		if ok {
			return ErrUserExists
		}
		return err
	}
	admin := 0
	if u.Admin {
		admin = 1
	}
	return s.exec(ctx, `INSERT INTO users (name, password_hash, admin, created_at) VALUES (?, ?, ?, ?)`,
		u.Name, u.PasswordHash, admin, u.CreatedAt.UTC().Format(time.RFC3339Nano))
}

// SetUserPassword replaces a user's password hash.
func (s *Store) SetUserPassword(ctx context.Context, name, hash string) error {
	return s.exec(ctx, `UPDATE users SET password_hash = ? WHERE name = ?`, hash, name)
}

// DeleteUser removes a user.
func (s *Store) DeleteUser(ctx context.Context, name string) error {
	return s.exec(ctx, `DELETE FROM users WHERE name = ?`, name)
}

// CountUsers counts the users, and the admins among them.
func (s *Store) CountUsers(ctx context.Context) (users, admins int, err error) {
	err = s.queryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(admin), 0) FROM users`).Scan(&users, &admins)
	return users, admins, err
}

// SessionKey is the key that signs the web UI's sessions. It is made the first time it is asked
// for and kept, so every gorun on this database accepts the same sessions, after a restart too.
func (s *Store) SessionKey(ctx context.Context) ([]byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	// whichever gorun saves one first wins; the rest read it
	if err := s.exec(ctx, s.dialect.upsert("settings", []string{"name", "value"}, []string{"name"}, nil), "session_key", hex.EncodeToString(b)); err != nil {
		return nil, err
	}
	var v string
	if err := s.queryRow(ctx, `SELECT value FROM settings WHERE name = ?`, "session_key").Scan(&v); err != nil {
		return nil, err
	}
	return hex.DecodeString(v)
}
