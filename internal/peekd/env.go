package peekd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func getenvInt64(k string, d int64) (int64, error) {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return d, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", k)
	}
	return n, nil
}

func getenvInt(k string, d int) (int, error) {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return d, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", k)
	}
	return n, nil
}

func getenvBool(k string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(k))) {
	case "":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", k)
	}
}
