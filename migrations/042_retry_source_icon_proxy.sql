-- Older builds recorded direct pbs.twimg.com failures before the bounded
-- image-proxy fallback existed. These rows contain no user data or image
-- bytes; removing them lets the new resolver retry immediately once.
DELETE FROM source_icon_cache
WHERE length(body) = 0
  AND lower(icon_url) LIKE 'https://pbs.twimg.com/%';
