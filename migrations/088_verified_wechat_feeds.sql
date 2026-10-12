-- Replace keyword-search collectors for the two maintained WeChat sources with
-- exact bounded feeds whose channel title and article account IDs are verified
-- by the runtime. Existing history and user preferences remain preserved.

CREATE TEMP TABLE plaud_verified_wechat_feeds (
    id TEXT PRIMARY KEY,
    legacy_url TEXT NOT NULL,
    legacy_folo_url TEXT NOT NULL,
    direct_url TEXT NOT NULL
);

INSERT INTO plaud_verified_wechat_feeds(id, legacy_url, legacy_folo_url, direct_url) VALUES
    ('wechat-founder-park',
     'https://rsshub.rssforever.com/wechat/sogou/Founder%20Park',
     'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2FFounder%2520Park',
     'https://wechat2rss.bestblogs.dev/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml'),
    ('wechat-liuxiaopai',
     'https://rsshub.rssforever.com/wechat/sogou/%E5%88%98%E5%B0%8F%E6%8E%92',
     'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2F%E5%88%98%E5%B0%8F%E6%8E%92',
     'https://wechat2rss.bestblogs.dev/feed/484d4199ae6c0b72ea01e7e0597a1f74933dfb62.xml');

-- A user may already have added the exact feed URL. Merge that duplicate into
-- the stable source before updating its URL so the unique index cannot abort
-- startup and existing history remains reachable from the maintained source.
CREATE TEMP TABLE plaud_verified_wechat_duplicates (
    duplicate_id TEXT PRIMARY KEY,
    default_id TEXT NOT NULL
);

INSERT INTO plaud_verified_wechat_duplicates(duplicate_id, default_id)
SELECT duplicate.id, verified.id
FROM feed_sources duplicate
JOIN plaud_verified_wechat_feeds verified ON verified.direct_url = duplicate.url
WHERE duplicate.id <> verified.id;

-- Map overlapping history to the stable row before moving the rest. Match the
-- collector identity first and fall back to the canonical URL, mirroring the
-- runtime upsert contract.
CREATE TEMP TABLE plaud_verified_wechat_news_duplicates (
    duplicate_news_id TEXT PRIMARY KEY,
    canonical_news_id TEXT NOT NULL,
    default_id TEXT NOT NULL
);

INSERT INTO plaud_verified_wechat_news_duplicates(duplicate_news_id, canonical_news_id, default_id)
SELECT duplicate_news_id, canonical_news_id, default_id
FROM (
    SELECT duplicate_news.id AS duplicate_news_id,
           duplicate_source.default_id AS default_id,
           COALESCE(
               (SELECT canonical.id
                FROM news canonical
                WHERE canonical.source_id = duplicate_source.default_id
                  AND duplicate_news.external_id <> ''
                  AND canonical.external_id = duplicate_news.external_id
                ORDER BY CASE WHEN canonical.deleted_at = '' THEN 0 ELSE 1 END,
                         canonical.updated_at DESC, canonical.id
                LIMIT 1),
               (SELECT canonical.id
                FROM news canonical
                WHERE canonical.source_id = duplicate_source.default_id
                  AND duplicate_news.url <> ''
                  AND canonical.url = duplicate_news.url
                ORDER BY CASE WHEN canonical.deleted_at = '' THEN 0 ELSE 1 END,
                         canonical.updated_at DESC, canonical.id
                LIMIT 1),
               ''
           ) AS canonical_news_id
    FROM news duplicate_news
    JOIN plaud_verified_wechat_duplicates duplicate_source
      ON duplicate_source.duplicate_id = duplicate_news.source_id
) candidates
WHERE canonical_news_id <> '';

INSERT INTO news_preferences(news_id, dismissed, updated_at)
SELECT duplicate_news.canonical_news_id, preference.dismissed, preference.updated_at
FROM plaud_verified_wechat_news_duplicates duplicate_news
JOIN news_preferences preference ON preference.news_id = duplicate_news.duplicate_news_id
WHERE 1
ON CONFLICT(news_id) DO UPDATE SET
    dismissed = MAX(news_preferences.dismissed, excluded.dismissed),
    updated_at = MAX(news_preferences.updated_at, excluded.updated_at);

DELETE FROM news_preferences
WHERE news_id IN (SELECT duplicate_news_id FROM plaud_verified_wechat_news_duplicates);

INSERT INTO item_preferences(item_key, is_read, starred, read_later, reading_progress, updated_at)
SELECT 'news:' || duplicate_news.canonical_news_id,
       preference.is_read, preference.starred, preference.read_later,
       preference.reading_progress, preference.updated_at
FROM plaud_verified_wechat_news_duplicates duplicate_news
JOIN item_preferences preference ON preference.item_key = 'news:' || duplicate_news.duplicate_news_id
WHERE 1
ON CONFLICT(item_key) DO UPDATE SET
    is_read = MAX(item_preferences.is_read, excluded.is_read),
    starred = MAX(item_preferences.starred, excluded.starred),
    read_later = MAX(item_preferences.read_later, excluded.read_later),
    reading_progress = MAX(item_preferences.reading_progress, excluded.reading_progress),
    updated_at = MAX(item_preferences.updated_at, excluded.updated_at);

DELETE FROM item_preferences
WHERE item_key IN (
    SELECT 'news:' || duplicate_news_id FROM plaud_verified_wechat_news_duplicates
);

INSERT INTO stable_context_items(
    owner_id, profile_id, item_key, state, added_by, added_at, removed_at,
    updated_at, content_hash, source_type, source_id, source_name,
    original_url, title, published_at, content_format, content_snapshot
)
SELECT context_item.owner_id, context_item.profile_id,
       'news:' || duplicate_news.canonical_news_id,
       context_item.state, context_item.added_by, context_item.added_at,
       context_item.removed_at, context_item.updated_at, context_item.content_hash,
       context_item.source_type, duplicate_news.default_id, context_item.source_name,
       context_item.original_url, context_item.title, context_item.published_at,
       context_item.content_format, context_item.content_snapshot
FROM plaud_verified_wechat_news_duplicates duplicate_news
JOIN stable_context_items context_item
  ON context_item.item_key = 'news:' || duplicate_news.duplicate_news_id
WHERE 1
ON CONFLICT(owner_id, profile_id, item_key) DO UPDATE SET
    state = CASE
        WHEN stable_context_items.state = 'active' OR excluded.state = 'active' THEN 'active'
        ELSE 'removed'
    END,
    added_by = CASE
        WHEN stable_context_items.state <> 'active' AND excluded.state = 'active' THEN excluded.added_by
        ELSE stable_context_items.added_by
    END,
    added_at = MIN(stable_context_items.added_at, excluded.added_at),
    removed_at = CASE
        WHEN stable_context_items.state = 'active' OR excluded.state = 'active' THEN ''
        ELSE MAX(stable_context_items.removed_at, excluded.removed_at)
    END,
    updated_at = MAX(stable_context_items.updated_at, excluded.updated_at),
    content_hash = CASE WHEN stable_context_items.content_hash = '' THEN excluded.content_hash ELSE stable_context_items.content_hash END,
    source_id = excluded.source_id,
    source_name = CASE WHEN stable_context_items.source_name = '' THEN excluded.source_name ELSE stable_context_items.source_name END,
    original_url = CASE WHEN stable_context_items.original_url = '' THEN excluded.original_url ELSE stable_context_items.original_url END,
    title = CASE WHEN stable_context_items.title = '' THEN excluded.title ELSE stable_context_items.title END,
    published_at = CASE WHEN stable_context_items.published_at = '' THEN excluded.published_at ELSE stable_context_items.published_at END,
    content_snapshot = CASE WHEN stable_context_items.content_snapshot = '' THEN excluded.content_snapshot ELSE stable_context_items.content_snapshot END;

DELETE FROM stable_context_items
WHERE item_key IN (
    SELECT 'news:' || duplicate_news_id FROM plaud_verified_wechat_news_duplicates
);

INSERT INTO entry_full_texts(
    entry_id, content_text, content_format, fetched_at, status, error_message,
    source_url, updated_at, content_hash, content_html, image_urls_json,
    lead_image_index
)
SELECT duplicate_news.canonical_news_id, full_text.content_text,
       full_text.content_format, full_text.fetched_at,
       CASE
           WHEN full_text.content_hash = canonical_news.content_hash
               THEN full_text.status
           WHEN full_text.content_text <> '' THEN 'stale'
           ELSE full_text.status
       END,
       full_text.error_message, canonical_news.url, full_text.updated_at,
       CASE
           WHEN full_text.content_hash = canonical_news.content_hash
               THEN full_text.content_hash
           ELSE ''
       END,
       full_text.content_html, full_text.image_urls_json,
       full_text.lead_image_index
FROM plaud_verified_wechat_news_duplicates duplicate_news
JOIN entry_full_texts full_text ON full_text.entry_id = duplicate_news.duplicate_news_id
JOIN news canonical_news ON canonical_news.id = duplicate_news.canonical_news_id
WHERE 1
ORDER BY duplicate_news.canonical_news_id,
         CASE
             WHEN full_text.content_hash = canonical_news.content_hash
                  AND full_text.content_text <> '' THEN 0
             ELSE 1
         END,
         CASE WHEN full_text.status IN ('ok', 'stale') THEN 0 ELSE 1 END,
         full_text.updated_at DESC,
         duplicate_news.duplicate_news_id
ON CONFLICT(entry_id) DO UPDATE SET
    content_text = CASE
        WHEN entry_full_texts.content_text = '' AND excluded.content_text <> ''
            THEN excluded.content_text
        ELSE entry_full_texts.content_text
    END,
    content_format = CASE
        WHEN entry_full_texts.content_text = '' AND excluded.content_text <> ''
            THEN excluded.content_format
        ELSE entry_full_texts.content_format
    END,
    fetched_at = CASE
        WHEN entry_full_texts.content_text = '' AND excluded.content_text <> ''
            THEN excluded.fetched_at
        ELSE entry_full_texts.fetched_at
    END,
    status = CASE
        WHEN entry_full_texts.content_text = '' AND excluded.content_text <> ''
            THEN CASE
                WHEN excluded.content_hash = '' THEN 'stale'
                WHEN excluded.status IN ('ok', 'stale') THEN excluded.status
                ELSE 'stale'
            END
        ELSE entry_full_texts.status
    END,
    error_message = CASE
        WHEN entry_full_texts.content_text = '' AND excluded.content_text <> ''
            THEN excluded.error_message
        ELSE entry_full_texts.error_message
    END,
    source_url = CASE
        WHEN entry_full_texts.content_text = '' AND excluded.content_text <> ''
            THEN excluded.source_url
        ELSE entry_full_texts.source_url
    END,
    updated_at = CASE
        WHEN entry_full_texts.content_text = '' AND excluded.content_text <> ''
            THEN MAX(entry_full_texts.updated_at, excluded.updated_at)
        WHEN entry_full_texts.content_hash <> ''
             AND entry_full_texts.content_hash = excluded.content_hash
             AND (
                 (entry_full_texts.content_html = '' AND excluded.content_html <> '')
                 OR (entry_full_texts.image_urls_json IN ('', '[]') AND excluded.image_urls_json NOT IN ('', '[]'))
                 OR (entry_full_texts.lead_image_index < 0 AND excluded.lead_image_index >= 0)
             )
            THEN MAX(entry_full_texts.updated_at, excluded.updated_at)
        ELSE entry_full_texts.updated_at
    END,
    content_hash = CASE
        WHEN entry_full_texts.content_text = '' AND excluded.content_text <> ''
            THEN excluded.content_hash
        ELSE entry_full_texts.content_hash
    END,
    content_html = CASE
        WHEN entry_full_texts.content_text = '' AND excluded.content_text <> ''
            THEN excluded.content_html
        WHEN entry_full_texts.content_html = ''
             AND excluded.content_html <> ''
             AND entry_full_texts.content_hash <> ''
             AND entry_full_texts.content_hash = excluded.content_hash
            THEN excluded.content_html
        ELSE entry_full_texts.content_html
    END,
    image_urls_json = CASE
        WHEN entry_full_texts.content_text = '' AND excluded.content_text <> ''
            THEN excluded.image_urls_json
        WHEN entry_full_texts.image_urls_json IN ('', '[]')
             AND excluded.image_urls_json NOT IN ('', '[]')
             AND entry_full_texts.content_hash <> ''
             AND entry_full_texts.content_hash = excluded.content_hash
            THEN excluded.image_urls_json
        ELSE entry_full_texts.image_urls_json
    END,
    lead_image_index = CASE
        WHEN entry_full_texts.content_text = '' AND excluded.content_text <> ''
            THEN excluded.lead_image_index
        WHEN entry_full_texts.lead_image_index < 0
             AND excluded.lead_image_index >= 0
             AND entry_full_texts.content_hash <> ''
             AND entry_full_texts.content_hash = excluded.content_hash
            THEN excluded.lead_image_index
        ELSE entry_full_texts.lead_image_index
    END;

DELETE FROM entry_full_texts
WHERE entry_id IN (SELECT duplicate_news_id FROM plaud_verified_wechat_news_duplicates);

INSERT INTO article_ai_overviews(
    entry_id, content_hash, status, summary, key_points_json, impact,
    generated_at, error_code, prompt_version
)
SELECT duplicate_news.canonical_news_id, overview.content_hash, overview.status,
       overview.summary, overview.key_points_json, overview.impact,
       overview.generated_at, overview.error_code, overview.prompt_version
FROM plaud_verified_wechat_news_duplicates duplicate_news
JOIN article_ai_overviews overview ON overview.entry_id = duplicate_news.duplicate_news_id
WHERE 1
ON CONFLICT(entry_id, content_hash) DO UPDATE SET
    status = excluded.status,
    summary = excluded.summary,
    key_points_json = excluded.key_points_json,
    impact = excluded.impact,
    generated_at = excluded.generated_at,
    error_code = excluded.error_code,
    prompt_version = excluded.prompt_version
WHERE (excluded.status = 'ready' AND article_ai_overviews.status <> 'ready')
   OR (article_ai_overviews.status <> 'ready'
       AND excluded.generated_at > article_ai_overviews.generated_at);

DELETE FROM article_ai_overviews
WHERE entry_id IN (SELECT duplicate_news_id FROM plaud_verified_wechat_news_duplicates);

UPDATE reader_digest_receipts
SET entry_id = COALESCE((
        SELECT duplicate_news.canonical_news_id
        FROM plaud_verified_wechat_news_duplicates duplicate_news
        WHERE duplicate_news.duplicate_news_id = reader_digest_receipts.entry_id
    ), entry_id),
    source_id = COALESCE((
        SELECT duplicate_news.default_id
        FROM plaud_verified_wechat_news_duplicates duplicate_news
        WHERE duplicate_news.duplicate_news_id = reader_digest_receipts.entry_id
    ), source_id)
WHERE entry_id IN (SELECT duplicate_news_id FROM plaud_verified_wechat_news_duplicates);

-- Reader focus claims are 15-minute restart-persistent reservations. Re-keying
-- several legacy rows can collide with the unique active-entry constraint, so
-- release only the affected temporary runs and let startup claim canonical
-- entries again. Formal Reader focus run history has a different idempotency
-- namespace and remains untouched.
CREATE TEMP TABLE plaud_verified_wechat_claim_runs (
    run_id TEXT PRIMARY KEY
);

INSERT INTO plaud_verified_wechat_claim_runs(run_id)
SELECT DISTINCT claim.run_id
FROM reader_focus_claim_items claim
JOIN plaud_verified_wechat_news_duplicates duplicate_news
  ON duplicate_news.duplicate_news_id = claim.entry_id
JOIN reader_focus_runs focus_run ON focus_run.id = claim.run_id
WHERE focus_run.idempotency_key LIKE 'reader-focus-claim:%';

DELETE FROM reader_focus_claim_items
WHERE run_id IN (SELECT run_id FROM plaud_verified_wechat_claim_runs);

DELETE FROM reader_focus_runs
WHERE id IN (SELECT run_id FROM plaud_verified_wechat_claim_runs);

-- If a visible duplicate only matched a previously soft-deleted canonical
-- row, keep one visible history item by restoring the stable row first.
UPDATE news
SET deleted_at = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT canonical_news_id FROM plaud_verified_wechat_news_duplicates)
  AND deleted_at <> ''
  AND EXISTS (
      SELECT 1
      FROM plaud_verified_wechat_news_duplicates duplicate_news
      JOIN news visible_duplicate
        ON visible_duplicate.id = duplicate_news.duplicate_news_id
       AND visible_duplicate.deleted_at = ''
      WHERE duplicate_news.canonical_news_id = news.id
  );

UPDATE news
SET deleted_at = CASE WHEN deleted_at = '' THEN CURRENT_TIMESTAMP ELSE deleted_at END,
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT duplicate_news_id FROM plaud_verified_wechat_news_duplicates);

-- Non-overlapping history can keep its stable news id, so every item key,
-- full-text row and context membership remains valid without rewriting.
UPDATE news
SET source_id = (
        SELECT duplicate_source.default_id
        FROM plaud_verified_wechat_duplicates duplicate_source
        WHERE duplicate_source.duplicate_id = news.source_id
    ),
    source = COALESCE((
        SELECT canonical_source.name
        FROM plaud_verified_wechat_duplicates duplicate_source
        JOIN feed_sources canonical_source ON canonical_source.id = duplicate_source.default_id
        WHERE duplicate_source.duplicate_id = news.source_id
    ), source),
    updated_at = CURRENT_TIMESTAMP
WHERE source_id IN (SELECT duplicate_id FROM plaud_verified_wechat_duplicates)
  AND id NOT IN (SELECT duplicate_news_id FROM plaud_verified_wechat_news_duplicates);

UPDATE reader_digest_receipts
SET source_id = COALESCE((
        SELECT migrated_news.source_id
        FROM news migrated_news
        WHERE migrated_news.id = reader_digest_receipts.entry_id
    ), source_id)
WHERE source_id IN (SELECT duplicate_id FROM plaud_verified_wechat_duplicates)
  AND entry_id IN (
      SELECT id FROM news
      WHERE source_id IN (SELECT default_id FROM plaud_verified_wechat_duplicates)
  );

UPDATE stable_context_items
SET source_id = COALESCE((
        SELECT news.source_id FROM news WHERE 'news:' || news.id = stable_context_items.item_key
    ), source_id),
    updated_at = CURRENT_TIMESTAMP
WHERE source_id IN (SELECT duplicate_id FROM plaud_verified_wechat_duplicates)
  AND item_key IN (
    SELECT 'news:' || id
    FROM news
    WHERE source_id IN (SELECT default_id FROM plaud_verified_wechat_duplicates)
);

-- Focus-space source selectors point at feed source IDs rather than entry IDs.
-- Preserve an existing canonical selector and merge any stronger user state.
INSERT INTO focus_space_sources(
    focus_id, owner_id, profile_id, source_kind, source_id, enabled, revision,
    created_at, updated_at
)
SELECT selection.focus_id, selection.owner_id, selection.profile_id,
       selection.source_kind, duplicate_source.default_id, selection.enabled,
       selection.revision, selection.created_at, selection.updated_at
FROM focus_space_sources selection
JOIN plaud_verified_wechat_duplicates duplicate_source
  ON selection.source_kind = 'rss'
 AND selection.source_id = duplicate_source.duplicate_id
WHERE 1
ON CONFLICT(focus_id, source_kind, source_id) DO UPDATE SET
    enabled = MAX(focus_space_sources.enabled, excluded.enabled),
    revision = MAX(focus_space_sources.revision, excluded.revision),
    updated_at = MAX(focus_space_sources.updated_at, excluded.updated_at);

DELETE FROM focus_space_sources
WHERE source_kind = 'rss'
  AND source_id IN (SELECT duplicate_id FROM plaud_verified_wechat_duplicates);

UPDATE feed_subscriptions
SET enabled = MAX(enabled, COALESCE((
        SELECT duplicate_subscription.enabled
        FROM plaud_verified_wechat_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), enabled)),
    folder_id = CASE WHEN folder_id = '' THEN COALESCE((
        SELECT duplicate_subscription.folder_id
        FROM plaud_verified_wechat_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), '') ELSE folder_id END,
    title_override = CASE WHEN title_override = '' THEN COALESCE((
        SELECT duplicate_subscription.title_override
        FROM plaud_verified_wechat_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), '') ELSE title_override END,
    pinned = MAX(pinned, COALESCE((
        SELECT duplicate_subscription.pinned
        FROM plaud_verified_wechat_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), pinned)),
    muted = MAX(muted, COALESCE((
        SELECT duplicate_subscription.muted
        FROM plaud_verified_wechat_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), muted)),
    sort_order = CASE WHEN sort_order = 0 THEN COALESCE((
        SELECT duplicate_subscription.sort_order
        FROM plaud_verified_wechat_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), 0) ELSE sort_order END,
    updated_at = CURRENT_TIMESTAMP
WHERE source_id IN (SELECT default_id FROM plaud_verified_wechat_duplicates);

INSERT OR IGNORE INTO feed_subscription_tags(source_id, tag_id, created_at)
SELECT duplicate.default_id, tag.tag_id, tag.created_at
FROM plaud_verified_wechat_duplicates duplicate
JOIN feed_subscription_tags tag ON tag.source_id = duplicate.duplicate_id;

INSERT OR IGNORE INTO source_pack_members(pack_id, source_id, sort_order)
SELECT member.pack_id, duplicate.default_id, member.sort_order
FROM plaud_verified_wechat_duplicates duplicate
JOIN source_pack_members member ON member.source_id = duplicate.duplicate_id;

UPDATE feed_sources
SET url = 'source-merged://' || id,
    collection_mode = 'agent',
    hidden = 1,
    status = 'partial',
    next_sync_at = '',
    last_error = '同一公开来源已由默认订阅接管',
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT duplicate_id FROM plaud_verified_wechat_duplicates);

UPDATE feed_subscriptions
SET enabled = 0,
    updated_at = CURRENT_TIMESTAMP
WHERE source_id IN (SELECT duplicate_id FROM plaud_verified_wechat_duplicates);

UPDATE sync_requests
SET status = 'superseded',
    completed_at = CURRENT_TIMESTAMP,
    message = '重复来源已合并到默认订阅'
WHERE status IN ('pending', 'retry_wait')
  AND source_id IN (SELECT duplicate_id FROM plaud_verified_wechat_duplicates);

UPDATE feed_sources
SET kind = 'rss',
    url = (SELECT verified.direct_url FROM plaud_verified_wechat_feeds verified WHERE verified.id = feed_sources.id),
    collection_mode = 'app',
    sync_interval_seconds = 21600,
    etag = '',
    last_modified = '',
    backfill_cursor = '',
    last_sync_at = '',
    next_sync_at = '',
    last_error = '',
    gateway_status = '',
    gateway_cursor = '',
    gateway_has_more = 0,
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM plaud_verified_wechat_feeds)
  AND url IN (
      SELECT legacy_url FROM plaud_verified_wechat_feeds WHERE id = feed_sources.id
      UNION ALL
      SELECT legacy_folo_url FROM plaud_verified_wechat_feeds WHERE id = feed_sources.id
      UNION ALL
      SELECT direct_url FROM plaud_verified_wechat_feeds WHERE id = feed_sources.id
  );

DROP TABLE plaud_verified_wechat_claim_runs;
DROP TABLE plaud_verified_wechat_news_duplicates;
DROP TABLE plaud_verified_wechat_duplicates;
DROP TABLE plaud_verified_wechat_feeds;
