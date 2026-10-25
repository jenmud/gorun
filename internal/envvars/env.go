package envvars

import (
	"os"
	"strings"

	_ "github.com/joho/godotenv/autoload"
)

// Get will fetch the OS environment key's value if found.
func Get(key string) string {
	return os.Getenv(key)
}

// Bool will fetch and parse envvar key's value returning true is the value is 1, yes, on, or true.
func Bool(key string) bool {
	switch v := Get(key); strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
