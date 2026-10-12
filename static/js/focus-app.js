      const httpPage =
        location.protocol === "http:" || location.protocol === "https:";
      const normalized = !httpPage || location.pathname.endsWith("/");
      if (!normalized)
        location.replace(
          location.pathname + "/" + location.search + location.hash
        );

      const $ = (id) => document.getElementById(id);
      const esc = (value) =>
        String(value ?? "").replace(
          /[&<>"']/g,
          (char) =>
            ({
              "&": "&amp;",
              "<": "&lt;",
              ">": "&gt;",
              '"': "&quot;",
              "'": "&#39;",
            }[char])
        );
      const renderObsidianEvidence = (raw, notePath) => {
        const renderer = window.__renderObsidianMarkdown;
        if (typeof renderer === "function") {
          try {
            return renderer(raw, notePath);
          } catch (error) {
            console.warn("[obsidian] Markdown rendering failed:", error);
          }
        }
        return (
          '<pre class="obsidian-note-fallback">' +
          esc(String(raw ?? "").slice(0, 32000)) +
          "</pre>"
        );
      };
      const icon = (name) =>
        ({
          rss: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M5 5a14 14 0 0 1 14 14M5 11a8 8 0 0 1 8 8M6 18h.01"/></svg>',
          video:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="6" width="16" height="12" rx="2"/><path d="m10 9 5 3-5 3z"/></svg>',
          audio:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M9 18V6l9-2v12"/><circle cx="6.5" cy="18" r="2.5"/><circle cx="15.5" cy="16" r="2.5"/></svg>',
          picture:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="5" width="16" height="14" rx="2"/><circle cx="9" cy="10" r="1.5"/><path d="m5 17 4-4 3 3 2-2 5 4"/></svg>',
          meeting:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="6" width="16" height="12" rx="2"/><path d="M8 6V4M16 6V4M8 11h8M8 14h5"/></svg>',
          obsidian:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M6 4h9l3 3v13H6z"/><path d="M15 4v4h4M9 12h6M9 15h5"/></svg>',
          folder:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M3.5 6.5h6l2 2h9v10.5h-17z"/><path d="M3.5 6.5V5h6l2 2"/></svg>',
          note:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M6 4h9l3 3v13H6z"/><path d="M15 4v4h4M9 12h6M9 15h4"/></svg>',
          action:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M5 6h14M5 12h14M5 18h9"/><path d="m17 16 2 2 3-4"/></svg>',
          star: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="m12 4 2.5 5 5.5.8-4 3.9.9 5.5-4.9-2.6-4.9 2.6.9-5.5-4-3.9 5.5-.8z"/></svg>',
          starFill:
            '<svg viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="1.2"><path d="m12 4 2.5 5 5.5.8-4 3.9.9 5.5-4.9-2.6-4.9 2.6.9-5.5-4-3.9 5.5-.8z"/></svg>',
          trash:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M5 7h14M10 11v6M14 11v6M8 7l1-3h6l1 3M7 7l1 14h8l1-14"/></svg>',
          eye: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M3 12s3.2-5 9-5 9 5 9 5-3.2 5-9 5-9-5-9-5z"/><circle cx="12" cy="12" r="2"/></svg>',
          eyeOff:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M3 3l18 18M10.6 6.2A9.8 9.8 0 0 1 12 6c5.8 0 9 6 9 6a16 16 0 0 1-3.2 3.7M6.1 6.8C4 8.3 3 12 3 12s3.2 6 9 6c1.1 0 2.1-.2 3-.5"/></svg>',
          external:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M14 5h5v5M19 5l-8 8"/><path d="M18 13v5H5V5h5"/></svg>',
          check:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="m5 12 4 4L19 6"/></svg>',
          arrow:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M5 12h13M13 6l6 6-6 6"/></svg>',
          chevron:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="m8 10 4 4 4-4"/></svg>',
          inbox:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 5h16v14H4z"/><path d="M4 14h4l2 2h4l2-2h4"/></svg>',
          signal:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M5 17V9M9 17V5M13 17v-3M17 17V7M21 17v-5"/></svg>',
          decision:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M12 4v16M12 8 6 5M12 8l6-3M12 16l-6 3M12 16l6 3"/><circle cx="12" cy="4" r="2"/><circle cx="6" cy="5" r="2"/><circle cx="18" cy="5" r="2"/></svg>',
          metric:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M5 18V9M12 18V5M19 18v-7"/><path d="M3 20h18"/></svg>',
          project:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 7h6l2 2h8v10H4z"/><path d="M4 7V5h6l2 2"/></svg>',
          search:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="10.8" cy="10.8" r="6.3"/><path d="m16 16 4 4"/></svg>',
          close:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M6 6l12 12M18 6 6 18"/></svg>',
          layout:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 5h16v14H4zM12 5v14"/></svg>',
          filter:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 5h16l-6.5 7.2v5.3l-3 1.5v-6.8z"/></svg>',
          maximize:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M8 3H3v5M16 3h5v5M8 21H3v-5M16 21h5v-5"/><path d="m3 8 5-5m13 5-5-5M3 16l5 5m13-5-5 5"/></svg>',
          minimize:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M8 3v5H3M16 3v5h5M8 21v-5H3M16 21v-5h5"/><path d="M8 8 3 3m13 5 5-5M8 16l-5 5m13-5 5 5"/></svg>',
          edit:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="m4 16.5-.7 3.2 3.2-.7L18.8 6.7a2.2 2.2 0 0 0-3.1-3.1L4 16.5Z"/><path d="m14.5 5.5 4 4"/></svg>',
          menu: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 6h16M4 12h16M4 18h16"/></svg>',
          plus: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M12 5v14M5 12h14"/></svg>',
          refresh:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M20 11a8 8 0 0 0-14.7-4L4 9"/><path d="M4 5v4h4M4 13a8 8 0 0 0 14.7 4L20 15M20 19v-4h-4"/></svg>',
          sparkles:
            '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="m12 3-1.15 3.1a4 4 0 0 1-2.35 2.35L5.4 9.6l3.1 1.15a4 4 0 0 1 2.35 2.35L12 16.2l1.15-3.1a4 4 0 0 1 2.35-2.35l3.1-1.15-3.1-1.15a4 4 0 0 1-2.35-2.35L12 3Z"/><path d="m19 16-.55 1.45L17 18l1.45.55L19 20l.55-1.45L21 18l-1.45-.55L19 16Z"/></svg>',
        }[name] || "");

      if (window.plaudI18n) {
        document.documentElement.lang = window.plaudI18n.locale;
        document.title = window.plaudI18n.t("dashboard");
      }
      const moduleNames = {
        news: "关键证据",
        actions: "逾期与今日行动",
        metrics: "经营快照",
        projects: "项目脉搏",
        signals: "关键风险与机会",
        decisions: "待我决策",
        sources: "异常来源",
        morning_brief: "AI 经营概览",
          reader_focus: "当前焦点",
          focus_space: "焦点空间",
      };
      const typeNames = {
        rss: "订阅文章",
        video: "视频",
        youtube: "YouTube 视频",
        bilibili: "B 站视频",
        social: "社交动态",
        meeting: "会议",
        obsidian: "Obsidian",
        action: "行动",
        metric: "经营指标",
        project: "项目",
        signal: "信号",
        decision: "待决策",
        evidence: "证据",
      };
      const persistedReaderView = (() => {
        try {
          const value = String(localStorage.getItem("plaud.reader.view") || "");
          return value && (value.startsWith("source:") || value.startsWith("obsidian-folder:") || [
            "today", "decision", "action", "signal", "all", "rss", "video", "youtube", "picture", "meeting", "obsidian", "starred", "recording", "later", "context",
          ].includes(value))
            ? value
            : "today";
        } catch {
          return "today";
        }
      })();
      const state = {
        dashboard: null,
        prefs: new Map(),
        prefsLoaded: false,
        dashboardResolved: false,
        sourcesResolved: false,
        dashboardError: "",
        reader: { sidebar_width: 256, timeline_width: 380 },
        // Keep today's focus as the canonical default; restore a previously
        // selected reader/source view only after the base state exists.
        view: "today",
        query: "",
        selected: null,
        resize: null,
        busy: false,
        manualSyncing: false,
        total_entries: 0,
        // Live device sources are merged into the same read model as the
        // persisted dashboard snapshot. They are populated by reader-parity.js
        // and remain empty for older hosts/apps without the optional bridge.
        liveMeetings: [],
        liveObsidian: [],
        liveObsidianFolders: [],
      };
      state.view = persistedReaderView;
      const clamp = (n, min, max) => Math.min(max, Math.max(min, n));
      const text = (value, fallback = "") => {
        if (value === null || value === undefined || value === "") return String(fallback ?? "").trim();
        if (typeof value === "object") {
          if (Array.isArray(value)) {
            const joined = value.map((item) => text(item)).filter(Boolean).join(" · ");
            return joined || String(fallback ?? "").trim();
          }
          const preferred = ["message", "detail", "title", "name", "summary", "text", "label", "content"]
            .map((key) => value[key])
            .find((item) => item !== null && item !== undefined && item !== "");
          if (preferred !== undefined) return text(preferred, fallback);
          try {
            return JSON.stringify(value);
          } catch {
            return String(fallback ?? "").trim();
          }
        }
        return String(value ?? fallback).trim();
      };
      const formatDate = (value) => {
        const raw = text(value);
        if (!raw) return window.plaudI18n?.locale === "en" ? "Time to be confirmed" : "时间待确认";
        const date = new Date(raw);
        if (Number.isNaN(date.getTime())) return raw.slice(0, 40);
        return (window.plaudI18n?.formatDate || ((value, options) => new Date(value).toLocaleString(undefined, options)))(raw, {
          month: "short",
          day: "numeric",
          hour: "2-digit",
          minute: "2-digit",
        });
      };
      const dateBucket = (value) => {
        const raw = text(value);
        if (!raw) return window.plaudI18n?.locale === "en" ? "No date" : "未标注时间";
        const date = new Date(raw);
        if (Number.isNaN(date.getTime())) return "未标注时间";
        const now = new Date();
        return date.toDateString() === now.toDateString()
          ? (window.plaudI18n?.locale === "en" ? "Today" : "今天")
          : (window.plaudI18n?.formatDate || ((value, options) => new Date(value).toLocaleDateString(undefined, options)))(raw, {
              year: "numeric",
              month: "long",
              day: "numeric",
            });
      };
      const apiError = async (response) => {
        try {
          const body = await response.json();
          const message = text(body.error);
          return message && message.length <= 80 ? message : "操作暂时没有完成";
        } catch {
          return "操作暂时没有完成";
        }
      };
      async function request(path, options = {}) {
        const timeoutMs = clamp(Number(options.timeoutMs) || 8000, 1000, 30000);
        const controller = new AbortController();
        const callerSignal = options.signal;
        let timedOut = false;
        const abortFromCaller = () => controller.abort();
        if (callerSignal?.aborted) controller.abort();
        else callerSignal?.addEventListener("abort", abortFromCaller, { once: true });
        const timer = window.setTimeout(() => {
          timedOut = true;
          controller.abort();
        }, timeoutMs);
        try {
          const { timeoutMs: _timeoutMs, signal: _signal, ...fetchOptions } = options;
          const response = await fetch(path, {
            ...fetchOptions,
            signal: controller.signal,
          });
          if (!response.ok) {
            const error = new Error(await apiError(response));
            // Keep the transport status on the error so typed callers can
            // distinguish a retryable source-initialization response from a
            // permanent auth/validation failure without exposing protocol
            // details in the user-facing message.
            error.status = response.status;
            throw error;
          }
          return response.status === 204 ? null : await response.json();
        } catch (error) {
          if (timedOut) throw new Error("请求超时，请重试");
          throw error;
        } finally {
          window.clearTimeout(timer);
          callerSignal?.removeEventListener("abort", abortFromCaller);
        }
      }
      // App-data hosts may wrap a handler response in {data}; direct HTTP
      // hosts return the legacy object directly. Keep both transports on the
      // same read contract so a fresh install cannot appear empty only because
      // the bridge envelope was not unwrapped by the bootstrap path.
      function responseBody(value) {
        return value && value.data && typeof value.data === "object" && !Array.isArray(value.data)
          ? value.data
          : value;
      }
      function sourceGroup(sourceType, news = false) {
        const value = text(sourceType).toLowerCase();
        if (news || /rss|atom|api|video|audio|web|connector/.test(value))
          return "rss";
        if (/obsidian|markdown|note|file/.test(value)) return "obsidian";
        if (/meeting|transcript|recording|audio/.test(value)) return "meeting";
        return "meeting";
      }
      function newsDisplayType(item) {
        const value = text(item.view_type, "article").toLowerCase();
        return ["video", "audio", "picture"].includes(value) ? value : "rss";
      }
      function entryDate(entry) {
        return (
          entry.date ||
          entry.published_at ||
          entry.observed_at ||
          entry.due_at ||
          ""
        );
      }
      function kindIcon(entry) {
        if (entry.kind === "news")
          return entry.type === "video"
            ? icon("video")
            : entry.type === "audio"
            ? icon("audio")
            : entry.type === "picture"
            ? icon("picture")
            : icon("rss");
        return icon(
          entry.type === "meeting"
            ? "meeting"
            : entry.type === "obsidian"
            ? "obsidian"
            : entry.type === "action"
            ? "action"
            : entry.type === "metric"
            ? "metric"
            : entry.type === "project"
            ? "project"
            : entry.type === "signal"
            ? "signal"
            : entry.type === "decision"
            ? "decision"
            : "inbox"
        );
      }
      function kindClass(entry) {
        return entry.kind === "news" && entry.type !== "rss"
          ? entry.type
          : entry.type;
      }
      function badge(label, kind = "") {
        return label
          ? '<span class="badge ' + kind + '">' + esc(label) + "</span>"
          : "";
      }
      function statusKind(status) {
        return status === "ok" ? "" : status === "partial" ? "partial" : "bad";
      }
      function renderHidden() {
        const hidden = state.dashboard?.hidden_modules || [];
        const area = $("hidden-area");
        area.hidden = !hidden.length;
        $("hidden-list").innerHTML = hidden
          .map(
            (id) =>
              '<div class="hidden-chip"><span>' +
              esc(moduleNames[id] || id) +
              '</span><button type="button" data-restore-module="' +
              esc(id) +
              '">恢复</button></div>'
          )
          .join("");
      }
      function renderTimeline() {
        const entries = filteredEntries();
        const counts = entryCounts();
        $("timeline-count").textContent = String(
          state.query ? entries.length : counts[state.view] ?? entries.length
        );
        if (!entries.length) {
          const empty =
            state.view === "today"
              ? [
                  "当前没有待决策或待行动事项",
                  "新的风险、机会或决策草案会出现在这里",
                ]
              : ["没有匹配内容", "换一个筛选或等待下一次来源同步"];
          $("timeline").innerHTML =
            '<div class="empty-state">' +
            icon("inbox") +
            "<strong>" +
            esc(empty[0]) +
            "</strong><p>" +
            esc(empty[1]) +
            "</p></div>";
          return;
        }
        let last = "";
        let html = "";
        entries.forEach((entry) => {
          const bucket =
            state.view === "today"
              ? {
                  decision: "待我决策",
                  action: "逾期与今日行动",
                  signal: "关键风险与机会",
                }[entry.kind] || "其他"
              : dateBucket(entryDate(entry));
          if (bucket !== last) {
            html += '<div class="date-label">' + esc(bucket) + "</div>";
            last = bucket;
          }
          html += renderEntry(entry);
        });
        $("timeline").innerHTML = html;
      }
      function safeHTML(raw) {
        const doc = new DOMParser().parseFromString(
          String(raw || ""),
          "text/html"
        );
        const allowed = new Set([
          "ARTICLE",
          "P",
          "DIV",
          "MAIN",
          "SECTION",
          "BR",
          "STRONG",
          "B",
          "EM",
          "I",
          "U",
          "UL",
          "OL",
          "LI",
          "A",
          "BLOCKQUOTE",
          "CODE",
          "PRE",
          "H1",
          "H2",
          "H3",
        ]);
        doc.body.querySelectorAll("*").forEach((node) => {
          if (!allowed.has(node.tagName)) {
            node.replaceWith(...node.childNodes);
            return;
          }
          [...node.attributes].forEach((attr) => {
            if (
              node.tagName === "A" &&
              attr.name.toLowerCase() === "href" &&
              /^https?:\/\//i.test(attr.value)
            ) {
              node.setAttribute("target", "_blank");
              node.setAttribute("rel", "noopener noreferrer");
            } else node.removeAttribute(attr.name);
          });
        });
        return doc.body.innerHTML;
      }
      function updateSourceBar() {
        const source = state.dashboard?.source || {},
          status = source.status || "unavailable";
        $("bar-status").innerHTML =
          '<i class="status-dot ' +
          statusKind(status) +
          '"></i><span>' +
          esc(
            status === "ok"
              ? "来源正常"
              : status === "partial"
              ? "部分来源可用"
              : "来源暂不可用"
          ) +
          "</span>";
      }
      function showError(message) {
        $("error-bar").textContent = message;
        $("error-bar").hidden = false;
        window.clearTimeout(showError.timer);
        showError.timer = window.setTimeout(() => {
          $("error-bar").hidden = true;
        }, 4200);
      }
      function showToast(message) {
        $("toast").textContent = message;
        $("toast").hidden = false;
        window.clearTimeout(showToast.timer);
        showToast.timer = window.setTimeout(() => {
          $("toast").hidden = true;
        }, 3600);
      }
      async function mutate(path, method, payload) {
        try {
          await request(path, {
            method,
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload),
          });
          await refresh();
        } catch (error) {
          showError(error.message || "操作没有保存");
        }
      }
      function applyReaderWidths() {
        const root = $("reader-shell"),
          viewport = Math.max(320, window.innerWidth || root.clientWidth || 0);
        let sidebar = clamp(
            Number(state.reader.sidebar_width) || 256,
            220,
            360
          ),
          timeline = clamp(
            Number(state.reader.timeline_width) || 450,
            360,
            640
          );
        if (viewport > 900) {
          const detailMinimum = 300,
            splitters = 16,
            available = viewport - detailMinimum - splitters;
          timeline = Math.min(timeline, Math.max(320, available - sidebar));
          if (sidebar + timeline > available)
            sidebar = Math.max(200, available - timeline);
        }
        root.style.setProperty("--sidebar-width", sidebar + "px");
        root.style.setProperty("--timeline-width", timeline + "px");
      }
      function persistReader() {
        return request("./api/reader", {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            sidebar_width: state.reader.sidebar_width,
            timeline_width: state.reader.timeline_width,
          }),
        }).catch((error) => showError(error.message || "栏宽没有保存"));
      }
      function startColumnResize(event, kind) {
        if (event.button !== 0 || window.innerWidth <= 900) return;
        event.preventDefault();
        const startX = event.clientX;
        const start = { ...state.reader };
        state.resize = { kind, startX, start };
        event.currentTarget.setPointerCapture?.(event.pointerId);
        document.body.classList.add("resizing");
      }
      function moveColumnResize(event) {
        const resize = state.resize;
        if (!resize) return;
        const delta = event.clientX - resize.startX;
        if (resize.kind === "sidebar")
          state.reader.sidebar_width = clamp(
            Math.round(resize.start.sidebar_width + delta),
            220,
            360
          );
        else
          state.reader.timeline_width = clamp(
            Math.round(resize.start.timeline_width + delta),
            360,
            640
          );
        applyReaderWidths();
      }
      function finishColumnResize() {
        if (!state.resize) return;
        state.resize = null;
        document.body.classList.remove("resizing");
        void persistReader();
      }
      function changeColumn(kind, direction) {
        if (kind === "sidebar")
          state.reader.sidebar_width = clamp(
            state.reader.sidebar_width + direction * 16,
            220,
            360
          );
        else
          state.reader.timeline_width = clamp(
            state.reader.timeline_width + direction * 16,
            360,
            640
          );
        applyReaderWidths();
        void persistReader();
      }
      function bindTimelineEvents() {
        const timeline = $("timeline");
        timeline.addEventListener("click", (event) => {
          const select = event.target.closest("[data-select-entry]");
          if (select) {
            selectEntry(select.dataset.selectEntry);
            return;
          }
          const obsidianToggle = event.target.closest(
            "[data-toggle-obsidian-folder]"
          );
          if (obsidianToggle) {
            event.preventDefault();
            event.stopPropagation();
            window.__toggleReaderObsidianFolder?.(
              obsidianToggle.dataset.toggleObsidianFolder || ""
            );
            return;
          }
          const obsidianView = event.target.closest(
            ".obsidian-directory [data-view]"
          );
          if (obsidianView) {
            event.preventDefault();
            event.stopPropagation();
            state.view = obsidianView.dataset.view;
            state.selected = null;
            document.body.classList.remove("detail-open");
            render();
            return;
          }
          const star = event.target.closest("[data-star-entry]");
          if (star) {
            event.stopPropagation();
            void itemMutation(
              star.dataset.starEntry,
              "starred",
              !pref({ key: star.dataset.starEntry }).starred
            );
            return;
          }
          const del = event.target.closest("[data-delete-action]");
          if (del) {
            event.stopPropagation();
            void mutate("./api/actions", "DELETE", {
              id: del.dataset.deleteAction,
            });
          }
        });
        timeline.addEventListener("change", (event) => {
          const complete = event.target.closest("[data-complete-action]");
          if (complete)
            void mutate("./api/actions", "PATCH", {
              id: complete.dataset.completeAction,
              completed: complete.checked,
            });
        });
      }
      window.__plaudStandaloneParity = true;
      function privateHost(hostname) {
        const host = hostname
          .toLowerCase()
          .replace(/^\[|\]$/g, "")
          .replace(/\.$/, "");
        if (
          host === "localhost" ||
          host.endsWith(".localhost") ||
          host === "localhost.localdomain" ||
          host === "::" ||
          host === "::1" ||
          host.startsWith("::ffff:")
        )
          return true;
        if (
          host.includes(":") &&
          (host.startsWith("fc") ||
            host.startsWith("fd") ||
            /^fe[89ab][0-9a-f]:/i.test(host))
        )
          return true;
        const octets = host.split(".").map(Number);
        if (
          octets.length !== 4 ||
          octets.some(
            (value) => !Number.isInteger(value) || value < 0 || value > 255
          )
        )
          return false;
        const [first, second] = octets;
        return (
          first === 0 ||
          first === 10 ||
          first === 127 ||
          (first === 100 && second >= 64 && second <= 127) ||
          (first === 169 && second === 254) ||
          (first === 172 && second >= 16 && second <= 31) ||
          (first === 192 && second === 168) ||
          (first === 198 && (second === 18 || second === 19)) ||
          first >= 224
        );
      }
      function publicURL(raw) {
        try {
          const url = new URL(raw);
          if (
            !["http:", "https:"].includes(url.protocol) ||
            url.username ||
            url.password ||
            privateHost(url.hostname)
          )
            return false;
          for (const key of new URLSearchParams(url.search).keys())
            if (/token|secret|password|api[-_]?key|authorization/i.test(key))
              return false;
          return url.href.length <= 1000;
        } catch {
          return false;
        }
      }
      /* Profile-owned Reader state shared by recordings and source subscriptions. */
      state.sources = [];
      state.recordingDetails = new Map();
      state.recordingPrefs = new Map();
      // The lightweight source directory intentionally returns zero
      // placeholders for entry/unread counts.  Keep the last authoritative
      // values when that projection is refreshed, otherwise a transient
      // counts-request failure makes populated sources look empty.  A source
      // with no historical value still gets an explicit zero so the sidebar
      // layout remains stable.
      function mergeSourceCatalogCounts(previousSources, nextSources, countsIncluded) {
        const incoming = Array.isArray(nextSources) ? nextSources : [];
        if (countsIncluded) return incoming;
        const previousByID = new Map(
          (Array.isArray(previousSources) ? previousSources : [])
            .filter((source) => source && source.id)
            .map((source) => [source.id, source])
        );
        return incoming.map((source) => {
          const previous = previousByID.get(source?.id);
          if (!previous) return source;
          const merged = { ...source };
          for (const key of ["entry_count", "unread_count"]) {
            const value = Number(previous[key]);
            if (Number.isFinite(value) && value >= 0) merged[key] = value;
          }
          return merged;
        });
      }
      window.__plaudMergeSourceCatalogCounts = mergeSourceCatalogCounts;
      const sourceStatusLabel = (status) =>
        status === "ok"
          ? "正常"
          : status === "partial"
          ? "部分可用"
          : "暂不可用";
      const recordingStatusLabel = (status) =>
        ({
          done: "已完成",
          transcribing: "转写中",
          summarizing: "整理中",
          failed: "处理失败",
        }[text(status)] || "处理中");
      const secondsLabel = (value) => {
        const seconds = Math.max(0, Math.floor(Number(value) || 0)),
          m = Math.floor(seconds / 60),
          s = String(seconds % 60).padStart(2, "0");
        return m + ":" + s;
      };
      const audioURL = (ref) => {
        const value = text(ref).replace(/^\/+/, "");
        return value.endsWith("/audio") &&
          (value.startsWith("_zettlab/recordings/") ||
            value.startsWith("_zettlab/device-data/v1/meetings/"))
          ? "./" + value
          : "";
      };
      function sourceIDForEntry(entry) {
        return text(entry.sourceID || entry.item?.source_id);
      }
      function sourceLabel(entry) {
        return entry.kind === "recording"
          ? entry.item?.live_device
            ? "设备会议"
            : "我的录音"
          : text(entry.source, "未知来源");
      }
      function videoProviderForItem(item) {
        const sourceID = text(item?.source_id).toLowerCase();
        const source = (state.sources || []).find(
          (candidate) => candidate.id === item?.source_id
        );
        const sourceKind = text(source?.kind).toLowerCase();
        const feedFormat = text(source?.feed_format).toLowerCase();
        const sourceType = text(item?.source_type).toLowerCase();
        let host = "";
        let path = "";
        try {
          const target = new URL(text(item?.url));
          host = target.hostname.toLowerCase();
          path = target.pathname.toLowerCase();
        } catch {}
        if (
          sourceKind === "youtube" ||
          feedFormat === "youtube" ||
          sourceType === "youtube" ||
          sourceID.startsWith("youtube-") ||
            /(?:youtube\.com\/(?:watch|shorts|embed|live)|youtu\.be\/)/i.test(
            `${host}${path}`
          )
        )
          return "youtube";
        if (
          sourceKind === "bilibili" ||
          sourceKind === "bilibili-video" ||
          feedFormat === "bilibili" ||
          feedFormat === "bilibili-video" ||
          sourceType === "bilibili" ||
          sourceType === "bilibili-video" ||
          /^(?:bilibili|bilibili-video|b23)-/.test(sourceID) ||
          (/(?:^|\.)bilibili\.com$/.test(host) &&
            /\/video\//.test(path)) ||
          (host === "b23.tv" && path.length > 1)
        )
          return "bilibili";
        // X/social video attachments stay in their social source; only
        // YouTube and Bilibili populate the dedicated video gallery.
        return "";
      }
      function isVideoNews(item) {
        return Boolean(videoProviderForItem(item));
      }
      function isYouTubeNews(item) {
        return videoProviderForItem(item) === "youtube";
      }
      function makeEntries(d) {
        const entries = [];
        const liveMeetingByID = new Map(
          (state.liveMeetings || []).map((item) => [item.id, item])
        );
        (d.news || []).slice(0, 1000).forEach((item) => {
          const videoProvider = videoProviderForItem(item);
          const video = Boolean(videoProvider);
          entries.push({
            key: "news:" + item.id,
            kind: "news",
            group: video ? "video" : "rss",
            type: video ? videoProvider : newsDisplayType(item),
            videoProvider: video ? videoProvider : "",
            id: item.id,
            title: item.title,
            summary: item.summary_text,
            source: item.source || "未知来源",
            sourceID: item.source_id,
            date: item.published_at,
            item,
          });
        });
        (d.recordings || []).slice(0, 50).forEach((item) => {
          const live = liveMeetingByID.get(item.id);
          const current = live ? { ...item, ...live, live_device: true } : item;
          entries.push({
            key: "recording:" + item.id,
            kind: "recording",
            group: "recording",
            type: "meeting",
            id: item.id,
            title: current.title || "未命名录音",
            summary: current.summary || recordingStatusLabel(current.status),
            source: "我的录音",
            sourceID: "my-recordings",
            date: current.started_at || current.created_at,
            item: current,
          });
        });
        (state.liveMeetings || []).slice(0, 50).forEach((item) => {
          if ((d.recordings || []).some((recording) => recording.id === item.id)) return;
          entries.push({
            key: "recording:" + item.id,
            kind: "recording",
            group: "recording",
            type: "meeting",
            id: item.id,
            title: item.title || "未命名会议",
            summary: item.summary || recordingStatusLabel(item.status),
            source: "设备会议",
            sourceID: "device-meetings",
            date: item.started_at || item.created_at,
            item,
          });
        });
        (d.evidence || []).slice(0, 100).forEach((item) => {
          const group = sourceGroup(item.source_type);
          entries.push({
            key: "evidence:" + item.id,
            kind: "evidence",
            group,
            type: group,
            id: item.id,
            title: item.title,
            summary: item.quote,
            source: item.source_type || "会议记录",
            sourceID: item.source_type,
            date: item.observed_at,
            item,
          });
        });
        (state.liveObsidian || []).slice(0, 500).forEach((item) => {
          if ((d.evidence || []).some((evidence) => evidence.id === item.id)) return;
          entries.push({
            key: "evidence:" + item.id,
            kind: "evidence",
            group: "obsidian",
            type: "obsidian",
            id: item.id,
            title: item.title || item.name || "未命名笔记",
            summary: item.quote || "来自 Obsidian 的实时笔记",
            source: "Obsidian",
            sourceID: "obsidian",
            date: item.observed_at || item.modified_at,
            item,
          });
        });
        (d.actions || []).slice(0, 100).forEach((item) =>
          entries.push({
            key: "action:" + item.id,
            kind: "action",
            group: "action",
            type: "action",
            id: item.id,
            title: item.task,
            summary: item.owner || "负责人待确认",
            source: "行动",
            date: item.due_at,
            item,
          })
        );
        (d.metrics || []).slice(0, 100).forEach((item) =>
          entries.push({
            key: "metric:" + item.id,
            kind: "metric",
            group: "action",
            type: "metric",
            id: item.id,
            title: item.name,
            summary: item.value,
            source: "经营指标",
            date: item.period,
            item,
          })
        );
        (d.projects || []).slice(0, 100).forEach((item) =>
          entries.push({
            key: "project:" + item.id,
            kind: "project",
            group: "action",
            type: "project",
            id: item.id,
            title: item.name,
            summary: item.summary,
            source: "项目",
            date: "",
            item,
          })
        );
        (d.signals || []).slice(0, 100).forEach((item) =>
          entries.push({
            key: "signal:" + item.id,
            kind: "signal",
            group: "rss",
            type: "signal",
            id: item.id,
            title: item.title,
            summary: item.detail,
            source: "信号",
            date: "",
            item,
          })
        );
        (d.decisions || []).slice(0, 100).forEach((item) =>
          entries.push({
            key: "decision:" + item.id,
            kind: "decision",
            group: "action",
            type: "decision",
            id: item.id,
            title: item.question,
            summary: item.recommendation,
            source: "待决策",
            date: "",
            item,
          })
        );
        entries.sort((a, b) => {
          const ad = Date.parse(entryDate(a)) || 0,
            bd = Date.parse(entryDate(b)) || 0;
          return bd - ad || a.title.localeCompare(b.title, "zh-CN");
        });
        return entries.slice(0, 1000);
      }
      function pref(entry) {
        if (entry && entry.kind === "recording")
          return (
            state.recordingPrefs.get(entry.id) || {
              recording_id: entry.id,
              read: false,
              starred: false,
              playback_seconds: 0,
            }
          );
        return (
          state.prefs.get(entry?.key) || {
            key: entry?.key || "",
            read: false,
            starred: false,
          }
        );
      }
      function isOpenAction(entry) {
        const item = entry?.item || entry || {};
        return (
          text(item.status).toLowerCase() !== "completed" &&
          text(item.confirmation_status).toLowerCase() !== "rejected"
        );
      }
      function isPendingDecision(entry) {
        const item = entry?.item || entry || {};
        return !["accepted", "rejected", "resolved", "completed"].includes(
          text(item.status, "draft").toLowerCase()
        );
      }
      function executiveQueue(entries) {
        const order = { decision: 0, action: 1, signal: 2 };
        return entries
          .filter(
            (entry) =>
              (entry.kind === "decision" && isPendingDecision(entry)) ||
              (entry.kind === "action" && isOpenAction(entry)) ||
              entry.kind === "signal"
          )
          .sort((a, b) => {
            const byKind = (order[a.kind] ?? 9) - (order[b.kind] ?? 9);
            if (byKind) return byKind;
            const ad = Date.parse(entryDate(a)) || Number.MAX_SAFE_INTEGER;
            const bd = Date.parse(entryDate(b)) || Number.MAX_SAFE_INTEGER;
            return ad - bd || a.title.localeCompare(b.title, "zh-CN");
          });
      }
      function filteredEntries() {
        const d = state.dashboard || {};
        let entries = makeEntries(d);
        if (state.view === "today") entries = executiveQueue(entries);
        else if (state.view === "decision")
          entries = entries.filter(
            (entry) => entry.kind === "decision" && isPendingDecision(entry)
          );
        else if (state.view === "signal")
          entries = entries.filter((entry) => entry.kind === "signal");
        else if (state.view === "action")
          entries = entries.filter(
            (entry) => entry.kind === "action" && isOpenAction(entry)
          );
        else if (state.view === "rss")
          entries = entries.filter(
            (entry) => entry.kind === "news" && entry.group !== "video"
          );
        else if (state.view === "video")
          entries = entries.filter(
            (entry) => entry.kind === "news" && entry.group === "video"
          );
        else if (state.view === "youtube")
          entries = entries.filter(
            (entry) =>
              entry.kind === "news" &&
              entry.group === "video" &&
              entry.videoProvider === "youtube"
          );
        else if (state.view === "meeting")
          entries = entries.filter((entry) =>
            ["meeting", "recording"].includes(entry.group)
          );
        else if (state.view === "starred")
          entries = entries.filter((e) => pref(e).starred);
        else if (["obsidian", "recording"].includes(state.view))
          entries = entries.filter((e) => e.group === state.view);
        else if (state.view.startsWith("source:")) {
          const id = state.view.slice(7);
          entries = entries.filter(
            (e) => sourceIDForEntry(e) === id || sourceLabel(e) === id
          );
        }
        const q = state.query.toLowerCase();
        if (q)
          entries = entries.filter((e) =>
            [e.title, e.summary, e.source, e.type].some((v) =>
              text(v).toLowerCase().includes(q)
            )
          );
        return entries;
      }
      function entryCounts() {
        const dashboard = state.dashboard || {},
          entries = makeEntries(dashboard),
          counts = {
            all: Math.max(entries.length, Number(dashboard.total_entries) || 0),
            today: 0,
            rss: 0,
            video: 0,
            youtube: 0,
            bilibili: 0,
            meeting: 0,
            recording: 0,
            obsidian: 0,
            action: 0,
            decision: 0,
            signal: 0,
            starred: 0,
          };
        // /api/reader/counts is the authoritative aggregate projection. The
        // lightweight dashboard intentionally contains no news rows, so
        // deriving RSS/video badges from `entries` alone makes a populated
        // fresh install look like it has zero content.
        for (const [key, field] of [
          ["rss", "rss_entries"],
          ["video", "video_entries"],
          ["youtube", "youtube_entries"],
          ["picture", "picture_entries"],
        ]) {
          const total = Number(dashboard[field]);
          if (Number.isFinite(total) && total >= 0) counts[key] = total;
        }
        // Bilibili is intentionally not a separate aggregate query field, but
        // its source-level counts are authoritative and already arrive through
        // /api/reader/counts. Sum only Bilibili catalog rows so the sidebar
        // badge does not regress to zero while the dashboard is lightweight.
        const sourceBilibiliCount = (state.sources || [])
          .filter((source) => /bilibili|b23\.tv|哔哩|B站/i.test(`${source.id || ""} ${source.name || ""} ${source.url || ""}`))
          .reduce((total, source) => {
            const count = Number(source.entry_count);
            return total + (Number.isFinite(count) && count >= 0 ? count : 0);
          }, 0);
        entries.forEach((e) => {
          if (e.kind === "news") {
            if (e.group === "video") {
              counts.video++;
              if (e.videoProvider === "youtube") counts.youtube++;
              if (e.videoProvider === "bilibili") counts.bilibili++;
            }
            else counts.rss++;
          }
          if (["meeting", "recording"].includes(e.group)) counts.meeting++;
          if (e.group === "recording") counts.recording++;
          if (e.group === "obsidian") counts.obsidian++;
          if (e.kind === "action" && isOpenAction(e)) counts.action++;
          if (e.kind === "decision" && isPendingDecision(e)) counts.decision++;
          if (e.kind === "signal") counts.signal++;
          if (pref(e).starred) counts.starred++;
        });
        counts.bilibili = Math.max(counts.bilibili, sourceBilibiliCount);
        counts.today = counts.decision + counts.action + counts.signal;
        return counts;
      }
      function renderNav() {
        const counts = entryCounts();
        document.querySelectorAll("[data-count]").forEach((node) => {
          node.textContent = String(counts[node.dataset.count] ?? 0);
        });
        document
          .querySelectorAll("[data-view]")
          .forEach((button) =>
            button.classList.toggle(
              "active",
              button.dataset.view === state.view
            )
          );
        const title =
          state.view === "today"
            ? "优先队列"
            : state.view === "decision"
            ? "待我决策"
            : state.view === "signal"
            ? "风险与信号"
            : state.view === "action"
            ? "行动"
            : state.view === "all"
            ? "全部内容"
            : state.view === "starred"
            ? "收藏"
            : state.view === "recording"
            ? "我的录音"
            : typeNames[state.view] || state.view.slice(7);
        $("timeline-title").textContent = title;
        const visible = filteredEntries();
        $("timeline-count").textContent = String(
          state.query ? visible.length : counts[state.view] ?? visible.length
        );
        $("timeline-meta").textContent =
          state.view === "today"
            ? "只显示待决策、待行动和关键风险"
            : state.view === "decision"
            ? "需要你确认方向或取舍的事项"
            : state.view === "signal"
            ? "可能改变目标、资源或节奏的风险与机会"
            : state.view === "action"
            ? "尚未完成且需要跟进的行动"
            : state.view === "rss"
            ? "公开订阅文章"
            : state.view === "video"
            ? "YouTube、B 站与其他视频"
            : state.view === "youtube"
            ? "YouTube 视频订阅"
            : state.view === "recording"
            ? "当前 Agent 绑定的录音"
            : state.view === "meeting"
            ? "会议证据与逐字稿"
            : "会议、订阅、笔记与行动汇聚在这里";
        $("search").placeholder =
          state.view === "today"
            ? "搜索决策、行动或信号"
            : "搜索标题、来源或摘要";
      }
      function renderSources() {
        const entries = makeEntries(state.dashboard || {}),
          catalog = state.sources || [],
          rows = catalog.map((source) => {
            const projectedCount = Number(source.entry_count);
            const count = Number.isFinite(projectedCount) && projectedCount >= 0
              ? projectedCount
              : entries.filter(
                  (e) =>
                    sourceIDForEntry(e) === source.id ||
                    sourceLabel(e) === source.name
                ).length;
            return {
              id: source.id,
              label: source.name,
              count,
              group: "rss",
              status: source.status || "unavailable",
              subscribed: source.subscribed,
            };
          });
        const seen = new Set(rows.map((row) => row.label));
        entries.forEach((entry) => {
          const label = sourceLabel(entry);
          if (seen.has(label)) return;
          seen.add(label);
          rows.push({
            id: sourceIDForEntry(entry) || label,
            label,
            count: entries.filter((item) => sourceLabel(item) === label).length,
            group: entry.group,
            status: "ok",
            subscribed: true,
          });
        });
        rows.sort(
          (a, b) => b.count - a.count || a.label.localeCompare(b.label, "zh-CN")
        );
        $("source-count").textContent = String(rows.length);
        $("source-list").innerHTML =
          rows
            .slice(0, 16)
            .map(
              (row) =>
                '<button class="source-item ' +
                (state.view === "source:" + esc(row.id) ? "active" : "") +
                '" type="button" data-view="source:' +
                esc(row.id) +
                '"><i class="source-dot ' +
                statusKind(row.status) +
                '"></i>' +
                icon(
                  row.group === "obsidian"
                    ? "obsidian"
                    : row.group === "recording"
                    ? "meeting"
                    : row.group === "video"
                    ? "video"
                    : "rss"
                ) +
                '<span class="label">' +
                esc(row.label) +
                '</span><span class="count">' +
                row.count +
                "</span></button>"
            )
            .join("") || '<div class="source-empty">暂无来源记录</div>';
      }
      function renderCatalog() {
        const list = $("source-catalog");
        if (!list) return;
        const sources = state.sources || [];
        list.innerHTML = sources.length
          ? sources
              .map(
                (source) =>
                  '<div class="catalog-item"><div class="catalog-copy"><strong>' +
                  esc(source.name) +
                  "</strong><p>" +
                  esc(source.description || source.url) +
                  '</p><div class="catalog-meta">' +
                  badge(source.kind.toUpperCase(), "blue") +
                  badge(
                    sourceStatusLabel(source.status),
                    source.status === "ok"
                      ? "green"
                      : source.status === "partial"
                      ? "amber"
                      : "red"
                  ) +
                  (source.is_default ? badge("默认来源", "teal") : "") +
                  '</div></div><div class="catalog-actions"><button class="icon-btn ' +
                  (source.subscribed ? "starred" : "") +
                  '" type="button" data-subscribe-source="' +
                  esc(source.id) +
                  '" aria-label="' +
                  (source.subscribed ? "退订" : "订阅") +
                  source.name +
                  '" title="' +
                  (source.subscribed ? "退订" : "订阅") +
                  '">' +
                  (source.subscribed ? icon("check") : icon("plus")) +
                  '</button><button class="icon-btn" type="button" data-sync-source="' +
                  esc(source.id) +
                  '" aria-label="同步' +
                  esc(source.name) +
                  '" title="同步' +
                  esc(source.name) +
                  '">' +
                  icon("refresh") +
                  "</button></div></div>"
              )
              .join("")
          : '<div class="catalog-empty">暂无可发现来源</div>';
      }
      async function recordingMutation(id, payload) {
        const current = pref({ kind: "recording", id });
        state.recordingPrefs.set(id, {
          ...current,
          ...payload,
          recording_id: id,
        });
        render();
        try {
          await request("./api/recordings/preferences", {
            method: "PATCH",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ recording_id: id, ...payload }),
          });
          if (Object.prototype.hasOwnProperty.call(payload, "starred"))
            await refresh();
        } catch (error) {
          showError(error.message || "录音状态没有保存");
          void refresh();
        }
      }
      async function itemMutation(key, field, value) {
        if (String(key).startsWith("recording:"))
          return recordingMutation(String(key).slice(10), { [field]: value });
        const current = pref({ key });
        state.prefs.set(key, { ...current, [field]: value });
        window.__rebindReaderNewsProjection?.();
        // Full repaint only when the current view filters on this field
        // (starred / read-later views would keep stale cards), or when no
        // visible card can carry the local state flip.
        const filterSensitive =
          (state.view === "starred" && field === "starred") ||
          (state.view === "later" && field === "read_later");
        if (
          filterSensitive ||
          !window.__updateReaderCardLocally?.(key, field, value)
        )
          render();
        try {
          await request("./api/items", {
            method: "PATCH",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ key, [field]: value }),
          });
        } catch (error) {
          showError(error.message || "阅读状态没有保存");
          await refresh();
        }
      }
      function renderEntry(entry) {
        const p = pref(entry),
          item = entry.item || {},
          media = item.thumbnail_url
            ? '<span class="entry-media-wrap"><img src="' +
              esc(item.thumbnail_url) +
              '" alt=""><span class="play-mark">' +
              icon("arrow") +
              "</span></span>"
            : "";
        const actionControls =
          entry.kind === "action"
            ? '<input type="checkbox" data-complete-action="' +
              esc(entry.id) +
              '" ' +
              (item.status === "completed" ? "checked" : "") +
              ' aria-label="标记待办完成">'
            : "";
        const detail =
          entry.kind === "recording"
            ? badge(
                recordingStatusLabel(item.status),
                item.status === "done" ? "green" : "amber"
              )
            : "";
        const star = p.starred ? icon("starFill") : icon("star");
        return (
          '<article class="entry ' +
          (p.read ? "read " : "") +
          (state.selected === entry.key ? "selected" : "") +
          '" data-entry-key="' +
          esc(entry.key) +
          '"><div class="entry-icon ' +
          esc(kindClass(entry)) +
          '">' +
          kindIcon(entry) +
          '</div><button class="entry-main" type="button" data-select-entry="' +
          esc(entry.key) +
          '">' +
          media +
          '<span class="entry-title">' +
          esc(entry.title) +
          '</span><span class="entry-snippet">' +
          esc(entry.summary || "暂无摘要") +
          '</span><span class="entry-meta">' +
          badge(typeNames[entry.type] || typeNames[entry.kind], "blue") +
          detail +
          '<span class="source-name">' +
          esc(sourceLabel(entry)) +
          "</span><span>" +
          esc(formatDate(entryDate(entry))) +
          '</span></span></button><span class="entry-actions">' +
          actionControls +
          '<button class="icon-btn ' +
          (p.starred ? "starred" : "") +
          '" type="button" data-star-entry="' +
          esc(entry.key) +
          '" aria-label="' +
          (p.starred ? "取消收藏" : "收藏") +
          '" title="' +
          (p.starred ? "取消收藏" : "收藏") +
          '">' +
          star +
          "</button></span></article>"
        );
      }
      function renderOverview() {
        // 内容概览已下线：未选中条目时详情面板渲染缺省页，模块入口收敛到左侧导航。
        const panel = $("detail-panel");
        if (!panel) return;
        const links = [
          ["all", "全部内容"],
          ["meeting", "会议与录音"],
          ["obsidian", "Obsidian"],
        ]
          .map(
            ([view, label]) =>
              '<button class="overview-default-link" type="button" data-overview-view="' +
              esc(view) +
              '">' +
              esc(label) +
              "</button>"
          )
          .join("");
        panel.innerHTML =
          '<div class="overview-default"><img class="overview-default-mark" src="./static/zettlab-focus-logo.svg" alt=""><h2>焦点空间</h2><p>从左侧选择信息、会议或笔记，内容会在这里展开</p><div class="overview-default-links">' +
          links +
          "</div></div>";
        panel
          .querySelectorAll("[data-overview-view]")
          .forEach((button) =>
            button.addEventListener("click", () => {
              const view = button.dataset.overviewView;
              if (window.__plaudSwitchView) window.__plaudSwitchView(view);
              else renderViewSwitch(view);
            })
          );
      }
      function recordingDetail(id) {
        const value = state.recordingDetails.get(id);
        return (
          value ||
          (state.dashboard?.recordings || []).find((item) => item.id === id) ||
          {}
        );
      }
      function recordingTranscript(item) {
        if (Array.isArray(item.segments) && item.segments.length)
          return (
            '<div class="transcript">' +
            item.segments
              .map(
                (segment) =>
                  '<button class="transcript-segment" type="button" data-transcript-start="' +
                  esc(String(segment.start)) +
                  '"><span class="transcript-time">' +
                  esc(secondsLabel(segment.start)) +
                  '</span><span class="transcript-text">' +
                  esc(
                    (segment.speaker ? segment.speaker + "：" : "") +
                      segment.text
                  ) +
                  "</span></button>"
              )
              .join("") +
            "</div>"
          );
        return (
          '<div class="recording-summary">' +
          esc(item.transcript || "暂无逐字稿") +
          "</div>" +
          (item.transcript_truncated ? badge("逐字稿已截断", "amber") : "")
        );
      }
      function renderDetail() {
        const entry =
          state.selected &&
          makeEntries(state.dashboard || {}).find(
            (item) => item.key === state.selected
          );
        if (!entry) {
          renderOverview();
          return;
        }
        const p = pref(entry),
          item =
            entry.kind === "recording"
              ? recordingDetail(entry.id)
              : entry.item || {};
        let body = "";
        if (entry.kind === "recording") {
          const source = audioURL(item.audio_ref);
          body =
            '<div class="recording-status">' +
            badge(
              recordingStatusLabel(item.status),
              item.status === "done" ? "green" : "amber"
            ) +
            (item.speaker_count
              ? badge(item.speaker_count + " 位说话人", "blue")
              : "") +
            badge(secondsLabel(item.duration_seconds), "teal") +
            "</div>" +
            (source
              ? '<audio class="recording-player" id="recording-audio" controls preload="metadata" src="' +
                esc(source) +
                '"></audio>'
              : '<div class="source-empty">当前录音没有可播放音频</div>') +
            '<section class="recording-analysis"><h3>摘要</h3><div class="recording-summary">' +
            esc(item.summary || "暂无摘要") +
            '</div></section><section class="recording-analysis"><h3>逐字稿</h3>' +
            recordingTranscript(item) +
            "</section>" +
            (item.report
              ? '<section class="recording-analysis"><h3>分析报告</h3><div class="recording-summary">' +
                esc(item.report) +
                "</div></section>"
              : "");
        } else if (entry.kind === "news") {
          const youtubeVideo = typeof youtubeDetailMarkup === "function" ? youtubeDetailMarkup(item) : "";
          const videoProvider = videoProviderForItem(item);
          if (youtubeVideo)
            body += youtubeVideo;
          // Legacy static-contract marker: item.view_type === "video" && item.media_url
          else if (videoProvider && item.media_url)
            body +=
              '<video class="detail-media" controls preload="metadata" poster="' +
              esc(item.thumbnail_url || "") +
              '" src="' +
              esc(item.media_url) +
              '"></video>';
          else if (item.view_type === "audio" && item.media_url)
            body +=
              '<audio class="detail-media" controls preload="metadata" src="' +
              esc(item.media_url) +
              '"></audio>';
          // Legacy URL-contract marker: /(?:youtube\.com|youtu\.be)\//i.test(String(item.url || ""))
          else if (
            videoProvider &&
            videoProvider !== "youtube" &&
            item.thumbnail_url
          ) {
            body +=
              '<img class="detail-cover" src="' +
              esc(item.thumbnail_url) +
              '" alt=""><div class="detail-tools-row"><a class="text-btn" href="' +
              esc(item.url || "#") +
              '" target="_blank" rel="noopener noreferrer">打开视频 ' +
              icon("external") +
              "</a></div>";
          }
          else if (item.thumbnail_url)
            body +=
              '<img class="detail-cover" src="' +
              esc(item.thumbnail_url) +
              '" alt="">';
          // Video entries are self-contained media details. Do not append the
          // article renderer/AI/full-text surface below a YouTube player.
          if (!youtubeVideo && !videoProvider)
            body +=
              '<div class="detail-body">' +
              (item.safe_html || item.content_html
                ? safeHTML(item.safe_html || item.content_html)
                : "<p>" + esc(item.summary_text || "暂无摘要") + "</p>") +
              "</div>";
        } else if (entry.kind === "action") {
          body =
            '<div class="detail-body"><label class="overview-action"><input type="checkbox" data-complete-action="' +
            esc(item.id) +
            '" ' +
            (item.status === "completed" ? "checked" : "") +
            '><span>完成这项行动</span></label><div class="source-line"><span>负责人</span><span>' +
            esc(item.owner || "负责人待确认") +
            '</span></div><div class="source-line"><span>完成期限</span><span>' +
            esc(item.due_at || "待确认") +
            "</span></div></div>";
        } else if (entry.kind === "evidence") {
          const obsidianNote = item.source_type === "obsidian" && item.content_format === "markdown" && item.content;
          body =
            '<div class="detail-body">' +
            (obsidianNote
              ? '<div class="fulltext-rich fulltext-markdown obsidian-note-content">' + renderObsidianEvidence(item.content, item.locator) + '</div>'
              : '<blockquote>' + esc(item.quote || "暂无摘录") + '</blockquote>') +
            '<div class="source-line"><span>定位</span><span>' +
            esc(item.locator || "未提供") +
            "</span></div></div>";
        } else if (entry.kind === "metric") {
          body =
            '<div class="detail-body"><div class="stat"><div class="stat-value">' +
            esc(item.value || "暂无值") +
            '</div><div class="stat-label">' +
            esc(item.period || "周期待确认") +
            "</div></div><p>" +
            esc(item.comparison || "状态待验证") +
            "</p></div>";
        } else if (entry.kind === "project") {
          body =
            '<div class="detail-body"><p>' +
            esc(item.summary || "暂无项目摘要") +
            "</p></div>";
        } else if (entry.kind === "signal") {
          body =
            '<div class="detail-body"><p>' +
            esc(item.detail || "暂无详情") +
            "</p></div>";
        } else if (entry.kind === "decision") {
          body =
            '<div class="detail-body"><p>' +
            esc(item.recommendation || "尚未形成推荐") +
            "</p>" +
            ((item.options || []).length
              ? "<h3>可选方案</h3><ol>" +
                item.options
                  .map((option) => "<li>" + esc(option) + "</li>")
                  .join("") +
                "</ol>"
              : "") +
            ((item.risks || []).length
              ? "<h3>主要风险</h3><ul>" +
                item.risks
                  .map((risk) => "<li>" + esc(risk) + "</li>")
                  .join("") +
                "</ul>"
              : "") +
            "</div>";
        }
        const source = sourceLabel(entry);
        $("detail-panel").innerHTML =
          '<div class="detail-top"><div class="detail-top-copy"><div class="detail-kicker">' +
          kindIcon(entry) +
          badge(typeNames[entry.type] || typeNames[entry.kind], "blue") +
          "<span>" +
          esc(source) +
          '</span></div><h2 class="detail-title">' +
          esc(entry.title) +
          '</h2><p class="detail-subtitle">' +
          esc(formatDate(entryDate(entry))) +
          (entry.kind === "recording"
            ? " · " + esc(recordingStatusLabel(item.status))
            : "") +
          '</p></div><div class="detail-tools"><button class="icon-btn ' +
          (p.starred ? "starred" : "") +
          '" type="button" data-star-entry="' +
          esc(entry.key) +
          '" aria-label="' +
          (p.starred ? "取消收藏" : "收藏") +
          '" title="' +
          (p.starred ? "取消收藏" : "收藏") +
          '">' +
          (p.starred ? icon("starFill") : icon("star")) +
          '</button>' +
          (entry.kind === "news" ||
          entry.kind === "recording" ||
          (entry.kind === "evidence" && item.source_type === "obsidian")
            ? '<button class="icon-btn" type="button" data-add-entry-focus="' +
              esc(entry.key) +
              '" aria-label="加入焦点" title="加入焦点">' +
              icon("focus") +
              "</button>"
            : "") +
          '<button class="icon-btn" type="button" data-close-detail aria-label="关闭详情" title="关闭详情">' +
          icon("close") +
          "</button></div></div>" +
          body +
          (entry.kind === "news"
            ? '<div class="detail-tools-row"><a class="text-btn" href="' +
              esc(item.url) +
              '" target="_blank" rel="noopener noreferrer">打开原文 ' +
              icon("external") +
              '</a><button class="text-btn" type="button" data-dismiss-news="' +
              esc(item.id) +
              '">' +
              icon("eyeOff") +
              " 不感兴趣</button></div>"
            : entry.kind === "action"
            ? '<div class="detail-tools-row"><button class="text-btn" type="button" data-delete-action="' +
              esc(item.id) +
              '">' +
              icon("trash") +
              " 删除待办</button></div>"
            : entry.kind === "signal"
            ? '<div class="detail-tools-row"><button class="text-btn" type="button" data-dismiss-signal="' +
              esc(item.id) +
              '">' +
              icon("eyeOff") +
              " 不感兴趣</button></div>"
            : "");
        bindDetailEvents();
      }
      function bindDetailEvents() {
        const panel = $("detail-panel");
        panel.querySelectorAll("[data-close-detail]").forEach((node) =>
          node.addEventListener("click", () => {
            state.selected = null;
            document.body.classList.remove("detail-open");
            render();
          })
        );
        panel.querySelectorAll("[data-star-entry]").forEach((node) =>
          node.addEventListener(
            "click",
            () =>
              void itemMutation(
                node.dataset.starEntry,
                "starred",
                !pref({
                  key: node.dataset.starEntry,
                  kind: node.dataset.starEntry.startsWith("recording:")
                    ? "recording"
                    : undefined,
                  id: node.dataset.starEntry.slice(10),
                }).starred
              )
          )
        );
        panel.querySelectorAll("[data-complete-action]").forEach((node) =>
          node.addEventListener("change", () =>
            mutate("./api/actions", "PATCH", {
              id: node.dataset.completeAction,
              completed: node.checked,
            })
          )
        );
        panel.querySelectorAll("[data-delete-action]").forEach((node) =>
          node.addEventListener("click", () =>
            mutate("./api/actions", "DELETE", {
              id: node.dataset.deleteAction,
            })
          )
        );
        panel
          .querySelectorAll("[data-dismiss-news]")
          .forEach((node) =>
            node.addEventListener("click", () =>
              mutate("./api/news", "DELETE", { id: node.dataset.dismissNews })
            )
          );
        panel.querySelectorAll("[data-dismiss-signal]").forEach((node) =>
          node.addEventListener("click", () =>
            mutate("./api/signals", "DELETE", {
              id: node.dataset.dismissSignal,
            })
          )
        );
        panel.querySelectorAll("[data-transcript-start]").forEach((node) =>
          node.addEventListener("click", () => {
            const audio = $("recording-audio");
            if (audio) {
              audio.currentTime = Number(node.dataset.transcriptStart) || 0;
              void audio.play().catch(() => {});
            }
          })
        );
        const audio = $("recording-audio");
        if (audio) {
          const id = state.selected.slice(10),
            p = pref({ kind: "recording", id }),
            restore = () => {
              if (
                Number.isFinite(audio.duration) &&
                audio.duration > 0 &&
                p.playback_seconds > 0
              )
                audio.currentTime = Math.min(
                  p.playback_seconds,
                  Math.max(0, audio.duration - 0.1)
                );
            };
          audio.addEventListener("loadedmetadata", restore, { once: true });
          let last = 0;
          const save = () => {
            const now = Date.now();
            if (now - last < 900) return;
            last = now;
            void request("./api/recordings/preferences", {
              method: "PATCH",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({
                recording_id: id,
                playback_seconds: audio.currentTime,
              }),
            }).catch((error) => showError(error.message || "播放进度没有保存"));
          };
          audio.addEventListener("timeupdate", save);
          audio.addEventListener("pause", save);
          audio.addEventListener(
            "ended",
            () =>
              void recordingMutation(id, {
                read: true,
                playback_seconds: audio.currentTime,
              })
          );
        }
      }
      function renderViewSwitch(view) {
        state.view = view;
        try {
          localStorage.setItem("plaud.reader.view", String(view));
        } catch {}
        state.selected = null;
        document.body.classList.remove("detail-open");
        if (String(view).startsWith("source:")) {
          // Source navigation is a local projection change. Keep the source
          // directory, hidden modules, and overview shell mounted; the
          // parity layer owns the scoped timeline/detail request.
          renderNav();
          renderTimeline();
          if (state.selected) renderDetail();
          else renderOverview();
          updateSourceBar();
          return;
        }
        render();
      }
      function render() {
        renderNav();
        renderSources();
        renderHidden();
        renderTimeline();
        // Focus V3 owns the today/decision/action/signal projections. The
        // legacy overview renderer must not repaint the detail panel after
        // renderTimeline has mounted the Focus workspace.
        const focusV3View = ["today", "decision", "action", "signal"].includes(
          state.view
        );
        if (focusV3View) {
          // The external Focus renderer loads after this inline bootstrap. Do
          // not expose the retired overview while that script or its data is
          // still loading; it will mount the real workspace into the shell.
          const panel = $("detail-panel");
          if (panel) panel.replaceChildren();
        } else if (state.selected) renderDetail();
        else renderOverview();
        updateSourceBar();
        if (!$("discover-modal").hidden) renderCatalog();
      }
      async function refresh(options = {}) {
        if (state.busy) return;
        state.busy = true;
        $("refresh").disabled = true;
        try {
          // Dashboard is the first paint contract. Start optional metadata in
          // parallel, but render as soon as the dashboard response settles so
          // a slow preferences/sources request cannot leave a blank reader.
          const dashboardPromise = request(
            "./api/dashboard?include_news=0&include_sources=0&include_counts=0"
          );
          const optionalPromises = [
            request("./api/items"),
            request("./api/reader"),
            request("./api/sources?include_counts=0"),
            request("./api/recordings/preferences"),
          ];
          const dashboardSettled = await Promise.allSettled([dashboardPromise]);
          const dashboardResult = dashboardSettled[0];
          if (dashboardResult.status === "fulfilled") { state.dashboardResolved = true; state.dashboardError = ""; }
          else state.dashboardError = dashboardResult.reason?.message || "内容暂时读取失败";
          const dashboardPayload =
            dashboardResult.status === "fulfilled"
              ? responseBody(dashboardResult.value)
              : state.dashboard || {
                  source: {
                    status: "unavailable",
                    message: "部分内容暂时未载入",
                    inputs: [],
                  },
                  evidence: [],
                  actions: [],
                  metrics: [],
                  projects: [],
                  signals: [],
                  news: [],
                  recordings: [],
                  decisions: [],
                };
          const dashboard = dashboardPayload || state.dashboard || {};
          // The first/background dashboard request is intentionally
          // lightweight (news_included=false).  Keep the existing typed
          // reader window in memory while that shell response refreshes
          // actions/focus metadata; replacing it with news:[] makes an open
          // article disappear before the independent reader request settles.
          const previousDashboard = state.dashboard;
          state.dashboard =
            dashboard.news_included === false && previousDashboard
              ? {
                  ...previousDashboard,
                  ...dashboard,
                  news: Array.isArray(previousDashboard.news)
                    ? previousDashboard.news
                    : [],
                }
              : dashboard;
          applyReaderWidths();
          if (!options.deferRender) render();
          const optionalResults = await Promise.allSettled(optionalPromises);
          const [prefsResult, readerResult, sourcesResult, recordingPrefsResult] = optionalResults;
          const results = [dashboardResult, ...optionalResults];
          state.prefsLoaded = prefsResult.status === "fulfilled" &&
            Array.isArray(responseBody(prefsResult.value)?.items);
          if (state.prefsLoaded)
            state.prefs = new Map(
              (responseBody(prefsResult.value)?.items || []).map((item) => [item.key, item])
            );
          if (sourcesResult.status === "fulfilled") {
            state.sourcesResolved = true;
            const currentSources = Array.isArray(state.sources)
              ? state.sources
              : [];
            const sourcesPayload = responseBody(sourcesResult.value);
            const nextSources = Array.isArray(sourcesPayload?.sources)
              ? sourcesPayload.sources
              : [];
            // The source directory is eventually consistent during a fresh
            // install.  A transient empty/malformed response must not erase a
            // directory that was already rendered; the bounded warmup below
            // will reconcile it on a later poll.  An actually empty directory
            // is still represented as [] when there is no prior projection.
            if (nextSources.length || !currentSources.length)
              state.sources = window.__plaudMergeSourceCatalogCounts(
                currentSources,
                nextSources,
                false
              );
          }
          if (recordingPrefsResult.status === "fulfilled")
            state.recordingPrefs = new Map(
              (responseBody(recordingPrefsResult.value)?.items || []).map((item) => [
                item.recording_id,
                item,
              ])
            );
          if (
            readerResult.status === "fulfilled" &&
            readerResult.value?.sidebar_width
          )
            state.reader = readerResult.value;
          // Counts are an independent read model. Refresh them after the
          // lightweight source directory settles without making the first
          // paint wait for another round-trip.
          void window.__plaudRefreshReaderCounts?.();
          applyReaderWidths();
          // A background refresh must not blank an open detail while the
          // selected item is still present in the new snapshot.
          // The parity reader merges the bounded dashboard snapshot with its
          // cursor pages after this request. Do not clear an open detail before
          // that merge; doing so makes an automatic refresh jump to overview.
          if (
            state.selected &&
            !options.deferRender &&
            dashboard.news_included !== false &&
            !makeEntries(dashboard).some((e) => e.key === state.selected)
          )
            state.selected = null;
          if (!options.deferRender) render();
          const failedCount = results.filter(
            (result) => result.status === "rejected"
          ).length;
          if (failedCount)
            showError(
              failedCount === results.length
                ? "内容载入超时，请点击刷新重试"
                : "部分内容暂时未载入，可继续浏览或点击刷新重试"
            );
        } catch (error) {
          showError(error.message || "内容暂时读不出来");
        } finally {
          state.busy = false;
          $("refresh").disabled = state.manualSyncing;
        }
      }
      function openDiscover() {
        const modal = $("discover-modal");
        modal.hidden = false;
        const error = $("discover-error");
        if (error) error.textContent = "";
        renderCatalog();
        const content = $("parity-content");
        if (content) content.scrollTop = 0;
        $("feed-url")?.focus();
      }
      function closeDiscover() {
        $("discover-modal").hidden = true;
        const input = $("feed-url"),
          error = $("discover-error");
        if (input) input.value = "";
        if (error) error.textContent = "";
      }
      async function toggleSubscription(sourceID) {
        const handler = window.__plaudToggleReaderSource;
        if (typeof handler !== "function") return;
        return handler(sourceID);
      }
      async function requestSourceSync(sourceID) {
        const handler = window.__plaudSyncReaderSource;
        if (typeof handler !== "function") return;
        return handler(sourceID);
      }
      function submitDiscover(event) {
        event.preventDefault();
        return;
      }
      async function requestDashboardRefresh() {
        if (state.manualSyncing) return;
        state.manualSyncing = true;
        $("refresh").disabled = true;
        const previous = String(state.dashboard?.generated_at || "");
        try {
          const result = await request("./_zettlab/automation/run", {
            method: "POST",
          });
          showToast(
            result?.state === "already_running" ? "同步正在进行" : "已开始同步"
          );
          for (let attempt = 0; attempt < 12; attempt += 1) {
            await new Promise((resolve) => window.setTimeout(resolve, 5000));
            const dashboard = await request("./api/dashboard");
            if (String(dashboard?.generated_at || "") !== previous) {
              // The parity refresh owns pagination/detail preservation and
              // performs the single render after the new snapshot arrives.
              await refresh(true);
              showToast("看板已更新");
              return;
            }
          }
          showToast("同步仍在进行，稍后会自动刷新");
        } catch (error) {
          showError(error.message || "同步没有启动");
        } finally {
          state.manualSyncing = false;
          $("refresh").disabled = false;
        }
      }
      window.requestDashboardRefresh = requestDashboardRefresh;
      function setup() {
        bindTimelineEvents();
        document.querySelectorAll("[data-view]").forEach((button) =>
          button.addEventListener("click", () => {
            renderViewSwitch(button.dataset.view);
          })
        );
        $("source-list").addEventListener("click", (event) => {
          const note = event.target.closest("[data-select-entry]");
          if (note) {
            event.preventDefault();
            event.stopPropagation();
            selectEntry(note.dataset.selectEntry);
            return;
          }
          const obsidianToggle = event.target.closest(
            "[data-toggle-obsidian-folder]"
          );
          if (obsidianToggle) {
            event.preventDefault();
            event.stopPropagation();
            window.__toggleReaderObsidianFolder?.(
              obsidianToggle.dataset.toggleObsidianFolder || ""
            );
            return;
          }
          const groupToggle = event.target.closest("[data-toggle-source-group]");
          if (groupToggle) {
            event.preventDefault();
            event.stopPropagation();
            window.__toggleReaderSourceGroup?.(groupToggle.dataset.toggleSourceGroup || "订阅");
            return;
          }
          const button = event.target.closest("[data-view]");
          if (button) {
            // Source rows are rendered dynamically, so they reach this
            // delegated handler instead of the initial [data-view] binding.
            renderViewSwitch(button.dataset.view);
          }
        });
        $("source-catalog").addEventListener("click", (event) => {
          const subscribe = event.target.closest("[data-subscribe-source]");
          if (subscribe) {
            void toggleSubscription(subscribe.dataset.subscribeSource);
            return;
          }
          const sync = event.target.closest("[data-sync-source]");
          if (sync) void requestSourceSync(sync.dataset.syncSource);
        });
        $("hidden-list").addEventListener("click", (event) => {
          const button = event.target.closest("[data-restore-module]");
          if (button)
            void mutate("./api/modules", "PATCH", {
              id: button.dataset.restoreModule,
              hidden: false,
            });
        });
        $("search").addEventListener("input", (event) => {
          state.query = event.target.value;
          renderNav();
          renderTimeline();
        });
        $("clear-filter").addEventListener("click", () => {
          $("search").value = "";
          state.query = "";
          render();
        });
        $("refresh").addEventListener("click", requestDashboardRefresh);
        $("discover").addEventListener("click", openDiscover);
        $("sidebar-add").addEventListener("click", openDiscover);
        $("close-discover").addEventListener("click", closeDiscover);
        $("cancel-discover").addEventListener("click", closeDiscover);
        $("discover-modal").addEventListener("click", (event) => {
          if (event.target === $("discover-modal")) closeDiscover();
        });
        $("discover-form").addEventListener("submit", submitDiscover);
        $("sidebar-splitter").addEventListener("pointerdown", (event) =>
          startColumnResize(event, "sidebar")
        );
        $("timeline-splitter").addEventListener("pointerdown", (event) =>
          startColumnResize(event, "timeline")
        );
        $("sidebar-splitter").addEventListener("keydown", (event) => {
          if (event.key === "ArrowLeft") changeColumn("sidebar", -1);
          if (event.key === "ArrowRight") changeColumn("sidebar", 1);
        });
        $("timeline-splitter").addEventListener("keydown", (event) => {
          if (event.key === "ArrowLeft") changeColumn("timeline", -1);
          if (event.key === "ArrowRight") changeColumn("timeline", 1);
        });
        window.addEventListener("pointermove", moveColumnResize, {
          passive: false,
        });
        window.addEventListener("pointerup", finishColumnResize);
        window.addEventListener("pointercancel", finishColumnResize);
        window.addEventListener("blur", finishColumnResize);
      }
      function selectEntry(key) {
        const previousEntry =
          state.selected &&
          makeEntries(state.dashboard || {}).find(
            (entry) => entry.key === state.selected
          );
        if (previousEntry) window.__cacheReaderDetailEntry?.(previousEntry);
        state.selected = key;
        document.body.classList.add("detail-open");
        const entry = makeEntries(state.dashboard || {}).find(
          (item) => item.key === key
        );
        if (!entry) {
          render();
          return;
        }
        window.__cacheReaderDetailEntry?.(entry);
        if (entry.kind === "recording") {
          const id = entry.id,
            p = pref(entry);
          if (!entry.item?.live_device && !p.read) {
            void recordingMutation(id, { read: true });
          }
          render();
          if (entry.item?.live_device && !state.recordingDetails.has(id)) {
            void window.__plaudLiveMeetingDetail?.(id)
              .then((detail) => {
                if (detail) state.recordingDetails.set(id, detail);
                if (state.selected === key) render();
              })
              .catch((error) => showError(error.message || "设备会议详情暂时不可用"));
          } else if (!entry.item?.live_device && !state.recordingDetails.has(id)) {
            void request("./api/recordings/detail?id=" + encodeURIComponent(id))
              .then((result) => {
                state.recordingDetails.set(id, result.recording || {});
                if (state.selected === key) render();
              })
              .catch((error) =>
                showError(error.message || "录音详情暂时不可用")
              );
          }
          return;
        }
        if (entry.kind === "evidence" && entry.item?.live_device) {
          render();
          if (!entry.item.content && !entry.item.asset_url) {
            void window.__plaudLiveObsidianDetail?.(entry.item.locator)
              .then(() => {
                if (state.selected === key) render();
              })
              .catch((error) => showError(error.message || "Obsidian 笔记暂时不可用"));
          }
          return;
        }
        if (!pref(entry).read) {
          void itemMutation(key, "read", true);
        } else render();
      }
      if (normalized) {
        setup();
        // Do not let a fast dashboard response paint the legacy projection
        // before the V3 extension has installed its renderers.
        document.addEventListener("DOMContentLoaded", () => {
          if (window.__plaudParityReady) void refresh();
        }, { once: true });
        window.setInterval(() => {
          if (!document.hidden) void refresh();
        }, 60000);
        // Source warmup is polled through the small catalog response. Do not
        // reload the dashboard, preferences and device projections every three
        // seconds while the feed worker is still draining its bounded queue.
        let warmupPolls = 0;
        let warmupSourceFingerprint = "";
        let warmupInFlight = false;
        let warmupSourceCatalogReady =
          Array.isArray(state.sources) && state.sources.length > 0;
        const warmupTimer = window.setInterval(async () => {
          // A page may be opened in a background tab while the desktop shell
          // is restoring its last route.  Hidden must pause the bounded
          // warmup, not permanently cancel it; otherwise returning to the
          // app leaves a first-install directory empty until a manual reload.
          if (document.hidden) return;
          if (warmupPolls++ >= 12) {
            window.clearInterval(warmupTimer);
            return;
          }
          // The interval is intentionally conservative, but a cold bridge can
          // still take longer than one tick. Do not overlap directory reads;
          // an older response arriving after a newer reconciliation can
          // otherwise put the sidebar back into its pre-subscription state.
          if (warmupInFlight) return;
          warmupInFlight = true;
          const sources = await request("./api/sources?include_counts=0")
            .catch(() => null)
            .finally(() => {
              warmupInFlight = false;
            });
          if (!sources) return;
          const sourcesPayload = responseBody(sources);
          const nextSources = Array.isArray(sourcesPayload?.sources) ? sourcesPayload.sources : [];
          const currentSources = Array.isArray(state.sources) ? state.sources : [];
          const catalogResponseReady = nextSources.length > 0;
          if (catalogResponseReady) warmupSourceCatalogReady = true;
          const nextFingerprint = nextSources
            .map((source) => [
              source.id,
              source.status,
              source.entry_count,
              source.unread_count,
              source.last_sync_at,
              source.subscribed ? 1 : 0,
              source.collection_mode,
              source.gateway_status,
              source.hidden ? 1 : 0,
              source.folder_id,
              source.pinned ? 1 : 0,
              source.sort_order,
            ].join(":"))
            .join("|");
          // Never replace a known directory with a transient empty response.
          // The source gateway may still be materializing subscriptions on a
          // first install; keeping the previous projection avoids a blank
          // sidebar while the next bounded poll retries the directory.
          if (nextFingerprint !== warmupSourceFingerprint &&
              (catalogResponseReady || !currentSources.length)) {
            warmupSourceFingerprint = nextFingerprint;
            state.sources = window.__plaudMergeSourceCatalogCounts(
              currentSources,
              nextSources,
              false
            );
            renderSources();
            renderNav();
            updateSourceBar();
            // A persisted source view may have rendered before the catalog
            // request recovered.  Re-run only the timeline projection when
            // that source becomes known; a full render would reset the
            // surrounding reader shell and scroll state.
            if (state.view.startsWith("source:")) renderTimeline();
          }
          // Keep unread/total badges current through the dedicated counts
          // endpoint; the source catalog intentionally carries zero
          // placeholders on this lightweight polling path.
          void window.__plaudRefreshReaderCounts?.();
          const sourceNeedsWarmup = (source) => {
            const mode = String(source.collection_mode || "").toLowerCase();
            if (!["source_gateway", "app", "hybrid"].includes(mode)) return false;
            // Gateway catalog rows can be visible before their first head is
            // ready or before the subscription projection has reached this
            // app. Keep the bounded warmup alive while that row is
            // initializing, even if `subscribed` has not appeared yet.
            const gatewayStatus = String(source.gateway_status || "").toLowerCase();
            if (mode === "source_gateway" &&
                ["", "initializing"].includes(gatewayStatus) &&
                Number(source.entry_count || 0) <= 0) return true;
            if (!source.subscribed) return false;
            if (!source.last_sync_at) return true;
            const nextSync = Date.parse(String(source.next_sync_at || ""));
            return Number.isFinite(nextSync) && nextSync <= Date.now();
          };
          const warmupSources = catalogResponseReady ? nextSources : currentSources;
          const pending =
            !warmupSourceCatalogReady ||
            !catalogResponseReady ||
            warmupSources.some(sourceNeedsWarmup);
          // An empty catalog is never a successful warmup result.  Keep
          // polling (bounded by warmupPolls) so source rows that appear after
          // subscription reconciliation can hydrate without a manual reload.
          if (catalogResponseReady && !pending) {
            // A manual/background dashboard refresh may still own the shared
            // busy flag when the source catalog becomes ready.  Do not stop
            // the warmup (and silently drop the follow-up dashboard refresh)
            // in that window; leave the bounded timer alive and let the next
            // tick retry after the in-flight refresh releases the flag.
            if (state.busy) return;
            window.clearInterval(warmupTimer);
            await refresh();
          }
        }, 5000);
      }
