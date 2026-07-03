package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// integrationService is the stub IntegrationService implementation (task
// B1). B8 replaces every method body below; see the package doc comment
// (doc.go) for the error-handling contract every replacement must follow.
type integrationService struct {
	store *store.Store
}

// NewIntegrationService constructs the IntegrationService implementation.
func NewIntegrationService(st *store.Store) csil.IntegrationService {
	return &integrationService{store: st}
}

func (s *integrationService) CreateGithubMapping(ctx context.Context, req csil.CreateMappingRequest) (csil.GithubMapping, error) {
	return csil.GithubMapping{}, Unimplemented("IntegrationService.create-github-mapping")
}

func (s *integrationService) ListGithubMappings(ctx context.Context, req csil.Empty) (csil.MappingList, error) {
	return csil.MappingList{}, Unimplemented("IntegrationService.list-github-mappings")
}

func (s *integrationService) DeleteGithubMapping(ctx context.Context, req csil.MappingID) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("IntegrationService.delete-github-mapping")
}

func (s *integrationService) AddTrustedDomain(ctx context.Context, req csil.Domain) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("IntegrationService.add-trusted-domain")
}

func (s *integrationService) RemoveTrustedDomain(ctx context.Context, req csil.Domain) (csil.Empty, error) {
	return csil.Empty{}, Unimplemented("IntegrationService.remove-trusted-domain")
}

func (s *integrationService) ListTrustedDomains(ctx context.Context, req csil.Empty) (csil.DomainList, error) {
	return csil.DomainList{}, Unimplemented("IntegrationService.list-trusted-domains")
}
