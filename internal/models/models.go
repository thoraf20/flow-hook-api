package models

import (
	"time"

	"github.com/google/uuid"
)

type Endpoint struct {
	ID        uuid.UUID `json:"id"`
	Slug      string    `json:"slug"`
	Name      *string   `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Request struct {
	ID          uuid.UUID              `json:"id"`
	EndpointID  uuid.UUID              `json:"endpoint_id"`
	Method      string                 `json:"method"`
	Path        *string                `json:"path,omitempty"`
	Headers     map[string]interface{} `json:"headers"`
	QueryParams map[string]interface{} `json:"query_params"`
	IP          *string                 `json:"ip,omitempty"`
	BodyPath    *string                 `json:"body_path,omitempty"` // Deprecated: kept for backward compatibility
	Body        *string                 `json:"body,omitempty"`       // Request body stored in database
	BodySize    int64                  `json:"body_size"`
	ContentType *string                 `json:"content_type,omitempty"`
	ReceivedAt  time.Time              `json:"received_at"`
}

type CreateEndpointRequest struct {
	Name string `json:"name"`
}

type CreateEndpointResponse struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
	URL  string    `json:"url"`
}

type RequestListResponse struct {
	Requests []Request `json:"requests"`
	Total    int       `json:"total"`
	Limit    int       `json:"limit"`
	Offset   int       `json:"offset"`
}

type Replay struct {
	ID             uuid.UUID              `json:"id"`
	RequestID      uuid.UUID              `json:"request_id"`
	TargetURL      string                 `json:"target_url"`
	Method         string                 `json:"method"`
	Headers        map[string]interface{} `json:"headers"`
	Body           *string                `json:"body,omitempty"`
	Attempts       int                    `json:"attempts"`
	Status         string                 `json:"status"`
	ResponseStatus *int                    `json:"response_status,omitempty"`
	ResponseHeaders map[string]interface{} `json:"response_headers,omitempty"`
	ResponseBody   *string                 `json:"response_body,omitempty"`
	ErrorMessage   *string                 `json:"error_message,omitempty"`
	LastAttemptAt  *time.Time              `json:"last_attempt_at,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
}

type CreateReplayRequest struct {
	TargetURL string                 `json:"target_url"`
	Method    *string                `json:"method,omitempty"` // Optional, defaults to original method
	Headers   map[string]interface{} `json:"headers,omitempty"` // Optional, defaults to original headers
	Body      *string                `json:"body,omitempty"` // Optional, defaults to original body
}

type CreateReplayResponse struct {
	ReplayID uuid.UUID `json:"replay_id"`
	Status   string    `json:"status"`
}

type ForwardingRule struct {
	ID             uuid.UUID              `json:"id"`
	EndpointID     uuid.UUID              `json:"endpoint_id"`
	TargetURL      string                 `json:"target_url"`
	Method         *string                 `json:"method,omitempty"`
	Headers        map[string]interface{} `json:"headers"`
	Enabled        bool                   `json:"enabled"`
	MaxRetries     int                    `json:"max_retries"`
	BackoffConfig  map[string]interface{} `json:"backoff_config"`
	ConditionType  *string                 `json:"condition_type,omitempty"`
	ConditionConfig map[string]interface{} `json:"condition_config,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
}

type CreateForwardingRuleRequest struct {
	TargetURL      string                 `json:"target_url"`
	Method         *string                 `json:"method,omitempty"`
	Headers        map[string]interface{} `json:"headers,omitempty"`
	MaxRetries     *int                   `json:"max_retries,omitempty"`
	BackoffConfig  map[string]interface{} `json:"backoff_config,omitempty"`
	ConditionType  *string                 `json:"condition_type,omitempty"`
	ConditionConfig map[string]interface{} `json:"condition_config,omitempty"`
}

type ForwardAttempt struct {
	ID              uuid.UUID              `json:"id"`
	RequestID       uuid.UUID              `json:"request_id"`
	ForwardingRuleID uuid.UUID              `json:"forwarding_rule_id"`
	AttemptNumber   int                    `json:"attempt_number"`
	Status          string                 `json:"status"`
	ResponseStatus  *int                    `json:"response_status,omitempty"`
	ResponseHeaders map[string]interface{} `json:"response_headers,omitempty"`
	ResponseBody    *string                 `json:"response_body,omitempty"`
	ErrorMessage    *string                 `json:"error_message,omitempty"`
	DurationMs      *int                    `json:"duration_ms,omitempty"`
	AttemptedAt     time.Time               `json:"attempted_at"`
}

type Transformation struct {
	ID        uuid.UUID `json:"id"`
	EndpointID uuid.UUID `json:"endpoint_id"`
	Name      string    `json:"name"`
	Language  string    `json:"language"`
	Script    string    `json:"script"`
	ApplyTo   string    `json:"apply_to"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateTransformationRequest struct {
	Name     string `json:"name"`
	Language string `json:"language"` // jsonata|jq|javascript
	Script   string `json:"script"`
	ApplyTo  string `json:"apply_to"` // request|response|both
	Enabled  *bool  `json:"enabled,omitempty"`
}

type RetentionPolicy struct {
	ID            uuid.UUID `json:"id"`
	EndpointID    uuid.UUID `json:"endpoint_id"`
	RetentionDays int       `json:"retention_days"`
	AutoDelete    bool      `json:"auto_delete"`
	ArchiveEnabled bool      `json:"archive_enabled"`
	ArchivePath   *string   `json:"archive_path,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateRetentionPolicyRequest struct {
	RetentionDays *int    `json:"retention_days,omitempty"`
	AutoDelete    *bool   `json:"auto_delete,omitempty"`
	ArchiveEnabled *bool   `json:"archive_enabled,omitempty"`
	ArchivePath   *string `json:"archive_path,omitempty"`
}

type RequestTemplate struct {
	ID          uuid.UUID              `json:"id"`
	EndpointID  uuid.UUID              `json:"endpoint_id"`
	Name        string                 `json:"name"`
	Method      string                 `json:"method"`
	URL         string                 `json:"url"`
	Headers     map[string]interface{} `json:"headers"`
	Body        *string                `json:"body,omitempty"`
	Description *string                `json:"description,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

type CreateRequestTemplateRequest struct {
	Name        string                 `json:"name"`
	Method      string                 `json:"method"`
	URL         string                 `json:"url"`
	Headers     map[string]interface{} `json:"headers,omitempty"`
	Body        *string                 `json:"body,omitempty"`
	Description *string                `json:"description,omitempty"`
}

type User struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      *string   `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     *string `json:"name,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type APIKey struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"` // First 8 chars for display
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type CreateAPIKeyRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type CreateAPIKeyResponse struct {
	ID        uuid.UUID `json:"id"`
	Key       string    `json:"key"` // Only shown once on creation
	KeyPrefix string    `json:"key_prefix"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

