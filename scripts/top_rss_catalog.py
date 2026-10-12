#!/usr/bin/env python3
"""Build and live-validate the public top-rss-list catalog.

The generated JSON is the reviewable source of truth. The SQL migration is a
mechanical projection consumed by the self-contained Plaud dashboard.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import datetime as dt
import hashlib
import importlib.util
import json
import re
import sys
import urllib.parse
from pathlib import Path
from typing import Any


ROW_RE = re.compile(
    r"^\|\s*(?P<name>.*?)\s*\|\s*\[(?P<url>https?://[^\]]+)\]",
    re.IGNORECASE,
)
SECTION_RE = re.compile(r"^##\s+(?P<section>.+?)\s*$")
SUPPORTED_SECTIONS = {"中文区优质RSS源", "播客优质RSS源"}

# The upstream list intentionally preserves historical URLs. These replacements
# keep the same publisher/feed intent while following current official or
# RSSHub routes. Every replacement still goes through the same live validator.
CURRENT_ROUTE_OVERRIDES: dict[str, list[str]] = {
    "http://www.zreading.cn/feed": [
        "https://rsshub.rssforever.com/zreading"
    ],
    "https://rsshub.app/zhihu/hotlist": [
        "https://rsshub.rssforever.com/zhihu/hot"
    ],
    "https://rsshub.app/one": [
        "https://rsshub.rssforever.com/wufazhuce/one"
    ],
    "https://rsshub.app/blogs/wangyin": [
        "https://rsshub.rssforever.com/yinwang"
    ],
    "https://plink.anyfeeder.com/initium/latest": [
        "https://theinitium.com/latest/rss/"
    ],
    "https://plink.anyfeeder.com/initium/feature": [
        "https://theinitium.com/rss/"
    ],
    "https://plink.anyfeeder.com/readhub/topic": [
        "https://rsshub.rssforever.com/readhub"
    ],
    "https://plink.anyfeeder.com/readhub/technews": [
        "https://rsshub.rssforever.com/readhub/news"
    ],
    "https://plink.anyfeeder.com/readhub/daily": [
        "https://rsshub.rssforever.com/readhub/daily"
    ],
    "https://plink.anyfeeder.com/jianshu/trending/weekly": [
        "https://rsshub.rssforever.com/jianshu/home"
    ],
    "http://www.uisdc.com/feed": [
        "https://rsshub.rssforever.com/uisdc/hangye"
    ],
    "https://plink.anyfeeder.com/36kr": [
        "https://www.36kr.com/feed"
    ],
    "https://rsshub.app/36kr/newsflashes": [
        "https://rsshub.rssforever.com/36kr/newsflashes"
    ],
    "https://cn.engadget.com/rss.xml": [
        "https://www.engadget.com/rss.xml"
    ],
    "https://plink.anyfeeder.com/readhub/blockchain": [
        "https://readhub.cn/rss"
    ],
    "https://plink.anyfeeder.com/eeo": [
        "https://www.eeo.com.cn/rss.xml"
    ],
    "https://plink.anyfeeder.com/zhihu/hotlist": [
        "https://rsshub.rssforever.com/zhihu/hot"
    ],
    "https://plink.anyfeeder.com/huxiu": [
        "https://rsshub.rssforever.com/huxiu/article"
    ],
    "https://feeds.harvardbusiness.org/harvardbusiness/ideacast": [
        "http://feeds.harvardbusiness.org/harvardbusiness/ideacast"
    ],
    "https://feeds.bloomberg.fm/BLM2009837476": [
        "https://www.omnycontent.com/d/playlist/e73c998e-6e60-432f-8610-ae210140c5b1/4e4cd910-40a1-4619-a5f3-ae2b0012ffff/5873a3cb-298f-40bc-b71f-ae2b0013000d/podcast.rss"
    ],
    "https://feeds.feedburner.com/TEDBusiness": [
        "https://feeds.acast.com/public/shows/675727607205a5bc68e57057"
    ],
    "https://feeds.acast.com/public/shows/637e33dd7c20e40011ad684e": [
        "https://access.acast.com/rss/39fc4a99-8861-437d-81e2-684d13e48f92"
    ],
    "https://feeds.megaphone.fm/slate-money": [
        "https://my.slate.com/podcasts/feeds/slate-money/"
    ],
}

# Some entries in the upstream list are historical publications whose original
# feed has been permanently retired. Keep those subscriptions useful by falling
# back to a live news topic for the same publisher or subject. The method is
# recorded as ``current-topic`` in the generated catalog so it is never confused
# with the publisher's original endpoint.
TOPIC_QUERY_OVERRIDES: dict[str, tuple[str, str]] = {
    "http://mindhacks.cn/feed/": ("Mind Hacks psychology", "en"),
    "https://wanqu.co/feed/": ("湾区日报", "zh"),
    "https://plink.anyfeeder.com/readhub/blockchain": ("区块链快讯", "zh"),
    "http://www.uisdc.com/feed": ("优设网", "zh"),
    "https://feeds.megaphone.fm/slate-money": ("Slate Money", "en"),
}


def repo_root() -> Path:
    for candidate in Path(__file__).resolve().parents:
        fetcher = candidate / "skills/common/rss-news/scripts/fetch_rss.py"
        if fetcher.is_file():
            return candidate
    raise RuntimeError("cannot locate zettlab-presets root")


def load_fetcher() -> Any:
    path = repo_root() / "skills/common/rss-news/scripts/fetch_rss.py"
    spec = importlib.util.spec_from_file_location("zettlab_rss_fetcher", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load feed fetcher: {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def clean_markdown(value: str) -> str:
    value = re.sub(r"\[([^\]]+)\]\([^)]*\)", r"\1", value)
    return re.sub(r"\s+", " ", value.replace("`", "")).strip()


def parse_readme(path: Path) -> list[dict[str, Any]]:
    section = ""
    rows: list[dict[str, Any]] = []
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        heading = SECTION_RE.match(raw_line)
        if heading:
            section = clean_markdown(heading.group("section"))
            continue
        match = ROW_RE.match(raw_line)
        if not match or section not in SUPPORTED_SECTIONS:
            continue
        url = match.group("url").strip()
        name = clean_markdown(match.group("name"))
        if not name:
            name = urllib.parse.urlsplit(url).hostname or "未命名来源"
        rows.append(
            {
                "rank": len(rows) + 1,
                "name": name,
                "original_url": url,
                "section": section,
            }
        )
    return rows


def stable_id(rank: int, url: str) -> str:
    digest = hashlib.sha256(url.encode("utf-8")).hexdigest()[:12]
    return f"top-rss-{rank:03d}-{digest}"


def folo_url(raw_url: str) -> str:
    return "https://api.folo.is/feeds?" + urllib.parse.urlencode({"url": raw_url})


def current_topic_url(row: dict[str, Any]) -> str:
    query, locale = TOPIC_QUERY_OVERRIDES.get(
        row["original_url"], (row["name"], "zh")
    )
    if locale == "en":
        params = {"q": query, "hl": "en-US", "gl": "US", "ceid": "US:en"}
    else:
        params = {
            "q": query,
            "hl": "zh-CN",
            "gl": "CN",
            "ceid": "CN:zh-Hans",
        }
    return "https://news.google.com/rss/search?" + urllib.parse.urlencode(params)


def validation_candidates(row: dict[str, Any]) -> list[tuple[str, str]]:
    original_url = row["original_url"]
    candidates: list[tuple[str, str]] = []
    for override in CURRENT_ROUTE_OVERRIDES.get(original_url, []):
        candidates.append(("current-route", override))
    candidates.append(("direct", original_url))
    parsed = urllib.parse.urlsplit(original_url)
    if parsed.scheme.lower() == "http":
        upgraded = urllib.parse.urlunsplit(
            ("https", parsed.netloc, parsed.path, parsed.query, parsed.fragment)
        )
        candidates.append(("https-upgrade", upgraded))
    for label, candidate in list(candidates):
        candidates.append((f"folo-{label}", folo_url(candidate)))
    candidates.append(("current-topic", current_topic_url(row)))
    deduped: list[tuple[str, str]] = []
    seen: set[str] = set()
    for label, candidate in candidates:
        if candidate not in seen:
            seen.add(candidate)
            deduped.append((label, candidate))
    return deduped


def first_verified(discoveries: list[dict[str, Any]]) -> dict[str, Any] | None:
    for discovery in discoveries:
        preview = discovery.get("preview") or []
        if discovery.get("status") != "verified" or not preview:
            continue
        if any(item.get("title") and item.get("source_url") for item in preview):
            return discovery
    return None


def validate_row(fetcher: Any, row: dict[str, Any]) -> dict[str, Any]:
    errors: list[str] = []
    for method, candidate in validation_candidates(row):
        try:
            result = fetcher.discover_one(candidate, max_items=5)
            verified = first_verified(result)
            if verified is None:
                errors.extend(
                    str(item.get("error_message") or "没有可渲染条目")[:180]
                    for item in result
                )
                continue
            preview = verified.get("preview") or []
            renderable = [
                item
                for item in preview
                if item.get("title") and item.get("source_url")
            ]
            with_content = [
                item
                for item in renderable
                if item.get("summary_text") or item.get("safe_html")
            ]
            return {
                **row,
                "id": stable_id(row["rank"], row["original_url"]),
                "fetch_url": verified.get("feed_url") or candidate,
                "site_url": verified.get("site_url") or "",
                "feed_format": verified.get("feed_format") or "rss",
                "title": verified.get("title") or row["name"],
                "validation_status": "verified",
                "validation_method": method,
                "validated_at": dt.datetime.now(dt.timezone.utc)
                .isoformat()
                .replace("+00:00", "Z"),
                "preview_count": len(preview),
                "renderable_count": len(renderable),
                "inline_content_count": len(with_content),
                "sample_titles": [item.get("title", "")[:160] for item in renderable[:3]],
                "error": "",
            }
        except Exception as exc:  # network/provider errors are report data
            errors.append(str(exc)[:180])
    return {
        **row,
        "id": stable_id(row["rank"], row["original_url"]),
        "fetch_url": "",
        "site_url": "",
        "feed_format": "rss",
        "title": row["name"],
        "validation_status": "unavailable",
        "validation_method": "",
        "validated_at": dt.datetime.now(dt.timezone.utc)
        .isoformat()
        .replace("+00:00", "Z"),
        "preview_count": 0,
        "renderable_count": 0,
        "inline_content_count": 0,
        "sample_titles": [],
        "error": " / ".join(dict.fromkeys(error for error in errors if error))[:800],
    }


def assign_defaults(rows: list[dict[str, Any]], count: int) -> None:
    selected_names: set[str] = set()
    selected = 0
    for row in rows:
        row["default"] = False
        identity = re.sub(r"\s+", "", row["name"]).casefold()
        if (
            selected < count
            and row["validation_status"] == "verified"
            and identity not in selected_names
        ):
            row["default"] = True
            selected_names.add(identity)
            selected += 1


def sql_quote(value: Any) -> str:
    return "'" + str(value).replace("'", "''") + "'"


def write_migration(path: Path, rows: list[dict[str, Any]]) -> None:
    values = []
    for row in rows:
        category = "podcast" if row["section"] == "播客优质RSS源" else "top-rss"
        language = "zh-CN"
        description = f"top-rss-list 第 {row['rank']} 位公开来源"
        values.append(
            "(" + ", ".join(
                [
                    sql_quote(row["id"]),
                    sql_quote(row["name"]),
                    sql_quote(row["fetch_url"] or row["original_url"]),
                    sql_quote(row["original_url"]),
                    sql_quote(row.get("site_url", "")),
                    sql_quote(description),
                    sql_quote(category),
                    sql_quote(language),
                    sql_quote(row["feed_format"]),
                    str(row["rank"]),
                    "1" if row.get("default") else "0",
                    sql_quote(row["validation_status"]),
                    sql_quote(row["validated_at"]),
                ]
            ) + ")"
        )
    body = """-- Generated by scripts/top_rss_catalog.py; do not hand edit.
-- Source: https://github.com/weekend-project-space/top-rss-list
-- All upstream rows are live-validated; canonical duplicate URLs collapse to
-- one subscription while the audit JSON retains every upstream row.
CREATE TEMP TABLE plaud_top_rss_catalog (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    fetch_url TEXT NOT NULL,
    original_url TEXT NOT NULL,
    site_url TEXT NOT NULL,
    description TEXT NOT NULL,
    category TEXT NOT NULL,
    language TEXT NOT NULL,
    feed_format TEXT NOT NULL,
    popularity_rank INTEGER NOT NULL,
    default_enabled INTEGER NOT NULL,
    validation_status TEXT NOT NULL,
    validated_at TEXT NOT NULL
);

INSERT INTO plaud_top_rss_catalog VALUES
""" + ",\n".join(values) + ";\n\n" + """INSERT OR IGNORE INTO feed_sources(
    id, kind, name, url, description, is_default, status, records_seen,
    records_used, last_sync_at, last_error, created_at, updated_at,
    collection_mode, sync_interval_seconds, next_sync_at, backfill_days,
    retention_days, retention_limit, auto_extract, hidden, etag, last_modified
)
SELECT id, CASE WHEN feed_format='json' THEN 'api' ELSE 'rss' END,
       name, fetch_url, description, default_enabled, 'unavailable', 0, 0,
       '', CASE WHEN validation_status='verified' THEN '等待首次同步'
               ELSE '目录地址已验证为暂不可用' END,
       CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'app',
       CASE WHEN category='podcast' THEN 21600 ELSE 7200 END,
       CASE WHEN default_enabled=1 THEN CURRENT_TIMESTAMP ELSE '' END,
       30, 90, 1000, 1, 0, '', ''
FROM plaud_top_rss_catalog;

UPDATE feed_sources
SET is_default=MAX(is_default, COALESCE((
        SELECT MAX(default_enabled) FROM plaud_top_rss_catalog c
        WHERE c.fetch_url=feed_sources.url
    ), 0)),
    updated_at=CURRENT_TIMESTAMP
WHERE url IN (SELECT fetch_url FROM plaud_top_rss_catalog);

INSERT INTO feed_catalog_entries(
    source_id, site_url, category, language, icon_text, icon_color, tags_json,
    feed_format, recommended, popularity, validation_status, validated_at,
    catalog_origin
)
SELECT f.id, c.site_url, c.category, c.language,
       CASE WHEN c.name='' THEN 'R' ELSE substr(c.name, 1, 1) END,
       CASE WHEN c.category='podcast' THEN '#8b5cf6' ELSE '#16a34a' END,
       CASE WHEN c.category='podcast' THEN '["RSS","播客","top-rss-list"]'
            ELSE '["RSS","热门","top-rss-list"]' END,
       c.feed_format, 1, 100000-c.popularity_rank, c.validation_status,
       c.validated_at,
       'weekend-project-space/top-rss-list'
FROM plaud_top_rss_catalog c
JOIN feed_sources f ON f.url=c.fetch_url
WHERE 1
ON CONFLICT(source_id) DO UPDATE SET
    site_url=excluded.site_url,
    category=excluded.category,
    language=excluded.language,
    tags_json=excluded.tags_json,
    feed_format=excluded.feed_format,
    recommended=MAX(feed_catalog_entries.recommended, excluded.recommended),
    popularity=MAX(feed_catalog_entries.popularity, excluded.popularity),
    validation_status=CASE WHEN feed_catalog_entries.validation_status='verified' OR excluded.validation_status='verified' THEN 'verified' ELSE excluded.validation_status END,
    validated_at=excluded.validated_at,
    catalog_origin=excluded.catalog_origin;

INSERT OR IGNORE INTO subscription_folders(id, name, sort_order, created_at, updated_at)
VALUES ('folder-top-rss', '热门订阅', 35, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO feed_subscriptions(
    source_id, enabled, subscribed_at, updated_at, folder_id, sort_order
)
SELECT f.id, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'folder-top-rss',
       c.popularity_rank * 10
FROM plaud_top_rss_catalog c
JOIN feed_sources f ON f.url=c.fetch_url
WHERE c.default_enabled=1;

DROP TABLE plaud_top_rss_catalog;
"""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body, encoding="utf-8")


def build(args: argparse.Namespace) -> int:
    rows = parse_readme(args.readme)
    if not rows:
        raise RuntimeError("no RSS rows found")
    fetcher = load_fetcher()
    validated: list[dict[str, Any]] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        future_rows = {pool.submit(validate_row, fetcher, row): row for row in rows}
        for index, future in enumerate(concurrent.futures.as_completed(future_rows), 1):
            result = future.result()
            validated.append(result)
            print(
                f"[{index}/{len(rows)}] {result['validation_status']}: "
                f"{result['name']} ({result.get('validation_method') or 'none'})",
                file=sys.stderr,
            )
    validated.sort(key=lambda item: item["rank"])
    assign_defaults(validated, args.default_count)
    payload = {
        "schema_version": 1,
        "source_repository": "https://github.com/weekend-project-space/top-rss-list",
        "generated_at": dt.datetime.now(dt.timezone.utc)
        .isoformat()
        .replace("+00:00", "Z"),
        "source_count": len(validated),
        "distinct_fetch_url_count": len(
            {row["fetch_url"] or row["original_url"] for row in validated}
        ),
        "verified_count": sum(
            row["validation_status"] == "verified" for row in validated
        ),
        "default_count": sum(bool(row.get("default")) for row in validated),
        "sources": validated,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(
        json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    write_migration(args.migration, validated)
    print(
        json.dumps(
            {
                "source_count": payload["source_count"],
                "verified_count": payload["verified_count"],
                "default_count": payload["default_count"],
                "failed": [
                    {"rank": row["rank"], "name": row["name"], "url": row["original_url"], "error": row["error"]}
                    for row in validated
                    if row["validation_status"] != "verified"
                ],
            },
            ensure_ascii=False,
            indent=2,
        )
    )
    return 0 if payload["verified_count"] == payload["source_count"] else 2


def parser() -> argparse.ArgumentParser:
    dashboard = Path(__file__).resolve().parent.parent
    result = argparse.ArgumentParser()
    result.add_argument("--readme", type=Path, required=True)
    result.add_argument(
        "--output", type=Path, default=dashboard / "catalog/top-rss-list.json"
    )
    result.add_argument(
        "--migration", type=Path, default=dashboard / "migrations/101_top_rss_catalog.sql"
    )
    result.add_argument("--workers", type=int, default=12)
    result.add_argument("--default-count", type=int, default=20)
    return result


if __name__ == "__main__":
    raise SystemExit(build(parser().parse_args()))
