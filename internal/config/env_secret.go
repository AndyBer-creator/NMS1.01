package config

import (
	"os"
	"strings"
)

// EnvOrFile returns secret value from <NAME>_FILE first, then <NAME>.
// Empty/whitespace-only values are treated as unset.
func EnvOrFile(name string) string {
	filePath := strings.TrimSpace(os.Getenv(name + "_FILE"))
	if filePath != "" {
		if b, err := os.ReadFile(filePath); err == nil {
			if v := strings.TrimSpace(string(b)); v != "" {
				return v
			}
		}
	}
	return strings.TrimSpace(os.Getenv(name))
}

