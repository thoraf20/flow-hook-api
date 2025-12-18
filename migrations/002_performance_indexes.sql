-- Performance optimization indexes
-- These indexes improve query performance for common operations

-- Index for filtering requests by endpoint and date (most common query)
CREATE INDEX IF NOT EXISTS idx_requests_endpoint_date 
ON requests(endpoint_id, received_at DESC);

-- Index for method filtering
CREATE INDEX IF NOT EXISTS idx_requests_method 
ON requests(method) WHERE method IS NOT NULL;

-- Index for search operations (GIN index for JSONB)
CREATE INDEX IF NOT EXISTS idx_requests_headers_gin 
ON requests USING GIN (headers);

-- Index for forwarding rules lookups
CREATE INDEX IF NOT EXISTS idx_forwarding_rules_endpoint_enabled 
ON forwarding_rules(endpoint_id, enabled) WHERE enabled = true;

-- Index for forward attempts by rule and status
CREATE INDEX IF NOT EXISTS idx_forward_attempts_rule_status 
ON forward_attempts(forwarding_rule_id, status, attempted_at DESC);

-- Index for replays by request
CREATE INDEX IF NOT EXISTS idx_replays_request_id 
ON replays(request_id, created_at DESC);

-- Index for user sessions (for fast lookups)
-- Note: Cannot use NOW() in index predicate as it's not IMMUTABLE
-- Instead, we'll create a simple index and filter in queries
CREATE INDEX IF NOT EXISTS idx_user_sessions_token 
ON user_sessions(token);
CREATE INDEX IF NOT EXISTS idx_user_sessions_expires_at 
ON user_sessions(expires_at);

-- Index for endpoint lookups by slug (already should exist, but ensuring it)
CREATE INDEX IF NOT EXISTS idx_endpoints_slug 
ON endpoints(slug);

-- Composite index for analytics queries
CREATE INDEX IF NOT EXISTS idx_requests_analytics 
ON requests(endpoint_id, received_at DESC, method);

-- Index for retention policy cleanup
-- Note: Cannot use NOW() in index predicate as it's not IMMUTABLE
-- Create simple index and filter in queries
CREATE INDEX IF NOT EXISTS idx_requests_received_at 
ON requests(received_at);

