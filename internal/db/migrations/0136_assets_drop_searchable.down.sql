ALTER TABLE aura.assets
    ADD COLUMN searchable_at timestamptz,
    DROP CONSTRAINT assets_status_check,
    ADD CONSTRAINT assets_status_check CHECK (status IN (
        'created', 'presigned', 'uploaded', 'accepted', 'processing', 'searchable',
        'embedding', 'complete', 'failed', 'refused', 'deleting', 'deleted', 'canceled'
    ));
