package browserauth

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound    = errors.New("browser auth record not found")
	ErrConflict    = errors.New("browser auth conflict")
	ErrUnauthorized = errors.New("browser auth unauthorized")
)

type Account struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	SuperAdmin  bool   `json:"superAdmin"`
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Identity struct {
	ProviderID string `json:"providerId"`
	Issuer     string `json:"-"`
	Subject    string `json:"-"`
	Email      string `json:"email"`
}

type Tenant struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Projects        []Project `json:"projects"`
	EmailDomains    []string  `json:"emailDomains"`
	ResourceVersion string    `json:"resourceVersion"`
	CanManage       bool      `json:"canManage"`
	Permissions     []string  `json:"-"`
}

type Access struct {
	Account    Account
	Tenants    []Tenant
	Identities []Identity
}

type Session struct {
	Access    Access
	CSRFHash  [32]byte
	ExpiresAt time.Time
}

type SessionCreate struct {
	SessionHash [32]byte
	CSRFHash    [32]byte
	AccountID   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type OIDCFlow struct {
	StateHash    [32]byte
	SessionHash  [32]byte
	ProviderID   string
	RedirectURI  string
	CodeVerifier string
	Nonce        string
	AccountID    string
	Link         bool
	ExpiresAt    time.Time
}

type Store interface {
	LookupPasswordAccount(context.Context, string) (Account, string, error)
	LookupIdentityAccount(context.Context, string, string, string) (Account, error)
	AccountAccess(context.Context, string) (Access, error)
	CreateSession(context.Context, SessionCreate) error
	SessionByHash(context.Context, [32]byte, time.Time) (Session, error)
	RevokeSession(context.Context, [32]byte, time.Time) error
	CreateOIDCFlow(context.Context, OIDCFlow) error
	ConsumeOIDCFlow(context.Context, [32]byte, time.Time) (OIDCFlow, error)
	LinkIdentity(context.Context, string, Identity) error
	UpdateTenantEmailDomains(context.Context, [32]byte, string, []string, string) (Tenant, error)
}
