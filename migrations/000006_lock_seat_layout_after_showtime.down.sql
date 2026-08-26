DROP TRIGGER IF EXISTS showtimes_lock_studio_layout ON showtimes;
DROP TRIGGER IF EXISTS seats_reject_layout_changes_with_showtimes ON seats;
DROP FUNCTION IF EXISTS lock_showtime_studio_layout();
DROP FUNCTION IF EXISTS reject_seat_layout_change_with_showtimes();
DROP FUNCTION IF EXISTS lock_studio_layout(UUID, UUID);
