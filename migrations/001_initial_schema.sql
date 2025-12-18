-- FlowHook Phase 1: Initial Schema
-- Simplified schema for MVP webhook capture

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Endpoints table
CREATE TABLE IF NOT EXISTS endpoints (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    slug VARCHAR(128) NOT NULL UNIQUE,
    name VARCHAR(255),
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_endpoints_slug ON endpoints(slug);
CREATE INDEX IF NOT EXISTS idx_endpoints_created_at ON endpoints(created_at);

-- Requests table
CREATE TABLE IF NOT EXISTS requests (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    endpoint_id UUID NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    method VARCHAR(10) NOT NULL,
    path TEXT,
    headers JSONB DEFAULT '{}'::jsonb,
    query_params JSONB DEFAULT '{}'::jsonb,
    ip INET,
    body_path TEXT,
    body_size BIGINT DEFAULT 0,
    content_type TEXT,
    received_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_requests_endpoint_id ON requests(endpoint_id);
CREATE INDEX IF NOT EXISTS idx_requests_received_at ON requests(received_at DESC);
CREATE INDEX IF NOT EXISTS idx_requests_method ON requests(method);

-- Replays table for request replay functionality
CREATE TABLE IF NOT EXISTS replays (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    request_id UUID NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    target_url TEXT NOT NULL,
    method VARCHAR(10) NOT NULL,
    headers JSONB DEFAULT '{}'::jsonb,
    body TEXT,
    attempts INTEGER DEFAULT 0,
    status VARCHAR(32) DEFAULT 'pending', -- pending|success|failed
    response_status INTEGER,
    response_headers JSONB,
    response_body TEXT,
    error_message TEXT,
    last_attempt_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_replays_request_id ON replays(request_id);
CREATE INDEX IF NOT EXISTS idx_replays_status ON replays(status);
CREATE INDEX IF NOT EXISTS idx_replays_created_at ON replays(created_at DESC);

-- Forwarding rules table
CREATE TABLE IF NOT EXISTS forwarding_rules (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    endpoint_id UUID NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    target_url TEXT NOT NULL,
    method VARCHAR(10),
    headers JSONB DEFAULT '{}'::jsonb,
    enabled BOOLEAN DEFAULT TRUE,
    max_retries INTEGER DEFAULT 3,
    backoff_config JSONB DEFAULT '{"type":"exponential","base":2,"min_ms":1000,"max_ms":30000}'::jsonb,
    condition_type VARCHAR(32), -- always|header_match|body_match
    condition_config JSONB, -- e.g., {"header": "Content-Type", "value": "application/json"}
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_forwarding_rules_endpoint_id ON forwarding_rules(endpoint_id);
CREATE INDEX IF NOT EXISTS idx_forwarding_rules_enabled ON forwarding_rules(enabled);

-- Forward attempts table
CREATE TABLE IF NOT EXISTS forward_attempts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    request_id UUID NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    forwarding_rule_id UUID NOT NULL REFERENCES forwarding_rules(id) ON DELETE CASCADE,
    attempt_number INTEGER DEFAULT 1,
    status VARCHAR(32) DEFAULT 'pending', -- pending|success|failed
    response_status INTEGER,
    response_headers JSONB,
    response_body TEXT,
    error_message TEXT,
    duration_ms INTEGER,
    attempted_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_forward_attempts_request_id ON forward_attempts(request_id);
CREATE INDEX IF NOT EXISTS idx_forward_attempts_rule_id ON forward_attempts(forwarding_rule_id);
CREATE INDEX IF NOT EXISTS idx_forward_attempts_status ON forward_attempts(status);
CREATE INDEX IF NOT EXISTS idx_forward_attempts_attempted_at ON forward_attempts(attempted_at DESC);

-- Delivery statistics view (materialized for performance)
CREATE MATERIALIZED VIEW IF NOT EXISTS delivery_stats AS
SELECT 
    forwarding_rule_id,
    DATE_TRUNC('hour', attempted_at) as hour,
    COUNT(*) as total_attempts,
    COUNT(*) FILTER (WHERE status = 'success') as successful,
    COUNT(*) FILTER (WHERE status = 'failed') as failed,
    AVG(duration_ms) FILTER (WHERE duration_ms IS NOT NULL) as avg_duration_ms,
    MAX(duration_ms) as max_duration_ms,
    MIN(duration_ms) as min_duration_ms
FROM forward_attempts
GROUP BY forwarding_rule_id, DATE_TRUNC('hour', attempted_at);

CREATE INDEX IF NOT EXISTS idx_delivery_stats_rule_id ON delivery_stats(forwarding_rule_id);
CREATE INDEX IF NOT EXISTS idx_delivery_stats_hour ON delivery_stats(hour DESC);

-- Transformations table
CREATE TABLE IF NOT EXISTS transformations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    endpoint_id UUID NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    language VARCHAR(32) DEFAULT 'jsonata', -- jsonata|jq|javascript
    script TEXT NOT NULL,
    apply_to VARCHAR(32) DEFAULT 'request', -- request|response|both
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_transformations_endpoint_id ON transformations(endpoint_id);
CREATE INDEX IF NOT EXISTS idx_transformations_enabled ON transformations(enabled);

-- Endpoint settings for signature verification and rate limiting
CREATE TABLE IF NOT EXISTS endpoint_settings (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    endpoint_id UUID NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE UNIQUE,
    hmac_secret TEXT,
    hmac_algorithm VARCHAR(32) DEFAULT 'sha256', -- sha256|sha1|sha512
    rate_limit_per_minute INTEGER,
    rate_limit_per_hour INTEGER,
    rate_limit_per_day INTEGER,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_endpoint_settings_endpoint_id ON endpoint_settings(endpoint_id);

-- Request retention policies
CREATE TABLE IF NOT EXISTS retention_policies (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    endpoint_id UUID NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE UNIQUE,
    retention_days INTEGER DEFAULT 30,
    auto_delete BOOLEAN DEFAULT FALSE,
    archive_enabled BOOLEAN DEFAULT FALSE,
    archive_path TEXT,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_retention_policies_endpoint_id ON retention_policies(endpoint_id);

-- Request templates
CREATE TABLE IF NOT EXISTS request_templates (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    endpoint_id UUID NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    method VARCHAR(10) NOT NULL,
    url TEXT NOT NULL,
    headers JSONB DEFAULT '{}'::jsonb,
    body TEXT,
    description TEXT,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_request_templates_endpoint_id ON request_templates(endpoint_id);

-- Users table for authentication
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    name VARCHAR(255),
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

-- User sessions
CREATE TABLE IF NOT EXISTS user_sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token TEXT UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id ON user_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_user_sessions_token ON user_sessions(token);
CREATE INDEX IF NOT EXISTS idx_user_sessions_expires_at ON user_sessions(expires_at);

-- Add user_id to endpoints
ALTER TABLE endpoints ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_endpoints_user_id ON endpoints(user_id);

