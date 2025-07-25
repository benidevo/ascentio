package quota

import (
	"context"
	"fmt"
	"time"

	ctxutil "github.com/benidevo/vega/internal/common/context"
	timeutil "github.com/benidevo/vega/internal/common/time"
)

// SearchQuotaService handles search-related quota management
type SearchQuotaService struct {
	repo        Repository
	isCloudMode bool
}

// NewSearchQuotaService creates a new search quota service
func NewSearchQuotaService(repo Repository, isCloudMode bool) *SearchQuotaService {
	return &SearchQuotaService{
		repo:        repo,
		isCloudMode: isCloudMode,
	}
}

// isUserAdmin checks if the user in context has admin role
func (s *SearchQuotaService) isUserAdmin(ctx context.Context) bool {
	role, _ := ctxutil.GetRole(ctx)
	return role == "Admin"
}

// CanSearchJobs checks if a user can search for more jobs
func (s *SearchQuotaService) CanSearchJobs(ctx context.Context, userID int) (*QuotaCheckResult, error) {
	today := timeutil.GetCurrentDate()
	usage, err := s.repo.GetDailyUsage(ctx, userID, today, QuotaKeyJobsFound)
	if err != nil {
		return nil, fmt.Errorf("failed to get job search usage: %w", err)
	}

	// Check if user is admin (admins have unlimited quota in cloud mode)
	if s.isCloudMode && s.isUserAdmin(ctx) {
		return &QuotaCheckResult{
			Allowed: true,
			Reason:  QuotaReasonOK,
			Status: QuotaStatus{
				Used:      usage,
				Limit:     -1,
				ResetDate: time.Time{},
			},
		}, nil
	}

	if !s.isCloudMode {
		// In self-hosted mode, always allow but show actual usage
		return &QuotaCheckResult{
			Allowed: true,
			Reason:  QuotaReasonOK,
			Status: QuotaStatus{
				Used:      usage,
				Limit:     -1,
				ResetDate: time.Time{},
			},
		}, nil
	}

	// Cloud mode: get quota configuration
	quotaConfig, err := s.repo.GetQuotaConfig(ctx, "job_search_daily")
	if err != nil {
		return nil, fmt.Errorf("failed to get quota config: %w", err)
	}

	limit := quotaConfig.FreeLimit
	status := QuotaStatus{
		Used:      usage,
		Limit:     limit,
		ResetDate: timeutil.GetTomorrowStart(),
	}

	if usage >= limit {
		return &QuotaCheckResult{
			Allowed: false,
			Reason:  QuotaReasonLimitReached,
			Status:  status,
		}, nil
	}

	return &QuotaCheckResult{
		Allowed: true,
		Reason:  QuotaReasonOK,
		Status:  status,
	}, nil
}

// RecordJobsFound records that jobs were found
func (s *SearchQuotaService) RecordJobsFound(ctx context.Context, userID int, count int) error {
	// Always record usage for tracking purposes
	today := timeutil.GetCurrentDate()
	return s.repo.IncrementDailyUsage(ctx, userID, today, QuotaKeyJobsFound, count)
}

// GetStatus returns the current search quota status
func (s *SearchQuotaService) GetStatus(ctx context.Context, userID int) (*QuotaCheckResult, error) {
	today := timeutil.GetCurrentDate()
	jobsFound, err := s.repo.GetDailyUsage(ctx, userID, today, QuotaKeyJobsFound)
	if err != nil {
		return nil, fmt.Errorf("failed to get job search usage: %w", err)
	}

	// Check if user is admin (admins have unlimited quota in cloud mode)
	if s.isCloudMode && s.isUserAdmin(ctx) {
		return &QuotaCheckResult{
			Allowed: true,
			Reason:  QuotaReasonOK,
			Status: QuotaStatus{
				Used:      jobsFound,
				Limit:     -1,
				ResetDate: time.Time{},
			},
		}, nil
	}

	if !s.isCloudMode {
		// In self-hosted mode, return actual usage but unlimited limit
		return &QuotaCheckResult{
			Allowed: true,
			Reason:  QuotaReasonOK,
			Status: QuotaStatus{
				Used:      jobsFound,
				Limit:     -1,
				ResetDate: time.Time{},
			},
		}, nil
	}

	// Cloud mode: get quota configuration
	quotaConfig, err := s.repo.GetQuotaConfig(ctx, "job_search_daily")
	if err != nil {
		return nil, fmt.Errorf("failed to get quota config: %w", err)
	}

	limit := quotaConfig.FreeLimit
	return &QuotaCheckResult{
		Allowed: jobsFound < limit,
		Reason:  QuotaReasonOK,
		Status: QuotaStatus{
			Used:      jobsFound,
			Limit:     limit,
			ResetDate: timeutil.GetTomorrowStart(),
		},
	}, nil
}
