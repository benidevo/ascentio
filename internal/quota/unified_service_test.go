package quota

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	timeutil "github.com/benidevo/vega/internal/common/time"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockUnifiedJobRepository struct {
	mock.Mock
}

func (m *MockUnifiedJobRepository) GetByID(ctx context.Context, userID, jobID int) (*Job, error) {
	args := m.Called(ctx, userID, jobID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Job), args.Error(1)
}

func (m *MockUnifiedJobRepository) SetFirstAnalyzedAt(ctx context.Context, jobID int) error {
	args := m.Called(ctx, jobID)
	return args.Error(0)
}

func TestUnifiedService_CheckQuota(t *testing.T) {
	tests := []struct {
		name           string
		userID         int
		quotaType      string
		metadata       map[string]interface{}
		setupMock      func(sqlmock.Sqlmock, *MockUnifiedJobRepository)
		expectedResult *QuotaCheckResult
		expectError    bool
		errorContains  string
	}{
		{
			name:      "should_check_ai_analysis_quota_when_job_never_analyzed",
			userID:    1,
			quotaType: QuotaTypeAIAnalysis,
			metadata: map[string]interface{}{
				"job_id": 123,
			},
			setupMock: func(mock sqlmock.Sqlmock, jobRepo *MockUnifiedJobRepository) {
				// Job repo returns no analysis
				jobRepo.On("GetByID", context.Background(), 1, 123).
					Return(&Job{ID: 123, FirstAnalyzedAt: nil}, nil)

				// Monthly usage check
				monthYear := timeutil.GetCurrentMonthYear()
				mock.ExpectQuery("SELECT user_id, month_year, jobs_analyzed, updated_at FROM user_quota_usage").
					WithArgs(1, monthYear).
					WillReturnError(sql.ErrNoRows)

				// Quota config check
				mock.ExpectQuery("SELECT quota_type, free_limit, description, created_at, updated_at FROM quota_configs").
					WithArgs("ai_analysis_monthly").
					WillReturnRows(sqlmock.NewRows([]string{"quota_type", "free_limit", "description", "created_at", "updated_at"}).
						AddRow("ai_analysis_monthly", 10, "AI Analysis quota", time.Now(), time.Now()))
			},
			expectedResult: &QuotaCheckResult{
				Allowed: true,
				Reason:  QuotaReasonOK,
				Status: QuotaStatus{
					Used:  0,
					Limit: 10,
				},
			},
		},
		{
			name:      "should_check_ai_analysis_quota_when_reanalysis",
			userID:    2,
			quotaType: QuotaTypeAIAnalysis,
			metadata: map[string]interface{}{
				"job_id": 456,
			},
			setupMock: func(mock sqlmock.Sqlmock, jobRepo *MockUnifiedJobRepository) {
				// Job was analyzed before
				analyzedAt := time.Now().Add(-24 * time.Hour)
				jobRepo.On("GetByID", context.Background(), 2, 456).
					Return(&Job{ID: 456, FirstAnalyzedAt: &analyzedAt}, nil)

				// The service still checks monthly usage even for reanalysis
				monthYear := timeutil.GetCurrentMonthYear()
				mock.ExpectQuery("SELECT user_id, month_year, jobs_analyzed, updated_at FROM user_quota_usage").
					WithArgs(2, monthYear).
					WillReturnRows(sqlmock.NewRows([]string{"user_id", "month_year", "jobs_analyzed", "updated_at"}).
						AddRow(2, monthYear, 3, time.Now()))

				// Quota config check
				mock.ExpectQuery("SELECT quota_type, free_limit, description, created_at, updated_at FROM quota_configs").
					WithArgs("ai_analysis_monthly").
					WillReturnRows(sqlmock.NewRows([]string{"quota_type", "free_limit", "description", "created_at", "updated_at"}).
						AddRow("ai_analysis_monthly", 10, "AI Analysis quota", time.Now(), time.Now()))
			},
			expectedResult: &QuotaCheckResult{
				Allowed: true,
				Reason:  QuotaReasonReanalysis,
				Status: QuotaStatus{
					Used:      3,
					Limit:     10,
					ResetDate: time.Time{},
				},
			},
		},
		{
			name:      "should_check_job_search_quota",
			userID:    3,
			quotaType: QuotaTypeJobCapture,
			metadata:  map[string]interface{}{},
			setupMock: func(mock sqlmock.Sqlmock, jobRepo *MockUnifiedJobRepository) {
				// Daily usage check
				today := timeutil.GetCurrentDate()
				mock.ExpectQuery("SELECT value FROM user_daily_quotas").
					WithArgs(3, today, QuotaKeyJobsCaptured).
					WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(25))
			},
			expectedResult: &QuotaCheckResult{
				Allowed: true,
				Reason:  QuotaReasonOK,
				Status: QuotaStatus{
					Used:      25,
					Limit:     -1,
					ResetDate: time.Time{},
				},
			},
		},
		{
			name:      "should_return_error_when_job_id_missing_for_ai_quota",
			userID:    1,
			quotaType: QuotaTypeAIAnalysis,
			metadata:  map[string]interface{}{},
			setupMock: func(mock sqlmock.Sqlmock, jobRepo *MockUnifiedJobRepository) {
			},
			expectError:   true,
			errorContains: "job_id required for AI analysis quota check",
		},
		{
			name:      "should_return_error_when_unknown_quota_type",
			userID:    1,
			quotaType: "unknown_type",
			metadata:  map[string]interface{}{},
			setupMock: func(mock sqlmock.Sqlmock, jobRepo *MockUnifiedJobRepository) {
			},
			expectError:   true,
			errorContains: "unknown quota type: unknown_type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, sqlMock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()

			jobRepo := &MockUnifiedJobRepository{}

			if tt.setupMock != nil {
				tt.setupMock(sqlMock, jobRepo)
			}

			service := NewUnifiedService(db, jobRepo, true)
			result, err := service.CheckQuota(context.Background(), tt.userID, tt.quotaType, tt.metadata)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.expectedResult.Allowed, result.Allowed)
				assert.Equal(t, tt.expectedResult.Reason, result.Reason)
				assert.Equal(t, tt.expectedResult.Status.Used, result.Status.Used)
				assert.Equal(t, tt.expectedResult.Status.Limit, result.Status.Limit)
			}

			assert.NoError(t, sqlMock.ExpectationsWereMet())
			jobRepo.AssertExpectations(t)
		})
	}
}

func TestUnifiedService_RecordUsage(t *testing.T) {
	tests := []struct {
		name          string
		userID        int
		quotaType     string
		metadata      map[string]interface{}
		setupMock     func(sqlmock.Sqlmock, *MockUnifiedJobRepository)
		expectError   bool
		errorContains string
	}{
		{
			name:      "should_record_ai_analysis_usage",
			userID:    1,
			quotaType: QuotaTypeAIAnalysis,
			metadata: map[string]interface{}{
				"job_id": 123,
			},
			setupMock: func(mock sqlmock.Sqlmock, jobRepo *MockUnifiedJobRepository) {
				// Expect transaction
				mock.ExpectBegin()

				// Expect SetFirstAnalyzedAt call
				jobRepo.On("SetFirstAnalyzedAt", context.Background(), 123).Return(nil)

				// Record analysis
				monthYear := timeutil.GetCurrentMonthYear()
				mock.ExpectExec("INSERT INTO user_quota_usage").
					WithArgs(1, monthYear).
					WillReturnResult(sqlmock.NewResult(1, 1))

				// Expect commit
				mock.ExpectCommit()
			},
		},
		{
			name:      "should_record_job_search_usage_with_count",
			userID:    2,
			quotaType: QuotaTypeJobCapture,
			metadata: map[string]interface{}{
				"count": 10,
			},
			setupMock: func(mock sqlmock.Sqlmock, jobRepo *MockUnifiedJobRepository) {
				// Record jobs found
				today := timeutil.GetCurrentDate()
				mock.ExpectExec("INSERT INTO user_daily_quotas").
					WithArgs(2, today, QuotaKeyJobsCaptured, 10, 10).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
		},
		{
			name:      "should_record_job_search_usage_with_default_count",
			userID:    3,
			quotaType: QuotaTypeJobCapture,
			metadata:  map[string]interface{}{},
			setupMock: func(mock sqlmock.Sqlmock, jobRepo *MockUnifiedJobRepository) {
				// Record jobs found with default count of 1
				today := timeutil.GetCurrentDate()
				mock.ExpectExec("INSERT INTO user_daily_quotas").
					WithArgs(3, today, QuotaKeyJobsCaptured, 1, 1).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
		},
		{
			name:      "should_return_error_when_job_id_missing_for_ai_recording",
			userID:    1,
			quotaType: QuotaTypeAIAnalysis,
			metadata:  map[string]interface{}{},
			setupMock: func(mock sqlmock.Sqlmock, jobRepo *MockUnifiedJobRepository) {
			},
			expectError:   true,
			errorContains: "job_id required for AI analysis recording",
		},
		{
			name:      "should_return_error_when_unknown_quota_type",
			userID:    1,
			quotaType: "unknown_type",
			metadata:  map[string]interface{}{},
			setupMock: func(mock sqlmock.Sqlmock, jobRepo *MockUnifiedJobRepository) {
			},
			expectError:   true,
			errorContains: "unknown quota type: unknown_type",
		},
		{
			name:      "should_return_error_when_database_fails",
			userID:    1,
			quotaType: QuotaTypeAIAnalysis,
			metadata: map[string]interface{}{
				"job_id": 123,
			},
			setupMock: func(mock sqlmock.Sqlmock, jobRepo *MockUnifiedJobRepository) {
				// Expect transaction
				mock.ExpectBegin()

				// Expect SetFirstAnalyzedAt call
				jobRepo.On("SetFirstAnalyzedAt", context.Background(), 123).Return(nil)

				monthYear := timeutil.GetCurrentMonthYear()
				mock.ExpectExec("INSERT INTO user_quota_usage").
					WithArgs(1, monthYear).
					WillReturnError(errors.New("database error"))

				// Expect rollback
				mock.ExpectRollback()
			},
			expectError:   true,
			errorContains: "database error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, sqlMock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()

			jobRepo := &MockUnifiedJobRepository{}

			if tt.setupMock != nil {
				tt.setupMock(sqlMock, jobRepo)
			}

			service := NewUnifiedService(db, jobRepo, true)
			err = service.RecordUsage(context.Background(), tt.userID, tt.quotaType, tt.metadata)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, sqlMock.ExpectationsWereMet())
			jobRepo.AssertExpectations(t)
		})
	}
}

func TestUnifiedService_GetAllQuotaStatus(t *testing.T) {
	tests := []struct {
		name          string
		userID        int
		setupMock     func(sqlmock.Sqlmock)
		expectedAI    QuotaStatus
		expectedJob   QuotaStatus
		expectError   bool
		errorContains string
	}{
		{
			name:   "should_return_all_quota_statuses",
			userID: 1,
			setupMock: func(mock sqlmock.Sqlmock) {
				monthYear := timeutil.GetCurrentMonthYear()
				today := timeutil.GetCurrentDate()

				// AI quota status - monthly usage
				mock.ExpectQuery("SELECT user_id, month_year, jobs_analyzed, updated_at FROM user_quota_usage").
					WithArgs(1, monthYear).
					WillReturnRows(sqlmock.NewRows([]string{"user_id", "month_year", "jobs_analyzed", "updated_at"}).
						AddRow(1, monthYear, 5, time.Now()))

				// Quota config
				mock.ExpectQuery("SELECT quota_type, free_limit, description, created_at, updated_at FROM quota_configs").
					WithArgs("ai_analysis_monthly").
					WillReturnRows(sqlmock.NewRows([]string{"quota_type", "free_limit", "description", "created_at", "updated_at"}).
						AddRow("ai_analysis_monthly", 10, "AI Analysis quota", time.Now(), time.Now()))

				// Job search quota - daily usage
				mock.ExpectQuery("SELECT value FROM user_daily_quotas").
					WithArgs(1, today, QuotaKeyJobsCaptured).
					WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(20))
			},
			expectedAI: QuotaStatus{
				Used:  5,
				Limit: 10,
			},
			expectedJob: QuotaStatus{
				Used:      20,
				Limit:     -1,
				ResetDate: time.Time{},
			},
		},
		{
			name:   "should_handle_no_usage",
			userID: 2,
			setupMock: func(mock sqlmock.Sqlmock) {
				monthYear := timeutil.GetCurrentMonthYear()
				today := timeutil.GetCurrentDate()

				// AI quota - no usage
				mock.ExpectQuery("SELECT user_id, month_year, jobs_analyzed, updated_at FROM user_quota_usage").
					WithArgs(2, monthYear).
					WillReturnError(sql.ErrNoRows)

				// Quota config
				mock.ExpectQuery("SELECT quota_type, free_limit, description, created_at, updated_at FROM quota_configs").
					WithArgs("ai_analysis_monthly").
					WillReturnRows(sqlmock.NewRows([]string{"quota_type", "free_limit", "description", "created_at", "updated_at"}).
						AddRow("ai_analysis_monthly", 10, "AI Analysis quota", time.Now(), time.Now()))

				// Job search - no usage
				mock.ExpectQuery("SELECT value FROM user_daily_quotas").
					WithArgs(2, today, QuotaKeyJobsCaptured).
					WillReturnError(sql.ErrNoRows)
			},
			expectedAI: QuotaStatus{
				Used:  0,
				Limit: 10,
			},
			expectedJob: QuotaStatus{
				Used:      0,
				Limit:     -1,
				ResetDate: time.Time{},
			},
		},
		{
			name:   "should_return_error_when_ai_quota_fails",
			userID: 3,
			setupMock: func(mock sqlmock.Sqlmock) {
				monthYear := timeutil.GetCurrentMonthYear()

				// AI quota fails
				mock.ExpectQuery("SELECT user_id, month_year, jobs_analyzed, updated_at FROM user_quota_usage").
					WithArgs(3, monthYear).
					WillReturnError(errors.New("database error"))
			},
			expectError:   true,
			errorContains: "failed to get AI quota status",
		},
		{
			name:   "should_return_error_when_job_search_quota_fails",
			userID: 4,
			setupMock: func(mock sqlmock.Sqlmock) {
				monthYear := timeutil.GetCurrentMonthYear()
				today := timeutil.GetCurrentDate()

				// AI quota succeeds
				mock.ExpectQuery("SELECT user_id, month_year, jobs_analyzed, updated_at FROM user_quota_usage").
					WithArgs(4, monthYear).
					WillReturnError(sql.ErrNoRows)

				mock.ExpectQuery("SELECT quota_type, free_limit, description, created_at, updated_at FROM quota_configs").
					WithArgs("ai_analysis_monthly").
					WillReturnRows(sqlmock.NewRows([]string{"quota_type", "free_limit", "description", "created_at", "updated_at"}).
						AddRow("ai_analysis_monthly", 10, "AI Analysis quota", time.Now(), time.Now()))

				// Job search quota fails
				mock.ExpectQuery("SELECT value FROM user_daily_quotas").
					WithArgs(4, today, QuotaKeyJobsCaptured).
					WillReturnError(errors.New("database error"))
			},
			expectError:   true,
			errorContains: "failed to get job capture quota status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, sqlMock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()

			jobRepo := &MockUnifiedJobRepository{}

			if tt.setupMock != nil {
				tt.setupMock(sqlMock)
			}

			service := NewUnifiedService(db, jobRepo, true)
			result, err := service.GetAllQuotaStatus(context.Background(), tt.userID)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, result)

				status, ok := result.(*UnifiedQuotaStatus)
				require.True(t, ok)

				assert.Equal(t, tt.expectedAI.Used, status.AIAnalysis.Used)
				assert.Equal(t, tt.expectedAI.Limit, status.AIAnalysis.Limit)

				assert.Equal(t, tt.expectedJob.Used, status.JobCapture.Used)
				assert.Equal(t, tt.expectedJob.Limit, status.JobCapture.Limit)
			}

			assert.NoError(t, sqlMock.ExpectationsWereMet())
		})
	}
}

func TestUnifiedService_ExposedServices(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	jobRepo := &MockUnifiedJobRepository{}
	service := NewUnifiedService(db, jobRepo, true)

	t.Run("should_expose_ai_quota_service", func(t *testing.T) {
		aiService := service.AIQuotaService()
		assert.NotNil(t, aiService)
		assert.IsType(t, &Service{}, aiService)
	})

	t.Run("should_expose_job_capture_service", func(t *testing.T) {
		jobCaptureService := service.JobCaptureService()
		assert.NotNil(t, jobCaptureService)
		assert.IsType(t, &JobCaptureService{}, jobCaptureService)
	})
}
