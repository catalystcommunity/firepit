package store

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Category is a topic label that can be available in several boards.
type Category struct {
	ID                string    `gorm:"column:id;type:uuid;primaryKey;default:generate_ulid()"`
	Slug              string    `gorm:"column:slug;not null"`
	Name              string    `gorm:"column:name;not null"`
	Description       string    `gorm:"column:description;not null;default:''"`
	CrossBoardPosting bool      `gorm:"column:cross_board_posting;not null;default:false"`
	CreatedBy         string    `gorm:"column:created_by;type:uuid;not null"`
	CreatedAt         time.Time `gorm:"column:created_at;not null"`
	UpdatedAt         time.Time `gorm:"column:updated_at;not null"`
}

func (Category) TableName() string { return "categories" }

type BoardCategory struct {
	BoardID    string    `gorm:"column:board_id;type:uuid;primaryKey"`
	CategoryID string    `gorm:"column:category_id;type:uuid;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at;not null"`
}

func (BoardCategory) TableName() string { return "board_categories" }

type PostCategory struct {
	PostID     string    `gorm:"column:post_id;type:uuid;primaryKey"`
	CategoryID string    `gorm:"column:category_id;type:uuid;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at;not null"`
}

func (PostCategory) TableName() string { return "post_categories" }

func (s *Store) ListCategoriesByBoard(ctx context.Context, boardID string) ([]Category, error) {
	var categories []Category
	err := s.DB.WithContext(ctx).Table("categories c").
		Select("c.*").Joins("JOIN board_categories bc ON bc.category_id = c.id").
		Where("bc.board_id = ?", boardID).Order("c.name ASC, c.id ASC").Scan(&categories).Error
	return categories, err
}

func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	var categories []Category
	err := s.DB.WithContext(ctx).Order("name ASC, id ASC").Find(&categories).Error
	return categories, err
}

func (s *Store) GetCategory(ctx context.Context, db *gorm.DB, id string) (*Category, error) {
	var category Category
	if err := db.WithContext(ctx).First(&category, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &category, nil
}

func (s *Store) GetCategoryBoardIDs(ctx context.Context, db *gorm.DB, categoryID string) ([]string, error) {
	var ids []string
	err := db.WithContext(ctx).Model(&BoardCategory{}).Where("category_id = ?", categoryID).
		Order("board_id ASC").Pluck("board_id", &ids).Error
	return ids, err
}

func (s *Store) CreateCategory(ctx context.Context, db *gorm.DB, category *Category, boardIDs []string) error {
	if err := db.WithContext(ctx).Create(category).Error; err != nil {
		return err
	}
	return s.ReplaceCategoryBoards(ctx, db, category.ID, boardIDs)
}

func (s *Store) ReplaceCategoryBoards(ctx context.Context, db *gorm.DB, categoryID string, boardIDs []string) error {
	// A stored post category must be available in the post's origin board.
	// Remove assignments from posts whose origin board is no longer selected.
	if len(boardIDs) > 0 {
		if err := db.WithContext(ctx).Exec(`DELETE FROM post_categories pc USING posts p
			WHERE pc.post_id = p.id AND pc.category_id = ? AND p.board_id NOT IN ?`, categoryID, boardIDs).Error; err != nil {
			return err
		}
	}
	if err := db.WithContext(ctx).Where("category_id = ?", categoryID).Delete(&BoardCategory{}).Error; err != nil {
		return err
	}
	rows := make([]BoardCategory, 0, len(boardIDs))
	for _, boardID := range boardIDs {
		rows = append(rows, BoardCategory{BoardID: boardID, CategoryID: categoryID})
	}
	if len(rows) == 0 {
		return nil
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (s *Store) DeleteCategory(ctx context.Context, db *gorm.DB, id string) error {
	return db.WithContext(ctx).Delete(&Category{}, "id = ?", id).Error
}

func (s *Store) CountCategoryPosts(ctx context.Context, db *gorm.DB, id string) (int64, error) {
	var count int64
	err := db.WithContext(ctx).Model(&PostCategory{}).Where("category_id = ?", id).Count(&count).Error
	return count, err
}

func (s *Store) CategoryIDsForPosts(ctx context.Context, db *gorm.DB, postIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(postIDs))
	if len(postIDs) == 0 {
		return out, nil
	}
	var rows []PostCategory
	if err := db.WithContext(ctx).Where("post_id IN ?", postIDs).Order("category_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.PostID] = append(out[row.PostID], row.CategoryID)
	}
	return out, nil
}

func (s *Store) ReplacePostCategories(ctx context.Context, db *gorm.DB, postID string, categoryIDs []string) error {
	if err := db.WithContext(ctx).Where("post_id = ?", postID).Delete(&PostCategory{}).Error; err != nil {
		return err
	}
	rows := make([]PostCategory, 0, len(categoryIDs))
	for _, categoryID := range categoryIDs {
		rows = append(rows, PostCategory{PostID: postID, CategoryID: categoryID})
	}
	if len(rows) == 0 {
		return nil
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// ValidatePostCategories confirms that each category is available in the
// origin board. It returns the number of distinct valid categories.
func (s *Store) ValidatePostCategories(ctx context.Context, db *gorm.DB, boardID string, categoryIDs []string) (int64, error) {
	if len(categoryIDs) == 0 {
		return 0, nil
	}
	var count int64
	err := db.WithContext(ctx).Table("board_categories").
		Where("board_id = ? AND category_id IN ?", boardID, categoryIDs).
		Distinct("category_id").Count(&count).Error
	return count, err
}
