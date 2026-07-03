package store

import (
	"context"
	"time"
)

// GithubMapping mirrors the `github_mappings` table: per-repo webhook
// routing into a board.
type GithubMapping struct {
	ID         string      `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	BoardID    string      `gorm:"column:board_id;type:uuid;not null"`
	Repo       string      `gorm:"column:repo;not null"`
	Events     StringArray `gorm:"column:events;type:text[];not null"`
	SecretRef  string      `gorm:"column:secret_ref;not null"`
	ThreadMode string      `gorm:"column:thread_mode;type:github_thread_mode;not null;default:'post_per_issue'"`
	CreatedBy  string      `gorm:"column:created_by;type:uuid;not null"`
	CreatedAt  time.Time   `gorm:"column:created_at;not null"`
}

func (GithubMapping) TableName() string { return "github_mappings" }

// CreateGithubMapping inserts m, populating its generated id/created_at
// (gorm's postgres driver issues a RETURNING clause for DB-side defaults —
// see the id column's `default:generate_ulid()` tag — so m is filled in on
// return, same as every other Create call in this package).
func (s *Store) CreateGithubMapping(ctx context.Context, m *GithubMapping) error {
	return s.DB.WithContext(ctx).Create(m).Error
}

// ListGithubMappings returns every configured mapping, oldest first.
func (s *Store) ListGithubMappings(ctx context.Context) ([]GithubMapping, error) {
	var out []GithubMapping
	if err := s.DB.WithContext(ctx).Order("created_at").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// GetGithubMapping looks up a mapping by id, returning gorm.ErrRecordNotFound
// (see IsNotFound) if there is none.
func (s *Store) GetGithubMapping(ctx context.Context, id string) (*GithubMapping, error) {
	var m GithubMapping
	if err := s.DB.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// GetGithubMappingByRepo looks up the mapping for repo ("owner/name"), or
// IsNotFound(err) if none is configured — the webhook receiver's "unmapped
// repo" case. The `repo` column carries a UNIQUE constraint (baseline
// migration), so there is at most one row per repo.
func (s *Store) GetGithubMappingByRepo(ctx context.Context, repo string) (*GithubMapping, error) {
	var m GithubMapping
	if err := s.DB.WithContext(ctx).First(&m, "repo = ?", repo).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// DeleteGithubMapping removes a mapping by id. Deleting a mapping that
// doesn't exist is not itself an error at this layer — callers that need
// "not found" semantics (IntegrationService.delete-github-mapping) check
// existence first via GetGithubMapping.
func (s *Store) DeleteGithubMapping(ctx context.Context, id string) error {
	return s.DB.WithContext(ctx).Delete(&GithubMapping{}, "id = ?", id).Error
}

// --- trusted_domains ---
//
// store.TrustedDomain itself is declared in board.go (it was scaffolded
// there alongside board_members in the A3 baseline); its store methods live
// here instead, since github_mapping.go is the one store file task B8 owns
// end to end (PLANDOC.md §7 B8's ownership note: "trusted-domain ops
// wherever they fit in existing files you own").

// CreateTrustedDomain inserts d. Callers that want "already trusted is a
// no-op, not a conflict" semantics (IntegrationService.add-trusted-domain)
// check GetTrustedDomain first.
func (s *Store) CreateTrustedDomain(ctx context.Context, d *TrustedDomain) error {
	return s.DB.WithContext(ctx).Create(d).Error
}

// ListTrustedDomains returns every trusted domain, oldest first.
func (s *Store) ListTrustedDomains(ctx context.Context) ([]TrustedDomain, error) {
	var out []TrustedDomain
	if err := s.DB.WithContext(ctx).Order("created_at").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// GetTrustedDomain looks up a trusted domain by its (already-normalized,
// lowercased) domain string, returning gorm.ErrRecordNotFound (see
// IsNotFound) if it isn't trusted.
func (s *Store) GetTrustedDomain(ctx context.Context, domain string) (*TrustedDomain, error) {
	var d TrustedDomain
	if err := s.DB.WithContext(ctx).First(&d, "domain = ?", domain).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

// DeleteTrustedDomain removes a trusted domain. Removing one that isn't
// trusted is a no-op, not an error — retract-style ops in this schema are
// idempotent by convention (see e.g. EndorsementService.retract).
func (s *Store) DeleteTrustedDomain(ctx context.Context, domain string) error {
	return s.DB.WithContext(ctx).Delete(&TrustedDomain{}, "domain = ?", domain).Error
}
