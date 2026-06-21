package models

import "time"

type Token struct {
	ID        int64
	AccountID int64
	Token     string
	Name      string
	Email     string
	IsAdmin   bool
	Disabled  bool
	CreatedAt time.Time
	ExpiresAt int64 // unix timestamp, 0 = no expiry
}

type Account struct {
	ID           int64
	Email        string
	Name         string
	PasswordHash string
	IsAdmin      bool
	Disabled     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type OAuthIdentity struct {
	ID             int64
	AccountID      int64
	Provider       string
	ProviderUserID string
	Email          string
	Name           string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Invite struct {
	ID                 int64
	Token              string
	Email              string
	CreatedByAccountID int64
	CreatedAt          time.Time
	ExpiresAt          time.Time
	UsedAt             time.Time
	RevokedAt          time.Time
}

type CLILoginDevice struct {
	ID         int64
	UserCode   string
	Status     string
	AccountID  int64
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ConsumedAt time.Time
}

type Upload struct {
	ID             int64
	Slug           string
	OwnerTokenID   int64
	OwnerAccountID int64
	OwnerName      string
	Name           string
	Size           int64
	PasswordHash   string // "" => no password
	CreatedAt      time.Time
}

type Comment struct {
	ID              int64
	UploadID        int64
	ElementSelector string
	ElementText     string
	AnchorKind      string
	AuthorName      string
	AuthorCookie    string
	Body            string
	CreatedAt       time.Time
}

type Visit struct {
	ID            int64
	UploadID      int64
	VisitorCookie string
	VisitorName   string
	IP            string
	UserAgent     string
	VisitedAt     time.Time
}

type Visitor struct {
	Cookie    string
	Name      string
	CreatedAt time.Time
	LastSeen  time.Time
}

type Stats struct {
	Slug           string
	Name           string
	TotalVisits    int
	UniqueVisitors int
	Recent         []Visit
}
