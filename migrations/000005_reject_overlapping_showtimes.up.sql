CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE showtimes
    ADD CONSTRAINT showtimes_no_studio_overlap
    EXCLUDE USING gist (
        studio_id WITH =,
        tstzrange(starts_at, ends_at, '[)') WITH &&
    );
