package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/benidevo/vega/internal/cache"
	commonerrors "github.com/benidevo/vega/internal/common/errors"
	"github.com/benidevo/vega/internal/job/models"
)

// SQLiteCompanyRepository is a SQLite implementation of CompanyRepository
type SQLiteCompanyRepository struct {
	db    *sql.DB
	cache cache.Cache
}

// NewSQLiteCompanyRepository creates a new SQLiteCompanyRepository instance
func NewSQLiteCompanyRepository(db *sql.DB, cache cache.Cache) *SQLiteCompanyRepository {
	return &SQLiteCompanyRepository{db: db, cache: cache}
}

// GetOrCreate retrieves a company by name or creates it if it doesn't exist
func (r *SQLiteCompanyRepository) GetOrCreate(ctx context.Context, name string) (*models.Company, error) {
	normalizedName, err := validateCompanyName(name)
	if err != nil {
		return nil, err
	}

	var company models.Company
	err = r.db.QueryRowContext(
		ctx,
		"SELECT id, name, created_at, updated_at FROM companies WHERE LOWER(name) = LOWER(?)",
		normalizedName,
	).Scan(&company.ID, &company.Name, &company.CreatedAt, &company.UpdatedAt)

	if err == sql.ErrNoRows {
		now := time.Now().UTC()
		result, err := r.db.ExecContext(
			ctx,
			"INSERT INTO companies (name, created_at, updated_at) VALUES (?, ?, ?)",
			normalizedName, now, now,
		)
		if err != nil {
			return nil, &commonerrors.RepositoryError{
				SentinelError: models.ErrFailedToCreateCompany,
				InnerError:    err,
			}
		}

		id, err := result.LastInsertId()
		if err != nil {
			return nil, &commonerrors.RepositoryError{
				SentinelError: models.ErrFailedToCreateCompany,
				InnerError:    err,
			}
		}

		company = models.Company{
			ID:        int(id),
			Name:      normalizedName,
			CreatedAt: now,
			UpdatedAt: now,
		}
		return &company, nil
	} else if err != nil {
		return nil, &commonerrors.RepositoryError{
			SentinelError: models.ErrCompanyNotFound,
			InnerError:    err,
		}
	}

	return &company, nil
}

// wrapError is a helper function to create a repository error
func wrapError(sentinel, inner error) error {
	return &commonerrors.RepositoryError{
		SentinelError: sentinel,
		InnerError:    inner,
	}
}

// GetByID retrieves a company by its ID
func (r *SQLiteCompanyRepository) GetByID(ctx context.Context, id int) (*models.Company, error) {
	if id <= 0 {
		return nil, models.ErrInvalidCompanyID
	}

	query := "SELECT id, name, created_at, updated_at FROM companies WHERE id = ?"
	var company models.Company
	err := r.db.QueryRowContext(ctx, query, id).Scan(&company.ID, &company.Name, &company.CreatedAt, &company.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrCompanyNotFound
		}
		return nil, wrapError(models.ErrCompanyNotFound, err)
	}
	return &company, nil
}

// GetByName retrieves a company by its name
func (r *SQLiteCompanyRepository) GetByName(ctx context.Context, name string) (*models.Company, error) {
	normalizedName, err := validateCompanyName(name)
	if err != nil {
		return nil, err
	}

	query := "SELECT id, name, created_at, updated_at FROM companies WHERE LOWER(name) = LOWER(?)"
	var company models.Company
	err = r.db.QueryRowContext(ctx, query, normalizedName).Scan(&company.ID, &company.Name, &company.CreatedAt, &company.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, models.ErrCompanyNotFound
		}
		return nil, wrapError(models.ErrCompanyNotFound, err)
	}

	return &company, nil
}

// validateCompanyName checks if the company name is valid and normalizes it
func validateCompanyName(name string) (string, error) {
	if name == "" {
		return "", models.ErrCompanyNameRequired
	}

	normalizedName := strings.TrimSpace(name)
	if normalizedName == "" {
		return "", models.ErrCompanyNameRequired
	}

	return normalizedName, nil
}

// GetAll retrieves all companies from the database
func (r *SQLiteCompanyRepository) GetAll(ctx context.Context) ([]*models.Company, error) {
	query := "SELECT id, name, created_at, updated_at FROM companies ORDER BY name"

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, wrapError(models.ErrFailedToCreateCompany, err)
	}
	defer rows.Close()

	var companies []*models.Company
	for rows.Next() {
		var company models.Company
		err := rows.Scan(&company.ID, &company.Name, &company.CreatedAt, &company.UpdatedAt)
		if err != nil {
			return nil, wrapError(models.ErrFailedToCreateCompany, err)
		}
		companies = append(companies, &company)
	}

	if err = rows.Err(); err != nil {
		return nil, wrapError(models.ErrFailedToCreateCompany, err)
	}

	return companies, nil
}

// Delete removes a company from the database by ID
func (r *SQLiteCompanyRepository) Delete(ctx context.Context, id int) error {
	if id <= 0 {
		return models.ErrInvalidCompanyID
	}

	query := "DELETE FROM companies WHERE id = ?"

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return wrapError(models.ErrFailedToDeleteCompany, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return wrapError(models.ErrFailedToDeleteCompany, err)
	}

	if rowsAffected == 0 {
		return models.ErrCompanyNotFound
	}

	return nil
}

// Update updates a company in the database
func (r *SQLiteCompanyRepository) Update(ctx context.Context, company *models.Company) error {
	if company == nil {
		return models.ErrInvalidCompanyID
	}

	if company.ID <= 0 {
		return models.ErrInvalidCompanyID
	}

	normalizedName, err := validateCompanyName(company.Name)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	query := "UPDATE companies SET name = ?, updated_at = ? WHERE id = ?"

	result, err := r.db.ExecContext(ctx, query, normalizedName, now, company.ID)
	if err != nil {
		return wrapError(models.ErrFailedToUpdateCompany, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return wrapError(models.ErrFailedToUpdateCompany, err)
	}

	if rowsAffected == 0 {
		return models.ErrCompanyNotFound
	}

	company.Name = normalizedName
	company.UpdatedAt = now

	return nil
}
