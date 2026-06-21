package uploads

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const slugRetries = 5

func generateSlug(store slugChecker) (string, error) {
	for range slugRetries {
		s, err := randID(10)
		if err != nil {
			return "", err
		}
		exists, err := store.UploadSlugExists(s)
		if err != nil {
			return "", fmt.Errorf("check slug availability: %w", err)
		}
		if !exists {
			return s, nil
		}
	}
	return "", errors.New("slug collision after retries")
}

type slugChecker interface {
	UploadSlugExists(slug string) (bool, error)
}

func randID(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
