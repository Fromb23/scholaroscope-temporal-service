ALTER TABLE integration_installation
    ADD COLUMN IF NOT EXISTS signing_secret text,
    ADD COLUMN IF NOT EXISTS callback_url text;

UPDATE integration_installation
SET signing_secret = signing_key_id
WHERE signing_secret IS NULL;

ALTER TABLE integration_installation
    ALTER COLUMN signing_secret SET NOT NULL;

CREATE INDEX IF NOT EXISTS integration_installation_key_ref_idx
    ON integration_installation(scholaroscope_installation_ref, signing_key_id);
