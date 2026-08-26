CREATE FUNCTION lock_studio_layout(studio_a UUID, studio_b UUID)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    IF studio_b IS NULL OR studio_a = studio_b THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(studio_a::TEXT, 0));
    ELSIF studio_a < studio_b THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(studio_a::TEXT, 0));
        PERFORM pg_advisory_xact_lock(hashtextextended(studio_b::TEXT, 0));
    ELSE
        PERFORM pg_advisory_xact_lock(hashtextextended(studio_b::TEXT, 0));
        PERFORM pg_advisory_xact_lock(hashtextextended(studio_a::TEXT, 0));
    END IF;
END;
$$;

CREATE FUNCTION reject_seat_layout_change_with_showtimes()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        PERFORM lock_studio_layout(NEW.studio_id, NULL);
        IF EXISTS (SELECT 1 FROM showtimes WHERE studio_id = NEW.studio_id) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                CONSTRAINT = 'seat_layout_locked_by_showtime',
                MESSAGE = 'seat layout cannot change after a showtime exists';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'DELETE' THEN
        PERFORM lock_studio_layout(OLD.studio_id, NULL);
        IF EXISTS (SELECT 1 FROM showtimes WHERE studio_id = OLD.studio_id) THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                CONSTRAINT = 'seat_layout_locked_by_showtime',
                MESSAGE = 'seat layout cannot change after a showtime exists';
        END IF;
        RETURN OLD;
    END IF;

    IF OLD.studio_id = NEW.studio_id
        AND OLD.row_label = NEW.row_label
        AND OLD.seat_number = NEW.seat_number
        AND OLD.seat_type = NEW.seat_type THEN
        RETURN NEW;
    END IF;

    PERFORM lock_studio_layout(OLD.studio_id, NEW.studio_id);
    IF EXISTS (
        SELECT 1
        FROM showtimes
        WHERE studio_id = OLD.studio_id OR studio_id = NEW.studio_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            CONSTRAINT = 'seat_layout_locked_by_showtime',
            MESSAGE = 'seat layout cannot change after a showtime exists';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION lock_showtime_studio_layout()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        PERFORM lock_studio_layout(OLD.studio_id, NEW.studio_id);
    ELSE
        PERFORM lock_studio_layout(NEW.studio_id, NULL);
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER seats_reject_layout_changes_with_showtimes
BEFORE INSERT OR UPDATE OR DELETE ON seats
FOR EACH ROW EXECUTE FUNCTION reject_seat_layout_change_with_showtimes();

CREATE TRIGGER showtimes_lock_studio_layout
BEFORE INSERT OR UPDATE ON showtimes
FOR EACH ROW EXECUTE FUNCTION lock_showtime_studio_layout();
