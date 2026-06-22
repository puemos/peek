package server

import (
	"context"
	"strconv"

	"github.com/puemos/peek/internal/db"
)

func initDefaultSettings(ctx context.Context, store *db.Store, secret string, maxUpload, maxTotalSize int64, retentionDays int, s3Defaults map[string]string) error {
	upsert := func(key, val string) error {
		_, err := store.GetSetting(ctx, key)
		if err == nil {
			return nil
		}
		if secretSettingKeys[key] && secret != "" && val != "" {
			enc, err := encryptSecret(secret, val)
			if err != nil {
				return err
			}
			val = enc
		}
		return store.SetSetting(ctx, key, val)
	}
	defaults := map[string]string{
		"auth_token_login_enabled":    "true",
		authAllowedEmailDomainSetting: "",
		"max_upload":                  strconv.FormatInt(maxUpload, 10),
		"max_total_size":              strconv.FormatInt(maxTotalSize, 10),
		"retention_days":              strconv.Itoa(retentionDays),
	}
	for k, v := range defaults {
		if err := upsert(k, v); err != nil {
			return err
		}
	}
	for k, v := range s3Defaults {
		if v != "" {
			if err := upsert(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) settingInt64(ctx context.Context, key string, def int64) int64 {
	v, err := s.encryptedGetSetting(ctx, key)
	if err != nil || v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) settingInt(ctx context.Context, key string, def int) int {
	v, err := s.encryptedGetSetting(ctx, key)
	if err != nil || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) encryptedGetSetting(ctx context.Context, key string) (string, error) {
	v, err := s.store.GetSetting(ctx, key)
	if err != nil {
		return "", err
	}
	if secretSettingKeys[key] {
		return decryptSecret(s.secret, v)
	}
	return v, nil
}

func decryptedStoreSetting(ctx context.Context, store *db.Store, secret, key string) string {
	v, err := store.GetSetting(ctx, key)
	if err != nil || v == "" {
		return ""
	}
	if !secretSettingKeys[key] {
		return v
	}
	dec, err := decryptSecret(secret, v)
	if err != nil {
		return ""
	}
	return dec
}

func (s *Server) encryptedSetSetting(ctx context.Context, key, val string) error {
	if secretSettingKeys[key] && val != "" {
		enc, err := encryptSecret(s.secret, val)
		if err != nil {
			return err
		}
		val = enc
	}
	return s.store.SetSetting(ctx, key, val)
}

func (s *Server) encryptedGetAllSettings(ctx context.Context) (map[string]string, error) {
	raw, err := s.store.GetAllSettings(ctx)
	if err != nil {
		return nil, err
	}
	for k, v := range raw {
		if secretSettingKeys[k] {
			dec, err := decryptSecret(s.secret, v)
			if err != nil {
				raw[k] = ""
			} else {
				raw[k] = dec
			}
		}
	}
	return raw, nil
}
