-- Curated topic packs are presentation data only. Reusing stable source IDs
-- preserves existing subscriptions, entries, folders, and user preferences.
INSERT INTO source_packs(id, name, description, category, icon_text, sort_order)
VALUES
    ('ai-engineering', 'AI 前沿', '模型、智能体、AI 研究与产业进展', 'ai', 'AI', 10),
    ('engineering-development', '工程与开发', '开发平台、开源、Web 与基础设施实践', 'programming', '工', 20),
    ('science-innovation', '科研与创新', '论文、自然科学、太空任务与前沿研究', 'journal', '研', 30),
    ('global-tech-business', '全球科技与商业', '全球科技产品、产业趋势与商业环境', 'new-media', '商', 40),
    ('policy-regulation', '政策与监管', '主要市场的政策、法规与上市公司披露', 'traditional-media', '政', 50),
    ('chinese-tech-policy', '中文科技与政策', '中文科技内容、中国政策与资本市场公告', 'new-media', '中', 60),
    ('operational-intelligence', '经营情报', '财报、政策、学术和专利官方来源', 'finance', '营', 70)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    description = excluded.description,
    category = excluded.category,
    icon_text = excluded.icon_text,
    sort_order = excluded.sort_order;

DELETE FROM source_pack_members
WHERE pack_id IN (
    'ai-engineering',
    'engineering-development',
    'science-innovation',
    'global-tech-business',
    'policy-regulation',
    'chinese-tech-policy',
    'operational-intelligence'
);

-- These legacy packs are fully replaced by the more focused topics above.
DELETE FROM source_pack_members WHERE pack_id IN ('science-world', 'chinese-productivity');
DELETE FROM source_packs WHERE id IN ('science-world', 'chinese-productivity');

INSERT INTO source_pack_members(pack_id, source_id, sort_order)
VALUES
    ('ai-engineering', 'openai-news', 10),
    ('ai-engineering', 'hacker-news', 20),
    ('ai-engineering', 'mit-technology-review', 30),
    ('ai-engineering', 'arxiv-ai', 40),
    ('ai-engineering', 'crossref-research', 50),

    ('engineering-development', 'github-blog', 10),
    ('engineering-development', 'github-changelog', 20),
    ('engineering-development', 'cloudflare-blog', 30),
    ('engineering-development', 'mozilla-hacks', 40),
    ('engineering-development', 'smashing-magazine', 50),

    ('science-innovation', 'nature', 10),
    ('science-innovation', 'arxiv-ai', 20),
    ('science-innovation', 'crossref-research', 30),
    ('science-innovation', 'nasa-live-missions', 40),
    ('science-innovation', 'nasa-podcast', 50),
    ('science-innovation', 'nasa-image-of-the-day', 60),

    ('global-tech-business', 'meta-newsroom', 10),
    ('global-tech-business', 'the-verge', 20),
    ('global-tech-business', 'mit-technology-review', 30),
    ('global-tech-business', 'bbc-world', 40),
    ('global-tech-business', 'npr-technology', 50),
    ('global-tech-business', 'sec-press-releases', 60),

    ('policy-regulation', 'govinfo-federal-register', 10),
    ('policy-regulation', 'eurlex-legislation', 20),
    ('policy-regulation', 'china-policy-library', 30),
    ('policy-regulation', 'sec-xbrl-filings', 40),
    ('policy-regulation', 'hkex-regulatory-announcements', 50),
    ('policy-regulation', 'cninfo-disclosures', 60),
    ('policy-regulation', 'sse-periodic-reports', 70),

    ('chinese-tech-policy', 'sspai', 10),
    ('chinese-tech-policy', 'china-policy-library', 20),
    ('chinese-tech-policy', 'cninfo-disclosures', 30),
    ('chinese-tech-policy', 'sse-periodic-reports', 40),
    ('chinese-tech-policy', 'hkex-regulatory-announcements', 50),
    ('chinese-tech-policy', 'cnipa-patent-data', 60),

    ('operational-intelligence', 'sec-xbrl-filings', 10),
    ('operational-intelligence', 'hkex-regulatory-announcements', 20),
    ('operational-intelligence', 'cninfo-disclosures', 30),
    ('operational-intelligence', 'sse-periodic-reports', 40),
    ('operational-intelligence', 'govinfo-federal-register', 50),
    ('operational-intelligence', 'eurlex-legislation', 60),
    ('operational-intelligence', 'china-policy-library', 70),
    ('operational-intelligence', 'arxiv-ai', 80),
    ('operational-intelligence', 'crossref-research', 90),
    ('operational-intelligence', 'epo-publication-server', 100),
    ('operational-intelligence', 'cnipa-patent-data', 110),
    ('operational-intelligence', 'wipo-patentscope', 120);
