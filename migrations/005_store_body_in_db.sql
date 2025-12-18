-- Migration: Store request bodies in PostgreSQL instead of files
-- This migration adds a body column to the requests table

-- Add body column (TEXT to store request body as string)
-- We'll use TEXT instead of BYTEA since most webhook payloads are JSON/text
ALTER TABLE requests ADD COLUMN IF NOT EXISTS body TEXT;

-- Create index on body_size for analytics queries (if not exists)
CREATE INDEX IF NOT EXISTS idx_requests_body_size ON requests(body_size) WHERE body_size > 0;

-- Note: body_path column is kept for backward compatibility during migration
-- It can be removed in a future migration if desired

