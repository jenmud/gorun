package gorun

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
)

// envPrelude reads the export line written by envLine from stdin and evaluates it.
const envPrelude = `IFS= read -r __gorun_env && eval "$__gorun_env" && unset __gorun_env` + "\n"

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// expandEnv expands $VAR and ${VAR} in an envvars value from gorun's own
// environment (including .env), so secrets such as tokens stay out of the
// Gorunfile. $$ is a literal $.
func expandEnv(value string) string {
	return os.Expand(value, func(name string) string {
		if name == "$" {
			return "$"
		}
		return os.Getenv(name)
	})
}

// envStdin returns the stdin that feeds envPrelude, or nil when there are no vars.
// The values travel over stdin rather than the command line, so they stay out of
// logs, events and the process list, and survive sudo, which resets the environment.
// The prelude must run in a plain sh: the task's own shell may not be sh, or may
// trace its commands (sh -x), which would print the values.
func envStdin(vars map[string]string) (io.Reader, error) {
	if len(vars) == 0 {
		return nil, nil
	}

	line, err := envLine(vars)
	if err != nil {
		return nil, err
	}

	return strings.NewReader(line + "\n"), nil
}

// envLine renders vars as a single `export KEY='value' ...` shell line, keys
// upper-cased and sorted.
func envLine(vars map[string]string) (string, error) {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var b strings.Builder
	b.WriteString("export")
	for _, k := range keys {
		name := strings.ToUpper(k)
		if !envName.MatchString(name) {
			return "", fmt.Errorf("envvar %q is not a valid variable name", k)
		}

		v := vars[k]
		if strings.ContainsAny(v, "\n\r") {
			return "", fmt.Errorf("envvar %s must not contain a newline", name)
		}

		fmt.Fprintf(&b, " %s='%s'", name, strings.ReplaceAll(v, "'", `'\''`))
	}

	return b.String(), nil
}

// envNames returns the upper-cased, sorted names of vars, for logging without the values.
func envNames(vars map[string]string) []string {
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, strings.ToUpper(k))
	}
	slices.Sort(names)
	return names
}
