package store

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// dialect is the database behind a Store.
type dialect string

const (
	dialectSQLite   dialect = "sqlite"
	dialectPostgres dialect = "postgres"
	dialectMySQL    dialect = "mysql"
)

// IsURL reports whether location names a database server (postgres:// or mysql://) rather than
// an SQLite file.
func IsURL(location string) bool {
	return dialectOf(location) != dialectSQLite
}

func dialectOf(location string) dialect {
	switch {
	case strings.HasPrefix(location, "postgres://"), strings.HasPrefix(location, "postgresql://"):
		return dialectPostgres
	case strings.HasPrefix(location, "mysql://"):
		return dialectMySQL
	}
	return dialectSQLite
}

// Redact returns location without its password, for logs and the startup message.
func Redact(location string) string {
	if !IsURL(location) {
		return location
	}
	u, err := url.Parse(location)
	if err != nil {
		return strings.SplitN(location, "://", 2)[0] + "://…"
	}
	return u.Redacted()
}

// driver returns the database/sql driver name and data source for location.
func driver(location string) (string, string, error) {
	switch dialectOf(location) {
	case dialectPostgres:
		return "pgx", location, nil
	case dialectMySQL:
		dsn, err := mysqlDSN(location)
		return "mysql", dsn, err
	}
	return "sqlite", location, nil
}

// mysqlDSN turns mysql://user:pass@host:port/db?tls=true into the driver's own DSN. Query
// parameters other than tls are passed on as the driver's params.
func mysqlDSN(location string) (string, error) {
	u, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("mysql url: %w", err)
	}
	cfg := mysql.NewConfig()
	cfg.User = u.User.Username()
	cfg.Passwd, _ = u.User.Password()
	cfg.Net = "tcp"
	cfg.Addr = u.Host
	if u.Port() == "" {
		cfg.Addr = u.Hostname() + ":3306"
	}
	cfg.DBName = strings.TrimPrefix(u.Path, "/")
	if cfg.DBName == "" {
		return "", fmt.Errorf("mysql url: no database name, as in mysql://user:pass@host:3306/gorun")
	}
	for k, v := range u.Query() {
		if k == "tls" {
			cfg.TLSConfig = v[0]
			continue
		}
		if cfg.Params == nil {
			cfg.Params = map[string]string{}
		}
		cfg.Params[k] = v[0]
	}
	return cfg.FormatDSN(), nil
}

// rebind turns ? placeholders into $1, $2… for Postgres. No query here has a ? in a string.
func (d dialect) rebind(query string) string {
	if d != dialectPostgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// upsert is an INSERT of cols into table that, when a row with the same conflict columns
// exists, sets the update columns from the new values instead, or leaves the row alone when
// update is empty.
func (d dialect) upsert(table string, cols, conflict, update []string) string {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
	insert := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ","), marks)
	sets := make([]string, len(update))
	for i, c := range update {
		if d == dialectMySQL {
			sets[i] = c + "=VALUES(" + c + ")"
		} else {
			sets[i] = c + "=excluded." + c
		}
	}
	switch {
	case d == dialectMySQL && len(update) == 0:
		return "INSERT IGNORE" + strings.TrimPrefix(insert, "INSERT")
	case d == dialectMySQL:
		return insert + " ON DUPLICATE KEY UPDATE " + strings.Join(sets, ",")
	case len(update) == 0:
		return insert + " ON CONFLICT(" + strings.Join(conflict, ",") + ") DO NOTHING"
	}
	return insert + " ON CONFLICT(" + strings.Join(conflict, ",") + ") DO UPDATE SET " + strings.Join(sets, ",")
}

// clean makes a string argument fit the database: task output can hold bytes that aren't UTF-8,
// which Postgres and MySQL reject, and NUL bytes, which Postgres text can't hold.
func (d dialect) clean(args []any) []any {
	if d == dialectSQLite {
		return args
	}
	for i, a := range args {
		s, ok := a.(string)
		if !ok {
			continue
		}
		s = strings.ToValidUTF8(s, "�")
		if d == dialectPostgres {
			s = strings.ReplaceAll(s, "\x00", "")
		}
		args[i] = s
	}
	return args
}
