package peekd

import (
	"os"
	"strconv"
)

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func getenvInt(k string, d int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return d
}

func getenvIntAsInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return d
}

func getenvBool(k string) bool {
	v := os.Getenv(k)
	return v == "1" || v == "true" || v == "yes"
}
