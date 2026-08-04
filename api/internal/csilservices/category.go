package csilservices

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/reqctx"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

var categorySlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type categoryService struct{ store *store.Store }

func NewCategoryService(st *store.Store) csil.CategoryService {
	return &categoryService{store: st}
}

func (s *categoryService) ListBoardCategories(ctx context.Context, boardID csil.BoardID) (csil.CategoryList, error) {
	categories, err := s.store.ListCategoriesByBoard(ctx, string(boardID))
	if err != nil {
		return csil.CategoryList{}, err
	}
	out := csil.CategoryList{Categories: make([]csil.Category, 0, len(categories))}
	for i := range categories {
		category, err := s.toWire(ctx, s.store.DB, &categories[i])
		if err != nil {
			return csil.CategoryList{}, err
		}
		out.Categories = append(out.Categories, category)
	}
	return out, nil
}

func (s *categoryService) ListCategories(ctx context.Context, _ csil.Empty) (csil.CategoryList, error) {
	categories, err := s.store.ListCategories(ctx)
	if err != nil {
		return csil.CategoryList{}, err
	}
	out := csil.CategoryList{Categories: make([]csil.Category, 0, len(categories))}
	for i := range categories {
		category, err := s.toWire(ctx, s.store.DB, &categories[i])
		if err != nil {
			return csil.CategoryList{}, err
		}
		out.Categories = append(out.Categories, category)
	}
	return out, nil
}

func (s *categoryService) CreateCategory(ctx context.Context, req csil.CreateCategoryRequest) (csil.Category, error) {
	user, err := requireCategoryAdmin(ctx)
	if err != nil {
		return csil.Category{}, err
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	name := strings.TrimSpace(req.Name)
	description := optionalText(req.Description)
	boardIDs, err := normalizeBoardIDs(req.BoardIds)
	if err != nil {
		return csil.Category{}, err
	}
	if appErr := validateCategory(slug, name, description); appErr != nil {
		return csil.Category{}, appErr
	}

	category := &store.Category{Slug: slug, Name: name, Description: description, CrossBoardPosting: req.CrossBoardPosting, CreatedBy: user.ID}
	txErr := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireBoards(ctx, tx, boardIDs); err != nil {
			return err
		}
		return s.store.CreateCategory(ctx, tx, category, boardIDs)
	})
	if txErr != nil {
		if store.IsUniqueViolation(txErr) {
			return csil.Category{}, Conflict("a category with this slug already exists")
		}
		return csil.Category{}, asAppError(txErr)
	}
	return s.toWire(ctx, s.store.DB, category)
}

func (s *categoryService) UpdateCategory(ctx context.Context, req csil.UpdateCategoryRequest) (csil.Category, error) {
	if _, err := requireCategoryAdmin(ctx); err != nil {
		return csil.Category{}, err
	}
	var category *store.Category
	txErr := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		category, err = s.store.GetCategory(ctx, tx, string(req.Id))
		if err != nil {
			if store.IsNotFound(err) {
				return NotFound("category", "category not found")
			}
			return err
		}
		if req.Name != nil {
			category.Name = strings.TrimSpace(*req.Name)
		}
		if req.Description != nil {
			category.Description = *req.Description
		}
		if req.CrossBoardPosting != nil {
			category.CrossBoardPosting = *req.CrossBoardPosting
		}
		if appErr := validateCategory(category.Slug, category.Name, category.Description); appErr != nil {
			return appErr
		}
		category.UpdatedAt = time.Now().UTC()
		if err := tx.WithContext(ctx).Save(category).Error; err != nil {
			return err
		}
		if req.BoardIds != nil {
			boardIDs, err := normalizeBoardIDs(req.BoardIds)
			if err != nil {
				return err
			}
			if err := requireBoards(ctx, tx, boardIDs); err != nil {
				return err
			}
			if err := s.store.ReplaceCategoryBoards(ctx, tx, category.ID, boardIDs); err != nil {
				return err
			}
		}
		return nil
	})
	if txErr != nil {
		return csil.Category{}, asAppError(txErr)
	}
	return s.toWire(ctx, s.store.DB, category)
}

func (s *categoryService) DeleteCategory(ctx context.Context, req csil.DeleteCategoryRequest) (csil.Empty, error) {
	if _, err := requireCategoryAdmin(ctx); err != nil {
		return csil.Empty{}, err
	}
	if !req.RemoveFromPosts {
		return csil.Empty{}, Validation("remove_from_posts", "confirm removal of this category from all posts")
	}
	txErr := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.store.GetCategory(ctx, tx, string(req.Id)); err != nil {
			if store.IsNotFound(err) {
				return NotFound("category", "category not found")
			}
			return err
		}
		return s.store.DeleteCategory(ctx, tx, string(req.Id))
	})
	if txErr != nil {
		return csil.Empty{}, asAppError(txErr)
	}
	return csil.Empty{}, nil
}

func requireCategoryAdmin(ctx context.Context) (*store.User, error) {
	user, ok := reqctx.User(ctx)
	if !ok {
		return nil, Unauthenticated("login required")
	}
	if !IsInstanceAdmin(user) {
		return nil, Forbidden("only an instance admin may manage categories")
	}
	return user, nil
}

func validateCategory(slug, name, description string) *AppError {
	if len(slug) < 1 || len(slug) > 64 || !categorySlugPattern.MatchString(slug) {
		return Validation("slug", "slug must use lowercase letters, numbers, and single hyphens")
	}
	if name == "" || len(name) > 80 {
		return Validation("name", "name must be 1-80 characters")
	}
	if len(description) > 500 {
		return Validation("description", "description must be at most 500 characters")
	}
	return nil
}

func normalizeBoardIDs(ids []csil.BoardID) ([]string, error) {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(string(raw))
		if id == "" {
			return nil, Validation("board_ids", "board ID must not be empty")
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, Validation("board_ids", "select at least one board")
	}
	sort.Strings(out)
	return out, nil
}

func requireBoards(ctx context.Context, tx *gorm.DB, boardIDs []string) error {
	var count int64
	if err := tx.WithContext(ctx).Model(&store.Board{}).Where("id IN ?", boardIDs).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(boardIDs)) {
		return NotFound("board", "one or more boards were not found")
	}
	return nil
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *categoryService) toWire(ctx context.Context, db *gorm.DB, category *store.Category) (csil.Category, error) {
	boardIDs, err := s.store.GetCategoryBoardIDs(ctx, db, category.ID)
	if err != nil {
		return csil.Category{}, err
	}
	wireIDs := make([]csil.BoardID, 0, len(boardIDs))
	for _, id := range boardIDs {
		wireIDs = append(wireIDs, csil.BoardID(id))
	}
	out := csil.Category{Id: csil.CategoryID(category.ID), Slug: category.Slug, Name: category.Name, CrossBoardPosting: category.CrossBoardPosting, BoardIds: wireIDs, CreatedAt: category.CreatedAt}
	if category.Description != "" {
		description := category.Description
		out.Description = &description
	}
	return out, nil
}
