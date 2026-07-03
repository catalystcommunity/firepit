package csilservices

import (
	"context"
	"strings"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// integrationService is the IntegrationService implementation (task B8):
// admin-managed GitHub repo->board mappings and the instance's trusted
// linkkeys domains (PLANDOC.md §5, §7 B8). Every mutating op here is
// instance-admin only (users.roles contains "admin") — see requireAdmin.
type integrationService struct {
	store *store.Store
}

// NewIntegrationService constructs the IntegrationService implementation.
func NewIntegrationService(st *store.Store) csil.IntegrationService {
	return &integrationService{store: st}
}

// requireAdmin returns the caller's user row if ctx carries an authenticated
// session whose roles include "admin", or an *AppError describing why not:
// Unauthenticated if there's no session at all, Forbidden if there is one
// but it lacks the admin role. Every mutating op in this file starts with
// this call.
func requireAdmin(ctx context.Context) (*store.User, *AppError) {
	u, ok := reqctx.User(ctx)
	if !ok {
		return nil, Unauthenticated("login required")
	}
	for _, role := range u.Roles {
		if role == "admin" {
			return u, nil
		}
	}
	return nil, Forbidden("instance-admin role required")
}

// CreateGithubMapping validates and inserts a new per-repo webhook mapping.
// req.SecretRef is NOT the HMAC secret itself — see api/internal/github's
// package doc comment for the FIREPIT_GH_SECRET_<...> env-var-name
// convention this field documents; this method just stores whatever
// reference string the admin supplies, unvalidated beyond non-empty (the
// webhook receiver is what discovers a bad reference, at delivery time).
func (s *integrationService) CreateGithubMapping(ctx context.Context, req csil.CreateMappingRequest) (csil.GithubMapping, error) {
	admin, aerr := requireAdmin(ctx)
	if aerr != nil {
		return csil.GithubMapping{}, aerr
	}

	repo := strings.TrimSpace(req.Repo)
	if repo == "" || !strings.Contains(repo, "/") || strings.Count(repo, "/") != 1 {
		return csil.GithubMapping{}, Validation("repo", `repo must be in "owner/name" form`)
	}
	if strings.TrimSpace(req.SecretRef) == "" {
		return csil.GithubMapping{}, Validation("secret_ref", "secret_ref is required")
	}
	if len(req.Events) == 0 {
		return csil.GithubMapping{}, Validation("events", "at least one event type is required")
	}
	threadMode := string(req.ThreadMode)
	if threadMode == "" {
		threadMode = "post_per_issue"
	}
	switch threadMode {
	case "post_per_issue", "post_per_release", "post_per_pull_request":
	default:
		return csil.GithubMapping{}, Validation("thread_mode", "unrecognized thread_mode")
	}

	if req.BoardId == "" {
		return csil.GithubMapping{}, Validation("board_id", "board_id is required")
	}
	var boardCount int64
	if err := s.store.DB.WithContext(ctx).Table("boards").
		Where("id = ?", string(req.BoardId)).Count(&boardCount).Error; err != nil {
		return csil.GithubMapping{}, Internal("looking up board: " + err.Error())
	}
	if boardCount == 0 {
		return csil.GithubMapping{}, NotFound("board", "board not found")
	}

	if _, err := s.store.GetGithubMappingByRepo(ctx, repo); err == nil {
		return csil.GithubMapping{}, Conflict("a github mapping for this repo already exists")
	} else if !store.IsNotFound(err) {
		return csil.GithubMapping{}, Internal("looking up existing mapping: " + err.Error())
	}

	m := &store.GithubMapping{
		BoardID:    string(req.BoardId),
		Repo:       repo,
		Events:     store.StringArray(req.Events),
		SecretRef:  req.SecretRef,
		ThreadMode: threadMode,
		CreatedBy:  admin.ID,
	}
	if err := s.store.CreateGithubMapping(ctx, m); err != nil {
		return csil.GithubMapping{}, Internal("creating github mapping: " + err.Error())
	}
	return mappingToWire(*m), nil
}

// ListGithubMappings has no declared ServiceError arm (see dispatch.go's
// routeInfallible and the csilservices package doc comment), so it can't
// return a typed Forbidden to a non-admin caller. Per that doc comment's
// own guidance ("an expected 'nothing to show' case is a normal empty-page
// reply, not an error"), a non-admin caller here gets an empty list rather
// than an opaque internal-error transport failure.
func (s *integrationService) ListGithubMappings(ctx context.Context, req csil.Empty) (csil.MappingList, error) {
	if _, aerr := requireAdmin(ctx); aerr != nil {
		return csil.MappingList{}, nil
	}
	mappings, err := s.store.ListGithubMappings(ctx)
	if err != nil {
		return csil.MappingList{}, err
	}
	out := make([]csil.GithubMapping, 0, len(mappings))
	for _, m := range mappings {
		out = append(out, mappingToWire(m))
	}
	return csil.MappingList{Mappings: out}, nil
}

func (s *integrationService) DeleteGithubMapping(ctx context.Context, req csil.MappingID) (csil.Empty, error) {
	if _, aerr := requireAdmin(ctx); aerr != nil {
		return csil.Empty{}, aerr
	}
	if _, err := s.store.GetGithubMapping(ctx, string(req)); err != nil {
		// Any lookup failure here — genuinely missing, or a malformed id
		// (e.g. not valid uuid syntax, which Postgres itself rejects
		// before gorm ever gets to say "not found") — reads as NotFound.
		// This schema doesn't distinguish "doesn't exist" from "malformed
		// reference" anywhere else either (see NotFound's doc comment:
		// don't leak existence information over that seam).
		return csil.Empty{}, NotFound("github_mapping", "github mapping not found")
	}
	if err := s.store.DeleteGithubMapping(ctx, string(req)); err != nil {
		return csil.Empty{}, Internal("deleting github mapping: " + err.Error())
	}
	return csil.Empty{}, nil
}

// AddTrustedDomain is idempotent: adding an already-trusted domain succeeds
// silently rather than conflicting, consistent with this schema's retract
// endorsements/subscriptions handled elsewhere.
func (s *integrationService) AddTrustedDomain(ctx context.Context, req csil.Domain) (csil.Empty, error) {
	admin, aerr := requireAdmin(ctx)
	if aerr != nil {
		return csil.Empty{}, aerr
	}
	domain := normalizeDomain(string(req))
	if domain == "" {
		return csil.Empty{}, Validation("domain", "domain is required")
	}
	if _, err := s.store.GetTrustedDomain(ctx, domain); err == nil {
		return csil.Empty{}, nil
	} else if !store.IsNotFound(err) {
		return csil.Empty{}, Internal("looking up trusted domain: " + err.Error())
	}
	if err := s.store.CreateTrustedDomain(ctx, &store.TrustedDomain{Domain: domain, AddedBy: admin.ID}); err != nil {
		return csil.Empty{}, Internal("adding trusted domain: " + err.Error())
	}
	return csil.Empty{}, nil
}

// RemoveTrustedDomain is idempotent: removing a domain that isn't trusted
// is a no-op, not a not-found error.
func (s *integrationService) RemoveTrustedDomain(ctx context.Context, req csil.Domain) (csil.Empty, error) {
	if _, aerr := requireAdmin(ctx); aerr != nil {
		return csil.Empty{}, aerr
	}
	domain := normalizeDomain(string(req))
	if err := s.store.DeleteTrustedDomain(ctx, domain); err != nil {
		return csil.Empty{}, Internal("removing trusted domain: " + err.Error())
	}
	return csil.Empty{}, nil
}

// ListTrustedDomains — see the comment on ListGithubMappings for why a
// non-admin caller gets an empty list rather than an error (no declared
// ServiceError arm on this op).
func (s *integrationService) ListTrustedDomains(ctx context.Context, req csil.Empty) (csil.DomainList, error) {
	if _, aerr := requireAdmin(ctx); aerr != nil {
		return csil.DomainList{}, nil
	}
	domains, err := s.store.ListTrustedDomains(ctx)
	if err != nil {
		return csil.DomainList{}, err
	}
	out := make([]csil.DomainEntry, 0, len(domains))
	for _, d := range domains {
		out = append(out, csil.DomainEntry{
			Domain:    d.Domain,
			AddedBy:   csil.UserID(d.AddedBy),
			CreatedAt: d.CreatedAt,
		})
	}
	return csil.DomainList{Domains: out}, nil
}

func normalizeDomain(d string) string {
	return strings.ToLower(strings.TrimSpace(d))
}

func mappingToWire(m store.GithubMapping) csil.GithubMapping {
	return csil.GithubMapping{
		Id:         csil.MappingID(m.ID),
		BoardId:    csil.BoardID(m.BoardID),
		Repo:       m.Repo,
		Events:     []string(m.Events),
		ThreadMode: csil.ThreadMode(m.ThreadMode),
		CreatedBy:  csil.UserID(m.CreatedBy),
		CreatedAt:  m.CreatedAt,
	}
}
