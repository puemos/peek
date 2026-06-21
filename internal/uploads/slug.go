package uploads

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	slugRetries   = 5
	maxSlugLength = 60
	clashSuffix   = 4
)

func slugify(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	prevDash := true
	for i := 0; i < len(name); i++ {
		r := name[i]
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
			r += 'a' - 'A'
		case r >= '0' && r <= '9':
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
			continue
		}
		b.WriteByte(r)
		prevDash = false
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > maxSlugLength {
		s = s[:maxSlugLength]
		s = strings.TrimRight(s, "-")
	}
	return s
}

func generateSlugFromName(ctx context.Context, name string, store slugChecker) (string, error) {
	base := slugify(name)
	if base == "" {
		return generateRandomSlug(ctx, store)
	}
	exists, err := store.UploadSlugExists(ctx, base)
	if err != nil {
		return "", fmt.Errorf("check slug availability: %w", err)
	}
	if !exists {
		return base, nil
	}
	for range slugRetries {
		suffix, err := randID(clashSuffix)
		if err != nil {
			return "", err
		}
		candidate := base + "-" + suffix
		exists, err := store.UploadSlugExists(ctx, candidate)
		if err != nil {
			return "", fmt.Errorf("check slug availability: %w", err)
		}
		if !exists {
			return candidate, nil
		}
	}
	return generateRandomSlug(ctx, store)
}

func generateRandomSlug(ctx context.Context, store slugChecker) (string, error) {
	for range slugRetries {
		s, err := randID(10)
		if err != nil {
			return "", err
		}
		exists, err := store.UploadSlugExists(ctx, s)
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
	UploadSlugExists(ctx context.Context, slug string) (bool, error)
}

func randID(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
