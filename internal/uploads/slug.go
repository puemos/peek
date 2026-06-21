package uploads

import (
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/puemos/peek/internal/models"
)

const slugRetries = 5

func generateSlug(store slugChecker) (string, error) {
	for range slugRetries {
		s, err := randID(10)
		if err != nil {
			return "", err
		}
		if _, err := store.GetUpload(s); err != nil {
			return s, nil
		}
	}
	return "", errors.New("slug collision after retries")
}

type slugChecker interface {
	GetUpload(slug string) (*models.Upload, error)
}

func randID(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
