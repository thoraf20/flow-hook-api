-- Add key_prefix column to api_keys table if it doesn't exist
-- This migration is safe to run multiple times

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_prefix VARCHAR(32);

-- Update existing rows to have a key_prefix (if any exist)
-- Since we can't derive the prefix from the hash, we'll set a placeholder
UPDATE api_keys SET key_prefix = 'fh_****' WHERE key_prefix IS NULL;

-- Make it NOT NULL after setting defaults
ALTER TABLE api_keys ALTER COLUMN key_prefix SET NOT NULL;

