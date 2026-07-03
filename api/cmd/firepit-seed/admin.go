// This file (admin.go): the `--admin domain:user_id` bootstrap path — see
// docs/OPERATING.md's "Admin bootstrap" section for the operator-facing
// walkthrough. It exists because every admin-only CSIL op
// (BoardService.create-board, IntegrationService.*, ...) requires an
// existing admin to call it (csilservices.requireAdmin /
// csilservices.IsInstanceAdmin) — there is no chicken, so this is the egg.
package main

import (
	"context"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/catalystcommunity/firepit/api/internal/store"
)

// instanceAdminRole is the users.roles value that grants instance-wide
// admin privileges. Matches csilservices.RoleAdmin
// (api/internal/csilservices/authz.go) exactly; duplicated as a literal
// here rather than imported for the same reason util.go's slugify is
// duplicated rather than imported — see that file's doc comment.
const instanceAdminRole = "admin"

// bootstrapAdmins grants instance-admin to every "domain:user_id" spec in
// specs, upserting a `users` row for that linkkeys identity first if this
// is its first appearance. Both steps are idempotent: UpsertUser is a
// documented no-op-if-exists (store/user.go), and the role grant only
// writes when "admin" isn't already present.
func bootstrapAdmins(ctx context.Context, st *store.Store, specs []string) error {
	for _, spec := range specs {
		domain, userID, err := parseAdminSpec(spec)
		if err != nil {
			return err
		}

		baseHandle := slugify(userID)
		if baseHandle == "" {
			baseHandle = "admin"
		}
		user, err := st.UpsertUser(ctx, domain, userID, baseHandle, titleCaseWords(baseHandle))
		if err != nil {
			return fmt.Errorf("upserting admin user %s:%s: %w", domain, userID, err)
		}

		granted, err := ensureAdminRole(ctx, st, user)
		if err != nil {
			return fmt.Errorf("granting admin role to %s:%s: %w", domain, userID, err)
		}
		log.WithFields(log.Fields{
			"linkkeys_domain":  domain,
			"linkkeys_user_id": userID,
			"handle":           user.Handle,
			"user_id":          user.ID,
			"role_granted":     granted,
		}).Info("firepit-seed: admin bootstrapped")
	}
	return nil
}

// parseAdminSpec splits a "domain:user_id" --admin value on the first
// colon (a linkkeys user_id is operator-chosen and could itself contain
// characters other than ':', but a domain never does, so splitting on the
// FIRST colon rather than the last is the correct, unambiguous choice).
func parseAdminSpec(spec string) (domain, userID string, err error) {
	idx := strings.Index(spec, ":")
	if idx <= 0 || idx == len(spec)-1 {
		return "", "", fmt.Errorf("invalid --admin value %q: expected domain:user_id", spec)
	}
	domain = strings.TrimSpace(spec[:idx])
	userID = strings.TrimSpace(spec[idx+1:])
	if domain == "" || userID == "" {
		return "", "", fmt.Errorf("invalid --admin value %q: expected domain:user_id", spec)
	}
	return domain, userID, nil
}

// ensureAdminRole grants user the instance-admin role if it doesn't
// already have it, reporting whether this call actually changed anything
// (false means the user was already an admin — a normal, idempotent no-op
// on a re-run). Goes straight through st.DB (store.User's Roles column)
// rather than adding a new store method: this is the one place in the
// whole codebase that ever grants the FIRST admin, so there's no service
// layer for it to belong to — see this file's package doc comment.
func ensureAdminRole(ctx context.Context, st *store.Store, user *store.User) (bool, error) {
	for _, r := range user.Roles {
		if r == instanceAdminRole {
			return false, nil
		}
	}
	newRoles := make(store.StringArray, 0, len(user.Roles)+1)
	newRoles = append(newRoles, []string(user.Roles)...)
	newRoles = append(newRoles, instanceAdminRole)

	if err := st.DB.WithContext(ctx).Model(&store.User{}).
		Where("id = ?", user.ID).
		Update("roles", newRoles).Error; err != nil {
		return false, err
	}
	user.Roles = newRoles
	return true, nil
}
