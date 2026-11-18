package factories

import (
	"context"
	"fmt"

	"mailai/database/models"
	"mailai/services/inbound"

	"github.com/uptrace/bun"
)

func NewProject(ctx context.Context, db *bun.DB, userID string) (*models.Project, error) {
	project := &models.Project{
		Title:       "Test project",
		Description: "A project created for testing",
		Status:      models.ProjectStatus("pending"),
		UserId:      userID,
	}

	// Project email addresses are unique, so each fixture needs a free one.
	for attempt := 1; ; attempt++ {
		project.EmailAddress = inbound.GenerateAddress("example.com", project.Title, attempt)
		taken, err := db.NewSelect().
			Model(&models.Project{}).
			Where("email_address = ?", project.EmailAddress).
			WhereAllWithDeleted().
			Exists(ctx)
		if err != nil {
			return nil, fmt.Errorf("check project fixture email address: %w", err)
		}
		if !taken {
			break
		}
	}

	if _, err := db.NewInsert().Model(project).Exec(ctx); err != nil {
		return nil, fmt.Errorf("insert project fixture: %w", err)
	}

	return project, nil
}
