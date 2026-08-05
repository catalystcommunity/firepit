package main

import (
	"context"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/catalystcommunity/firepit/api/internal/store"
)

// normalizeTrustedDomain applies the same normalization as the admin CSIL
// operation. It rejects an empty value before a database operation occurs.
func normalizeTrustedDomain(spec string) (string, error) {
	domain := strings.ToLower(strings.TrimSpace(spec))
	if domain == "" {
		return "", fmt.Errorf("trusted domain is required")
	}
	return domain, nil
}

type trustedDomainStore interface {
	GetTrustedDomain(context.Context, string) (*store.TrustedDomain, error)
	CreateTrustedDomain(context.Context, *store.TrustedDomain) error
}

// seedTrustedDomains adds the requested domains without changing existing
// rows. This makes the command safe to run after each deployment upgrade.
func seedTrustedDomains(ctx context.Context, st trustedDomainStore, addedBy string, specs []string) error {
	for _, spec := range specs {
		domain, err := normalizeTrustedDomain(spec)
		if err != nil {
			return err
		}

		if _, err := st.GetTrustedDomain(ctx, domain); err == nil {
			log.WithFields(log.Fields{"domain": domain, "created": false}).Info("firepit-seed: trusted domain")
			continue
		} else if !store.IsNotFound(err) {
			return fmt.Errorf("looking up %q: %w", domain, err)
		}

		row := &store.TrustedDomain{Domain: domain, AddedBy: addedBy}
		if err := st.CreateTrustedDomain(ctx, row); err != nil {
			// A concurrent seed can create the same row after our lookup.
			// Treat that race as success, as the requested state now exists.
			if !store.IsUniqueViolation(err) {
				return fmt.Errorf("adding %q: %w", domain, err)
			}
		}
		log.WithFields(log.Fields{"domain": domain, "created": true}).Info("firepit-seed: trusted domain")
	}
	return nil
}
