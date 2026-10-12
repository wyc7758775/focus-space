-- Seed the twelve bounded public sources from the Memo P0 registry. These
-- sources are Agent-routed: the Python skill owns provider-specific parsing,
-- while the dashboard stores the typed result and per-source health state.
CREATE TEMP TABLE _memo_operational_sources (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    description TEXT NOT NULL,
    folder_id TEXT NOT NULL,
    folder_name TEXT NOT NULL,
    folder_sort INTEGER NOT NULL,
    source_sort INTEGER NOT NULL,
    site_url TEXT NOT NULL,
    category TEXT NOT NULL,
    language TEXT NOT NULL,
    icon_text TEXT NOT NULL,
    icon_color TEXT NOT NULL,
    tags_json TEXT NOT NULL,
    feed_format TEXT NOT NULL
);

INSERT INTO _memo_operational_sources
    (id, kind, name, url, description, folder_id, folder_name, folder_sort,
     source_sort, site_url, category, language, icon_text, icon_color,
     tags_json, feed_format)
VALUES
    ('sec-xbrl-filings', 'rss', 'SEC XBRL 财报申报', 'https://www.sec.gov/Archives/edgar/xbrl-inline.rss.xml', '美国上市公司结构化财报与申报更新', 'folder-financial', '财报', 10, 10, 'https://www.sec.gov/data-research/structured-data/structured-disclosure-rss-feeds', 'finance', 'en', 'S', '#0f766e', '["财报","上市公司","SEC"]', 'rss'),
    ('hkex-regulatory-announcements', 'rss', '港交所监管公告', 'https://www.hkex.com.hk/Services/RSS-Feeds/regulatory-announcements?sc_lang=zh-HK', '港交所上市公司公告与监管披露', 'folder-financial', '财报', 10, 20, 'https://www.hkex.com.hk/Services/RSS-Feeds?sc_lang=zh-HK', 'finance', 'zh-HK', '港', '#dc2626', '["财报","公告","港股"]', 'rss'),
    ('cninfo-disclosures', 'web', '巨潮资讯公告', 'https://www.cninfo.com.cn/new/disclosure', '中国上市公司公告与定期报告官方披露入口', 'folder-financial', '财报', 10, 30, 'https://www.cninfo.com.cn/new/index', 'finance', 'zh-CN', '巨', '#b91c1c', '["财报","公告","A股"]', 'web'),
    ('sse-periodic-reports', 'web', '上交所定期报告', 'https://www.sse.com.cn/disclosure/listedinfo/regular/', '上海证券交易所上市公司定期报告', 'folder-financial', '财报', 10, 40, 'https://www.sse.com.cn/disclosure/listedinfo/regular/', 'finance', 'zh-CN', '沪', '#2563eb', '["财报","公告","上交所"]', 'web'),
    ('govinfo-federal-register', 'rss', '美国联邦政策公报', 'https://www.govinfo.gov/rss/fr.xml', '美国联邦法规、拟议规则与政策通知', 'folder-policy', '政策', 20, 10, 'https://www.govinfo.gov/app/collection/FR', 'traditional-media', 'en', '美', '#1d4ed8', '["政策","法规","美国"]', 'rss'),
    ('eurlex-legislation', 'rss', '欧盟立法动态', 'https://eur-lex.europa.eu/EN/display-feed.rss?rssId=162', '欧洲议会与欧盟理事会立法更新', 'folder-policy', '政策', 20, 20, 'https://eur-lex.europa.eu/', 'traditional-media', 'en', '欧', '#1e40af', '["政策","法规","欧盟"]', 'rss'),
    ('china-policy-library', 'web', '中国政府政策文件库', 'https://www.gov.cn/zhengce/', '国务院政策文件、解读与最新发布', 'folder-policy', '政策', 20, 30, 'https://www.gov.cn/zhengce/', 'traditional-media', 'zh-CN', '政', '#dc2626', '["政策","国务院","中国"]', 'web'),
    ('arxiv-ai', 'atom', 'arXiv AI 最新论文', 'https://export.arxiv.org/api/query?search_query=cat:cs.AI&start=0&max_results=50&sortBy=submittedDate&sortOrder=descending', '人工智能方向最新预印本论文', 'folder-academic', '学术', 30, 10, 'https://arxiv.org/list/cs.AI/recent', 'journal', 'en', 'A', '#b91c1c', '["学术","AI","论文"]', 'atom'),
    ('crossref-research', 'api', 'Crossref 研究成果', 'https://api.crossref.org/works?query=artificial%20intelligence&rows=50&sort=published&order=desc', '跨出版机构的 DOI 论文与研究成果索引', 'folder-academic', '学术', 30, 20, 'https://www.crossref.org/', 'journal', 'en', 'C', '#2563eb', '["学术","DOI","论文"]', 'json'),
    ('epo-publication-server', 'api', 'EPO 欧洲专利发布', 'https://data.epo.org/publication-server/rest/v1.2/publication-dates', '欧洲专利局公开专利发布数据', 'folder-patent', '专利', 40, 10, 'https://www.epo.org/en/searching-for-patents/data/publication-server', 'journal', 'en', 'E', '#0f766e', '["专利","欧洲","EPO"]', 'json'),
    ('cnipa-patent-data', 'web', '中国专利公布公告', 'https://ggfw.cnipa.gov.cn', '国家知识产权局专利公布公告与检索入口', 'folder-patent', '专利', 40, 20, 'https://ggfw.cnipa.gov.cn', 'journal', 'zh-CN', '专', '#dc2626', '["专利","中国","CNIPA"]', 'web'),
    ('wipo-patentscope', 'web', 'WIPO PATENTSCOPE', 'https://patentscope.wipo.int/search/en/search.jsf', 'WIPO 国际专利申请与公开专利检索', 'folder-patent', '专利', 40, 30, 'https://patentscope.wipo.int/', 'journal', 'en', 'W', '#4338ca', '["专利","国际","WIPO"]', 'web');

INSERT OR IGNORE INTO feed_sources
    (id, kind, name, url, description, is_default, status, records_seen,
     records_used, last_sync_at, last_error, created_at, updated_at,
     collection_mode, sync_interval_seconds, next_sync_at, backfill_days,
     retention_days, retention_limit, auto_extract, hidden, etag, last_modified)
SELECT id, kind, name, url, description, 1, 'partial', 0, 0, '',
       '等待 Agent 采集', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP,
       'agent', 21600, '', 30, 90, 1000, 1, 0, '', ''
FROM _memo_operational_sources;

UPDATE feed_sources SET is_default=1, collection_mode='agent',
    sync_interval_seconds=21600, backfill_days=30, retention_days=90,
    retention_limit=1000, auto_extract=1, hidden=0, updated_at=CURRENT_TIMESTAMP
WHERE url IN (SELECT url FROM _memo_operational_sources);

INSERT OR IGNORE INTO subscription_folders(id, name, sort_order, created_at, updated_at)
SELECT folder_id, folder_name, folder_sort, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM _memo_operational_sources GROUP BY folder_id, folder_name, folder_sort;

INSERT OR IGNORE INTO feed_subscriptions
    (source_id, enabled, subscribed_at, updated_at, folder_id, sort_order)
SELECT f.id, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, d.folder_id, d.source_sort
FROM _memo_operational_sources d JOIN feed_sources f ON f.url=d.url;

INSERT INTO feed_catalog_entries
    (source_id, site_url, category, language, icon_text, icon_color, tags_json,
     feed_format, recommended, popularity, validation_status, validated_at,
     catalog_origin)
SELECT f.id, d.site_url, d.category, d.language, d.icon_text, d.icon_color,
       d.tags_json, d.feed_format, 1, 0, 'verified', '2026-08-10T00:00:00Z',
       'zettlab-maintained-official-work-sources-v1'
FROM _memo_operational_sources d JOIN feed_sources f ON f.url=d.url
ON CONFLICT(source_id) DO UPDATE SET site_url=excluded.site_url,
    category=excluded.category, language=excluded.language,
    icon_text=excluded.icon_text, icon_color=excluded.icon_color,
    tags_json=excluded.tags_json, feed_format=excluded.feed_format,
    recommended=excluded.recommended, validation_status=excluded.validation_status,
    validated_at=excluded.validated_at, catalog_origin=excluded.catalog_origin;

INSERT OR IGNORE INTO source_packs(id, name, description, category, icon_text, sort_order)
VALUES ('operational-intelligence', '经营与研究', '财报、政策、学术和专利官方来源', 'finance', '研', 5);
INSERT OR IGNORE INTO source_pack_members(pack_id, source_id, sort_order)
SELECT 'operational-intelligence', f.id, d.folder_sort*100+d.source_sort
FROM _memo_operational_sources d JOIN feed_sources f ON f.url=d.url;

DROP TABLE _memo_operational_sources;
