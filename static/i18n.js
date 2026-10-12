(() => {
  const dictionaries = {
    "zh-CN": {
      dashboard: "焦点空间",
      focusSpace: "焦点空间",
      focusSubtitle: "目标、证据、行动与决策",
      todayFocus: "今日焦点",
      currentFocus: "当前焦点",
      importantChanges: "重要变化",
      goal: "目标",
      decisionQuestion: "关键问题",
      timeHorizon: "时间范围",
      supportingEvidence: "支持证据",
      counterEvidence: "反向证据",
      pendingConclusions: "待确认结论",
      nextStep: "下一步",
      knowledgeBase: "知识库",
      addToKnowledge: "沉淀到知识库",
      removeFromKnowledge: "移出知识库",
      favorites: "收藏",
      readLater: "稍后读",
      library: "资料库",
      allContent: "全部内容",
      subscribedArticles: "订阅文章",
      videos: "视频",
      meetingsAndRecordings: "会议与录音",
      loading: "正在读取内容",
      noMatch: "没有匹配内容",
      retry: "重试",
      operationFailed: "操作暂时没有完成",
      sourceUnavailable: "来源暂不可用",
      updatedAt: "更新于 {value}",
      sourceAvailableCount: "共 {count} 条可浏览内容",
      sourceAvailableCountDegraded: "共 {count} 条可浏览内容，部分能力降级",
      gatewayPreparing: "云端来源准备中",
      subscriptionWaiting: "订阅已保留，等待来源恢复",
      savedContent: "已保存的内容",
      knowledgeOnly: "仅显示已明确加入的内容",
      pinnedSources: "置顶",
      subscriptions: "订阅",
      aiVideo: "AI 视频",
      builtInSources: "内置来源",
    },
    en: {
      dashboard: "Focus Space",
      focusSpace: "Focus Space",
      focusSubtitle: "Goals, evidence, actions and decisions",
      todayFocus: "Today's focus",
      currentFocus: "Current focus",
      importantChanges: "Important changes",
      goal: "Goal",
      decisionQuestion: "Decision question",
      timeHorizon: "Time horizon",
      supportingEvidence: "Supporting evidence",
      counterEvidence: "Counter-evidence",
      pendingConclusions: "Unconfirmed conclusions",
      nextStep: "Next step",
      knowledgeBase: "Knowledge base",
      addToKnowledge: "Save to knowledge base",
      removeFromKnowledge: "Remove from knowledge base",
      favorites: "Favorites",
      readLater: "Read later",
      library: "Library",
      allContent: "All content",
      subscribedArticles: "Subscribed articles",
      videos: "Videos",
      meetingsAndRecordings: "Meetings and recordings",
      loading: "Loading content",
      noMatch: "No matching content",
      retry: "Retry",
      operationFailed: "The operation did not complete",
      sourceUnavailable: "Source unavailable",
      updatedAt: "Updated {value}",
      sourceAvailableCount: "{count} items available",
      sourceAvailableCountDegraded: "{count} items available; some features are degraded",
      gatewayPreparing: "Preparing cloud source",
      subscriptionWaiting: "Subscription saved; waiting for the source to recover",
      savedContent: "Saved content",
      knowledgeOnly: "Only explicitly saved content is shown",
      pinnedSources: "Pinned",
      subscriptions: "Subscriptions",
      aiVideo: "AI video",
      builtInSources: "Built-in sources",
    },
  };
  // Exact-match translations are deliberately limited to product-owned UI
  // labels. User titles, article bodies, transcripts and generated content
  // are never translated by this layer.
  const fixedTranslations = {
    "决策工作台": "Focus Space",
    "焦点空间": "Focus Space",
    "决策、行动与证据": "Goals, evidence, actions and decisions",
    "目标、证据、行动与决策": "Goals, evidence, actions and decisions",
    "今日焦点": "Today's focus",
    "当前焦点": "Current focus",
    "重要变化": "Important changes",
    "目标": "Goal",
    "关键问题": "Decision question",
    "时间范围": "Time horizon",
    "支持证据": "Supporting evidence",
    "反向证据": "Counter-evidence",
    "待确认结论": "Unconfirmed conclusions",
    "下一步": "Next step",
    "焦点名称": "Focus name",
    "检索词 / 别名": "Search terms / aliases",
    "新增焦点": "Add focus",
    "检索词 / 别名（逗号分隔）": "Search terms / aliases (comma separated)",
    "还没有设置当前焦点。先添加一个焦点，系统只会在确认后消耗分析额度。": "No current focus yet. Add one; analysis only starts after your confirmation.",
    "当前焦点设置暂时不可用": "Current focus settings are temporarily unavailable",
    "正在读取当前焦点…": "Loading current focus…",
    "当前看板暂不支持焦点设置": "Focus settings are not available on this board",
    "正在准备当前焦点推荐": "Preparing current-focus recommendations",
    "决策、行动与证据": "Decisions, actions and evidence",
    "正在读取来源": "Reading sources",
    "工作区面板": "Workspace panels",
    "收起导航": "Collapse navigation",
    "展开导航": "Expand navigation",
    "收起优先队列": "Collapse priority queue",
    "展开优先队列": "Expand priority queue",
    "添加来源": "Add source",
    "刷新内容": "Refresh content",
    "内容导航": "Content navigation",
    "导航": "Navigation",
    "工作区导航": "Workspace navigation",
    "今日总览": "Today's overview",
    "待决策": "Decisions",
    "行动": "Actions",
    "风险与信号": "Risks and signals",
    "信息库": "Library",
    "全部内容": "All content",
    "RSS 与视频": "Subscribed articles",
    "订阅文章": "Subscribed articles",
    "YouTube 视频": "YouTube videos",
    "会议与录音": "Meetings and recordings",
    "来源": "Sources",
    "快捷筛选": "Quick filters",
    "收藏": "Favorites",
    "已隐藏模块": "Hidden modules",
    "优先队列": "Priority queue",
    "概览": "Overview",
    "只显示待决策、待行动和关键风险": "Showing decisions, actions and key risks",
    "搜索内容": "Search content",
    "搜索标题、来源或摘要": "Search title, source or summary",
    "清除筛选": "Clear filter",
    "正在读取内容": "Loading content",
    "正在准备你的信息流": "Preparing your feed",
    "发现来源": "Discover sources",
    "订阅公开内容，会议录音由当前 Agent 自动维护": "Subscribe to public content; meetings are maintained by this Agent",
    "推荐来源": "Recommended sources",
    "点击加号即可订阅或退订；同步由本地 Focus Space 处理。": "Select plus to subscribe or unsubscribe; sync is handled by local Focus Space.",
    "正在读取来源目录": "Loading source catalog",
    "添加其他公开 RSS / Atom": "Add another public RSS / Atom",
    "关闭": "Close",
    "添加并首次同步": "Add and sync now",
    "自定义地址由本地 Focus Space 验证；无法由本地运行时验证时，可在 Chat 中让 Agent 处理。": "Custom addresses are verified by local Focus Space; ask the Agent in Chat when the local runtime cannot verify them.",
    "本轮引用": "This round",
    "稍后读": "Read later",
    "知识库": "Knowledge base",
    "沉淀到知识库": "Save to knowledge base",
    "移出知识库": "Remove from knowledge base",
    "正在生成": "Generating",
    "生成失败": "Generation failed",
    "按需生成": "Generate on demand",
    "准备生成": "Preparing",
    "排队中": "Queued",
    "已生成": "Ready",
    "重试生成": "Retry generation",
    "生成 AI 概览": "Generate AI overview",
    "来源摘要": "Source summary",
    "暂无摘要": "No summary",
    "没有匹配内容": "No matching content",
    "换一个筛选或等待下一次来源同步": "Try another filter or wait for the next sync",
    "暂无来源记录": "No source records",
    "暂无录音记录": "No recordings",
    "暂无需要你处理的事项": "Nothing needs your attention",
    "来源正常": "Sources healthy",
    "部分来源可用": "Some sources available",
    "来源暂不可用": "Sources unavailable",
    "操作暂时没有完成": "The operation did not complete",
    "重试": "Retry",
    "恢复": "Restore",
    "隐藏": "Hide",
    "已完成": "Completed",
    "行动草案": "Action draft",
    "草案": "Draft",
    "待验证": "Needs verification",
    "其他": "Other",
    "我的录音": "My recordings",
    "会议": "Meeting",
    "信号": "Signal",
    "决策": "Decision",
    "证据": "Evidence",
    "关键证据": "Key evidence",
    "逾期与今日行动": "Overdue and today's actions",
    "关键风险与机会": "Key risks and opportunities",
    "待我决策": "Needs my decision",
    "异常来源": "Source alerts",
    "AI 经营概览": "AI operating brief",
    "关注主题": "Followed topics",
    "有界预分析": "Bounded analysis",
    "今日推荐": "Today's recommendations",
    "待分析": "Pending analysis",
    "已分析": "Analyzed",
    "分析失败": "Analysis failed",
    "还没有启用关注主题": "No followed topics yet",
    "暂无匹配的未读内容": "No matching unread content",
    "时间待确认": "Time to be confirmed",
    "未标注时间": "No date",
    "今天": "Today",
    "正在打开文章": "Opening article",
    "正在读取来源内容": "Reading source content",
    "正在读取更多内容": "Loading more content",
    "保留当前列表，稍候补充": "Keeping the current list while loading",
    "只加载当前来源的最新内容": "Loading the latest content from this source",
    "未命名内容": "Untitled content",
    "未知来源": "Unknown source",
    "来源目录": "Source catalog",
    "设置": "Settings",
    "保存设置": "Save settings",
    "当前设备设置": "Current device settings",
    "自动处理与提醒": "Automation and notifications",
    "设备设置已保存": "Device settings saved",
    "设备设置没有保存": "Device settings were not saved",
    "全部文章订阅": "All subscribed articles",
    "添加订阅": "Add subscription",
    "浅色": "Light",
    "深色": "Dark",
    "系统": "System",
    "外观模式": "Appearance",
    "选择焦点": "Choose focus",
    "搜索焦点": "Search focuses",
    "还没有可用焦点": "No focuses available",
    "没有匹配的焦点": "No matching focus",
    "积累": "Accumulating",
    "执行": "Executing",
    "归档": "Archived",
    "持续收集真实资料": "Collecting verified materials",
    "形成并确认判断": "Forming and confirming a decision",
    "推进已确认行动": "Advancing confirmed actions",
    "保留结果与复盘": "Keeping results and review history",
    "已结束的焦点": "Archived focus",
  };
  const hashLanguage = new URLSearchParams(window.location.hash.replace(/^#/, "")).get("zl-lang");
  const raw = new URLSearchParams(window.location.search).get("lang") || hashLanguage || navigator.language || "en";
  const locale = raw.toLowerCase().startsWith("zh") ? "zh-CN" : "en";
  const dict = dictionaries[locale];
  const t = (key, vars = {}) => {
    let value = dict[key] || dictionaries.en[key] || key;
    return value.replace(/\{(\w+)\}/g, (_, name) => String(vars[name] ?? ""));
  };
  const formatDate = (value, options) => {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return String(value || "");
    return new Intl.DateTimeFormat(locale, options || { year: "numeric", month: "short", day: "numeric" }).format(date);
  };
  const translateFixed = (value) => {
    if (locale !== "en" || typeof value !== "string") return value;
    const trimmed = value.trim();
    const translated = fixedTranslations[trimmed];
    if (translated) {
      const start = value.indexOf(trimmed);
      return value.slice(0, start) + translated + value.slice(start + trimmed.length);
    }
    // Preserve dynamic values (source names, counts and dates) while
    // translating only product-owned prefixes/suffixes.
    const fragments = [
      ["已订阅 ", "Subscribed "],
      ["已退订 ", "Unsubscribed "],
      ["已添加 ", "Added "],
      ["正在同步 ", "Syncing "],
      ["预览", "Preview"],
      ["同步", "Sync"],
      ["订阅", "Subscribe"],
      ["退订", "Unsubscribe"],
      ["查看", "View"],
      ["打开", "Open"],
      ["没有匹配来源", "No matching sources"],
      ["尚未添加", "Not added yet"],
      ["正在读取", "Loading"],
      ["暂时不可用", "temporarily unavailable"],
    ];
    return fragments.reduce((current, [source, target]) => current.replaceAll(source, target), value);
  };
  const apply = (root) => {
    if (locale !== "en" || !root) return;
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    const nodes = [];
    while (walker.nextNode()) nodes.push(walker.currentNode);
    for (const node of nodes) {
      if (node.parentElement?.closest("script,style,pre,code,.entry-title,.entry-snippet,.fulltext-rich,.transcript,.x-entry-text,.x-detail-post,.article-ai-overview-summary,.article-ai-overview-points,.morning-brief-summary,.recording-summary")) continue;
      const translated = translateFixed(node.nodeValue || "");
      if (translated !== node.nodeValue) node.nodeValue = translated;
    }
    root.querySelectorAll?.("[aria-label],[title],[placeholder]").forEach((element) => {
      for (const attribute of ["aria-label", "title", "placeholder"]) {
        if (element.hasAttribute(attribute)) {
          const current = element.getAttribute(attribute);
          const translated = translateFixed(current || "");
          if (translated !== current) element.setAttribute(attribute, translated);
        }
      }
    });
  };
  document.documentElement.lang = locale;
  document.title = dict.dashboard;
  window.plaudI18n = { locale, t, formatDate, apply, translateFixed, dictionaries: Object.keys(dictionaries) };
  if (locale === "en") {
    const observe = () => {
      apply(document.body);
      const observer = new MutationObserver((records) => {
        for (const record of records) {
          for (const node of record.addedNodes) {
            if (node.nodeType === Node.ELEMENT_NODE) apply(node);
            else if (node.nodeType === Node.TEXT_NODE) {
              const translated = translateFixed(node.nodeValue || "");
              if (translated !== node.nodeValue) node.nodeValue = translated;
            }
          }
        }
      });
      observer.observe(document.body, { childList: true, subtree: true });
    };
    if (document.body) observe();
    else document.addEventListener("DOMContentLoaded", observe, { once: true });
  }
})();
