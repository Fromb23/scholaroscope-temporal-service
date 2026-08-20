DROP INDEX IF EXISTS integration_installation_key_ref_idx;

ALTER TABLE integration_installation
    DROP COLUMN IF EXISTS callback_url,
    DROP COLUMN IF EXISTS signing_secret;
