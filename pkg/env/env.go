package env

import (
	"log"
	"os"
	"strings"
)

func Default(name, defaultValue string) string {
	name = strings.TrimSpace(name)
	defaultValue = strings.TrimSpace(defaultValue)
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		log.Println("# ", name, "=", v)
		return v
	}
	log.Println("# ", name, "=", defaultValue, "(default)")
	return defaultValue
}

type secret string

func (s secret) String() string {
	if s == "" {
		return "(nil)"
	}
	return "***"
}
func (s secret) Secret() string {
	return string(s)
}
func Secret(name, defaultValue string) secret {
	name = strings.TrimSpace(name)
	defaultValue = strings.TrimSpace(defaultValue)
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		log.Println("# ", name, "=", secret(v))
		return secret(v)
	}
	log.Println("# ", name, "=", secret(defaultValue), "(default)")
	return secret(defaultValue)
}
