package models

import "time"

type Token struct {
	ID        int64
	Token     string
	Name      string
	IsAdmin   bool
	CreatedAt time.Time
}

type Upload struct {
	ID           int64
	Slug         string
	OwnerTokenID int64
	OwnerName    string
	Filename     string
	Size         int64
	PasswordHash string // "" => no password
	CreatedAt    time.Time
}

type Comment struct {
	ID              int64
	UploadID        int64
	ElementSelector string
	ElementText     string
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
	Filename       string
	TotalVisits    int
	UniqueVisitors int
	Recent         []Visit
}
