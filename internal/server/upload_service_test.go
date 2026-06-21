package server

import (
	"errors"
	"net/http"
	"testing"

	"github.com/puemos/peek/internal/uploads"
)

func TestUploadHTTPErrorMapsDomainKinds(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "invalid html",
			err:        &uploads.Error{Kind: uploads.KindInvalidHTML, Message: "file does not look like HTML"},
			wantStatus: http.StatusUnsupportedMediaType,
			wantMsg:    "file does not look like HTML",
		},
		{
			name:       "quota",
			err:        &uploads.Error{Kind: uploads.KindTotalQuotaExceeded, Message: "total storage quota exceeded"},
			wantStatus: http.StatusRequestEntityTooLarge,
			wantMsg:    "total storage quota exceeded",
		},
		{
			name: "joined cleanup error",
			err: errors.Join(
				&uploads.Error{Kind: uploads.KindTotalQuotaExceeded, Message: "total storage quota exceeded"},
				&uploads.CleanupError{Slug: "page", Err: errors.New("delete failed")},
			),
			wantStatus: http.StatusRequestEntityTooLarge,
			wantMsg:    "total storage quota exceeded",
		},
		{
			name:       "plain error",
			err:        errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
			wantMsg:    "upload failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, msg := uploadHTTPError(tc.err)
			if status != tc.wantStatus || msg != tc.wantMsg {
				t.Fatalf("uploadHTTPError() = (%d, %q), want (%d, %q)", status, msg, tc.wantStatus, tc.wantMsg)
			}
		})
	}
}
