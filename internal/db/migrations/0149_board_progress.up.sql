ALTER TABLE aura.board_cards
    ADD COLUMN progress double precision NOT NULL DEFAULT 0
    CONSTRAINT board_cards_progress_range CHECK (progress >= 0 AND progress <= 1);
