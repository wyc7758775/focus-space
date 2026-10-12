const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const source = fs.readFileSync(path.join(__dirname, "static/reader-parity.js"), "utf8");
const bootstrap = fs.readFileSync(path.join(__dirname, "static/reader-bootstrap.js"), "utf8");

function sourceFunction(name) {
  const match = source.match(new RegExp(`  function ${name}\\([^]*?\\n  \\}`));
  assert.ok(match, `missing production function ${name}`);
  return match[0];
}

test("article projection flow selects the HTML renderer across supported response shapes", () => {
  const context = vm.createContext({
    URL,
    location: { href: "https://app.example/" },
    text: (value) => String(value ?? ""),
    articleRenderers: { text: "text", markdown: "markdown", html: "html", wechat: "wechat" },
  });
  vm.runInContext(["articleHTML", "articleSourceHost", "articleRendererFor"].map(sourceFunction).join("\n"), context);
  const entry = { item: { source_id: "wechat-news", url: "https://mp.weixin.qq.com/s/example" } };
  for (const projection of [
    { content_html: "<p>正文</p>" },
    { safe_html: "<p>正文</p>" },
    { content_format: "html", content: "<p>正文</p>" },
    { content_html: "  ", safe_html: "<p>正文</p>" },
  ]) {
    assert.equal(context.articleHTML(projection), "<p>正文</p>");
    assert.equal(context.articleRendererFor(entry, projection), "wechat");
  }
  assert.equal(context.articleHTML({ content_format: "markdown", content: "# 正文" }), "");
  assert.equal(context.articleRendererFor(entry, { content_format: "markdown", content_text: "# 正文" }), "markdown");
  assert.equal(context.articleRendererFor(entry, { content_text: "正文" }), "text");
});

test("summary fallback stays readable and exposes a retry state", () => {
  const context = vm.createContext({
    text: (value) => String(value ?? ""),
    esc: (value) => String(value ?? "").replaceAll("&", "&amp;").replaceAll("<", "&lt;"),
  });
  vm.runInContext(sourceFunction("articleContentStatusNotice"), context);
  const notice = context.articleContentStatusNotice({
    status: "summary",
    error_message: "云端来源暂不可用，显示上次内容",
  });
  assert.match(notice, /当前显示摘要/);
  assert.match(notice, /重试读取正文/);
  assert.match(notice, /云端来源暂不可用/);
  assert.doesNotMatch(notice, /<script/i);
});

function runBootstrap({ ready = true, renderError = false } = {}) {
  const app = { hidden: true };
  const status = { removed: false, remove() { this.removed = true; } };
  const retry = { hidden: true, addEventListener() {} };
  const message = {};
  const nodes = { app, "focus-bootstrap": status, "focus-bootstrap-retry": retry, "focus-bootstrap-message": message };
  let renders = 0;
  vm.runInNewContext(bootstrap, {
    document: { getElementById: (id) => nodes[id] },
    window: { __plaudParityReady: ready, plaudI18n: { locale: "zh-CN" } },
    console: { error() {} },
    render() {
      renders += 1;
      assert.equal(app.hidden, false);
      if (renderError) throw new Error("render failed");
    },
  });
  return { app, status, retry, message, renders };
}

test("first paint flow reveals only after the production renderer is ready", () => {
  const result = runBootstrap();
  assert.equal(result.renders, 1);
  assert.equal(result.app.hidden, false);
  assert.equal(result.status.removed, true);
});

test("first paint flow retains a recoverable screen when the extension or renderer fails", () => {
  for (const options of [{ ready: false }, { renderError: true }]) {
    const result = runBootstrap(options);
    assert.equal(result.app.hidden, true);
    assert.equal(result.status.removed, false);
    assert.equal(result.retry.hidden, false);
    assert.match(result.message.textContent, /重新加载/);
  }
});

test("Bilibili links keep a playable provider URL and tolerate http feeds", () => {
  const context = vm.createContext({
    URL,
    URLSearchParams,
    text: (value) => String(value ?? ""),
  });
  vm.runInContext(
    ["bilibiliVideoID", "bilibiliPlayerURL"].map(sourceFunction).join("\n"),
    context
  );
  const parsed = context.bilibiliVideoID("http://www.bilibili.com/video/BV17PYC6KEXu");
  assert.equal(parsed.aid, "");
  assert.equal(parsed.bvid, "BV17PYC6KEXu");
  assert.match(
    context.bilibiliPlayerURL("https://www.bilibili.com/video/BV17PYC6KEXu"),
    /player\.bilibili\.com\/player\.html\?.*bvid=BV17PYC6KEXu/
  );
  assert.equal(context.bilibiliPlayerURL("https://b23.tv/example"), "");
});

test("reader keeps source failures separate from an empty source", () => {
  assert.match(source, /entryScopeError/);
  assert.match(source, /sourceEntryErrorMarkup/);
  assert.match(source, /data-retry-source-entry/);
  assert.match(source, /view === "obsidian" \? ""/);
});

test("source display mode resolution accepts only the frozen mode set", () => {
  const context = vm.createContext({
    readerFocusCopy: (zh) => zh,
  });
  vm.runInContext(
    ["validSourceDisplayMode", "sourceDisplayModeLabel"].map(sourceFunction).join("\n"),
    context
  );
  for (const mode of ["auto", "article", "video", "gallery"]) {
    assert.equal(context.validSourceDisplayMode(mode), true, mode);
  }
  for (const mode of ["", "magazine", "Video", "pictures", null, undefined]) {
    assert.equal(context.validSourceDisplayMode(mode), false, String(mode));
  }
  assert.equal(context.sourceDisplayModeLabel("article"), "文章列表");
  assert.equal(context.sourceDisplayModeLabel("auto"), "自动");
  assert.equal(context.sourceDisplayModeLabel("gallery"), "媒体墙");
});

test("mark-all undo restores only entries that were unread before the bulk", () => {
  // The snapshot must be taken before any optimistic state change: the marker
  // functions capture pref() at call time, and markVisibleRead re-reads pref()
  // for the undo snapshot before mutating anything.
  assert.match(source, /async function markVisibleRead\(\)[\s\S]*?previouslyUnreadNews = entries\.filter\([\s\S]*?!pref\(entry\)\.read/);
  assert.match(source, /previouslyUnreadRecordings[\s\S]*?!pref\(entry\)\.read/);
  // Undo reverses read in bounded 200-key batches (the bulk endpoint cap).
  assert.match(source, /index \+= 200[\s\S]*?keys: newsKeys\.slice\(index, index \+ 200\)[\s\S]*?read: false/);
  // Live device meetings stay in local storage on undo, never on the
  // recordings endpoint.
  assert.match(source, /liveIDs\.forEach\(\(id\) => storeLiveMeetingPreference\(id, \{ read: false \}\)\);/);
});

test("picture aggregate mirror keeps videos out and bare thumbnails insufficient", () => {
  const context = vm.createContext({
    text: (value) => String(value ?? ""),
    videoProviderForItem: (item) => (item && item.source_type === "youtube" ? "youtube" : ""),
  });
  vm.runInContext(sourceFunction("aggregatePictureNews"), context);
  const picture = context.aggregatePictureNews;
  assert.equal(picture({ view_type: "picture" }), true);
  assert.equal(picture({ media_mime: "image/jpeg" }), true);
  assert.equal(picture({ media_mime: "IMAGE/PNG" }), true, "media mime match is case-insensitive");
  assert.equal(picture({ view_type: "PICTURE" }), true, "view_type match is case-insensitive");
  assert.equal(picture({ thumbnail_url: "https://cdn.example.com/t.webp" }), false, "bare article thumbnails never enter the wall");
  assert.equal(picture({ view_type: "article" }), false);
  assert.equal(picture({ view_type: "picture", source_type: "youtube" }), false, "video identity always wins");
  assert.equal(picture(null), false);
});

test("picture lightbox steps clamp at both ends instead of wrapping", () => {
  const steps = [];
  const context = vm.createContext({
    syncPictureLightbox: () => steps.push("sync"),
  });
  vm.runInContext(
    `var pictureLightboxState = { entries: [{ key: "a" }, { key: "b" }, { key: "c" }], index: 0 };\n` +
      sourceFunction("stepPictureLightbox"),
    context
  );
  const step = context.stepPictureLightbox;
  step(-1);
  assert.equal(context.pictureLightboxState.index, 0, "no wrap past the first image");
  step(1);
  step(1);
  step(1);
  assert.equal(context.pictureLightboxState.index, 2, "no wrap past the last image");
  assert.equal(steps.length, 2, "only real moves repaint the stage");
});

test("subscription manager search filter matches names, urls and folders only", () => {
  const context = vm.createContext({
    text: (value) => String(value ?? ""),
  });
  vm.runInContext(sourceFunction("managerSourceMatchesQuery"), context);
  const source = {
    title_override: "设计周刊",
    name: "design weekly",
    url: "https://example.com/design.xml",
    site_url: "https://example.com/",
    folder_name: "设计",
    tags: ["inspiration"],
  };
  const matches = context.managerSourceMatchesQuery;
  assert.equal(matches(source, ""), true, "empty query keeps every source");
  assert.equal(matches(source, "设计周刊"), true);
  assert.equal(matches(source, "DESIGN.XML"), true, "match is case-insensitive");
  assert.equal(matches(source, "设计"), true, "folder name is searchable");
  assert.equal(matches(source, "Inspiration".toUpperCase()), true, "tags are searchable");
  assert.equal(matches(source, "不存在"), false);
  assert.equal(matches(source, "   "), true, "whitespace-only query matches all");
});

test("overview default page renders hint and quick links, then routes clicks through the shared view switcher", () => {
  const focusApp = fs.readFileSync(path.join(__dirname, "static/js/focus-app.js"), "utf8");
  const match = focusApp.match(/function renderOverview\(\) \{[^]*?\n      \}/);
  assert.ok(match, "missing production renderOverview in focus-app.js");
  // 缺省页按钮从生成的 markup 里解析出来再触发，确保测的是真实接线而不是预置桩；
  // 同一 view 的按钮对象跨调用缓存，模拟真实 DOM 元素身份稳定。
  const panel = {
    html: "",
    buttons: new Map(),
    set innerHTML(value) { this.html = value; },
    querySelectorAll(selector) {
      assert.equal(selector, "[data-overview-view]");
      return [...this.html.matchAll(/data-overview-view="([^"]+)"/g)].map((found) => {
        if (!this.buttons.has(found[1])) {
          this.buttons.set(found[1], {
            dataset: { overviewView: found[1] },
            addEventListener(event, handler) { this.click = handler; },
          });
        }
        return this.buttons.get(found[1]);
      });
    },
  };
  const switched = [];
  const context = vm.createContext({
    $: (id) => (id === "detail-panel" ? panel : null),
    esc: (value) => String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;"),
    window: { __plaudSwitchView: (view) => switched.push(["shared", view]) },
    renderViewSwitch: (view) => switched.push(["fallback", view]),
  });
  vm.runInContext(match[0], context);

  context.renderOverview();
  assert.match(panel.html, /焦点空间/);
  assert.match(panel.html, /从左侧选择信息、会议或笔记，内容会在这里展开/);
  assert.doesNotMatch(panel.html, /内容概览|待我决策|待办行动/);

  const buttons = panel.querySelectorAll("[data-overview-view]");
  assert.deepEqual(buttons.map((button) => button.dataset.overviewView), ["all", "meeting", "obsidian"]);
  for (const button of buttons) {
    button.click();
  }
  assert.deepEqual(switched, [
    ["shared", "all"],
    ["shared", "meeting"],
    ["shared", "obsidian"],
  ], "quick links must reuse the sidebar switch path (live device refresh + stale-request cancel)");

  // parity 层未加载时退回 renderViewSwitch，保证单独的 focus-app 仍可导航。
  const fallbackContext = vm.createContext({
    $: () => panel,
    esc: (value) => String(value),
    window: {},
    renderViewSwitch: (view) => switched.push(["fallback", view]),
  });
  vm.runInContext(match[0], fallbackContext);
  fallbackContext.renderOverview();
  panel.querySelectorAll("[data-overview-view]")[0].click();
  assert.deepEqual(switched.at(-1), ["fallback", "all"]);
});

// node 没有 DOMParser：给 parseXThread 的流程测试配一个只覆盖扁平标签 + img 属性
// 读取的 mini 实现（真实解析仍在浏览器里走原生 DOMParser）。
class TinyDOMParser {
  parseFromString(html) {
    const root = new TinyElement("body", {});
    const stack = [root];
    const tokenizer = /<\/?([a-zA-Z0-9]+)((?:[^>"']|"[^"]*"|'[^']*')*)>|([^<]+)/g;
    let match;
    while ((match = tokenizer.exec(html))) {
      if (match[3] != null) {
        if (match[3].trim()) stack.at(-1).childNodes.push({ nodeType: 3, textContent: match[3] });
        continue;
      }
      const [full, tag, attrText] = match;
      if (full.startsWith("</")) {
        if (stack.length > 1) stack.pop();
        continue;
      }
      const attributes = {};
      const attrRe = /([a-zA-Z-]+)="([^"]*)"/g;
      let pair;
      while ((pair = attrRe.exec(attrText || ""))) attributes[pair[1]] = pair[2];
      const element = new TinyElement(tag, attributes);
      stack.at(-1).childNodes.push(element);
      if (!/^(img|br|hr|input)$/i.test(tag)) stack.push(element);
    }
    return { body: root };
  }
}
class TinyElement {
  constructor(tagName, attributes) {
    this.tagName = tagName.toUpperCase();
    this.attributes = attributes;
    this.childNodes = [];
    this.nodeType = 1;
  }
  get textContent() {
    return this.childNodes.map((node) => node.textContent).join("");
  }
  getAttribute(name) {
    return this.attributes[name] ?? null;
  }
  querySelector(selector) {
    const imageSelector = selector.match(/^img\[([^\]]+)\]$/);
    const walk = (node) => {
      for (const child of node.childNodes) {
        if (child.nodeType !== 1) continue;
        if (imageSelector && child.tagName === "IMG" && child.getAttribute(imageSelector[1]) != null) return child;
        const found = walk(child);
        if (found) return found;
      }
      return null;
    };
    return walk(this);
  }
}

test("x thread parser extracts quote, stats and replies, and cuts at the login wall", () => {
  const context = vm.createContext({
    text: (value) => String(value ?? ""),
    DOMParser: TinyDOMParser,
    Node: { TEXT_NODE: 3 },
  });
  vm.runInContext(sourceFunction("parseXThread"), context);
  const html = [
    `<a></a><h2 data-block-id="b1">Post </h2><a>Log in </a><a>Sign up </a>`,
    `<h1 data-block-id="b2">宝玉 on X: "o3-mini" </h1>`,
    `<a><img data-entry-image="0" data-entry-image-url="u0" alt="X"></a><a>宝玉 </a><a>@DotBao </a>o3-mini 刚发布我第一时间测了\n\n结果不错 `,
    `<a><img data-entry-image="2" alt=""></a>`,
    `<a><img data-entry-image="1" alt="@tester"></a><a>Tester </a><a>@tester </a><a>Sep 27 </a>恭喜发布！期待详细评测 `,
    `<a><img data-entry-image="5" alt="quote-pic"></a>`,
    `<a>4:54 AM · Sep 16, 2026 </a>· <a>4.3MViews </a><a>1.9K </a>`,
    `<a><img data-entry-image="3" alt="@reply1"></a><a>拉风的花花 </a><a>@reply1 </a><a>Sep 28 </a>写的很好，收藏了 `,
    `<a><img data-entry-image="4" alt="@reply2"></a><a>swordfish </a><a>@reply2 </a>转发了吗 `,
    `<h2>Relevant people </h2><a>Elon Musk@elonmusk </a><a>Follow </a>`,
    `<h2>Trending now </h2><a href="https://x.com/tos">Terms </a>`,
  ].join("");
  const thread = context.parseXThread({ content_html: html });
  assert.ok(thread, "parser should produce a thread");
  assert.equal(thread.post.handle, "DotBao");
  assert.match(thread.post.text, /o3-mini 刚发布/);
  assert.equal(JSON.stringify(thread.post.images), "[2]");
  assert.equal(thread.quote.handle, "tester");
  assert.match(thread.quote.text, /恭喜发布/);
  assert.equal(thread.quote.time, "Sep 27");
  assert.equal(JSON.stringify(thread.quote.images), "[5]");
  assert.equal(JSON.stringify(thread.stats), JSON.stringify(["4.3M 浏览", "1.9K"]));
  assert.equal(thread.replies.length, 2, "replies after the stats line");
  assert.equal(thread.replies[0].handle, "reply1");
  assert.equal(thread.replies[0].name, "拉风的花花");
  assert.equal(thread.replies[0].avatarIndex, 3);
  assert.match(thread.replies[0].text, /收藏了/);
  assert.equal(thread.replies[1].handle, "reply2");
  const textOnly = context.parseXThread({ content_text: "no html" });
  assert.equal(textOnly, null, "no content_html means no structured thread");
});

test("x thread markup renders sanitized reply rows with media proxies", () => {
  const context = vm.createContext({
    text: (value) => String(value ?? ""),
    esc: (value) => String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;"),
  });
  vm.runInContext(sourceFunction("xThreadMarkup"), context);
  const entry = { item: { id: "entry-x1", author: "宝玉" } };
  const thread = {
    post: { images: [2] },
    quote: null,
    stats: ["1.9K"],
    replies: [
      { name: "拉风的花花<script>", handle: "reply1", time: "Sep 28", text: "写的很好", images: [5], avatarIndex: 3 },
      { name: "", handle: "reply2", time: "", text: "转发了吗", images: [], avatarIndex: null },
    ],
  };
  const markup = context.xThreadMarkup(entry, thread);
  assert.match(markup, /data-x-thread/);
  assert.match(markup, /x-thread-stats/);
  assert.match(markup, /回复<\/strong><span>2/);
  assert.match(markup, /image\?id=entry-x1&index=3/);
  assert.match(markup, /image\?id=entry-x1&index=5/);
  assert.doesNotMatch(markup, /<script/i);
  assert.match(markup, /x-reply-fallback/, "missing avatar falls back to initial letter");
});

test("manager sort orderings are read-only render views; manual keeps server order", () => {
  const context = vm.createContext({
    text: (value) => String(value ?? ""),
    parity: { managerSortMode: "manual" },
  });
  vm.runInContext(sourceFunction("managerSortedSources"), context);
  const sources = [
    { id: "b", name: "Beta", unread_count: 2, last_sync_at: "2026-09-28T01:00:00Z" },
    { id: "a", name: "Alpha", unread_count: 7, last_sync_at: "2026-09-27T01:00:00Z" },
    { id: "c", name: "Gamma", unread_count: 0, last_sync_at: "2026-09-28T02:00:00Z" },
  ];
  // Manual mode is the identity: sort_order from the server stays untouched.
  const ids = (list) => list.map((item) => item.id).join(",");
  assert.equal(ids(context.managerSortedSources(sources)), "b,a,c");
  assert.equal(ids(sources), "b,a,c", "input list must not be mutated");

  context.parity.managerSortMode = "name";
  assert.equal(ids(context.managerSortedSources(sources)), "a,b,c");

  context.parity.managerSortMode = "unread";
  assert.equal(ids(context.managerSortedSources(sources)), "a,b,c");

  context.parity.managerSortMode = "sync";
  assert.equal(ids(context.managerSortedSources(sources)), "c,b,a");

  context.parity.managerSortMode = "unknown-mode";
  assert.equal(ids(context.managerSortedSources(sources)), "b,a,c", "unknown mode falls back to manual");
});

test("manager last-sync stamp never fakes a loading state", () => {
  const context = vm.createContext({
    text: (value) => String(value ?? ""),
    readerFocusCopy: (zh) => zh,
  });
  vm.runInContext(sourceFunction("managerRelativeTime"), context);
  assert.match(context.managerRelativeTime(""), /从未同步/);
  assert.match(context.managerRelativeTime("not-a-date"), /从未同步/);
  assert.match(context.managerRelativeTime(new Date(Date.now() - 3 * 60000).toISOString()), /3 分钟前/);
  assert.match(context.managerRelativeTime(new Date(Date.now() - 5 * 3600000).toISOString()), /5 小时前/);
  assert.match(context.managerRelativeTime(new Date(Date.now() - 2 * 86400000).toISOString()), /2 天前/);
});
