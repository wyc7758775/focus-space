UPDATE reader_preferences
SET timeline_width = 380,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = 'default' AND timeline_width = 450;
