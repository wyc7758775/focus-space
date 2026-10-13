(() => {
  const t = (key, vars) => window.plaudI18n?.t(key, vars) || key;
  const formatDateByLocale = (value, options) =>
    window.plaudI18n?.formatDate(value, options) || String(value || "");
  // This is a local-first app surface. It shares the app database with the
  // Agent and exposes only display-safe scheduling candidates to a trusted host.
  window.__plaudStandaloneParity = true;
  const parity = {
    tab: "discover",
    mode: "recommended",
    category: "",
    language: "",
    contentType: "all",
    query: "",
    input: "",
    inputMode: "standard",
    packs: [],
    folders: [],
    tags: [],
    discoveries: [],
    syncRequests: [],
    discoveryNotice: "",
    syncMonitors: new Map(),
    sourceHistoryLoading: new Set(),
    settings: null,
    settingsResolved: false,
    readerFocus: null,
    readerFocusLoading: false,
    readerFocusBusy: false,
    readerFocusMessage: "",
    selectedSources: new Set(),
    managerRenameSource: "",
    managerSortMode: "manual",
    folderDeleteArmed: false,
    folderDeleteTarget: "",
    folderDeleteArmedAt: 0,
    sourceDisplayPrefs: new Map(),
    packBusy: new Set(),
    catalogOrigin: "",
    catalogPolicy: "",
    trendPolicy: "",
    popularityAvailable: false,
    sourcePreview: null,
    sourceBusy: new Set(),
    modalReturnFocus: null,
    pendingCatalogLoad: false,
    contextMemberships: new Map(),
    contextMembershipsReady: false,
    contextMutationKeys: new Set(),
    fullText: new Map(),
    fullTextLoading: new Set(),
    fullTextAttempted: new Set(),
    fullTextHydrating: new Set(),
    articleOverviewLoading: new Set(),
    articleOverviewAttempted: new Set(),
    articleOverviewPollTimers: new Map(),
    articleOverviewErrors: new Map(),
    loading: false,
    progressTimer: 0,
    restoredKey: "",
    entryCursor: "",
    entryPagesRead: 0,
    entryHasMore: false,
    entryLoading: false,
    entryPageSize: 50,
    entryBaseNews: null,
    // Keep the current aggregate reader window while a source-scoped or
    // Focus projection temporarily owns dashboard.news.  The snapshot is
    // bounded by boundReaderNews and is replaced on every fresh dashboard
    // reset, so navigation never loses the user's loaded page state.
    entryAggregateSnapshot: null,
    // Typed aggregate views (all/rss/video/youtube) are fetched lazily on
    // first navigation. Keep one bounded page snapshot per view so returning
    // to a tab does not issue a duplicate request or reuse another view's
    // rows.
    entryViewSnapshots: new Map(),
    // A snapshot is only reusable after its typed request has completed.  The
    // dashboard reset path also creates an aggregate snapshot, but that
    // snapshot is merely a fallback and must not suppress the first lazy load
    // (including a valid, empty response).
    entryViewLoaded: new Set(),
    entryViewErrors: new Map(),
    entryViewLoading: "",
    // Keep each aggregate tab's server-reported total independent.  In
    // particular, a YouTube-filtered page must never overwrite the broader
    // video total, and an RSS page must never overwrite the all-entries total.
    entryViewTotals: new Map(),
    // 每个聚合视图允许一次"冷启动唤醒竞态"自动重试；真实故障仍落在
    // 显式重试按钮上，不叠加第二套机制。
    entryViewAutoRetries: new Map(),
    entryScope: "",
    entryScopeError: "",
    entryScopeLoading: false,
    entryScopeSnapshots: new Map(),
    detailLoading: false,
    detailCache: new Map(),
    detailCacheLimit: 4,
    entryRequestToken: 0,
    // Incremented at the capture phase for every reader navigation.  A
    // setTimeout used to defer the typed aggregate request can otherwise run
    // after two rapid tab changes and hydrate the wrong view.
    navigationToken: 0,
    entryLoadingOwnerToken: null,
    entryLoadingView: "",
    entryPageError: "",
    entryObserver: null,
    xFeedObserver: null,
    xFeedHydrationActive: 0,
    xFeedHydrationQueue: [],
    xFeedHydrationQueued: new Set(),
    xFeedAvatarEntryID: "",
    autoSelectEntry: false,
    videoInitialRequestStarted: false,
    sourceIconObserver: null,
    host: {
      trusted: false,
      native: false,
      source: null,
      origin: "",
      capabilities: new Set(),
      slug: "",
    },
    feedbackBusy: new Set(),
    decisionRuns: new Map(),
    decisionRunLoading: new Set(),
    decisionRunLoaded: new Set(),
    decisionRunBusy: new Set(),
    decisionRunTurnBusy: new Set(),
    decisionRunErrors: new Map(),
    decisionRunMessages: new Map(),
    decisionRunDrafts: new Map(),
    focusSpaces: [],
    focusPolicy: null,
    focusAnalyses: new Map(),
    focusAnalysisSnapshots: new Map(),
    focusAnalysisRunSelections: new Map(),
    focusWorkflowStates: new Map(),
    focusLoading: false,
    focusLoaded: false,
    focusViewMode: "board",
    actionViewMode: "board",
    actionFocusFilter: "all",
    selectedActionID: "",
    selectedFocusID: "",
    focusPanel: "overview",
    focusBusy: new Set(),
    focusPickerTrigger: null,
    focusPickerCleanup: null,
    productV3NavigationMarkup: "",
    productV3NavigationScrollTop: 0,
    obsidianDirectoryScrollTops: new Map(),
    decisionRunPollTimers: new Map(),
    decisionHistories: new Map(),
    decisionHistoryViews: new Map(),
    decisionHistoryBusy: new Set(),
    scheduleBusy: new Set(),
    pendingSchedules: new Map(),
    capabilityProbeTimer: 0,
    capabilityProbeAttempt: 0,
    liveMeetingPrefs: new Map(),
    counts: null,
    countsLoading: false,
    countsStale: false,
    deviceCounts: {},
    deviceCountsLoading: null,
    obsidianCounts: new Map(),
    deviceListError: "",
  };
  const liveMeetingPreferenceStorageKey = "plaud-live-meeting-preferences-v1";
  const liveMeetingPreferenceLimit = 100;
  const detailLoadingKey = "__reader_detail_loading__";
  // The device-side retention job currently keeps at most 500 ordinary news
  // rows. Keep that window plus a separate protected window while keyset
  // pagination advances independently, so a long reading session cannot grow
  // the dashboard state or timeline DOM without bound.
  const readerNewsStateLimit = 500;
  // User-owned saved/context rows are exempt from the device's ordinary
  // retention slice. Keep a second, explicit window for those rows so the
  // protected views do not silently disappear while the total projection
  // remains bounded at 1,000 entries (the existing makeEntries display cap).
  const readerProtectedNewsLimit = 500;
  const readerNewsTotalLimit = readerNewsStateLimit + readerProtectedNewsLimit;
  const contextMutationLimit = 500;
  // Maintained history sources share one bounded device-side slot. Keep the
  // monitor alive through one queued source plus the active source timeout while
  // backing off reads so a slow upstream cannot create a polling storm.
  const sourceSyncMonitorMaxWaitMs = 180000;
  const sourceSyncMonitorMaxPollMs = 5000;
  // The device cursor contract caps offsets at 10,000 (50 rows per page).
  // Keep the client from asking for the first invalid cursor after that cap.
  const readerEntryPageLimit = 200;
  // Source-scoped reads may perform one bounded gateway history pull after the
  // local SQLite projection misses. Keep the longer budget local to that
  // first-page request; aggregate tabs, counts, and metadata retain their
  // shorter/default budgets.
  const sourceEntryRequestTimeoutMs = 12000;

  function loadLiveMeetingPreferences() {
    try {
      const saved = JSON.parse(
        window.localStorage.getItem(liveMeetingPreferenceStorageKey) || "[]"
      );
      if (!Array.isArray(saved)) return;
      saved.slice(0, liveMeetingPreferenceLimit).forEach((item) => {
        const id = text(item?.recording_id);
        if (!id) return;
        parity.liveMeetingPrefs.set(id, {
          recording_id: id,
          read: Boolean(item.read),
          starred: Boolean(item.starred),
          playback_seconds: Math.max(0, Number(item.playback_seconds) || 0),
          updated_at: Math.max(0, Number(item.updated_at) || 0),
        });
      });
    } catch {
      parity.liveMeetingPrefs.clear();
    }
  }

  function persistLiveMeetingPreferences() {
    try {
      const saved = Array.from(parity.liveMeetingPrefs.values())
        .sort((left, right) => right.updated_at - left.updated_at)
        .slice(0, liveMeetingPreferenceLimit);
      parity.liveMeetingPrefs = new Map(
        saved.map((item) => [item.recording_id, item])
      );
      window.localStorage.setItem(
        liveMeetingPreferenceStorageKey,
        JSON.stringify(saved)
      );
    } catch {
      // Private browsing or host policy may disable storage. The bounded
      // in-memory preference map remains valid for the current session.
    }
  }

  function liveMeetingEntry(id) {
    const meetingID = text(id);
    if (!meetingID) return null;
    return (
      makeEntries(state.dashboard || {}).find(
        (entry) =>
          entry.kind === "recording" &&
          text(entry.id) === meetingID &&
          entry.item?.live_device
      ) || null
    );
  }

  function liveMeetingPreference(id) {
    const meetingID = text(id);
    return (
      parity.liveMeetingPrefs.get(meetingID) || {
        recording_id: meetingID,
        read: false,
        starred: false,
        playback_seconds: 0,
        updated_at: 0,
      }
    );
  }

  function storeLiveMeetingPreference(id, payload) {
    const meetingID = text(id);
    if (!meetingID) return liveMeetingPreference(meetingID);
    const next = {
      ...liveMeetingPreference(meetingID),
      ...payload,
      recording_id: meetingID,
      updated_at: Date.now(),
    };
    next.playback_seconds = Math.max(
      0,
      Number(next.playback_seconds) || 0
    );
    parity.liveMeetingPrefs.set(meetingID, next);
    persistLiveMeetingPreferences();
    return next;
  }

  loadLiveMeetingPreferences();

  const basePref = pref;
  pref = function prefWithLiveDeviceMeeting(entry) {
    if (entry?.kind === "recording") {
      const id = text(entry.id);
      if (entry.item?.live_device || liveMeetingEntry(id))
        return liveMeetingPreference(id);
    }
    return basePref(entry);
  };

  const baseRecordingMutation = recordingMutation;
  recordingMutation = async function mutateLiveDeviceMeeting(id, payload) {
    if (!liveMeetingEntry(id)) return baseRecordingMutation(id, payload);
    storeLiveMeetingPreference(id, payload);
    render();
  };

  function entryRemotePageRemaining(totalEntries) {
    const total = Number(totalEntries);
    // Older hosts may omit total_entries; retain the cursor/page hard cap as
    // the fallback in that case.
    return (
      !Number.isFinite(total) ||
      total > parity.entryPagesRead * parity.entryPageSize
    );
  }

  function sortReaderNews(items) {
    return items.sort((left, right) => {
      const leftTime = Date.parse(left.published_at || "") || 0;
      const rightTime = Date.parse(right.published_at || "") || 0;
      const leftUpdated = Date.parse(
        left.updated_at || left.fetched_at || ""
      ) || 0;
      const rightUpdated = Date.parse(
        right.updated_at || right.fetched_at || ""
      ) || 0;
      return (
        rightTime - leftTime ||
        rightUpdated - leftUpdated ||
        text(right.id).localeCompare(text(left.id), "en")
      );
    });
  }

  function readerNewsIsProtected(item) {
    const id = text(item?.id || item?.item?.id);
    if (!id) return false;
    const key = text(item?.key, `news:${id}`);
    const preference = state.prefs?.get(key);
    return Boolean(
      preference?.starred ||
      preference?.read_later ||
      parity.contextMutationKeys.has(key) ||
      (parity.contextMembershipsReady && contextIsActive(key))
    );
  }

  function preserveSelectedReaderNews(sorted, bounded) {
    const selectedKey = text(state.selected);
    const selectedID = selectedKey.startsWith("news:")
      ? selectedKey.slice("news:".length)
      : "";
    if (!selectedID || bounded.some((item) => item.id === selectedID))
      return bounded;
    const selected = sorted.find((item) => item.id === selectedID);
    if (!selected) return bounded;
    let replacement = -1;
    if (readerNewsIsProtected(selected)) {
      // A protected row outside the window implies that the protected window
      // is full; replace another protected row, never an ordinary row.
      for (let index = bounded.length - 1; index >= 0; index -= 1) {
        if (readerNewsIsProtected(bounded[index])) {
          replacement = index;
          break;
        }
      }
    } else {
      for (let index = bounded.length - 1; index >= 0; index -= 1) {
        if (!readerNewsIsProtected(bounded[index])) {
          replacement = index;
          break;
        }
      }
    }
    const index = replacement >= 0 ? replacement : bounded.length - 1;
    bounded[index] = selected;
    return bounded;
  }

  function boundReaderNews(items) {
    const sorted = sortReaderNews(items);
    const protectedItems = sorted.filter(readerNewsIsProtected);
    const ordinaryItems = sorted.filter((item) => !readerNewsIsProtected(item));
    if (!parity.contextMembershipsReady) {
      if (
        sorted.length <= readerNewsTotalLimit &&
        protectedItems.length <= readerProtectedNewsLimit
      )
        return sorted;
      const protectedWindow = protectedItems.slice(0, readerProtectedNewsLimit);
      const ordinaryCapacity = Math.max(
        0,
        readerNewsTotalLimit - protectedWindow.length
      );
      return sortReaderNews(
        preserveSelectedReaderNews(sorted, [
          ...ordinaryItems.slice(0, ordinaryCapacity),
          ...protectedWindow,
        ])
      );
    }
    if (
      ordinaryItems.length <= readerNewsStateLimit &&
      protectedItems.length <= readerProtectedNewsLimit
    )
      return sorted;
    const bounded = [
      ...ordinaryItems.slice(0, readerNewsStateLimit),
      ...protectedItems.slice(0, readerProtectedNewsLimit),
    ];
    // A refresh or an older page may evict the currently opened article. Keep
    // that one display-safe row in the bounded window so detail does not jump
    // to the overview; the four-entry detail cache remains the heavy-content
    // fallback for rows that are not in the projection.
    return sortReaderNews(preserveSelectedReaderNews(sorted, bounded));
  }

  function readerNewsWindowStatus(items) {
    const list = Array.isArray(items) ? items : [];
    const protectedCount = list.filter(readerNewsIsProtected).length;
    const ordinaryCount = list.length - protectedCount;
    return {
      ordinaryCount,
      protectedCount,
      ordinaryFull: ordinaryCount >= readerNewsStateLimit,
      protectedFull: protectedCount >= readerProtectedNewsLimit,
      windowsFull:
        ordinaryCount >= readerNewsStateLimit &&
        protectedCount >= readerProtectedNewsLimit,
    };
  }

  function rebindReaderNewsProjection() {
    if (state.dashboard?.news)
      state.dashboard.news = boundReaderNews(state.dashboard.news);
    if (Array.isArray(parity.entryBaseNews))
      parity.entryBaseNews = boundReaderNews(parity.entryBaseNews);
    // Context membership can arrive after the first page. If the ordinary
    // window had filled before that response, resume the authoritative cursor
    // while the protected window still has capacity; otherwise old protected
    // rows would remain hidden behind the first 500 ordinary rows.
    if (parity.entryCursor && parity.entryCursor !== "0" && state.dashboard?.news) {
      const status = readerNewsWindowStatus(state.dashboard.news);
      if (
        !status.windowsFull &&
        parity.entryPagesRead < readerEntryPageLimit &&
        entryRemotePageRemaining(entryTotalForView(state.view))
      )
        parity.entryHasMore = true;
    }
    if (!parity.entryScope) rememberAggregateEntrySnapshot();
  }

  // The official app favicon is embedded so an unavailable third-party icon
  // always degrades to the Zettlab brand without another network request.
  const projectLogoDataURL =
    "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAADAAAAAwCAYAAABXAvmHAAAMOElEQVR42s2aXWwUV5bHf7dufXT7G7BjYzmTGBMc7AABYqNkCQus" +
    "iOBlhBRp2KAwEQ/2JFE0y+RhB2nzsJpF+7JPLJF4yEOySImUKA+RiJRJ5iFEIhKBSBGYNsYG/AU2drc/ut1f9b0PuEp2u7vtJLsj" +
    "WirJt+pW1f+ce875/88ti2eeecbnMfj5vl9yXHht6U99nMGXA/5YGFAIUFEUfN/H87w136s+Lh4XQpBMJgGorq4uacQKox8X8KZp" +
    "8h9/+Quvvvoqc3NzCCHK3uv7Pr7v/31XoBCAEAIpJel0mo6ODnp6ewHYtWsX7733HlVVVXieVzZH1L8ncCFEePi+j2VZeJ6HZVmc" +
    "PHkSgFQqxa1bt8J8KFaNlp77fw8hIQSqqqIoCo7jsLCwQDweZ25ujsrKSjZv3oyUkkQiwb179+jt7eWjjz6ioqJiWR6Uqkzi/5IH" +
    "Cj1s2zb5fB7TNJFSsmHDBrZs2cLu3bvZs2cP27Zto7GxkS8vXqSnt5fm5maGh4cxDANN09A0bVUu+FUGBGCD5bYsi3w+j2VZKwB3" +
    "d3ezfft2mpqaij7r4MGDXL9+nVcOHeLE73+P67q89dZboRGlDFB/DWDbtsnlcliWhSolG+rr2bFjRwh427ZtKwD7vo/ruuGzPM9D" +
    "SsmuXbu4cuUKh48cYf/+/dy5cwfDMHAcB0VRSuZW2RUoC1hVqa+vX+bh5557rizgAMjSEuk4Dqqq8umnn9Lb20tjYyOGYeB5Hul0" +
    "umg5XRORKYqC67pks9kQcENDA88//zy7d++mq6urKGDXdcMSGYBW1dILHRi1a9cu1q1bh2ma5HI5AKSUq/KIWsrzuVyOiooKXnrp" +
    "pRBwR0cHGzduDOd5nkc+n0cIgWEY+L6PlDKsHqW8V/guz/Noa2ujoaGBiYmJ8FnlBF5JHghY8amnnuLDDz+kra0tnGxZFgsLC3ie" +
    "h6IooXdzuRz/ffYsl777jp6eHo4ePYpt2yiKEq5EKYNc10VVVb7++mvGxsaorKxcISOKJXDJEAqY8eDBg7S1tTE1NYWiKNTU1GCa" +
    "5iOrVTUUXaqqMjY2xr/++c8AVFZWcvTo0WWrsNSIpYYE8X/jxg16enrQNG1VGb2qFvI8D13X6e/vx7btENTHH3/MoUOHeOedd8J5" +
    "APl8ntbWVnbs2IGUkv7+fpLJJIqihDLA87zwCEIj8PzIyAivvfYalmWFoVMqXIppIaWYhdFolFgsxsTEBKqqoqoqn332GVevXuWL" +
    "L75gfHwcVVVxXRfLsqiurqarqwvf9xkZGWF4eBgpJa7rLgMdGOE4DlJKpqenOX78OFNTU1RXV4c59HOanaIroGkaiUSCGzduhMv6" +
    "yiuvoCgKmUyGWCyGpmkhQICXX34Zz/PIZDJcuXIlrGIB6ODvoKSmUil6enoYHBxkw4YNZLNZkskkMzMz4ZxSXi8ZQktFl+u6/PDD" +
    "DyiKQjabpaurC0VRkFJy7dq1cA5AJpNh7969dHZ2sn//frZv345lWaHXA/CO4yCEwLZt3nzzTS5evAjAzMwMdXV1HDp0iBMnThCJ" +
    "RLBtO8yVUhVpWRVaOiHIg59++ol0Oo3nebS3t9PQ0MDk5CSXL1/Gsqxwfj6fp76+nu+//55oNIrneWSz2WVMK6VE13VmZmY4ffo0" +
    "ly5d4tixY+zcuZPu7m46OztpaGgA4PPPP6enp4fa2tqyFcn3fcTmzZv9YhNc18UwDC5cuEBLSws1NTW8//77/Pjjjxw/fpwDBw5g" +
    "mSZisVQurS4BWCEElmWRSCQYHx/n8uXLaJrGjh076Orqor6+fkVJDST1iy++SCqVCiteqUZfLSVXNU1jbm6Ovr4+WltbmZ2d5e23" +
    "30bTNGzbDmle8f1lbGuaJtPT09y7d49YLMb169e5d/cusf5+2tvb+fLLL0P2DvIjYOxAstTV1bF161YuXbpETU1NUVkdMnG5uut5" +
    "Hjdu3ODw4cN4nkcikUAIgaZpGIYRenh6eprh4WFu3rxJX18ft2/f5uHDh2SzWTRNw3EcNre1heCDKhSALkZs3d3dfPPNN8vyoKQa" +
    "LUUYmqYRi8VwHCcE7DgOMzMzjIyMEIvF6OvrY3BwkMnJSXK5HIqiYBgGuq5TWVkZ6qj/uXCBpqYmTNNcFhaF7ByM9+zZg6ZpRVvK" +
    "kitQ6P1oNMrAwAAffPABL7zwQgh4aGioKOBoNLqM5CzLIpfL8cknn7B9+3ZSqVQYgktlRuEBPGp2nniCZEEerJDTmzZt8svJVd/3" +
    "yWQySCkxTTMUboZhhMtfzEuKojA7O8u5c+c4ceIEiUQCTdPCeF8KuHAMoKoqR48e5dtvv6W2tjZM8EJ8ymq7B0IIampqiEajrF+/" +
    "nrq6upDyXdcN5fPS2iylJB6Pc/r0aV5//XUmJibwfR/HcXAcB9u2sW17xTg4Z5omAN3d3cv4oBgnqGuhbMdxloVGOaM1TWNqaoqT" +
    "J0/y7rvvMjY+jiplSGLFPF54LnjP0jwoW0Z/6cZq4XVVVYnH4xw5fJgzZ87w4MGDR9cLGpxy4aMoCvg+2UyGzs5OGhsbSSaTYZku" +
    "xKaU2zQqB77wulRV5ufn2blzJ2fPniWRSJDP5/E8r2y4FB37PgvZLE/U19Px7LPkcrlQGxW+WykEtBrwYqsipSSTTtPc3Mz58+fJ" +
    "5fOkUqmwCSoGtCR410UsLGCn0whV5YW9ezHzedQiG12rEtlaZK2iKJimSSQS4fz580QiEaanpzEMI0zAtcS9EAIFcCIR8pcusfHq" +
    "VdwDB/jH5mb+q6aGmUyGGl3HLdYTr2UfvtSmbLA1eO7cOVpaWhgbG1umJtcS96GUAFzT5GYqRUMshjIwwJ76er777W/5z1iML4eG" +
    "qNX1R+8PVr+2tvbff+mXE0VRmJ+f58yZM+zbt487d+6g6/qKDmy1cSi7PQ9NUZixbSr6+qi1bfKZDC2pFMfa26loauJvw8OoS+SH" +
    "ulbgheeC/cxTp05x5MgRbt26hWEYWJa15mqzgoUBxfeJ1NUxG43yxOQkZiRC3nVRp6b4U1cXwjD4t6++ojYSwSu2K7GWRjool/98" +
    "7BhvvPEGsVgMVVWX1fqy4IVYJsODsZQSXwiq163Dqqoil05jeR6K7+MmkzwcHeWP+/bx/b17fHX7NjWRSHkiK1Z3pZTMzs6yf/9+" +
    "/uXUKfr7+4tun5fVOsGGl5RITQMpcR2Hhfl5kg8e0H/zJu3DwzztOOTSaRRAOg5oGubTT/OHPXv4ZnAQfzU1Wqzi5HI52tvb+dOp" +
    "U9y9e5d8Ph+yZSngAWApJZqUqELgWxbJVIrkxASzQ0PEBwZIDQ3hjY8z7Dhs7eggn81iBiuWy6EIQTwWo3PTJlrr6hhdWFipRldj" +
    "Zc/zeO13vyOdTpNKpdB1fVnFCY9FD6tSIgEvn2c+leLh5CTDIyMM3bnD0Pg4+uQke5NJGoANwB2geutWWoVgNp1G6DoCEL6PdF3c" +
    "kRE2SEmTrnO3MAfWYowQgrl4nHRjY1hCxZIYVhUFCbj5PPMLC0xPTTEyOsrd4WFG798nPjNDblHV6pqGEo3yt0iERtvG8jxqf/Mb" +
    "3qmvJ3XtGqYQSMd5ZAAgMhm0eBytqgrpefhCrK6FCkPIdhxisRibt2zBATRACoFnWSTTaeLT04zfv8/w6CjjDx6QmJsjvwg4+GhR" +
    "axjg+3iLOmlBCMYUhX9qbua92loS164xbdthuRRBfnkeMpmE2VnmLQspxNpDKAgfQ9e5OTjI83fvUlVTQyIe58HEBGP373N/cpLZ" +
    "+XnMxVXRNQ1VVanR9RBwIMOX9Q6+T6WiEH/4kMGBASwh0KREBN8RAiMAPZtlanSUcVXFUBTEk08+6a+WyIX9gWXbRA2DiGEwOz+P" +
    "tdhhaYu7eEs/0K3lo3WgVjOOw/FMhg7PI7tYYcSiYBOALwR1nsdfdZ2/VlVR6fvFW8pyn/p930fXNCzbJmea6LpOxDDwAd/z8BY9" +
    "vBZ5Ej4z8K6UXNQ0ZDZLrefB4jajsnhEfZ8fpOS7igoigAeIlpYW/5f0AeW2+34O+GU5JgQ510XL5/kHQE+nqViULJYQPFBVYpWV" +
    "oGkovg9BEv+cf7D4Nc3Pah86AmLqevZZqjMZxk2TmXye2WyWLOBqGtFF0ecHLP5r+oC1Nj9r/dmOw8b161mfy3F7YICKdJqdTU08" +
    "19qKqutUKcqjXFhyz/8CitF3UyhRRosAAAAASUVORK5CYII=";

  // YouTube channel portraits are resolved by the app-owned endpoint first.
  // Provider branding was previously pulled from a third-party icon host
  // (the Folo icon service). Besides the licensing boundary (no Folo-hosted
  // assets), a remote host is an availability dependency for a pure fallback
  // tier. YouTube channel portraits come from the app-owned resolver first;
  // when that misses, the offline-safe project mark below is the fallback.
  const youtubeBrandIconURL = "";

  // Public-client taxonomy from the pinned parity inventory. The application
  // owns its catalog data; these stable category keys do not call Folo APIs.
  const discoverCategories = [
    "all",
    "social-media",
    "new-media",
    "traditional-media",
    "blog",
    "ai",
    "programming",
    "design",
    "live",
    "multimedia",
    "picture",
    "anime",
    "program-update",
    "shopping",
    "game",
    "reading",
    "journal",
    "finance",
    "other",
  ];
  const categoryNames = {
    all: "全部",
    "social-media": "社交媒体",
    "new-media": "新媒体",
    "traditional-media": "传统媒体",
    bbs: "社区论坛",
    blog: "博客",
    programming: "编程",
    design: "设计",
    live: "直播",
    multimedia: "多媒体",
    picture: "图片",
    anime: "动漫",
    "program-update": "软件更新",
    shopping: "购物",
    game: "游戏",
    reading: "阅读",
    journal: "期刊",
    finance: "财经",
    other: "其他",
    // Compatibility labels for an older local catalog revision.
    ai: "AI",
    technology: "科技",
    productivity: "效率",
    world: "世界",
    science: "科学",
  };

  const contentTypeNames = {
    all: "全部",
    article: "文章",
    social: "社交媒体",
    picture: "图片",
    video: "视频",
    audio: "音频",
    notification: "通知",
  };

  const languageNames = {
    "zh-cn": "简体中文",
    "zh-tw": "繁体中文",
    en: "English",
    ja: "日本語",
    ko: "한국어",
    und: "未标注",
  };

  const baseRenderNav = renderNav;
  const baseRenderEntry = renderEntry;
  const baseRenderTimeline = renderTimeline;
  const baseRenderDetail = renderDetail;
  const baseRenderOverview = renderOverview;
  const baseFilteredEntries = filteredEntries;
  const baseEntryCounts = entryCounts;
  const baseApplyReaderWidths = applyReaderWidths;
  const baseUpdateSourceBar = updateSourceBar;
  const baseRefresh = refresh;

  state.reader.view_mode = state.reader.view_mode || "comfortable";

  const panelCollapseState = {
    sidebar: readPanelCollapse("sidebar"),
    timeline: readPanelCollapse("timeline"),
  };

  function readPanelCollapse(panel) {
    try {
      return localStorage.getItem(`memo.reader.${panel}.collapsed`) === "1";
    } catch {
      return false;
    }
  }

  function panelCanCollapse() {
    return window.matchMedia("(min-width: 901px)").matches;
  }

  function currentPanelCollapsed(panel) {
    return panelCanCollapse() && panelCollapseState[panel] === true;
  }

  function enforcePanelCollapseWidths() {
    const shell = $("reader-shell");
    if (!shell || !panelCanCollapse()) return;
    if (currentPanelCollapsed("sidebar"))
      shell.style.setProperty("--sidebar-width", "0px");
    if (currentPanelCollapsed("timeline"))
      shell.style.setProperty("--timeline-width", "0px");
  }

  function applyPanelCollapseState() {
    const shell = $("reader-shell");
    if (!shell) return;
    const desktop = panelCanCollapse();
    for (const [panel, label] of [
      ["sidebar", "来源导航"],
      ["timeline", "优先队列"],
    ]) {
      const collapsed = desktop && panelCollapseState[panel] === true;
      shell.classList.toggle(`${panel}-collapsed`, collapsed);
      const handle = $(`${panel}-collapse-handle`);
      if (handle) {
        const action = collapsed ? "展开" : "收起";
        handle.setAttribute("aria-expanded", String(!collapsed));
        handle.setAttribute("aria-label", `${action}${label}`);
        handle.title = `${action}${label}`;
      }
      const splitter = $(`${panel}-splitter`);
      if (splitter) splitter.setAttribute("aria-disabled", String(collapsed));
    }
    baseApplyReaderWidths();
    enforcePanelCollapseWidths();
  }

  function installPanelCollapseControls() {
    for (const [panel, label] of [
      ["sidebar", "来源导航"],
      ["timeline", "优先队列"],
    ]) {
      const handle = $(`${panel}-collapse-handle`);
      if (handle && handle.dataset.collapseBound !== "1") {
        handle.dataset.collapseBound = "1";
        handle.addEventListener("click", (event) => {
          event.preventDefault();
          event.stopPropagation();
          panelCollapseState[panel] = !currentPanelCollapsed(panel);
          try {
            localStorage.setItem(
              `memo.reader.${panel}.collapsed`,
              panelCollapseState[panel] ? "1" : "0"
            );
          } catch {
            // Keep the session state when browser storage is unavailable.
          }
          applyPanelCollapseState();
        });
      }
    }
    applyPanelCollapseState();
    window.addEventListener("resize", applyPanelCollapseState, {
      passive: true,
    });
  }

  function jsonRequest(path, method, payload, timeoutMs = 0) {
    const controller = timeoutMs > 0 && window.AbortController ? new AbortController() : null;
    const timer = controller ? window.setTimeout(() => controller.abort(), timeoutMs) : 0;
    return request(path, {
      method,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
      ...(timeoutMs > 0 ? { timeoutMs } : {}),
      ...(controller ? { signal: controller.signal } : {}),
    }).catch((error) => {
      if (error?.name === "AbortError") throw new Error("概览请求超时，请稍后重试");
      throw error;
    }).finally(() => {
      if (timer) window.clearTimeout(timer);
    });
  }

  // The schedule bridge is intentionally narrow. The page can display and
  // edit a candidate, but only a trusted App/Web host may receive it and
  // construct the actual Cron request from its current Agent route.
  const scheduleBridgeSlug = "plaud-action-dashboard";
  const scheduleBridgeCapability = "schedule_candidate_v1";
  const bridgeRequestType = "host_capabilities_request_v1";
  const bridgeCapabilitiesType = "host_capabilities_v1";
  const bridgeCandidateType = "schedule_candidate_v1";
  const bridgeReleaseType = "schedule_candidate_release_v1";
  const bridgeReceiptType = "schedule_receipt_v1";
  const bridgeKey = "memo.reader.pending-schedules.v1";
  const bridgeCapabilityRetryDelays = [1000, 2000, 4000, 8000, 12000, 12000];

  function uuidV4() {
    if (window.crypto?.randomUUID) return window.crypto.randomUUID();
    const bytes = new Uint8Array(16);
    if (window.crypto?.getRandomValues) window.crypto.getRandomValues(bytes);
    else
      for (let index = 0; index < bytes.length; index += 1)
        bytes[index] = Math.floor(Math.random() * 256);
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    const hex = [...bytes]
      .map((value) => value.toString(16).padStart(2, "0"))
      .join("");
    return (
      hex.slice(0, 8) +
      "-" +
      hex.slice(8, 12) +
      "-" +
      hex.slice(12, 16) +
      "-" +
      hex.slice(16, 20) +
      "-" +
      hex.slice(20)
    );
  }

  function nativeHostAvailable() {
    return Boolean(
      window.ReactNativeWebView &&
        typeof window.ReactNativeWebView.postMessage === "function"
    );
  }

  function bridgeMessage(raw) {
    if (raw && typeof raw === "object") return raw;
    if (typeof raw !== "string" || raw.length > 8192) return null;
    try {
      const value = JSON.parse(raw);
      return value && typeof value === "object" ? value : null;
    } catch {
      return null;
    }
  }

  function bridgeEventTrusted(event) {
    if (parity.host.native) {
      return (
        event.source === window ||
        event.source === null ||
        event.source === undefined
      );
    }
    if (window.parent === window || event.source !== window.parent)
      return false;
    if (!event.origin || event.origin === "null") return false;
    try {
      const origin = new URL(event.origin);
      if (!/^https?:$/.test(origin.protocol)) return false;
    } catch {
      return false;
    }
    return !parity.host.origin || parity.host.origin === event.origin;
  }

  function bridgePost(message, handshake = false) {
    if (parity.host.native && nativeHostAvailable()) {
      window.ReactNativeWebView.postMessage(JSON.stringify(message));
      return true;
    }
    if (parity.host.trusted && parity.host.source && parity.host.origin) {
      parity.host.source.postMessage(message, parity.host.origin);
      return true;
    }
    // A wildcard is permitted only for the capability handshake. Candidate
    // and receipt messages always use the origin captured from the handshake.
    if (handshake && window.parent !== window) {
      window.parent.postMessage(message, "*");
      return true;
    }
    return false;
  }

  function validBridgeKeys(value, allowed) {
    return (
      value &&
      typeof value === "object" &&
      !Object.keys(value).some((key) => !allowed.has(key))
    );
  }

  function acceptHostCapabilities(value, event) {
    const allowed = new Set(["type", "capabilities"]);
    if (
      !validBridgeKeys(value, allowed) ||
      value.type !== bridgeCapabilitiesType ||
      !Array.isArray(value.capabilities) ||
      value.capabilities.length > 16 ||
      value.capabilities.some(
        (item) => typeof item !== "string" || item.length > 128
      )
    )
      return false;
    if (!bridgeEventTrusted(event)) return false;
    stopHostCapabilityProbe();
    parity.host.trusted = true;
    parity.host.slug = scheduleBridgeSlug;
    parity.host.capabilities = new Set(value.capabilities);
    if (!parity.host.native) {
      parity.host.source = event.source;
      parity.host.origin = event.origin;
    }
    renderDetail();
    void resendPendingScheduleCandidates();
    return true;
  }

  function scheduleCapabilityAvailable() {
    return (
      parity.host.trusted &&
      parity.host.capabilities.has(scheduleBridgeCapability)
    );
  }

  function stopHostCapabilityProbe() {
    if (parity.capabilityProbeTimer)
      window.clearTimeout(parity.capabilityProbeTimer);
    parity.capabilityProbeTimer = 0;
  }

  function requestHostCapabilities() {
    if (parity.host.trusted || document.visibilityState === "hidden") {
      stopHostCapabilityProbe();
      return;
    }
    parity.host.native = nativeHostAvailable();
    // A standalone browser page has no trusted host. Do not keep probing or
    // expose scheduling merely because the dashboard itself is reachable.
    if (!parity.host.native && window.parent === window) {
      stopHostCapabilityProbe();
      return;
    }
    bridgePost(
      { type: bridgeRequestType, capabilities: [scheduleBridgeCapability] },
      true
    );
    if (parity.capabilityProbeAttempt >= bridgeCapabilityRetryDelays.length)
      return;
    const delay = bridgeCapabilityRetryDelays[parity.capabilityProbeAttempt++];
    parity.capabilityProbeTimer = window.setTimeout(() => {
      parity.capabilityProbeTimer = 0;
      requestHostCapabilities();
    }, delay);
  }

  function restartHostCapabilityProbe() {
    if (parity.host.trusted) return;
    stopHostCapabilityProbe();
    parity.capabilityProbeAttempt = 0;
    requestHostCapabilities();
  }

  function onScheduleBridgeVisibilityChange() {
    if (document.visibilityState === "hidden") stopHostCapabilityProbe();
    else restartHostCapabilityProbe();
  }

  function pendingStorageRead() {
    try {
      const value = JSON.parse(localStorage.getItem(bridgeKey) || "{}");
      return value && typeof value === "object" ? value : {};
    } catch {
      return {};
    }
  }

  function pendingStorageWrite(value) {
    try {
      const bounded = Object.fromEntries(Object.entries(value).slice(-100));
      localStorage.setItem(bridgeKey, JSON.stringify(bounded));
    } catch {
      // The dashboard sidecar remains the source of truth when storage is off.
    }
  }

  function rememberPendingSchedule(action, pending) {
    if (!action?.id || !pending?.request_id || !pending?.candidate_id) return;
    const value = {
      action_id: text(action.id).slice(0, 128),
      revision: Number(pending.revision) || Number(action.revision) || 1,
      request_id: text(pending.request_id).slice(0, 128),
      candidate_id: text(pending.candidate_id).slice(0, 128),
      client_request_id: text(pending.client_request_id).slice(0, 128),
      server_pending: pending.server_pending === true,
    };
    parity.pendingSchedules.set(value.action_id, value);
    const stored = pendingStorageRead();
    stored[value.action_id] = value;
    pendingStorageWrite(stored);
  }

  function restorePendingSchedules() {
    const stored = pendingStorageRead();
    const actions = Array.isArray(state.dashboard?.actions)
      ? state.dashboard.actions
      : [];
    const restored = {};
    parity.pendingSchedules.clear();
    for (const action of actions) {
      if (!action?.id || text(action.schedule_state) !== "pending") continue;
      const local = stored[action.id] || {};
      const requestID = text(action.schedule_request_id || local.request_id);
      const candidateID = text(action.id);
      const clientID = text(
        action.client_request_id || local.client_request_id
      );
      if (!requestID || !candidateID) continue;
      const pending = {
        action_id: text(action.id).slice(0, 128),
        revision: Number(action.revision) || Number(local.revision) || 1,
        request_id: requestID,
        candidate_id: candidateID,
        client_request_id: clientID,
        server_pending: true,
      };
      parity.pendingSchedules.set(pending.action_id, pending);
      restored[pending.action_id] = pending;
    }
    pendingStorageWrite(restored);
  }

  function actionByCandidate(candidateID) {
    const value = text(candidateID);
    if (!value) return null;
    const pending = [...parity.pendingSchedules.values()].find(
      (item) => item.candidate_id === value
    );
    if (pending)
      return (
        (state.dashboard?.actions || []).find(
          (item) => item.id === pending.action_id
        ) || null
      );
    return (
      (state.dashboard?.actions || []).find(
        (item) => item.schedule_candidate_id === value
      ) || null
    );
  }

  function validScheduleReceipt(value) {
    const allowed = new Set([
      "type",
      "request_id",
      "candidate_id",
      "revision",
      "status",
      "client_request_id",
      "job_id",
      "replayed",
      "next_run_at",
      "error_code",
    ]);
    if (
      !validBridgeKeys(value, allowed) ||
      value.type !== bridgeReceiptType ||
      !["scheduled", "failed"].includes(value.status)
    )
      return false;
    if (
      !Number.isInteger(value.revision) ||
      value.revision < 1 ||
      !/^[A-Za-z0-9._:-]{1,128}$/.test(text(value.request_id)) ||
      !/^[A-Za-z0-9._:-]{1,128}$/.test(text(value.candidate_id)) ||
      !/^[A-Za-z0-9._:-]{1,128}$/.test(text(value.client_request_id))
    )
      return false;
    if (
      value.status === "scheduled" &&
      !/^[A-Za-z0-9._:-]{1,128}$/.test(text(value.job_id))
    )
      return false;
    if (
      value.status === "failed" &&
      !/^[A-Za-z0-9._:-]{1,64}$/.test(text(value.error_code))
    )
      return false;
    if (
      value.next_run_at !== undefined &&
      value.next_run_at !== "" &&
      (typeof value.next_run_at !== "string" ||
        Number.isNaN(Date.parse(value.next_run_at)))
    )
      return false;
    return true;
  }

  function validScheduleRelease(value) {
    const allowed = new Set(["type", "request_id", "candidate_id", "revision"]);
    if (!validBridgeKeys(value, allowed) || value.type !== bridgeReleaseType)
      return false;
    return (
      Number.isInteger(value.revision) &&
      value.revision >= 1 &&
      /^[A-Za-z0-9._:-]{1,128}$/.test(text(value.request_id)) &&
      /^[A-Za-z0-9._:-]{1,128}$/.test(text(value.candidate_id))
    );
  }

  function receiveScheduleRelease(value) {
    if (!validScheduleRelease(value)) return;
    const action = actionByCandidate(value.candidate_id);
    if (!action) return;
    const pending = parity.pendingSchedules.get(action.id);
    if (
      !pending ||
      pending.server_pending ||
      pending.request_id !== value.request_id ||
      pending.candidate_id !== value.candidate_id ||
      Number(pending.revision) !== value.revision
    )
      return;
    parity.pendingSchedules.delete(action.id);
    const stored = pendingStorageRead();
    delete stored[action.id];
    pendingStorageWrite(stored);
    renderDetail();
  }

  async function receiveScheduleReceipt(value) {
    if (!validScheduleReceipt(value)) return;
    const action = actionByCandidate(value.candidate_id);
    if (!action) return;
    const pending = parity.pendingSchedules.get(action.id);
    if (
      !pending ||
      pending.request_id !== value.request_id ||
      (pending.client_request_id &&
        pending.client_request_id !== value.client_request_id)
    )
      return;
    // The trusted host has already bound the receipt through the owner-scoped
    // host API. The generated page can only refresh and verify that state; it
    // never calls action.schedule.receipt itself.
    await refresh();
    const current = (state.dashboard?.actions || []).find(
      (item) => item.id === action.id
    );
    const applied =
      current &&
      current.schedule_state === value.status &&
      current.client_request_id === value.client_request_id &&
      (value.status !== "scheduled" || current.job_id === value.job_id);
    if (!applied) {
      showToast("定时任务回执正在同步");
      return;
    }
    parity.pendingSchedules.delete(action.id);
    const stored = pendingStorageRead();
    delete stored[action.id];
    pendingStorageWrite(stored);
    showToast(
      value.status === "scheduled"
        ? "已创建定时任务"
        : "定时任务创建失败，已保留失败状态"
    );
  }

  function onBridgeMessage(event) {
    const value = bridgeMessage(event.data);
    if (!value) return;
    if (value.type === bridgeCapabilitiesType) {
      acceptHostCapabilities(value, event);
    } else if (value.type === bridgeReleaseType && bridgeEventTrusted(event)) {
      receiveScheduleRelease(value);
    } else if (value.type === bridgeReceiptType && bridgeEventTrusted(event)) {
      void receiveScheduleReceipt(value);
    }
  }

  function installScheduleBridge() {
    parity.host.native = nativeHostAvailable();
    if (parity.host.native) {
      window.__zettlabReceiveHostMessage = (raw) =>
        onBridgeMessage({
          data: raw,
          source: window,
          origin: window.location.origin,
        });
    }
    window.addEventListener("message", onBridgeMessage);
    document.addEventListener(
      "visibilitychange",
      onScheduleBridgeVisibilityChange
    );
    window.addEventListener("pagehide", stopHostCapabilityProbe);
    window.addEventListener("pageshow", restartHostCapabilityProbe);
    restartHostCapabilityProbe();
  }

  function actionJSONRequest(path, payload, idempotencyKey) {
    return request(path, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": idempotencyKey,
      },
      body: JSON.stringify(payload),
    });
  }

  async function resendPendingScheduleCandidates() {
    if (!scheduleCapabilityAvailable()) return;
    for (const pending of parity.pendingSchedules.values()) {
      const action = (state.dashboard?.actions || []).find(
        (item) => item.id === pending.action_id
      );
      if (
        !pending.server_pending ||
        !action ||
        action.confirmation_status !== "accepted" ||
        action.schedule_state !== "pending"
      )
        continue;
      const candidate = scheduleCandidateForAction(action, pending);
      bridgePost(candidate);
    }
  }

  async function loadContextMemberships(silent = false) {
    try {
      const result = await request("./api/context?membership=1");
      parity.contextMemberships = new Map(
        (result.items || []).map((item) => [item.item_key, item])
      );
      // A successful full read is authoritative. Drop optimistic keys that
      // could otherwise keep a removed context item protected forever.
      parity.contextMutationKeys.clear();
      parity.contextMembershipsReady = true;
      // Re-run the bounded partition after the async membership read. This
      // promotes active-context rows that were present in the dashboard
      // snapshot without waiting for another refresh; future keyset pages
      // are classified synchronously against the ready map.
      rebindReaderNewsProjection();
      return true;
    } catch (error) {
      if (!silent) showError(error.message || "上下文状态暂时不可用");
      return false;
    }
  }

  function contextMembership(entryOrKey) {
    const key = typeof entryOrKey === "string" ? entryOrKey : entryOrKey?.key;
    return key ? parity.contextMemberships.get(key) || null : null;
  }

  function contextIsActive(entryOrKey) {
    const key = typeof entryOrKey === "string" ? entryOrKey : entryOrKey?.key;
    return key
      ? parity.contextMutationKeys.has(key) ||
          contextMembership(key)?.state === "active"
      : false;
  }

  function localAppMediaURL(raw, entryID = "", kind = "media") {
    const value = text(raw);
    if (!value) return "";
    try {
      const resolved = new URL(value, location.href);
      if (
        entryID &&
        resolved.origin === location.origin &&
        resolved.pathname.startsWith(
          "/agent/v1/api/source-gateway/entries/"
        )
      )
        return `./api/media?id=${encodeURIComponent(entryID)}&kind=${
          kind === "thumbnail" ? "thumbnail" : "media"
        }`;
      if (resolved.origin === location.origin) return value;
    } catch {
      return "";
    }
    if (!entryID || !publicURL(value)) return "";
    return `./api/media?id=${encodeURIComponent(entryID)}&kind=${
      kind === "thumbnail" ? "thumbnail" : "media"
    }`;
  }

  const youtubeSourceIDs = new Set([
    "youtube-google-deepmind",
    "youtube-anthropic",
    "youtube-nvidia",
    "youtube-two-minute-papers",
    "youtube-ai-explained",
    "youtube-computerphile",
    "youtube-fireship",
    "youtube-lex-fridman",
  ]);
  const youtubeEmbedTimeoutMS = 8000;

  function youtubeThumbnailProxyURL(videoID) {
    if (!/^[A-Za-z0-9_-]{11}$/.test(text(videoID))) return "";
    return `https://i.ytimg.com/vi/${text(videoID)}/hqdefault.jpg`;
  }

  function youtubeVideoID(raw) {
    try {
      const target = new URL(text(raw));
      if (target.protocol !== "https:" || target.username || target.password || target.port || target.hash)
        return "";
      const host = target.hostname.toLowerCase();
      let value = "";
      if (host === "youtu.be") value = target.pathname.slice(1);
      else if (host === "youtube.com" || host === "www.youtube.com" || host === "m.youtube.com") {
        if (target.pathname === "/watch") value = target.searchParams.get("v") || "";
        else if (target.pathname.startsWith("/shorts/")) value = target.pathname.slice(8);
        else if (target.pathname.startsWith("/live/")) value = target.pathname.slice(6);
        else if (target.pathname.startsWith("/embed/")) value = target.pathname.slice(7);
      }
      return /^[A-Za-z0-9_-]{11}$/.test(value) ? value : "";
    } catch {
      return "";
    }
  }

  function bilibiliVideoID(raw) {
    try {
      const target = new URL(text(raw));
      if (
        !["http:", "https:"].includes(target.protocol) ||
        target.username ||
        target.password ||
        target.port ||
        target.hash
      )
        return null;
      const host = target.hostname.toLowerCase();
      if (
        host !== "bilibili.com" &&
        !host.endsWith(".bilibili.com")
      )
        return null;
      const match = target.pathname.match(/\/video\/((?:BV)[A-Za-z0-9]+|av[0-9]+)/i);
      if (!match) return null;
      const value = match[1];
      return value.toLowerCase().startsWith("av")
        ? { aid: value.slice(2), bvid: "" }
        : { aid: "", bvid: value };
    } catch {
      return null;
    }
  }

  function videoProviderForItem(item) {
    const sourceID = text(item?.source_id).toLowerCase();
    const sourceType = text(item?.source_type).toLowerCase();
    const source = (state.sources || []).find(
      (candidate) => candidate.id === item?.source_id
    );
    const sourceKind = text(source?.kind).toLowerCase();
    const feedFormat = text(source?.feed_format).toLowerCase();
    let host = "";
    try {
      host = new URL(text(item?.url)).hostname.toLowerCase();
    } catch {}
    if (
      youtubeVideoID(item?.url) ||
      sourceKind === "youtube" ||
      feedFormat === "youtube" ||
      sourceType === "youtube" ||
      sourceID.startsWith("youtube-")
    )
      return "youtube";
    let path = "";
    try {
      path = new URL(text(item?.url)).pathname.toLowerCase();
    } catch {}
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
    // Video navigation is intentionally limited to the two supported video
    // providers. Social sources may carry video attachments, but they remain
    // in their native social timeline instead of entering the video gallery.
    return "";
  }

  function videoProviderForSource(source) {
    if (!source) return "";
    // X/Twitter feeds can contain video attachments, but the feed itself is
    // still a social timeline.  Treating the catalog's content_types hint as
    // a video gallery is what turns the timeline into a narrow multi-column
    // grid.  Keep the X presentation in charge and only use the gallery for
    // dedicated video sources.
    if (xSourceIsX(source)) return "";
    const contentTypes = Array.isArray(source.content_types)
      ? source.content_types.map((value) => text(value).toLowerCase())
      : [];
    const hint = [source.id, source.name, source.title_override, source.url, source.feed_url]
      .map((value) => text(value).toLowerCase())
      .join(" ");
    if (/bilibili|b23\.tv|rsshub[^\s]*bili/.test(hint)) return "bilibili";
    if (/youtube|youtu\.be/.test(hint)) return "youtube";
    return "";
  }

  function youtubeVideoForItem(item) {
    if (!item || videoProviderForItem(item) !== "youtube") return null;
    const id = youtubeVideoID(item.url);
    if (!id) return null;
    // Only use the host media proxy when the entry actually records a
    // thumbnail.  The previous unconditional fallback sent every missing
    // YouTube cover to /api/media, creating a burst of 404/503 responses and
    // visible cover flashes while opening the gallery.
    const thumbnailDirectURL = localAppMediaURL(
      item.thumbnail_url,
      item.id,
      "thumbnail"
    );
    return {
      id,
      watchURL: `https://www.youtube.com/watch?v=${id}`,
      embedURL: `https://www.youtube-nocookie.com/embed/${id}?rel=0&modestbranding=1`,
      thumbnailURL: youtubeThumbnailProxyURL(id),
      thumbnailFallbackURL: `https://img.youtube.com/vi/${id}/hqdefault.jpg`,
      thumbnailDirectURL,
    };
  }

  function safeVideoLink(raw) {
    const value = text(raw);
    if (!value || !publicURL(value)) return "";
    try {
      const target = new URL(value);
      const host = target.hostname.toLowerCase();
      if (
        host === "youtube.com" ||
        host === "www.youtube.com" ||
        host === "m.youtube.com" ||
        host === "youtu.be" ||
        host === "bilibili.com" ||
        host.endsWith(".bilibili.com") ||
        host === "b23.tv"
      )
        return target.href;
      return target.href;
    } catch {
      return "";
    }
  }

  function videoForItem(item) {
    const provider = videoProviderForItem(item);
    if (!provider) return null;
    const youtube = provider === "youtube" ? youtubeVideoForItem(item) : null;
    const thumbnail =
      youtube?.thumbnailURL ||
      localAppMediaURL(item?.thumbnail_url, item?.id, "thumbnail") ||
      (publicURL(item?.thumbnail_url) ? text(item.thumbnail_url) : "");
    const thumbnailFallback =
      youtube?.thumbnailFallbackURL || thumbnail || "";
    const thumbnailDirect =
      youtube?.thumbnailDirectURL ||
      `./api/media?id=${encodeURIComponent(text(item?.id))}&kind=thumbnail`;
    return {
      provider,
      label:
        provider === "youtube"
          ? readerFocusCopy("YouTube", "YouTube")
          : provider === "bilibili"
          ? readerFocusCopy("B 站", "Bilibili")
          : readerFocusCopy("视频", "Video"),
      thumbnailURL: thumbnail,
      thumbnailFallbackURL: thumbnailFallback,
      thumbnailDirectURL: thumbnailDirect,
      mediaURL: localAppMediaURL(item?.media_url, item?.id, "media"),
      watchURL: youtube?.watchURL || safeVideoLink(item?.url),
      videoID: youtube?.id || "",
      durationSeconds: Number(item?.duration_seconds) || 0,
    };
  }

  function closeFloatingVideoPlayer() {
    document.querySelector("[data-floating-video-player]")?.remove();
  }

  function openFloatingVideoPlayer(entry) {
    if (entry?.kind !== "news") return false;
    const item = entry.item || {};
    const video = videoForItem(item);
    if (!video) return false;

    let media = "";
    if (video.provider === "youtube" && video.videoID) {
      const embedURL = `https://www.youtube-nocookie.com/embed/${encodeURIComponent(
        video.videoID
      )}?rel=0&modestbranding=1&autoplay=1`;
      media = `<iframe src="${esc(
        embedURL
      )}" title="${esc(text(entry.title, "YouTube video"))}" loading="eager" allow="autoplay; fullscreen; picture-in-picture" allowfullscreen></iframe>`;
    } else if (video.provider === "bilibili") {
      const playerURL = bilibiliPlayerURL(item.url);
      media = playerURL
        ? `<iframe src="${esc(playerURL)}" title="${esc(text(entry.title, "Bilibili video"))}" loading="eager" allow="autoplay; fullscreen; picture-in-picture" allowfullscreen></iframe>`
        : `<div class="video-floating-unavailable" role="status"><strong>${esc(readerFocusCopy("播放器暂时不可用", "Player unavailable"))}</strong><span>${esc(readerFocusCopy("请使用下方链接在 B 站打开", "Use the link below to open it on Bilibili"))}</span></div>`;
    } else if (video.mediaURL) {
      media = `<video src="${esc(
        video.mediaURL
      )}" controls autoplay playsinline></video>`;
    }
    if (!media) return false;

    closeFloatingVideoPlayer();
    const watchURL = safeVideoLink(video.watchURL || item.url);
    const title = text(entry.title, readerFocusCopy("视频", "Video"));
    const player = document.createElement("aside");
    player.className = "video-floating-player";
    player.dataset.floatingVideoPlayer = "1";
    player.setAttribute("role", "dialog");
    player.setAttribute(
      "aria-label",
      readerFocusCopy("悬浮视频播放器", "Floating video player")
    );
    player.innerHTML = `<div class="video-floating-card"><div class="video-floating-head"><strong class="video-floating-title">${esc(
      title
    )}</strong><button class="video-floating-close" type="button" data-close-floating-video aria-label="${esc(
      readerFocusCopy("关闭播放器", "Close player")
    )}" title="${esc(readerFocusCopy("关闭", "Close"))}">${icon(
      "close"
    )}</button></div><div class="video-floating-frame">${media}</div><div class="video-floating-actions"><span class="badge blue">${esc(
      video.label
    )}</span>${
      watchURL
        ? `<a class="text-btn" href="${esc(watchURL)}" target="_blank" rel="noopener noreferrer">${esc(
            video.provider === "bilibili"
              ? readerFocusCopy("在 B 站打开", "Open on Bilibili")
              : readerFocusCopy("在 YouTube 打开", "Open on YouTube")
          )} ${icon("external")}</a>`
        : ""
    }</div></div>`;
    document.body.appendChild(player);
    player
      .querySelector("[data-close-floating-video]")
      ?.addEventListener("click", closeFloatingVideoPlayer);
    const floatingFrame = player.querySelector("iframe");
    if (floatingFrame) {
      let settled = false;
      const timeoutID = window.setTimeout(() => {
        if (settled || !floatingFrame.isConnected) return;
        settled = true;
        floatingFrame.replaceWith(
          Object.assign(document.createElement("div"), {
            className: "video-floating-unavailable",
            role: "status",
            innerHTML: `<strong>${esc(readerFocusCopy("视频暂时无法连接", "Video is temporarily unavailable"))}</strong><span>${esc(readerFocusCopy("可以使用下方链接在 B 站打开。", "Open it on Bilibili using the link below."))}</span>`,
          })
        );
      }, youtubeEmbedTimeoutMS);
      floatingFrame.addEventListener("load", () => {
        settled = true;
        window.clearTimeout(timeoutID);
      }, { once: true });
    }
    player.addEventListener("click", (event) => {
      if (event.target === player) closeFloatingVideoPlayer();
    });
    return true;
  }

  function installFloatingVideoSelection() {
    const root = document.documentElement;
    if (!root || root.dataset.floatingVideoSelectionBound === "1") return;
    root.dataset.floatingVideoSelectionBound = "1";
    document.addEventListener(
      "click",
      (event) => {
        const select = event.target.closest("[data-select-entry]");
        if (!select) return;
        const entry = makeEntries(state.dashboard || {}).find(
          (candidate) => candidate.key === select.dataset.selectEntry
        );
        if (entry?.kind !== "news" || !videoForItem(entry.item)) return;
        event.preventDefault();
        event.stopImmediatePropagation();
        state.selected = null;
        document.body.classList.remove("detail-open");
        openFloatingVideoPlayer(entry);
        if (!pref(entry).read) void markReaderEntryReadLocally(entry);
      },
      true
    );
    document.addEventListener("keydown", (event) => {
      if (
        event.key === "Escape" &&
        document.querySelector("[data-floating-video-player]")
      ) {
        event.preventDefault();
        closeFloatingVideoPlayer();
      }
    });
  }

  function readerEntryCard(entry) {
    if (!entry?.key) return null;
    return document.querySelector(
      `[data-entry-key="${CSS.escape(entry.key)}"]`
    );
  }

  async function markReaderEntryReadLocally(entry) {
    if (!entry?.key || pref(entry).read) return;
    const previous = pref(entry);
    const sourceID = text(entry.item?.source_id);
    const source = (state.sources || []).find((item) => text(item.id) === sourceID);
    const previousSourceUnread = Number(source?.unread_count);
    state.prefs.set(entry.key, { ...previous, read: true });
    readerEntryCard(entry)?.classList.add("read");
    if (source && Number.isFinite(previousSourceUnread))
      source.unread_count = Math.max(0, previousSourceUnread - 1);
    try {
      await request("./api/items", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ key: entry.key, read: true }),
      });
      await loadEntryCounts(true);
    } catch (error) {
      state.prefs.set(entry.key, previous);
      readerEntryCard(entry)?.classList.remove("read");
      if (source && Number.isFinite(previousSourceUnread))
        source.unread_count = previousSourceUnread;
      showError(error.message || "阅读状态没有保存");
    }
  }

  function selectNewsEntryLocally(entry) {
    const previousEntry =
      state.selected &&
      makeEntries(state.dashboard || {}).find(
        (candidate) => candidate.key === state.selected
      );
    if (previousEntry) window.__cacheReaderDetailEntry?.(previousEntry);
    document
      .querySelectorAll("[data-entry-key].selected")
      .forEach((node) => node.classList.remove("selected"));
    state.selected = entry.key;
    document.body.classList.add("detail-open");
    window.__cacheReaderDetailEntry?.(entry);
    readerEntryCard(entry)?.classList.add("selected");
    renderDetail();
    updateSourceBar();
    if (!pref(entry).read) void markReaderEntryReadLocally(entry);
  }

  function installLocalReaderDetailNavigation() {
    const root = document.documentElement;
    if (!root || root.dataset.localReaderDetailNavigationBound === "1") return;
    root.dataset.localReaderDetailNavigationBound = "1";
    document.addEventListener(
      "click",
      (event) => {
        const close = event.target.closest("[data-close-detail]");
        if (!close || !state.selected) return;
        event.preventDefault();
        event.stopImmediatePropagation();
        state.selected = null;
        document.body.classList.remove("detail-open");
        document
          .querySelectorAll("[data-entry-key].selected")
          .forEach((node) => node.classList.remove("selected"));
        const panel = $("detail-panel");
        if (panel) panel.dataset.detailFingerprint = "";
        renderOverview();
        updateSourceBar();
      },
      true
    );
  }

  const baseSelectEntry = selectEntry;
  selectEntry = function selectParityEntry(key) {
    const entry = makeEntries(state.dashboard || {}).find(
      (candidate) => candidate.key === key
    );
    if (
      entry?.kind === "recording" &&
      entry.item?.live_device &&
      !pref(entry).read
    ) {
      storeLiveMeetingPreference(entry.id, { read: true });
    }
    if (entry?.kind === "news" && xAccountForItem(entry.item)) {
      // X 源视图是整列 feed（详情列被 CSS 隐藏），点卡即读；
      // 其他视图点开线程详情（正文 + 引用 + 互动数据 + 回复），
      // 与普通文章共用局部更新路径，避免整树重绘。
      if (document.body.classList.contains("x-feed")) {
        if (!pref(entry).read) void markReaderEntryReadLocally(entry);
        return;
      }
      return selectNewsEntryLocally(entry);
    }
    if (entry?.kind === "news" && videoForItem(entry.item)) {
      state.selected = null;
      document.body.classList.remove("detail-open");
      openFloatingVideoPlayer(entry);
      if (!pref(entry).read) void markReaderEntryReadLocally(entry);
      return;
    }
    if (entry?.kind === "news") return selectNewsEntryLocally(entry);
    return baseSelectEntry(key);
  };

  installLocalReaderDetailNavigation();

  function videoDurationLabel(value) {
    const total = Math.max(0, Math.floor(Number(value) || 0));
    const hours = Math.floor(total / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    const seconds = total % 60;
    const pad = (part) => String(part).padStart(2, "0");
    return hours > 0
      ? `${pad(hours)}:${pad(minutes)}:${pad(seconds)}`
      : `${pad(minutes)}:${pad(seconds)}`;
  }

  function relativeVideoTime(value) {
    const timestamp = Date.parse(text(value));
    if (!timestamp) return formatDate(value);
    const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
    const unit =
      seconds < 60
        ? [seconds, readerFocusCopy("秒", "second")]
        : seconds < 3600
        ? [Math.floor(seconds / 60), readerFocusCopy("分钟", "minute")]
        : seconds < 86400
        ? [Math.floor(seconds / 3600), readerFocusCopy("小时", "hour")]
        : [Math.floor(seconds / 86400), readerFocusCopy("天", "day")];
    if (window.plaudI18n?.locale === "en")
      return `${unit[0]} ${unit[1]}${unit[0] === 1 ? "" : "s"} ago`;
    return `${unit[0]}${unit[1]}前`;
  }

  function youtubeDetailMarkup(item) {
    const video = youtubeVideoForItem(item);
    if (!video) return "";
    const title = text(item.title, readerFocusCopy("YouTube 视频", "YouTube video"));
    return `<section class="youtube-detail" aria-label="${esc(
      readerFocusCopy("YouTube 视频", "YouTube video")
    )}"><div class="youtube-detail-frame" data-youtube-detail-frame data-player-state="poster"><img class="youtube-detail-poster" src="${esc(
      video.thumbnailDirectURL
    )}" data-fallback-src="${esc(
      video.thumbnailURL
    )}" data-second-fallback-src="${esc(
      video.thumbnailFallbackURL
    )}" alt="" decoding="async"><span class="youtube-thumbnail-placeholder" aria-hidden="true">${icon(
      "video"
    )}<span>${esc(
      readerFocusCopy("视频封面加载中", "Loading video cover")
    )}</span></span><button class="youtube-detail-play" type="button" data-youtube-detail-play data-youtube-video-id="${esc(
      video.id
    )}" data-youtube-title="${esc(title)}" aria-label="${esc(
      readerFocusCopy(`播放 ${title}`, `Play ${title}`)
    )}"><span class="youtube-detail-play-icon" aria-hidden="true">${icon(
      "arrow"
    )}</span><span>${esc(
      readerFocusCopy("播放视频", "Play video")
    )}</span></button><div class="youtube-detail-status" data-youtube-detail-status role="status" aria-live="polite" hidden><strong data-youtube-detail-status-title></strong><span data-youtube-detail-status-copy></span></div></div><div class="youtube-detail-actions"><span class="badge blue">${esc(
      readerFocusCopy("YouTube 视频", "YouTube video")
    )}</span><a class="text-btn" href="${esc(
      video.watchURL
    )}" target="_blank" rel="noopener noreferrer">${esc(
      readerFocusCopy("在 YouTube 打开", "Open on YouTube")
    )} ${icon("external")}</a></div></section>`;
  }

  function showYouTubeDetailStatus(frame, title, copy) {
    const status = frame?.querySelector("[data-youtube-detail-status]");
    if (!status) return;
    const titleNode = status.querySelector("[data-youtube-detail-status-title]");
    const copyNode = status.querySelector("[data-youtube-detail-status-copy]");
    if (titleNode) titleNode.textContent = title;
    if (copyNode) copyNode.textContent = copy;
    status.hidden = false;
  }

  function playYouTubeDetailVideo(button) {
    const frame = button?.closest("[data-youtube-detail-frame]");
    const videoID = text(button?.dataset.youtubeVideoId);
    if (!frame || !/^[A-Za-z0-9_-]{11}$/.test(videoID)) return;
    if (frame.querySelector("iframe")) return;
    const iframe = document.createElement("iframe");
    iframe.src = `https://www.youtube-nocookie.com/embed/${videoID}?rel=0&modestbranding=1&autoplay=1`;
    iframe.title = text(
      button.dataset.youtubeTitle,
      readerFocusCopy("YouTube 视频", "YouTube video")
    );
    iframe.referrerPolicy = "strict-origin-when-cross-origin";
    iframe.allow =
      "accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share";
    iframe.allowFullscreen = true;
    frame.dataset.playerState = "loading";
    frame.setAttribute("aria-busy", "true");
    button.hidden = true;
    showYouTubeDetailStatus(
      frame,
      readerFocusCopy("正在连接视频", "Connecting to video"),
      readerFocusCopy(
        "连接较慢时会自动回到封面。",
        "The cover will return automatically if the connection is slow."
      )
    );
    let settled = false;
    const timeoutID = window.setTimeout(() => {
      if (settled || !frame.isConnected) return;
      settled = true;
      iframe.remove();
      frame.dataset.playerState = "fallback";
      frame.removeAttribute("aria-busy");
      button.hidden = false;
      showYouTubeDetailStatus(
        frame,
        readerFocusCopy("视频暂时无法连接", "Video is temporarily unavailable"),
        readerFocusCopy(
          "可以重试，或使用下方链接在 YouTube 打开。",
          "Retry, or use the link below to open the video on YouTube."
        )
      );
      button.focus({ preventScroll: true });
    }, youtubeEmbedTimeoutMS);
    iframe.addEventListener(
      "load",
      () => {
        if (settled || !frame.isConnected) return;
        settled = true;
        window.clearTimeout(timeoutID);
        frame.dataset.playerState = "ready";
        frame.removeAttribute("aria-busy");
        const status = frame.querySelector("[data-youtube-detail-status]");
        if (status) status.hidden = true;
        const poster = frame.querySelector(".youtube-detail-poster");
        if (poster) poster.hidden = true;
      },
      { once: true }
    );
    frame.appendChild(iframe);
  }

  function bilibiliPlayerURL(raw) {
    const video = bilibiliVideoID(raw);
    if (!video) return "";
    const params = new URLSearchParams({
      isOutside: "true",
      autoplay: "true",
      danmaku: "true",
      muted: "true",
      highQuality: "true",
    });
    if (video.bvid) params.set("bvid", video.bvid);
    if (video.aid) params.set("aid", video.aid);
    return `https://player.bilibili.com/player.html?${params.toString()}`;
  }

  function bilibiliDetailMarkup(item) {
    const playerURL = bilibiliPlayerURL(item?.url);
    const link = safeVideoLink(item?.url);
    const iframe = playerURL
      ? `<iframe src="${esc(playerURL)}" title="${esc(text(item.title, "B 站视频"))}" loading="eager" allow="autoplay; fullscreen; picture-in-picture" allowfullscreen></iframe>`
      : "";
    const fallback = playerURL ? "" : readerFocusCopy("该视频需要在 B 站打开", "Open this video on Bilibili");
    return `<section class="youtube-detail bilibili-detail" aria-label="${esc(
      readerFocusCopy("B 站视频", "Bilibili video")
    )}"><div class="youtube-detail-frame" data-bilibili-detail-frame data-player-state="${playerURL ? "loading" : "fallback"}">${iframe}<div class="youtube-detail-status" data-bilibili-detail-status role="status" aria-live="polite"${playerURL ? " hidden" : ""}><strong data-bilibili-detail-status-title>${esc(playerURL ? "" : readerFocusCopy("播放器暂时不可用", "Player unavailable"))}</strong><span data-bilibili-detail-status-copy>${esc(fallback)}</span></div></div>${link ? `<div class="youtube-detail-actions"><span class="badge blue">${esc(readerFocusCopy("B 站视频", "Bilibili video"))}</span><a class="text-btn" href="${esc(link)}" target="_blank" rel="noopener noreferrer">${esc(readerFocusCopy("在 B 站打开", "Open on Bilibili"))} ${icon("external")}</a></div>` : ""}</section>`;
  }

  function installYouTubeDetailPresentation(entry) {
    if (entry?.kind !== "news") return false;
    const item = entry.item || {};
    if (!youtubeVideoForItem(item)) return false;
    const panel = $("detail-panel");
    if (!panel) return false;
    let media = panel.querySelector(".youtube-detail");
    const top = panel.querySelector(".detail-top");
    const topCopy = top?.querySelector(".detail-top-copy");
    if (!media) {
      const markup = youtubeDetailMarkup(item);
      if (!markup) return false;
      panel.insertAdjacentHTML("afterbegin", markup);
      media = panel.querySelector(".youtube-detail");
    }
    if (!media || !top || !topCopy) return false;
    const source = (state.sources || []).find(
      (candidate) => candidate.id === sourceIDForEntry(entry)
    ) || {
      id: sourceIDForEntry(entry),
      kind: "youtube",
      feed_format: "youtube",
      name: sourceLabel(entry),
      icon_text: sourceLabel(entry).slice(0, 1),
      icon_color: "#64748b",
      has_icon: true,
    };
    const transcript = text(item.transcript);
    const summary = text(item.summary_text, readerFocusCopy("暂无视频简介", "No video description"));
    const duration = Number(item.duration_seconds) > 0
      ? `<span>${esc(videoDurationLabel(item.duration_seconds))}</span>`
      : "";
    panel.classList.add("youtube-detail-panel");
    panel.insertBefore(media, top);
    topCopy.innerHTML = `<h2 class="detail-title">${esc(entry.title)}</h2><div class="youtube-detail-meta">${sourceAvatar(
      source,
      "youtube-detail-avatar"
    )}<strong>${esc(sourceLabel(entry))}</strong>${
      item.author && item.author !== sourceLabel(entry)
        ? `<span>${esc(item.author)}</span>`
        : ""
    }<time datetime="${esc(entryDate(entry))}">${esc(formatDate(entryDate(entry)))}</time>${duration}</div>`;
    top.insertAdjacentHTML(
      "afterend",
      `<div class="youtube-detail-tabs" role="tablist" aria-label="${esc(
        readerFocusCopy("视频详情", "Video details")
      )}"><button class="youtube-detail-tab active" type="button" role="tab" aria-selected="true" aria-controls="youtube-detail-content" data-youtube-detail-tab="content">${esc(
        readerFocusCopy("内容", "Content")
      )}</button><button class="youtube-detail-tab" type="button" role="tab" aria-selected="false" aria-controls="youtube-detail-transcript" data-youtube-detail-tab="transcript">${esc(
        readerFocusCopy("字幕", "Transcript")
      )}</button></div><section class="youtube-detail-copy" id="youtube-detail-content" role="tabpanel" data-youtube-detail-panel="content"><p>${esc(
        summary
      )}</p></section><section class="youtube-detail-copy youtube-detail-transcript" id="youtube-detail-transcript" role="tabpanel" data-youtube-detail-panel="transcript" hidden><p>${esc(
        transcript || readerFocusCopy("暂无可用字幕", "No transcript available")
      )}</p></section>`
    );
    panel.querySelector(".detail-body")?.remove();
    // The base renderer does not know about this extension's YouTube
    // presentation and may have emitted a local cover/media element first.
    // Remove it before the browser retries an unreachable upstream image on
    // every refresh; the iframe is the single source of video media here.
    panel.querySelector(".detail-cover")?.remove();
    panel.querySelector(".detail-media")?.remove();
    panel.querySelector(".detail-tools-row")?.remove();
    return true;
  }

  function installBilibiliDetailPresentation(entry) {
    if (entry?.kind !== "news") return false;
    const item = entry.item || {};
    if (videoProviderForItem(item) !== "bilibili") return false;
    const markup = bilibiliDetailMarkup(item);
    const panel = $("detail-panel");
    if (!markup || !panel) return false;
    const top = panel.querySelector(".detail-top");
    const topCopy = top?.querySelector(".detail-top-copy");
    if (!top || !topCopy) return false;
    panel.insertAdjacentHTML("afterbegin", markup);
    const media = panel.querySelector(".bilibili-detail");
    const frame = panel.querySelector("[data-bilibili-detail-frame]");
    const iframe = frame?.querySelector("iframe");
    if (!media || !frame) return false;
    if (!iframe) {
      frame.removeAttribute("aria-busy");
      return true;
    }
    let settled = false;
    const timeoutID = window.setTimeout(() => {
      if (settled || !frame.isConnected) return;
      settled = true;
      iframe.remove();
      frame.dataset.playerState = "fallback";
      frame.removeAttribute("aria-busy");
      const status = frame.querySelector("[data-bilibili-detail-status]");
      if (status) {
        status.hidden = false;
        const title = status.querySelector("[data-bilibili-detail-status-title]");
        const copy = status.querySelector("[data-bilibili-detail-status-copy]");
        if (title) title.textContent = readerFocusCopy("视频暂时无法连接", "Video is temporarily unavailable");
        if (copy) copy.textContent = readerFocusCopy("可以使用下方链接在 B 站打开。", "Open the video on Bilibili using the link below.");
      }
    }, youtubeEmbedTimeoutMS);
    iframe.addEventListener("load", () => {
      if (settled || !frame.isConnected) return;
      settled = true;
      window.clearTimeout(timeoutID);
      frame.dataset.playerState = "ready";
      frame.removeAttribute("aria-busy");
      const status = frame.querySelector("[data-bilibili-detail-status]");
      if (status) status.hidden = true;
    }, { once: true });
    frame.setAttribute("aria-busy", "true");
    const source = (state.sources || []).find(
      (candidate) => candidate.id === sourceIDForEntry(entry)
    ) || {
      id: sourceIDForEntry(entry),
      kind: "bilibili",
      feed_format: "bilibili",
      name: sourceLabel(entry),
      icon_text: sourceLabel(entry).slice(0, 1),
      icon_color: "#64748b",
      has_icon: true,
    };
    const duration = Number(item.duration_seconds) > 0
      ? `<span>${esc(videoDurationLabel(item.duration_seconds))}</span>`
      : "";
    panel.classList.add("youtube-detail-panel");
    panel.insertBefore(media, top);
    topCopy.innerHTML = `<h2 class="detail-title">${esc(entry.title)}</h2><div class="youtube-detail-meta">${sourceAvatar(
      source,
      "youtube-detail-avatar"
    )}<strong>${esc(sourceLabel(entry))}</strong>${
      item.author && item.author !== sourceLabel(entry)
        ? `<span>${esc(item.author)}</span>`
        : ""
    }<time datetime="${esc(entryDate(entry))}">${esc(formatDate(entryDate(entry)))}</time>${duration}</div>`;
    top.insertAdjacentHTML(
      "afterend",
      `<div class="youtube-detail-tabs" role="tablist" aria-label="${esc(
        readerFocusCopy("视频详情", "Video details")
      )}"><button class="youtube-detail-tab active" type="button" role="tab" aria-selected="true" aria-controls="youtube-detail-content" data-youtube-detail-tab="content">${esc(
        readerFocusCopy("内容", "Content")
      )}</button></div><section class="youtube-detail-copy" id="youtube-detail-content" role="tabpanel" data-youtube-detail-panel="content"><p>${esc(
        text(item.summary_text, readerFocusCopy("暂无视频简介", "No video description"))
      )}</p></section>`
    );
    panel.querySelector(".detail-body")?.remove();
    panel.querySelector(".detail-cover")?.remove();
    panel.querySelector(".detail-media")?.remove();
    panel.querySelector(".detail-tools-row")?.remove();
    return true;
  }

  function onYouTubeDetailTabClick(event) {
    const playButton = event.target.closest("[data-youtube-detail-play]");
    if (playButton) {
      event.preventDefault();
      playYouTubeDetailVideo(playButton);
      return;
    }
    const button = event.target.closest("[data-youtube-detail-tab]");
    if (!button) return;
    const panel = button.closest(".youtube-detail-panel");
    if (!panel) return;
    const target = text(button.dataset.youtubeDetailTab);
    panel.querySelectorAll("[data-youtube-detail-tab]").forEach((tab) => {
      const active = tab === button;
      tab.classList.toggle("active", active);
      tab.setAttribute("aria-selected", String(active));
    });
    panel.querySelectorAll("[data-youtube-detail-panel]").forEach((section) => {
      section.hidden = section.dataset.youtubeDetailPanel !== target;
    });
  }

  // Resolve the best display image for a picture-wall tile: the full media
  // attachment when it is an image, otherwise the proxied thumbnail. Both
  // paths stay on the existing media proxy contract.
  function pictureImageForItem(item) {
    if (!item) return "";
    const mime = text(item.media_mime).toLowerCase();
    if (mime.startsWith("image/")) {
      const media = localAppMediaURL(item.media_url, item.id, "media");
      if (media) return media;
    }
    return (
      localAppMediaURL(item.thumbnail_url, item.id, "thumbnail") ||
      (publicURL(item.thumbnail_url) ? text(item.thumbnail_url) : "")
    );
  }

  function pictureEntryFallbackImage(item) {
    return localAppMediaURL(item?.thumbnail_url, item?.id, "thumbnail");
  }

  function renderPictureEntry(entry) {
    const item = entry.item || {};
    const preference = pref(entry);
    const image = pictureImageForItem(item);
    if (!image) return baseRenderEntry(entry);
    const fallback = pictureEntryFallbackImage(item);
    const star = preference.starred ? icon("starFill") : icon("star");
    return `<article class="entry picture-entry ${preference.read ? "read " : ""}${
      state.selected === entry.key ? "selected" : ""
    }" data-entry-key="${esc(entry.key)}"><button class="picture-entry-main" type="button" data-select-entry="${esc(
      entry.key
    )}"><span class="picture-entry-frame"><img src="${esc(image)}"${
      fallback && fallback !== image
        ? ` data-fallback-src="${esc(fallback)}"`
        : ""
    }" alt="" loading="lazy" decoding="async">${
      preference.read
        ? ""
        : '<span class="picture-unread-dot" aria-label="未读"></span>'
    }</span><span class="picture-entry-copy"><span class="picture-entry-title">${esc(
      entry.title
    )}</span><span class="picture-entry-source">${esc(
      sourceLabel(entry)
    )} · ${esc(relativeVideoTime(entryDate(entry)))}</span></span></button><span class="entry-actions"><button class="icon-btn" type="button" data-open-picture="${esc(
      entry.key
    )}" aria-label="查看大图" title="查看大图">${icon(
      "maximize"
    )}</button><button class="icon-btn ${
      preference.starred ? "starred" : ""
    }" type="button" data-star-entry="${esc(
      entry.key
    )}" aria-label="${
      preference.starred ? "取消收藏" : "收藏"
    }" title="${preference.starred ? "取消收藏" : "收藏"}">${star}</button></span></article>`;
  }

  function renderVideoEntry(entry) {
    // Legacy markup contract: <span class="youtube-entry-media">
    // Legacy markup contract: <span class="youtube-entry-copy">
    const item = entry.item || {};
    const video = videoForItem(item);
    if (!video) return baseRenderEntry(entry);
    const preference = pref(entry);
    const source = (state.sources || []).find(
      (candidate) => candidate.id === sourceIDForEntry(entry)
    ) || {
      id: sourceIDForEntry(entry),
      kind: video.provider,
      feed_format: video.provider,
      name: sourceLabel(entry),
      icon_text: sourceLabel(entry).slice(0, 1),
      icon_color: "#64748b",
      has_icon: true,
    };
    const creatorAvatar = sourceAvatar(source, "video-entry-avatar");
    const star = preference.starred ? icon("starFill") : icon("star");
    const duration = video.durationSeconds > 0
      ? `<span class="video-duration-badge">${esc(videoDurationLabel(video.durationSeconds))}</span>`
      : "";
    const providerClass = video.provider === "youtube" ? " youtube-entry" : "";
    const mainClass =
      video.provider === "youtube" ? "youtube-entry-main" : "video-entry-main";
    const mediaClass =
      video.provider === "youtube" ? "youtube-entry-media" : "video-entry-media";
    const copyClass =
      video.provider === "youtube" ? "youtube-entry-copy" : "video-entry-copy";
    const imageMarkup = video.thumbnailURL
      ? `<img src="${esc(video.thumbnailURL)}" data-fallback-src="${esc(video.thumbnailFallbackURL)}" data-second-fallback-src="${esc(video.thumbnailDirectURL)}" alt="" loading="lazy" decoding="async">`
      : "";
    // 无封面数据（如网关未回传缩略图的 B 站条目）不是"加载中"：占位直接落在
    // 诚实的终态文案上，避免列表里出现永远转不完的假 loading。
    const placeholderLabel = video.thumbnailURL
      ? "视频缩略图加载中"
      : readerFocusCopy("暂无封面", "No cover");
    return `<article class="entry video-entry${providerClass} ${preference.read ? "read " : ""}${state.selected === entry.key ? "selected" : ""}" data-entry-key="${esc(entry.key)}" data-video-provider="${esc(video.provider)}">
      <button class="${mainClass}" type="button" data-select-entry="${esc(entry.key)}">
        <span class="${mediaClass}">${imageMarkup}<span class="youtube-thumbnail-placeholder" aria-hidden="true">${icon("video")}<span class="youtube-thumbnail-placeholder-label">${esc(placeholderLabel)}</span></span><span class="youtube-play-mark" aria-hidden="true">${icon("arrow")}</span>${duration}</span>
        <span class="${copyClass}"><span class="entry-title video-entry-title">${esc(entry.title)}</span><span class="video-entry-kicker youtube-entry-kicker">${creatorAvatar}<span class="video-entry-source youtube-entry-source">${esc(sourceLabel(entry))}</span><span class="video-entry-separator">—</span><span class="video-entry-platform">${esc(video.label)}</span><span class="video-entry-date youtube-entry-date">· ${esc(relativeVideoTime(entryDate(entry)))}</span></span><span class="entry-snippet video-entry-snippet">${esc(entry.summary || "暂无视频简介")}</span><span class="entry-meta video-entry-meta">${badge(video.label, "blue")}${item.author ? `<span>${esc(item.author)}</span>` : ""}</span></span>
      </button>
      <span class="entry-actions"><button class="icon-btn ${preference.starred ? "starred" : ""}" type="button" data-star-entry="${esc(entry.key)}" aria-label="${preference.starred ? "取消收藏" : "收藏"}" title="${preference.starred ? "取消收藏" : "收藏"}">${star}</button></span>
    </article>`;
  }

  function renderYouTubeEntry(entry) {
    return renderVideoEntry(entry);
  }

  const xAccounts = {
    "elon-musk-x": "elonmusk",
    "donald-trump-x": "realDonaldTrump",
    "sam-altman-x": "sama",
    "dario-amodei-x": "DarioAmodei",
    "demis-hassabis-x": "demishassabis",
    "jensen-huang-x": "JensenHuang",
    "satya-nadella-x": "satyanadella",
    "andrej-karpathy-x": "karpathy",
    "tibo-sottiaux-x": "thsottiaux",
  };

  function xProfileHandle(raw) {
    try {
      const target = new URL(text(raw));
      const host = target.hostname.toLowerCase();
      if (
        target.protocol !== "https:" ||
        (host !== "x.com" && host !== "twitter.com") ||
        target.username ||
        target.password ||
        target.port ||
        target.search ||
        target.hash
      )
        return "";
      const match = target.pathname.match(/^\/([A-Za-z0-9_]{1,30})\/?$/);
      return match ? match[1] : "";
    } catch {
      return "";
    }
  }

  function xSourceIsX(source) {
    if (!source) return false;
    if (xAccounts[text(source.id)]) return true;
    if (xProfileHandle(source.site_url)) return true;
    const hint = [source.url, source.feed_url, source.homepage_url]
      .map((value) => text(value).toLowerCase())
      .join(" ");
    let decodedHint = hint;
    try {
      decodedHint = decodeURIComponent(hint);
    } catch {}
    return /(?:rsshub[^\s/]*\/)?(?:twitter|x)\/user\/[a-z0-9_]+/i.test(
      decodedHint
    );
  }

  function xAccountForItem(item) {
    const sourceID = text(item?.source_id);
    const pinnedHandle = xAccounts[sourceID] || "";
    let handle = "";
    try {
      const target = new URL(text(item?.url));
      const host = target.hostname.toLowerCase();
      const statusMatch = target.pathname.match(
        /^\/([A-Za-z0-9_]{1,30})\/status\/[0-9]+$/
      );
      if (
        target.protocol !== "https:" ||
        target.username ||
        target.password ||
        target.port ||
        (host !== "x.com" && host !== "twitter.com") ||
        target.search ||
        target.hash ||
        !statusMatch
      )
        return "";
      handle = statusMatch[1];
    } catch {
      return "";
    }
    if (pinnedHandle && pinnedHandle.toLowerCase() !== handle.toLowerCase())
      return "";
    const source = xSourceForItem(item);
    const sourceHandle = xProfileHandle(source?.site_url);
    if (sourceHandle && sourceHandle.toLowerCase() !== handle.toLowerCase())
      return "";
    if (!pinnedHandle && !sourceHandle && !xSourceIsX(source)) return "";
    return sourceHandle || pinnedHandle || handle;
  }

  function trustedXVideoURL(raw, item) {
    if (!xAccountForItem(item)) return "";
    try {
      const target = new URL(text(raw));
      if (
        target.protocol !== "https:" ||
        target.hostname.toLowerCase() !== "video.twimg.com" ||
        target.username ||
        target.password ||
        target.port ||
        target.hash ||
        !target.pathname
      )
        return "";
      return target.href;
    } catch {
      return "";
    }
  }

  // X media is the one intentional external-media exception. Keep the
  // allowlist narrow so feed-provided URLs cannot turn into arbitrary image
  // requests or bypass the app-owned media proxy.
  function trustedXImageURL(raw, owner) {
    if (!owner || !xAccountForItem(owner)) return "";
    try {
      const target = new URL(text(raw));
      if (
        target.protocol !== "https:" ||
        target.hostname.toLowerCase() !== "pbs.twimg.com" ||
        target.username ||
        target.password ||
        target.port ||
        target.hash ||
        !target.pathname ||
        target.href.length > 1000
      )
        return "";
      return target.href;
    } catch {
      return "";
    }
  }

  function xSourceForItem(item) {
    return (state.sources || []).find(
      (source) => source.id === item?.source_id
    );
  }

  function xVideoFallbackMarkup(item, poster, hidden = true) {
    const cover = poster
      ? `<img src="${esc(poster)}" alt="${esc(
          (item.author || item.source || "公开账号") + " 的视频封面"
        )}">`
      : "";
    return `<a class="x-media-fallback" href="${esc(
      item.url
    )}" target="_blank" rel="noopener noreferrer" ${
      hidden ? "hidden" : ""
    }>${cover}<span>${icon("arrow")}在 X 查看</span></a>`;
  }

  function xEntryMediaMarkup(entry) {
    const item = entry.item || {};
    const poster =
      localAppMediaURL(item.thumbnail_url, item.id, "thumbnail") ||
      trustedXImageURL(item.thumbnail_url, item);
    const trustedVideo = trustedXVideoURL(item.media_url, item);
    const video = text(item.media_mime).toLowerCase().startsWith("video/")
      ? localAppMediaURL(item.media_url, item.id, "media")
      : trustedVideo
        ? localAppMediaURL(trustedVideo, item.id, "media")
        : "";
    if (video) {
      return `<div class="x-entry-media x-entry-video-shell" data-x-media-shell><video class="x-entry-video" controls playsinline preload="none" src="${esc(
        video
      )}" ${poster ? `poster="${esc(poster)}"` : ""}></video>${xVideoFallbackMarkup(
        item,
        poster
      )}</div>`;
    }
    if (!poster) return "";
    return `<a class="x-entry-media x-entry-photo" href="${esc(
      item.url
    )}" target="_blank" rel="noopener noreferrer" aria-label="在 X 查看 ${esc(
      item.author || item.source || "公开账号"
    )} 的图片"><img src="${esc(poster)}" alt="${esc(
      (item.author || item.source || "公开账号") + " 的动态图片"
    )}"></a>`;
  }

  function xPostTextFromContent(value, entry) {
    const raw = text(value?.content_text).replace(/\r\n?/g, "\n").trim();
    if (!raw) return "";
    const item = entry?.item || {};
    const handle = xAccountForItem(item);
    const author = text(item.author || item.source || `@${handle}`);
    const markers = [`${author} @${handle} `, `@${handle} `].filter(
      (marker) => marker.trim() !== "@"
    );
    let post = "";
    for (const marker of markers) {
      const index = raw.indexOf(marker);
      if (index >= 0) {
        post = raw.slice(index + marker.length);
        break;
      }
    }
    if (!post) return "";
    const timestamp = post.search(
      /\s+\d{1,2}:\d{2}\s+(?:AM|PM)\s+·\s+[A-Z][a-z]{2}\s+\d{1,2},\s+\d{4}(?:\s|$)/
    );
    if (timestamp >= 0) post = post.slice(0, timestamp);
    post = post.replace(/\n{3,}/g, "\n\n").trim().slice(0, 8000);
    if (
      !post ||
      /(?:Trending now|Terms · Privacy|Log in|Sign up|Continue with|Relevant people)/i.test(
        post
      ) ||
      /^Follow\b/i.test(post)
    )
      return "";
    return post;
  }

  function xEntryFallbackText(entry) {
    const summary = text(entry?.item?.summary_text);
    if (
      summary &&
      !/(?:on X:\s*"|Post Log in|Log in or sign up for X|Continue with|Trending now|Terms · Privacy)/i.test(
        summary
      )
    )
      return summary;
    return text(entry?.title || entry?.summary, "暂无正文");
  }

  function xAuthorAvatarMarkup(entry, source) {
    return sourceAvatar(source, "x-author-avatar");
  }

  function renderXEntry(entry) {
    const preference = pref(entry);
    const item = entry.item || {};
    const handle = xAccountForItem(item);
    const source = xSourceForItem(item) || {
      id: item.source_id,
      name: item.source,
      icon_text: (item.author || item.source || "X").slice(0, 1),
      icon_color: "#111827",
      has_icon: false,
    };
    const author = item.author || item.source || `@${handle}`;
    const star = preference.starred ? icon("starFill") : icon("star");
    const cached = cachedFullText(item.id, item.content_hash);
    const body =
      xPostTextFromContent(cached, entry) ||
      xEntryFallbackText(entry);
    const needsFullText = !cached;
    return `<article class="entry x-entry ${preference.read ? "read " : ""}${
      state.selected === entry.key ? "selected" : ""
    }" data-entry-key="${esc(entry.key)}" data-x-entry-id="${esc(
      item.id
    )}" data-x-needs-fulltext="${needsFullText ? "1" : "0"}"><div class="x-entry-avatar">${xAuthorAvatarMarkup(
      entry,
      source
    )}</div><div class="x-entry-content"><button class="x-entry-main" type="button" data-select-entry="${esc(
      entry.key
    )}"><span class="x-entry-head"><strong>${esc(
      author
    )}</strong><span>@${esc(handle)}</span><span aria-hidden="true">·</span><time>${esc(
      formatDate(entryDate(entry))
    )}</time></span><span class="x-entry-text">${esc(
      body
    )}</span></button>${xEntryMediaMarkup(
      entry
    )}<div class="x-entry-foot"><a href="${esc(
      item.url
    )}" target="_blank" rel="noopener noreferrer">在 X 查看 <span aria-hidden="true">↗</span></a><span class="x-entry-load-state" aria-live="polite"></span></div></div><span class="entry-actions"><button class="icon-btn ${
      preference.starred ? "starred" : ""
    }" type="button" data-star-entry="${esc(entry.key)}" aria-label="${
      preference.starred ? "取消收藏" : "收藏"
    }" title="${preference.starred ? "取消收藏" : "收藏"}">${star}</button></span></article>`;
  }

  function installXDetailPresentation(entry) {
    const item = entry?.item || {};
    const handle = xAccountForItem(item);
    if (!handle) return false;
    const panel = $("detail-panel");
    const source = xSourceForItem(item) || {
      id: item.source_id,
      name: item.source,
      icon_text: (item.author || item.source || "X").slice(0, 1),
      icon_color: "#111827",
      has_icon: false,
    };
    const author = item.author || item.source || `@${handle}`;
    const post = entry.title || entry.summary || "暂无正文";
    const summary = text(item.summary_text);
    const topCopy = panel.querySelector(".detail-top-copy");
    if (!topCopy) return false;

    panel.classList.add("x-detail-panel");
    topCopy.innerHTML = `<div class="x-detail-identity"><div class="x-detail-avatar">${sourceAvatar(
      source,
      "x-author-avatar"
    )}</div><div class="x-detail-author"><strong>${esc(
      author
    )}</strong><span>@${esc(handle)} · <time datetime="${esc(
      entryDate(entry)
    )}">${esc(formatDate(entryDate(entry)))}</time></span></div></div>`;

    const top = panel.querySelector(".detail-top");
    top?.insertAdjacentHTML(
      "afterend",
      `<section class="x-detail-post" aria-label="X 动态正文"><p>${esc(
        post
      )}</p></section>`
    );

    const detailBody = panel.querySelector(".detail-body");
    if (detailBody) {
      const cached = cachedFullText(item.id, item.content_hash);
      const thread = cached ? parseXThread(cached) : null;
      if (thread) {
        detailBody.innerHTML = xThreadMarkup(entry, thread);
        const postMedia = thread.post?.images?.length
          ? `<div class="x-thread-media">${thread.post.images
              .map(
                (index) =>
                  `<img class="x-thread-photo" src="./api/entries/content/image?id=${encodeURIComponent(
                    text(item.id)
                  )}&index=${index}" alt="${esc(
                    text(item.author || item.source || "公开账号") + " 的动态图片"
                  )}" loading="lazy" decoding="async">`
              )
              .join("")}</div>`
          : "";
        if (postMedia) {
          panel
            .querySelector(".x-detail-post")
            ?.insertAdjacentHTML("beforeend", postMedia);
        }
      } else if (cached) {
        detailBody.innerHTML =
          summary && summary !== post
            ? `<section class="x-detail-summary"><span>来源摘要</span><p>${esc(
                summary
              )}</p></section>`
            : "";
      } else if (!parity.xDetailThreadFailed?.has(text(item.id))) {
        // 没有缓存全文时拉一次线程（含引用 / 互动数据 / 回复），完成后局部重渲染。
        detailBody.innerHTML = `<div class="x-thread-loading" data-x-thread-loading role="status" aria-live="polite">正在加载动态与回复…</div>`;
        void hydrateXDetailThread(entry);
      } else {
        detailBody.innerHTML = `<div class="x-thread-loading" role="status">动态与回复暂时不可用，可稍后重试。</div>`;
      }
    }
    const sourceLink = panel.querySelector(".detail-tools-row a");
    if (sourceLink) sourceLink.innerHTML = `在 X 查看 ${icon("external")}`;
    return true;
  }

  // X 全文是一张拍平的线程页（正文 → 引用 → 时间戳 → 互动数据 → 回复 → 登录墙）。
  // parseXThread 把它还原成结构化块；登录墙之后的内容一律丢弃。
  function parseXThread(value) {
    const raw = text(value?.content_html);
    if (!raw) return null;
    const doc = new DOMParser().parseFromString(raw, "text/html");
    const junk =
      /Log in or sign up|Relevant people|Trending now|Terms · Privacy|Continue with|See what’s happening/i;
    const blocks = [];
    const stats = [];
    let current = null;
    let pendingName = "";
    let phase = "post";
    const push = () => {
      if (current && (current.text.trim() || current.handle)) blocks.push(current);
      current = null;
    };
    const startBlock = (handle, avatarIndex) => {
      push();
      current = {
        handle: handle || "",
        name: pendingName,
        text: "",
        time: "",
        images: [],
        avatarIndex: Number.isInteger(avatarIndex) ? avatarIndex : null,
        phase,
      };
      pendingName = "";
    };
    for (const node of [...doc.body.childNodes]) {
      if (node.nodeType === 3) {
        const label = node.textContent || "";
        if (!label.trim()) continue;
        if (junk.test(label)) return classify();
        if (current) current.text += label;
        continue;
      }
      if (node.nodeType !== 1) continue;
      const el = node;
      if (el.tagName === "H1" || el.tagName === "H2") {
        if (junk.test(el.textContent)) return classify();
        continue;
      }
      if (el.tagName === "P") {
        if (junk.test(el.textContent)) return classify();
        if (current) current.text += el.textContent;
        continue;
      }
      if (el.tagName !== "A") continue;
      const label = el.textContent.replace(/\s+/g, " ").trim();
      const image = el.querySelector("img[data-entry-image]");
      if (image) {
        const alt = text(image.getAttribute("alt")).toLowerCase();
        const index = Number(image.getAttribute("data-entry-image"));
        if (
          !Number.isInteger(index) ||
          index < 0 ||
          index >= 24 ||
          alt.includes("avatar")
        )
          continue;
        if (index === 0 || alt.startsWith("@")) {
          // 统计行之后出现的作者头像意味着回复区开始了。
          if (phase === "stats") phase = "replies";
          startBlock(alt.startsWith("@") ? alt.slice(1) : "", index);
          continue;
        }
        if (current) current.images.push(index);
        continue;
      }
      if (!label) continue;
      if (junk.test(label)) return classify();
      // 线程页固定前奏/操作词（Post / Log in / Follow 等）不属于任何动态块。
      if (/^(?:Post|Log in|Sign up|Follow|Show this thread|Show more)$/i.test(label))
        continue;
      if (/^@\S+$/.test(label)) {
        const handle = label.slice(1);
        // 头像 alt 与 handle 锚可能先后开同一个块（头像先到时 handle 已带出），
        // 正文尚未开始就合并，避免把一条动态拆成两块。
        if (
          current &&
          !current.text.trim() &&
          (!current.handle || current.handle === handle)
        ) {
          current.handle = handle;
          continue;
        }
        startBlock(handle);
        continue;
      }
      if (
        /^\d{1,2}:\d{2}\s*(?:AM|PM)?\s*·\s*[A-Z][a-z]{2}\s+\d{1,2},\s*\d{4}$/i.test(
          label
        )
      ) {
        if (current) {
          // 主时间戳只在没有自己的时间（如引用块带 Sep 27）时才落到块上。
          if (!current.time) current.time = label;
          push();
        }
        phase = "stats";
        continue;
      }
      if (/^[A-Z][a-z]{2}\s+\d{1,2}$/.test(label)) {
        if (current) current.time = label;
        continue;
      }
      if (phase === "stats") {
        if (/^\d[\d.,]*[KM]?\s*(?:Views)?$/i.test(label)) {
          stats.push(label.replace(/\s+/g, ""));
          continue;
        }
        phase = "replies";
      }
      if (current && !current.name && !current.text.trim() && label.length <= 48) {
        current.name = label;
        continue;
      }
      if (label.length <= 48 && !current) {
        pendingName = label;
        continue;
      }
      if (current) current.text += label + " ";
    }
    return classify();
    function classify() {
      push();
      const post = blocks[0] || null;
      const quote =
        blocks.slice(1).find((block) => block.phase === "post") || null;
      const replies = blocks.filter((block) => block.phase === "replies");
      const normalizedStats = stats
        .map((value) => value.replace(/Views$/i, " 浏览"))
        .slice(0, 6);
      if (!post && !quote && !replies.length && !normalizedStats.length)
        return null;
      return { post, quote, stats: normalizedStats, replies };
    }
  }

  function xThreadMarkup(entry, thread) {
    if (!thread) return "";
    const entryID = text(entry?.item?.id);
    const threadImage = (index, className, alt) =>
      `<img class="${className}" src="./api/entries/content/image?id=${encodeURIComponent(
        entryID
      )}&index=${index}" alt="${esc(alt)}" loading="lazy" decoding="async">`;
    const quote = thread.quote
      ? `<section class="x-thread-quote" aria-label="引用的动态"><div class="x-thread-quote-head"><strong>${esc(
          thread.quote.name || thread.quote.handle || "引用"
        )}</strong>${
          thread.quote.handle ? `<span>@${esc(thread.quote.handle)}</span>` : ""
        }${
          thread.quote.time ? `<time>${esc(thread.quote.time)}</time>` : ""
        }</div><p>${esc(thread.quote.text.trim())}</p>${
          thread.quote.images.length
            ? `<div class="x-thread-media">${thread.quote.images
                .map((index) =>
                  threadImage(index, "x-thread-photo", "引用动态配图")
                )
                .join("")}</div>`
            : ""
        }</section>`
      : "";
    const stats = thread.stats.length
      ? `<div class="x-thread-stats" role="group" aria-label="互动数据">${thread.stats
          .map((value) => `<span>${esc(value)}</span>`)
          .join("")}</div>`
      : "";
    const replies = thread.replies.length
      ? `<section class="x-thread-replies" aria-label="回复"><div class="x-thread-replies-title"><strong>回复</strong><span>${thread.replies.length}</span></div>${thread.replies
          .map(
            (reply) =>
              `<article class="x-reply"><div class="x-reply-avatar">${
                reply.avatarIndex != null
                  ? threadImage(
                      reply.avatarIndex,
                      "x-reply-avatar-img",
                      "@" + text(reply.handle) + " 的头像"
                    )
                  : `<span class="x-reply-fallback">${esc(
                      (reply.name || reply.handle || "?").slice(0, 1).toUpperCase()
                    )}</span>`
              }</div><div class="x-reply-main"><div class="x-reply-head"><strong>${esc(
                reply.name || reply.handle || "回复"
              )}</strong>${
                reply.handle ? `<span>@${esc(reply.handle)}</span>` : ""
              }${
                reply.time ? `<time>${esc(reply.time)}</time>` : ""
              }</div><p>${esc(reply.text.trim())}</p>${
                reply.images.length
                  ? `<div class="x-thread-media">${reply.images
                      .map((index) => threadImage(index, "x-thread-photo", "回复配图"))
                      .join("")}</div>`
                  : ""
              }</div></article>`
          )
          .join("")}</section>`
      : "";
    return `<div class="x-thread" data-x-thread>${quote}${stats}${replies}</div>`;
  }

  async function hydrateXDetailThread(entry) {
    const entryID = text(entry?.item?.id);
    if (!entryID || parity.fullTextLoading.has(entryID)) return;
    if (!parity.xDetailThreadFailed) parity.xDetailThreadFailed = new Set();
    parity.fullTextLoading.add(entryID);
    try {
      let result = await readBrowserFullText(entry);
      if (!result) {
        result = await request(
          `./api/entries/content?id=${encodeURIComponent(entryID)}`,
          { timeoutMs: 30000 }
        );
      }
      if (
        !text(result?.content_text) ||
        text(result?.status) === "summary" ||
        articleContentLooksBlocked(result)
      )
        throw new Error("完整动态暂时不可用");
      storeFullText(entryID, result);
      void writeBrowserFullText(entry, result);
    } catch {
      if (!parity.xDetailThreadFailed) parity.xDetailThreadFailed = new Set();
      parity.xDetailThreadFailed.add(entryID);
    } finally {
      parity.fullTextLoading.delete(entryID);
    }
    if (state.selected === entry.key) renderDetail();
  }

  function articleBlockedMarker(value) {
    const normalized = text(value).toLowerCase();
    return [
      "参数错误",
      "链接已过期",
      "请在微信客户端打开",
      "环境异常",
      "访问过于频繁",
      "页面不存在",
      "完成验证后",
    ].some((marker) => normalized.includes(marker.toLowerCase()));
  }

  function articleContentLooksBlocked(value) {
    return articleBlockedMarker(
      `${text(value?.content_text)}\n${text(value?.content_html)}`
    );
  }

  function articleProjectionLooksBlocked(item) {
    const cached = parity.fullText.get(text(item?.id));
    if (cached) return articleContentLooksBlocked(cached);
    return articleBlockedMarker(
      `${text(item?.summary_text)}\n${text(item?.safe_html)}`
    );
  }

  function articleAIOverviewMarkup(item) {
    const entryID = text(item?.id);
    const overviewKey = articleOverviewKey(item);
    const loading = overviewKey && parity.articleOverviewLoading.has(overviewKey);
    const overview = item?.ai_overview;
    if (articleProjectionLooksBlocked(item)) {
      return `<section class="article-ai-overview is-failed" data-article-ai-overview role="status" aria-live="polite">
        <div class="article-ai-overview-head"><span class="article-ai-overview-icon" aria-hidden="true">${icon(
          "sparkles"
        )}</span><strong>${esc(readerFocusCopy("AI 概览", "AI overview"))}</strong><span class="article-ai-overview-state">${esc(readerFocusCopy("原文未获取", "Source unavailable"))}</span></div>
        <p class="article-ai-overview-note">${esc(readerFocusCopy("来源返回了验证页，未生成 AI 概览，避免把失败页误认为正文。", "The source returned a verification page, so no AI overview was generated."))}</p>
      </section>`;
    }
    if (loading && !overview) {
      return `<section class="article-ai-overview is-loading" data-article-ai-overview role="status" aria-live="polite" aria-busy="true">
        <div class="article-ai-overview-head"><span class="article-ai-overview-icon" aria-hidden="true">${icon(
          "sparkles"
        )}</span><strong>${esc(readerFocusCopy("AI 概览", "AI overview"))}</strong><span class="article-ai-overview-state">${esc(readerFocusCopy("正在生成", "Generating"))}</span></div>
        <div class="article-ai-overview-skeleton" aria-hidden="true"><i></i><i></i><i></i></div>
        <p class="article-ai-overview-note">${esc(readerFocusCopy("正在阅读这篇文章并提炼重点，原文不受影响。", "Reading this article and extracting key points; the original is unaffected."))}</p>
      </section>`;
    }
    if (!overview) {
      const error = text(parity.articleOverviewErrors.get(overviewKey));
      if (error) {
        return `<section class="article-ai-overview is-failed" data-article-ai-overview role="status" aria-live="polite">
          <div class="article-ai-overview-head"><span class="article-ai-overview-icon" aria-hidden="true">${icon(
            "sparkles"
          )}</span><strong>${esc(readerFocusCopy("AI 概览", "AI overview"))}</strong><span class="article-ai-overview-state">${esc(readerFocusCopy("生成失败", "Generation failed"))}</span></div>
          <p class="article-ai-overview-note">${esc(readerFocusCopy("概览暂时没有生成，不影响阅读原文。", "The overview is not available yet; the original article remains readable."))}</p>
          <button class="article-ai-overview-action" type="button" data-retry-article-ai-overview="${esc(
            entryID
          )}">${icon("refresh")}<span>${esc(readerFocusCopy("重试生成", "Retry generation"))}</span></button>
        </section>`;
      }
      const manual =
        !parity.settingsResolved ||
        parity.settings?.auto_generate_article_overviews === false;
      return `<section class="article-ai-overview is-pending" data-article-ai-overview role="status" aria-live="polite">
        <div class="article-ai-overview-head"><span class="article-ai-overview-icon" aria-hidden="true">${icon(
          "sparkles"
        )}</span><strong>${esc(readerFocusCopy("AI 概览", "AI overview"))}</strong><span class="article-ai-overview-state">${
          manual ? esc(readerFocusCopy("按需生成", "Generate on demand")) : esc(readerFocusCopy("准备生成", "Preparing"))
        }</span></div>
        <p class="article-ai-overview-note">${
          manual
            ? parity.settingsResolved
              ? readerFocusCopy("按需提炼文章摘要、重点和与你相关的影响。", "Generate a summary, key points, and relevant impact on demand.")
              : readerFocusCopy("设置暂时未读取，点击后按需提炼文章摘要、重点和与你相关的影响。", "Settings are still loading; click to generate a summary, key points, and relevant impact.")
            : readerFocusCopy("正在准备文章内容，概览会显示在原文顶部。", "Preparing the article; the overview will appear above the original.")
        }</p>
        ${
          manual
            ? `<button class="article-ai-overview-action" type="button" data-generate-article-overview="${esc(
                entryID
              )}">${icon("sparkles")}<span>${esc(readerFocusCopy("生成 AI 概览", "Generate AI overview"))}</span></button>`
            : ""
        }
      </section>`;
    }
    if (overview.status === "pending") {
      const stage = articleAIOverviewStageCopy(overview.stage);
      return `<section class="article-ai-overview is-pending" data-article-ai-overview role="status" aria-live="polite" aria-busy="true">
        <div class="article-ai-overview-head"><span class="article-ai-overview-icon" aria-hidden="true">${icon("sparkles")}</span><strong>${esc(readerFocusCopy("AI 概览", "AI overview"))}</strong><span class="article-ai-overview-state">${esc(stage.label)}</span></div>
        <div class="article-ai-overview-skeleton" aria-hidden="true"><i></i><i></i><i></i></div>
        <p class="article-ai-overview-note">${esc(stage.note)}</p>
      </section>`;
    }
    if (overview.status === "failed") {
      const errorCode = text(overview.error_code);
      return `<section class="article-ai-overview is-failed" data-article-ai-overview role="status" aria-live="polite">
        <div class="article-ai-overview-head"><span class="article-ai-overview-icon" aria-hidden="true">${icon(
          "sparkles"
        )}</span><strong>${esc(readerFocusCopy("AI 概览", "AI overview"))}</strong><span class="article-ai-overview-state">${esc(readerFocusCopy("暂未生成", "Not generated"))}</span></div>
        <p class="article-ai-overview-note">${esc(articleAIOverviewErrorMessage(errorCode))} ${esc(readerFocusCopy("不影响阅读原文。", "Reading the original article is unaffected."))}</p>
        <button class="article-ai-overview-action" type="button" data-retry-article-ai-overview="${esc(
          entryID
        )}">${icon("refresh")}<span>${esc(readerFocusCopy("重试生成", "Retry generation"))}</span></button>
      </section>`;
    }
    const summary = text(overview.summary);
    const keyPoints = Array.isArray(overview.key_points)
      ? overview.key_points.map((point) => text(point)).filter(Boolean).slice(0, 3)
      : [];
    const impact = text(overview.impact);
    if (!summary) return "";
    return `<section class="article-ai-overview is-ready" data-article-ai-overview aria-labelledby="article-ai-overview-title">
      <div class="article-ai-overview-head"><span class="article-ai-overview-icon" aria-hidden="true">${icon(
        "sparkles"
      )}</span><strong id="article-ai-overview-title">${esc(readerFocusCopy("AI 概览", "AI overview"))}</strong><span class="article-ai-overview-state">${esc(readerFocusCopy("已生成", "Ready"))}</span></div>
      <p class="article-ai-overview-summary">${esc(summary)}</p>
      ${
        keyPoints.length
          ? `<ul class="article-ai-overview-points">${keyPoints
              .map((point) => `<li>${esc(point)}</li>`)
              .join("")}</ul>`
          : ""
      }
      ${
        impact
          ? `<p class="article-ai-overview-impact"><span>${esc(readerFocusCopy("与你相关", "Relevant to you"))}</span>${esc(
              impact
            )}</p>`
          : ""
      }
    </section>`;
  }

  function articleAIOverviewStageCopy(stage) {
    switch (text(stage)) {
      case "preparing_source":
        return { label: readerFocusCopy("准备正文", "Preparing source"), note: readerFocusCopy("正在读取可验证的文章正文，原文可以继续阅读。", "Reading the verifiable article body; you can continue reading.") };
      case "generating":
        return { label: readerFocusCopy("正在生成", "Generating"), note: readerFocusCopy("正在提炼文章重点，离开页面也会继续。", "Extracting key points; the job continues if you leave this page.") };
      case "validating":
        return { label: readerFocusCopy("正在校验", "Validating"), note: readerFocusCopy("正在校验概览结构与文章版本。", "Validating the overview structure and article revision.") };
      case "retry_wait":
        return { label: readerFocusCopy("自动恢复中", "Recovering"), note: readerFocusCopy("上游暂时不稳定，系统会自动恢复，不需要重复点击。", "The upstream is temporarily unstable. Recovery is automatic; no repeated clicks are needed.") };
      default:
        return { label: readerFocusCopy("排队中", "Queued"), note: readerFocusCopy("概览已进入队列，原文可以继续阅读。", "The overview is queued; you can continue reading the original.") };
    }
  }

  function articleAIOverviewErrorMessage(errorCode) {
    switch (text(errorCode)) {
      case "ai_unavailable":
        return readerFocusCopy("应用 AI 能力暂不可用，请确认已启用 AI 且设备在线。原文仍可阅读。", "App AI is unavailable. Check that AI is enabled and the device is online. You can still read the article.");
      case "ai_credits_insufficient":
        return readerFocusCopy("AI 额度不足，本次未完成。原文仍可阅读。", "Insufficient AI credits. You can still read the article.");
      case "ai_busy":
      case "ai_rate_limited":
        return readerFocusCopy("AI 正忙，请稍后手动重试。", "AI is busy. Try again manually later.");
      case "ai_timeout":
        return readerFocusCopy("本次 AI 处理超时，未自动重试。", "This AI request timed out and was not automatically retried.");
      case "ai_input_too_large":
        return readerFocusCopy("文章超过本次 AI 处理长度上限，原文仍可阅读。", "The article exceeds the AI input limit. You can still read it.");
      case "ai_content_blocked":
      case "ai_forbidden":
        return readerFocusCopy("本次内容无法进行 AI 处理。", "AI processing is unavailable for this content.");
      case "article_content_unavailable":
        return readerFocusCopy("这篇文章暂时没有可供分析的正文或摘要，请稍后重试。", "This article has no body or summary available for analysis. Try again later.");
      case "article_changed":
        return readerFocusCopy("文章内容刚刚更新，请重新打开文章后重试。", "The article changed just now. Reopen it and try again.");
      case "invalid_response":
      case "ai_invalid_response":
      case "ai_upstream_error":
        return readerFocusCopy("AI 返回结果暂时不可用，请稍后重试。", "The AI result is temporarily unavailable. Try again later.");
      default:
        return readerFocusCopy("本次提炼没有完成。", "This extraction did not complete.");
    }
  }

  // An article id is stable across feed refreshes; the content hash is the
  // revision. Keep generation state revision-scoped so a new article version
  // is never blocked by an older attempt or failure.
  function articleOverviewKey(item) {
    const entryID = text(item?.id);
    return entryID ? `${entryID}\u0000${text(item?.content_hash)}` : "";
  }

  function scheduleArticleAIOverviewPoll(entry, delayMs = 1500) {
    const overviewKey = articleOverviewKey(entry?.item);
    if (!overviewKey || parity.articleOverviewPollTimers.has(overviewKey)) return;
    const timer = window.setTimeout(() => {
      parity.articleOverviewPollTimers.delete(overviewKey);
      if (state.selected === entry.key) void requestArticleAIOverview(entry);
    }, delayMs);
    parity.articleOverviewPollTimers.set(overviewKey, timer);
  }

  function cancelArticleAIOverviewPoll(overviewKey) {
    const timer = parity.articleOverviewPollTimers.get(overviewKey);
    if (timer) window.clearTimeout(timer);
    parity.articleOverviewPollTimers.delete(overviewKey);
  }

  function installArticleAIOverview(entry) {
    if (!entry || entry.kind !== "news" || !entry.item) return;
    const panel = $("detail-panel");
    if (!panel || panel.querySelector("[data-article-ai-overview]")) return;
    const markup = articleAIOverviewMarkup(entry.item);
    if (!markup) return;
    const anchor = panel.querySelector(".detail-top");
    anchor?.insertAdjacentHTML("afterend", markup);
    const overviewKey = articleOverviewKey(entry.item);
    if (entry.item.ai_overview?.status === "pending") {
      scheduleArticleAIOverviewPoll(entry);
      return;
    }
    // Legacy detail contract: !xDetail && !isYouTube && ["news", "evidence"].includes(entry.kind)
    // Legacy detail contract: if (!xDetail && !isYouTube && ["news", "evidence"].includes(entry.kind))
    if (
      !articleProjectionLooksBlocked(entry.item) &&
      !entry.item.ai_overview &&
      parity.settingsResolved &&
      parity.settings?.auto_generate_article_overviews === true &&
      !parity.articleOverviewAttempted.has(overviewKey) &&
      !parity.articleOverviewErrors.has(overviewKey)
    ) {
      parity.articleOverviewAttempted.add(overviewKey);
      scheduleArticleAIOverviewPoll(entry, 0);
    }
  }

  function refreshArticleAIOverviewSection(entry) {
    if (!entry || state.selected !== entry.key) return;
    const panel = $("detail-panel");
    if (!panel) return;
    const markup = articleAIOverviewMarkup(entry.item);
    const current = panel.querySelector("[data-article-ai-overview]");
    if (current) {
      if (markup) current.outerHTML = markup;
      else current.remove();
      return;
    }
    if (markup) panel.querySelector(".detail-top")?.insertAdjacentHTML("afterend", markup);
  }

  function selectedNewsEntry() {
    return (
      state.selected &&
      makeEntries(state.dashboard || {}).find(
        (item) => item.key === state.selected && item.kind === "news"
      )
    );
  }

  function applyArticleAIOverview(entry, overview) {
    if (!entry?.item?.id || !overview) return false;
    // A request can finish after a feed refresh. Only attach a result to the
    // exact entry revision that was sent to the model.
    if (
      text(overview.entry_id) !== text(entry.item.id) ||
      text(overview.content_hash) !== text(entry.item.content_hash)
    )
      return false;
    const news = state.dashboard?.news || [];
    const item = news.find((candidate) => candidate.id === entry.item.id);
    if (!item || text(item.content_hash) !== text(entry.item.content_hash))
      return false;
    item.ai_overview = overview;
    return true;
  }

  async function requestArticleAIOverview(entry, force = false) {
    const overviewKey = articleOverviewKey(entry?.item);
    if (!overviewKey || parity.articleOverviewLoading.has(overviewKey)) return;
    const entryID = entry.item.id;
    parity.articleOverviewErrors.delete(overviewKey);
    parity.articleOverviewLoading.add(overviewKey);
    refreshArticleAIOverviewSection(entry);
    try {
      const result = await jsonRequest("./api/entries/ai-overview", "POST", {
        entry_id: entryID,
        ...(force ? { force: true } : {}),
	      }, 20000);
      const overview = result?.overview || result;
      if (
        !overview ||
        !["pending", "ready", "failed"].includes(overview.status)
      )
        throw new Error("概览结果格式不完整");
      if (!applyArticleAIOverview(entry, overview))
        throw new Error("文章内容已更新，请重新打开后生成概览");
      if (overview.status === "failed")
        parity.articleOverviewErrors.set(overviewKey, "generation_failed");
      else parity.articleOverviewErrors.delete(overviewKey);
      refreshArticleAIOverviewSection(entry);
      if (overview.status === "ready") {
        cancelArticleAIOverviewPoll(overviewKey);
        showToast("文章 AI 概览已生成");
      } else if (overview.status === "failed") {
        cancelArticleAIOverviewPoll(overviewKey);
      } else {
        scheduleArticleAIOverviewPoll(entry);
      }
    } catch (error) {
      const pendingAfterRequestFailure =
        entry.item.ai_overview?.status === "pending";
      if (pendingAfterRequestFailure) {
        parity.articleOverviewErrors.delete(overviewKey);
        scheduleArticleAIOverviewPoll(entry, 2500);
      } else {
        parity.articleOverviewErrors.set(overviewKey, "generation_failed");
        showError(error.message || "文章概览暂时生成失败");
      }
      refreshArticleAIOverviewSection(entry);
    } finally {
      parity.articleOverviewLoading.delete(overviewKey);
      refreshArticleAIOverviewSection(entry);
    }
  }

  function showXVideoFallback(video) {
    const shell = video.closest("[data-x-media-shell]");
    const fallback = shell?.querySelector(".x-media-fallback");
    if (!fallback) return;
    video.hidden = true;
    fallback.hidden = false;
  }

  function setMobileNavigation(open) {
    const enabled = open && window.matchMedia("(max-width: 640px)").matches;
    document.body.classList.toggle("mobile-nav-open", enabled);
    const toggle = $("mobile-nav-toggle");
    if (!toggle) return;
    const label = enabled ? "关闭导航" : "打开导航";
    toggle.setAttribute("aria-expanded", String(enabled));
    toggle.setAttribute("aria-label", label);
    toggle.title = label;
  }

  function closeMobileNavigation() {
    setMobileNavigation(false);
  }

  function toggleMobileNavigation() {
    setMobileNavigation(!document.body.classList.contains("mobile-nav-open"));
  }

  function installNavigation() {
    const libraryLabels = {
      all: t("allContent"),
      rss: t("subscribedArticles"),
      video: t("videos"),
      meeting: t("meetingsAndRecordings"),
      obsidian: "Obsidian",
    };
    const groups = document.querySelectorAll("#view-nav .nav-group-label");
    if (groups[1]) groups[1].textContent = t("library");
    for (const [view, label] of Object.entries(libraryLabels)) {
      const node = document.querySelector(`[data-view="${view}"] .label`);
      if (node) node.textContent = label;
    }
    const barActions = document.querySelector(".bar-actions");
    if (barActions && !$("mobile-nav-toggle")) {
      barActions.insertAdjacentHTML(
        "afterbegin",
        `<button class="icon-btn mobile-nav-toggle" id="mobile-nav-toggle" type="button" aria-label="打开导航" aria-controls="sidebar" aria-expanded="false" title="打开导航">${icon(
          "menu"
        )}</button>`
      );
      document.body.insertAdjacentHTML(
        "beforeend",
        '<button class="mobile-nav-backdrop" id="mobile-nav-backdrop" type="button" aria-label="关闭导航"></button>'
      );
      $("mobile-nav-toggle").addEventListener("click", toggleMobileNavigation);
      $("mobile-nav-backdrop").addEventListener("click", closeMobileNavigation);
      $("sidebar").addEventListener("click", (event) => {
        if (
          event.target.closest("[data-view]") ||
          event.target.closest("#sidebar-add")
        )
          closeMobileNavigation();
      });
      window.addEventListener("resize", () => {
        applyReaderWidths();
        if (window.innerWidth > 640) closeMobileNavigation();
      });
    }
    const starred = document.querySelector('[data-view="starred"]');
    if (starred && !document.querySelector('[data-view="later"]')) {
      starred.insertAdjacentHTML(
        "afterend",
        '<button class="nav-item" type="button" data-view="later">' +
          icon("inbox") +
          '<span class="label">' + t("readLater") + '</span><span class="count" data-count="later">0</span></button>'
      );
      const later = document.querySelector('[data-view="later"]');
      later.addEventListener("click", () => {
        state.view = "later";
        state.selected = null;
        document.body.classList.remove("detail-open");
        render();
      });
    }

    const later = document.querySelector('[data-view="later"]');
    if (later && !document.querySelector('[data-view="context"]')) {
      later.insertAdjacentHTML(
        "afterend",
        '<button class="nav-item" type="button" data-view="context">' +
          icon("check") +
          '<span class="label">' + t("knowledgeBase") + '</span><span class="count" data-count="context">0</span></button>'
      );
      later.nextElementSibling.addEventListener("click", () => {
        state.view = "context";
        state.selected = null;
        document.body.classList.remove("detail-open");
        render();
      });
    }

    const searchRow = document.querySelector(".search-row");
    if (searchRow && !$("reader-toolbar")) {
      searchRow.insertAdjacentHTML(
        "beforeend",
        '<div class="reader-toolbar" id="reader-toolbar">' +
          '<button class="icon-btn" id="mark-visible-read" type="button" aria-label="将当前列表标为已读" title="将当前列表标为已读">' +
          icon("check") +
          '</button><button class="icon-btn" id="toggle-view-mode" type="button" aria-label="切换列表密度" title="切换列表密度">' +
          icon("layout") +
          '</button><button class="icon-btn" id="toggle-scroll-read" type="button" aria-label="滚动时标记已读：关" title="滚动时标记已读：关">' +
          icon("eye") +
          '</button><button class="icon-btn shortcuts-btn" id="show-shortcuts" type="button" aria-label="键盘快捷键" title="键盘快捷键（?）">?</button></div>'
      );
      $("mark-visible-read").addEventListener(
        "click",
        () => void markVisibleRead()
      );
      $("toggle-view-mode").addEventListener(
        "click",
        () => void toggleViewMode()
      );
      $("toggle-scroll-read").addEventListener(
        "click",
        () => void toggleScrollRead()
      );
      $("show-shortcuts").addEventListener(
        "click",
        () => toggleShortcutsOverlay()
      );
      syncScrollReadButton();
    }
    const queueContentSelection = (event) => {
      const button = event.target.closest("[data-view]");
      // The source list also contains controls that are not navigation
      // targets (for example group toggles).  Ignore those clicks entirely:
      // advancing the token here would invalidate an in-flight projection
      // request even though the active view did not change.
      if (!button) return;
      const view = text(button.dataset.view);
      if (!view) return;
      const navigationToken = ++parity.navigationToken;
      if (
        ["all", "rss", "starred", "later", "context"].includes(view) ||
        view.startsWith("source:")
      )
        parity.autoSelectEntry = true;
      if (view.startsWith("source:")) loadSourceOnDemand(view.slice(7));
      if (["all", "rss", "video", "youtube", "picture"].includes(view))
        // Let the base click handler commit state.view first; renderTimeline
        // then starts the view-specific request with the new target.
        window.setTimeout(() => {
          // The click handler runs in capture phase, while the base page
          // commits state.view in the bubble phase.  Check both the current
          // view and the capture token before starting the deferred request.
          if (
            navigationToken !== parity.navigationToken ||
            state.view !== view
          )
            return;
          void loadAggregateEntriesOnDemand(view, navigationToken);
        }, 0);
      if (liveDeviceDataView(view))
        window.setTimeout(
          () => {
            if (
              navigationToken !== parity.navigationToken ||
              state.view !== view
            )
              return;
            scheduleLiveDeviceDataRefresh(false, false, view);
          },
          0
        );
    };
    $("view-nav")?.addEventListener("click", queueContentSelection, {
      capture: true,
    });
    $("source-list")?.addEventListener("click", queueContentSelection, {
      capture: true,
    });
  }

  function rebuildModal() {
    const modal = $("discover-modal");
    if (!modal || modal.querySelector(".parity-modal")) return;
    modal.innerHTML = `
      <section class="modal parity-modal" role="dialog" aria-modal="true" aria-labelledby="discover-title">
        <div class="modal-head">
          <div><h2 id="discover-title">来源</h2><p id="discover-description">公开 RSS / Atom</p></div>
          <button class="icon-btn" id="close-discover" type="button" aria-label="关闭" title="关闭">${icon(
            "close"
          )}</button>
        </div>
        <div class="parity-tabs" role="tablist" aria-label="来源视图">
          <button class="parity-tab active" id="source-tab-discover" type="button" role="tab" aria-controls="parity-content" aria-selected="true" data-parity-tab="discover">发现</button>
          <button class="parity-tab" id="source-tab-subscriptions" type="button" role="tab" aria-controls="parity-content" aria-selected="false" tabindex="-1" data-parity-tab="subscriptions">订阅管理</button>
          <button class="parity-tab" id="source-tab-settings" type="button" role="tab" aria-controls="parity-content" aria-selected="false" tabindex="-1" data-parity-tab="settings">设置</button>
        </div>
        <div class="parity-body" id="parity-content" role="tabpanel" aria-labelledby="source-tab-discover"><div class="empty-inline">正在读取来源</div></div>
      </section>`;
    $("close-discover").addEventListener("click", closeDiscover);
    modal.addEventListener("click", onModalClick);
    modal.addEventListener("change", onModalChange);
    modal.addEventListener("input", onModalInput);
    modal.addEventListener("submit", onModalSubmit);
    modal.addEventListener("keydown", onModalKeydown);
  }

  async function loadParityData(silent = false, minimal = false) {
    if (parity.loading) {
      if (!minimal) parity.pendingCatalogLoad = true;
      return;
    }
    parity.loading = true;
    try {
      const results = await Promise.allSettled([
        // The inline bootstrap owns the lightweight source-directory request.
        // Do not issue a second `/api/sources` call while that request is still
        // settling; the catalog modal will perform the full read when opened.
        minimal
          ? Promise.resolve(
              Array.isArray(state.sources) && state.sources.length
                ? { sources: state.sources }
                : null
            )
          : request("./api/sources"),
        minimal ? Promise.resolve(null) : request("./api/packs"),
        minimal ? Promise.resolve(null) : request("./api/folders"),
        minimal ? Promise.resolve(null) : request("./api/tags"),
        minimal ? Promise.resolve(null) : request("./api/discoveries"),
        minimal ? Promise.resolve(null) : request("./api/sync"),
        minimal
          ? Promise.resolve(null)
          : request("./api/context?membership=1"),
        parity.settingsResolved
          ? Promise.resolve(parity.settings)
          : request("./api/settings").catch(() => null),
        request("./api/source-gateway/status").catch(() => null),
      ]);
      const value = (index) =>
        results[index].status === "fulfilled" ? results[index].value : null;
      const catalogValue = value(0);
      const catalog =
        catalogValue?.data && typeof catalogValue.data === "object" && !Array.isArray(catalogValue.data)
          ? catalogValue.data
          : catalogValue;
      if (catalog) {
        // The modal requests the full source projection with authoritative
        // counts.  Replace the lightweight directory values rather than
        // preserving its zero placeholders. A cold gateway can still return
        // an empty projection while subscriptions are materializing; do not
        // let opening the source modal erase a directory that the reader has
        // already rendered.
        const currentSources = Array.isArray(state.sources) ? state.sources : [];
        const nextSources = Array.isArray(catalog.sources) ? catalog.sources : [];
        if (nextSources.length || !currentSources.length)
          state.sources = window.__plaudMergeSourceCatalogCounts
            ? window.__plaudMergeSourceCatalogCounts(
                currentSources,
                nextSources,
                true
              )
            : nextSources;
        parity.catalogOrigin = catalog.catalog_origin || "";
        parity.catalogPolicy = catalog.catalog_policy || "";
        parity.trendPolicy = catalog.trend_policy || "";
        parity.popularityAvailable = catalog.popularity_available === true;
      }
      const packs = value(1);
      if (packs) parity.packs = packs.packs || [];
      const folders = value(2);
      if (folders) parity.folders = folders.folders || [];
      const tags = value(3);
      if (tags) parity.tags = tags.tags || [];
      const discoveries = value(4);
      if (discoveries) parity.discoveries = discoveries.discoveries || [];
      const sync = value(5);
      if (sync) parity.syncRequests = sync.requests || [];
      const context = value(6);
      if (context) {
        parity.contextMemberships = new Map(
          (context.items || []).map((item) => [item.item_key, item])
        );
        parity.contextMutationKeys.clear();
        parity.contextMembershipsReady = true;
        rebindReaderNewsProjection();
      }
      const settings = value(7);
      if (settings !== null) {
        parity.settings = settings;
        parity.settingsResolved = true;
      }
      // Source Gateway health (V1): when the cloud path is down the reader
      // keeps showing the last synced local snapshot; the banner explains the
      // state instead of presenting it as "no content".
      const gateway = value(8);
      if (gateway && typeof gateway === "object") {
        parity.gatewayStatus = gateway.data || gateway;
      }
      render();
      const failedCount = results.filter(
        (result) => result.status === "rejected"
      ).length;
      if (!silent && failedCount)
        showError("部分来源设置暂时未载入，可继续浏览或重试");
    } finally {
      parity.loading = false;
      if (parity.pendingCatalogLoad) {
        parity.pendingCatalogLoad = false;
        void loadParityData(true, false);
      }
    }
  }

  function sourceLastRequest(sourceID) {
    return parity.syncRequests.find((item) => item.source_id === sourceID);
  }

  function selectedCatalogSource() {
    if (!state.view.startsWith("source:")) return null;
    const sourceID = state.view.slice(7);
    return (
      (state.sources || []).find((source) => source.id === sourceID) || null
    );
  }

  function gatewaySourcePreparing(source) {
    // A gateway-managed source that has not produced a first successful page
    // yet is "preparing": the UI shows a pending/skeleton state and never a
    // bare 0 that would read as "this source has no content".
    return (
      source?.collection_mode === "source_gateway" &&
      sourceLastRequest(source.id)?.status === "pending" &&
      !Number(source?.entry_count || 0) &&
      ["", "initializing"].includes(text(source?.gateway_status))
    );
  }

  function sourceEmptyMarkup(source) {
    const requestState = sourceLastRequest(source.id);
    const pending =
      requestState?.status === "pending" ||
      lazySourceLoading.has(source.id) ||
      gatewaySourcePreparing(source);
    if (pending) return timelineLoadingMarkup("scope");
    const failed =
      ["retry_wait", "agent_required", "failed"].includes(
        requestState?.status
      ) || source.status === "unavailable";
    const title = pending
      ? gatewaySourcePreparing(source)
        ? `${source.name} ${t("gatewayPreparing")}`
        : `正在同步 ${source.name}`
      : `${source.name} 暂无本地快照`;
    const detail = pending
      ? "首次同步完成后，文章会自动出现在这里"
      : failed
      ? `该来源暂时无法连接，订阅已保留。${text(requestState?.error_message || source.last_error).slice(0, 500) || "请稍后重试"}`
      : "打开页面不会触发网络抓取；点击刷新后，服务端会在后台同步，完成后内容会自动出现";
    const retry = pending
      ? ""
      : `<button class="primary-btn" type="button" data-retry-source="${esc(
          source.id
        )}">立即重试</button>`;
    return `<div class="empty-state">${icon("inbox")}<strong>${esc(
      title
    )}</strong><p>${esc(detail)}</p>${retry}</div>`;
  }

  function sourceEntryErrorMarkup(source, error) {
    const message = text(error, readerFocusCopy("来源内容暂时读取失败", "Source content is temporarily unavailable")).slice(0, 240);
    return `<div class="empty-inline aggregate-entry-error source-entry-error" role="alert"><strong>${esc(readerFocusCopy("来源内容暂时读取失败", "Source content is temporarily unavailable"))}</strong><span>${esc(message)}</span><button class="text-btn" type="button" data-retry-source-entry="${esc(source?.id)}">${icon("refresh")} ${esc(readerFocusCopy("重试", "Retry"))}</button></div>`;
  }

  function sourceByID(sourceID) {
    return (state.sources || []).find((item) => item.id === sourceID) || null;
  }


  function monitorSourceSync(sourceID) {
    if (parity.syncMonitors.has(sourceID))
      return parity.syncMonitors.get(sourceID);
    const task = (async () => {
      const startedAt = Date.now();
      let pollDelay = 500;
      while (Date.now() - startedAt < sourceSyncMonitorMaxWaitMs) {
        await new Promise((resolve) => window.setTimeout(resolve, pollDelay));
        const sync = await request("./api/sync").catch(() => null);
        if (sync) {
          parity.syncRequests = sync.requests || [];
          const current = sourceLastRequest(sourceID);
          if (current && current.status !== "pending") {
            // Sync completion updates only the selected source projection and
            // the independent counters. A full dashboard/catalog refresh here
            // used to rebuild every panel, reset scroll, and leave the active
            // source page showing the empty pre-sync snapshot.
            await refreshSourceMetadataAndCounts(true);
            const source = (state.sources || []).find(
              (item) => item.id === sourceID
            );
            if (current.status === "ok" || current.status === "partial") {
              // The first source-scoped read can legitimately finish while the
              // sync request is still pending, so it may have cached an empty
              // page. Refreshing the dashboard/catalog above does not re-run
              // that typed source query. When the user is still viewing this
              // source, invalidate that empty snapshot and fetch one fresh page.
              await reloadSourceScopeAfterSync(sourceID);
              showToast(`${source?.name || readerFocusCopy("来源", "Source")} ${readerFocusCopy("已同步", "synced")}`);
            } else {
              // A terminal failure should not make the explicit retry button
              // wait for the automatic 60-second hydration throttle.
              lazySourceLoadAt.delete(sourceID);
              lazySourceLoading.delete(sourceID);
              if (state.view === `source:${sourceID}`) renderTimeline();
              showError(
                `${source?.name || readerFocusCopy("来源", "Source")} ${readerFocusCopy("暂时无法连接，订阅已保留并会稍后重试", "is temporarily unavailable. The subscription is kept and will retry later.")}`
              );
            }
            return current;
          }
          renderTimeline();
        }
        pollDelay = Math.min(
          Math.round(pollDelay * 1.5),
          sourceSyncMonitorMaxPollMs
        );
      }
      await refreshSourceMetadataAndCounts(true);
      lazySourceLoading.delete(sourceID);
      if (state.view === `source:${sourceID}`) renderTimeline();
      showToast(readerFocusCopy("来源仍在后台同步，可以继续浏览其他内容", "The source is still syncing in the background. You can keep browsing."));
      return null;
    })().finally(() => {
      parity.syncMonitors.delete(sourceID);
      lazySourceLoading.delete(sourceID);
    });
    parity.syncMonitors.set(sourceID, task);
    return task;
  }

  function statusBadge(source) {
    if (!source.subscribed) return badge("未订阅");
    if (parity.sourceBusy.has(source.id)) return badge("提交中", "amber");
    const requestState = sourceLastRequest(source.id);
    if (requestState?.status === "pending") return badge(readerFocusCopy("已订阅 · 同步中", "Subscribed · Syncing"), "amber");
    if (requestState?.status === "retry_wait") return badge(readerFocusCopy("已订阅 · 等待重试", "Subscribed · Waiting to retry"), "red");
    if (["failed", "agent_required"].includes(requestState?.status) || source.status === "unavailable") return badge("已订阅 · 同步失败", "red");
    const cachedCount = knownCount(parity.counts?.by_source?.[source.id]?.total_entries) ?? knownCount(source.entry_count);
    if (cachedCount > 0) return badge("已订阅 · 已有缓存", "green");
    return badge(readerFocusCopy("已订阅 · 暂未获取内容", "Subscribed · No cached content yet"), "amber");
  }

  function classifyDiscoverInput(raw) {
    const value = text(raw);
    if (!value)
      return {
        kind: "empty",
        label: "关键词 / 网站 / Feed / RSSHub",
        valid: false,
        normalized: "",
      };
    const lower = value.toLowerCase();
    if (lower.startsWith("rsshub:") || value.startsWith("/")) {
      let route = value
        .replace(/^rsshub:\/\//i, "")
        .replace(/^rsshub:/i, "")
        .replace(/^\/+/, "");
      if (
        !route ||
        route.includes("..") ||
        route.includes("\\") ||
        route.includes("//")
      ) {
        return {
          kind: "invalid",
          label: "RSSHub route 格式不正确",
          valid: false,
          normalized: "",
        };
      }
      return {
        kind: "rsshub_route",
        label: "RSSHub route",
        valid: true,
        normalized: `https://rsshub.app/${route}`,
      };
    }
    try {
      const url = new URL(value);
      if (!publicURL(value))
        return {
          kind: "invalid",
          label: "地址含私网、凭据或敏感参数",
          valid: false,
          normalized: "",
        };
      const path = url.pathname.toLowerCase();
      const feed =
        /\.(rss|xml|atom|json)$/.test(path) ||
        ["/feed", "/feeds/", "/rss", "/atom", "/podcast"].some((marker) =>
          path.includes(marker)
        );
      const kind =
        url.hostname.toLowerCase() === "rsshub.app"
          ? "rsshub_route"
          : feed
          ? "feed_url"
          : "website_url";
      return {
        kind,
        label:
          kind === "feed_url"
            ? "Feed 地址"
            : kind === "rsshub_route"
            ? "RSSHub route"
            : "网站地址",
        valid: true,
        normalized: value,
      };
    } catch {
      if (
        !/\s/.test(value) &&
        (value.startsWith("www.") ||
          (value.includes(".") && value.includes("/")))
      ) {
        return {
          kind: "invalid",
          label: "网址需以 http:// 或 https:// 开头",
          valid: false,
          normalized: "",
        };
      }
      return {
        kind: "keyword",
        label: "目录关键词",
        valid: value.length <= 120,
        normalized: value,
      };
    }
  }

  function sourceActivity(source) {
    const total = parity.counts?.source_counts_available ? knownCount(source.entry_count) : null;
    const unread = sourceUnreadTotal(source);
    return `共 ${total ?? "—"} 条 · 未读 ${unread ?? "—"} 条${parity.countsStale ? " · 数字暂未更新" : ""}`;
  }

  function uniqueCatalogSources(sources) {
    const rows = new Map();
    for (const source of sources) {
      let identity = source.id;
      // Only normalize actual feed addresses; a shared website homepage is
      // not evidence that different channels are the same feed.
      const feedURL = source.feed_url || (source.feed_format || /^(rss|rsshub|atom)$/i.test(source.kind || "") ? source.url : "");
      if (feedURL) try {
        const url = new URL(feedURL);
        if (!["http:", "https:"].includes(url.protocol)) throw new Error("not a feed address");
        url.hash = "";
        url.searchParams.sort();
        identity = url.href;
      } catch {}
      const previous = rows.get(identity);
      if (!previous || (!previous.subscribed && source.subscribed)) rows.set(identity, source);
    }
    return [...rows.values()];
  }

  const sourceIconLoadLimit = 2;
  const sourceIconBatchLimit = 8;
  const sourceIconQueueLimit = 8;
  const sourceIconQueue = [];
  const queuedSourceIcons = new WeakSet();
  const sourceIconWatchdogs = new WeakMap();
  let sourceIconActive = 0;
  let sourceIconBudget = sourceIconBatchLimit;
  let sourceIconScanScheduled = false;
  let sourceIconBudgetRefillScheduled = false;
  let sourceIconScrollBound = false;

  function finishSourceIconRequest(image) {
    if (image?.dataset.sourceIconRequestActive !== "1") return;
    delete image.dataset.sourceIconRequestActive;
    const watchdog = sourceIconWatchdogs.get(image);
    if (watchdog) window.clearTimeout(watchdog);
    sourceIconWatchdogs.delete(image);
    sourceIconActive = Math.max(0, sourceIconActive - 1);
    pumpSourceIconQueue();
  }

  function pumpSourceIconQueue() {
    while (sourceIconActive < sourceIconLoadLimit && sourceIconQueue.length) {
      const job = sourceIconQueue.shift();
      const image = job?.image;
      if (image) queuedSourceIcons.delete(image);
      if (
        !(image instanceof HTMLImageElement) ||
        !image.isConnected ||
        !job.url ||
        image.dataset.sourceIconRequestActive === "1"
      )
        continue;
      image.dataset.sourceIconRequestActive = "1";
      image.classList.remove("is-loaded");
      sourceIconActive++;
      // The endpoint has its own bounded timeout. This watchdog only prevents
      // a detached WebView image from permanently occupying a client slot.
      sourceIconWatchdogs.set(
        image,
        window.setTimeout(() => finishSourceIconRequest(image), 30000)
      );
      image.src = job.url;
    }
  }

  function queueSourceIconRequest(image, url, priority = false) {
    if (
      !(image instanceof HTMLImageElement) ||
      !image.isConnected ||
      !url ||
      queuedSourceIcons.has(image) ||
      image.dataset.sourceIconRequestActive === "1" ||
      (!priority && sourceIconBudget <= 0)
    )
      return false;
    if (sourceIconQueue.length >= sourceIconQueueLimit) {
      if (!priority) return false;
      const displaced = sourceIconQueue.pop();
      if (displaced?.image) queuedSourceIcons.delete(displaced.image);
    }
    if (!priority) sourceIconBudget--;
    queuedSourceIcons.add(image);
    if (priority) sourceIconQueue.unshift({ image, url });
    else sourceIconQueue.push({ image, url });
    pumpSourceIconQueue();
    return true;
  }

  function resetPendingSourceIconObservers() {
    if (!parity.sourceIconObserver) return;
    document
      .querySelectorAll(
        '.source-avatar-image[data-source-icon-observed][data-source-icon-src]:not([data-source-icon-src=""])'
      )
      .forEach((image) => {
        if (
          image.dataset.sourceIconRequestActive === "1" ||
          queuedSourceIcons.has(image)
        )
          return;
        parity.sourceIconObserver.unobserve(image);
        delete image.dataset.sourceIconObserved;
      });
  }

  function scheduleSourceIconBudgetRefill() {
    if (sourceIconBudgetRefillScheduled) return;
    sourceIconBudgetRefillScheduled = true;
    window.setTimeout(() => {
      sourceIconBudgetRefillScheduled = false;
      sourceIconBudget = sourceIconBatchLimit;
      resetPendingSourceIconObservers();
      scheduleSourceIconScan();
    }, 180);
  }

  function observeSourceIcons(root = document) {
    if (!sourceIconScrollBound) {
      document.addEventListener("scroll", scheduleSourceIconBudgetRefill, {
        capture: true,
        passive: true,
      });
      sourceIconScrollBound = true;
    }
    const images = [...root.querySelectorAll(
      '.source-avatar-image[data-source-icon-src]:not([data-source-icon-src=""]):not([data-source-icon-observed])'
    )].sort(
      (left, right) =>
        Number(right.dataset.sourceIconPriority === "1") -
        Number(left.dataset.sourceIconPriority === "1")
    );
    if (!images.length) return;
    if (typeof window.IntersectionObserver !== "function") {
      images.forEach((image) => {
        if (queueSourceIconRequest(image, text(image.dataset.sourceIconSrc)))
          image.dataset.sourceIconObserved = "1";
      });
      return;
    }
    if (!parity.sourceIconObserver) {
      parity.sourceIconObserver = new IntersectionObserver(
        (entries, observer) => {
          for (const entry of entries) {
            if (!entry.isIntersecting) continue;
            const image = entry.target;
            if (
              queueSourceIconRequest(
                image,
                text(image.dataset.sourceIconSrc),
                image.dataset.sourceIconPriority === "1"
              )
            ) {
              observer.unobserve(image);
            }
          }
        },
        { rootMargin: "80px 0px", threshold: 0.01 }
      );
    }
    images.forEach((image) => {
      image.dataset.sourceIconObserved = "1";
      parity.sourceIconObserver.observe(image);
    });
  }

  function scheduleSourceIconScan(resetBudget = false) {
    if (resetBudget) sourceIconBudget = sourceIconBatchLimit;
    if (sourceIconScanScheduled) return;
    sourceIconScanScheduled = true;
    const scan = () => {
      sourceIconScanScheduled = false;
      observeSourceIcons();
    };
    if (typeof window.requestIdleCallback === "function") {
      window.requestIdleCallback(scan, { timeout: 1000 });
    } else {
      window.setTimeout(scan, 120);
    }
  }

  function retrySourceIcon(image) {
    finishSourceIconRequest(image);
    const retry = text(image.dataset.retrySrc);
    if (retry && !image.dataset.retryAttempted) {
      image.dataset.retryAttempted = "1";
      window.setTimeout(
        () =>
          queueSourceIconRequest(
            image,
            retry.split("#", 1)[0] + `&retry=${Date.now()}`,
            true
          ),
        350
      );
      return;
    }
    const brandFallback = text(image.dataset.brandFallbackSrc);
    if (
      brandFallback &&
      !image.dataset.brandFallbackAttempted &&
      image.src !== brandFallback
    ) {
      image.dataset.brandFallbackAttempted = "1";
      queueSourceIconRequest(image, brandFallback, true);
      return;
    }
    const fallback = text(image.dataset.fallbackSrc);
    if (fallback && image.src !== fallback) {
      image.src = fallback;
    } else {
      image.hidden = true;
    }
  }

  function sourceAvatar(source, extraClass = "") {
    const label =
      text(source?.icon_text) || text(source?.name, "RSS").slice(0, 1);
    const color = text(source?.icon_color, "#64748b");
    // Every source uses the same app-owned resolver. It discovers, validates,
    // caches and proxies public icons; the page never needs source-specific
    // remote-image rules.
    const provider = text(source?.feed_format || source?.kind).toLowerCase();
    const isYouTube =
      text(source?.kind).toLowerCase() === "youtube" ||
      provider === "youtube";
    const isVideoCreator = isYouTube || provider === "bilibili";
    const isXCreator = text(source?.id).toLowerCase().endsWith("-x");
    const iconRevision = "&v=4";
    // Older catalog rows may not carry has_icon yet. YouTube still goes
    // through the app-owned resolver so a real channel portrait can win
    // before the provider and project fallbacks.
    const remoteIcon = text(source?.id) && (isVideoCreator || source?.has_icon)
      ? `./api/sources/icon?id=${encodeURIComponent(text(source.id))}${iconRevision}`
      : "";
    const retryIcon = remoteIcon ? `${remoteIcon}#retry` : "";
    const brandFallback = isVideoCreator ? "" : isYouTube ? youtubeBrandIconURL : "";
    const projectFallback = isVideoCreator ? "" : projectLogoDataURL;
    const backgroundIcon = remoteIcon || brandFallback || projectFallback;
    // The built markup must stay byte-identical across icon-state transitions
    // (pending → loaded → failed). Baking the cached absolute src / is-loaded
    // class back into the string changes it after every icon settles, so the
    // nav memo guard fires and the whole sidebar innerHTML — including every
    // <img> — is destroyed and re-created on the next render (visible flash
    // while switching menus). Keep the deferred queue URL as the only baked
    // form; the runtime load handler owns src/is-loaded, and rebuilt nodes
    // re-hydrate from the browser cache through the same bounded queue.
    const deferredIcon = isVideoCreator
      ? ""
      : backgroundIcon === projectLogoDataURL
        ? ""
        : backgroundIcon;
    // Creator portraits are primary identity metadata, not decorative
    // background work. Start their app-local cached endpoint immediately;
    // every other source remains on the bounded idle/visibility queue.
    const priorityIcon = isVideoCreator || isXCreator;
    const immediateIcon = priorityIcon ? remoteIcon : "";
    const realIcon = `<img class="source-avatar-image" data-source-icon-id="${esc(
      text(source?.id)
    )}" data-source-icon-src="${esc(
      deferredIcon
    )}"${immediateIcon ? ` src="${esc(immediateIcon)}"` : ""} data-retry-src="${esc(
      retryIcon
    )}" data-brand-fallback-src="${esc(
      brandFallback
    )}" data-fallback-src="${esc(
      projectFallback
    )}"${priorityIcon ? ' data-source-icon-priority="1"' : ""} alt="" loading="lazy" decoding="async" fetchpriority="low" aria-hidden="true">`;
    const fallbackLabel = isVideoCreator ? "" : label;
    const markup = `<span class="source-avatar ${esc(
      `${extraClass}${isVideoCreator ? " video-creator-avatar" : ""}`.trim()
    )}" style="--avatar:${esc(
      color
    )}" aria-hidden="true"><span class="source-avatar-fallback">${esc(
      fallbackLabel
    )}</span>${realIcon}</span>`;
    scheduleSourceIconScan(true);
    return markup;
  }

  function sourceGroupStorageKey(group) {
    return `memo.reader.source-group.${encodeURIComponent(text(group, "订阅"))}.collapsed`;
  }

  function sourceGroupCollapsed(group) {
    try {
      return localStorage.getItem(sourceGroupStorageKey(group)) === "1";
    } catch {
      return false;
    }
  }

  function toggleSourceGroup(group) {
    const key = sourceGroupStorageKey(group);
    const next = !sourceGroupCollapsed(group);
    try {
      localStorage.setItem(key, next ? "1" : "0");
    } catch {
      // The rendered state is still refreshed for private browsing modes.
    }
    renderSources();
  }

  window.__toggleReaderSourceGroup = toggleSourceGroup;

  function normalizeObsidianPath(value) {
    return text(value).replace(/^\/+|\/+$/g, "");
  }

  function obsidianPathParent(path) {
    const normalized = normalizeObsidianPath(path);
    const slash = normalized.lastIndexOf("/");
    return slash < 0 ? "" : normalized.slice(0, slash);
  }

  function obsidianPathMatches(path, prefix) {
    const child = normalizeObsidianPath(path);
    const parent = normalizeObsidianPath(prefix);
    return !parent || child === parent || child.startsWith(parent + "/");
  }

  function obsidianFolderView(path) {
    return `obsidian-folder:${encodeURIComponent(normalizeObsidianPath(path))}`;
  }

  function obsidianFolderFromView(view = state.view) {
    if (!text(view).startsWith("obsidian-folder:")) return "";
    try {
      return normalizeObsidianPath(decodeURIComponent(text(view).slice(16)));
    } catch {
      return "";
    }
  }

  function obsidianFolderStorageKey(path) {
    return `memo.reader.obsidian-folder.${encodeURIComponent(
      normalizeObsidianPath(path)
    )}.collapsed`;
  }

  function obsidianFolderCollapsed(path) {
    try {
      const stored = localStorage.getItem(obsidianFolderStorageKey(path));
      if (stored !== null) return stored === "1";
    } catch {
      // Fall through to the same bounded default used without storage.
    }
    const normalized = normalizeObsidianPath(path);
    if (!normalized) return false;
    const selected = makeEntries(state.dashboard || {}).find(
      (entry) => entry.key === state.selected && entry.group === "obsidian"
    );
    const selectedPath = normalizeObsidianPath(
      selected?.item?.locator || selected?.item?.path
    );
    if (selectedPath && obsidianPathMatches(selectedPath, normalized)) return false;
    return true;
  }

  async function toggleObsidianFolder(path) {
    const normalized = normalizeObsidianPath(path);
    const next = !obsidianFolderCollapsed(normalized);
    try {
      localStorage.setItem(obsidianFolderStorageKey(normalized), next ? "1" : "0");
    } catch {
      // The tree is still rerendered for private browsing modes.
    }
    // Render the expanded row immediately, then hydrate only that folder.
    // A vault can contain hundreds of directories; walking the complete tree
    // on every entry made the Reader issue one request per directory.
    renderTimeline();
    if (!next) {
      try {
        const changed = await loadLiveObsidianFolder(normalized);
        if (changed && state.view === "obsidian") renderTimeline();
      } catch (error) {
        showError(error?.message || "Obsidian 文件夹暂时读取失败");
        renderTimeline();
      }
    }
  }

  window.__toggleReaderObsidianFolder = toggleObsidianFolder;

  function obsidianTreeData(entries) {
    const folders = (Array.isArray(state.liveObsidianFolders)
      ? state.liveObsidianFolders
      : []
    )
      .map((folder) => ({
        name: text(folder.name, normalizeObsidianPath(folder.path).split("/").pop()),
        path: normalizeObsidianPath(folder.path),
        parent_path: normalizeObsidianPath(
          folder.parent_path || obsidianPathParent(folder.path)
        ),
      }))
      .filter((folder) => folder.path);
    const notes = (Array.isArray(entries) ? entries : [])
      .filter((entry) => entry.group === "obsidian")
      .map((entry) => ({
        entry,
        path: normalizeObsidianPath(entry.item?.locator || entry.item?.path),
      }))
      .filter((item) => item.path);
    const foldersByParent = new Map();
    folders.forEach((folder) => {
      if (!foldersByParent.has(folder.parent_path)) foldersByParent.set(folder.parent_path, []);
      foldersByParent.get(folder.parent_path).push(folder);
    });
    const notesByParent = new Map();
    notes.forEach((note) => {
      const parent = obsidianPathParent(note.path);
      if (!notesByParent.has(parent)) notesByParent.set(parent, []);
      notesByParent.get(parent).push(note);
    });
    const compareName = (left, right) =>
      text(left.name || left.entry?.title).localeCompare(
        text(right.name || right.entry?.title),
        "zh-CN",
        { numeric: true, sensitivity: "base" }
      );
    foldersByParent.forEach((items) => items.sort(compareName));
    notesByParent.forEach((items) => items.sort(compareName));
    return { foldersByParent, notesByParent };
  }

  function renderObsidianTree(entries) {
    const tree = obsidianTreeData(entries);
    const notes = (Array.isArray(entries) ? entries : []).filter(
      (entry) => entry.group === "obsidian"
    );
    const folderIcon = (expanded) => expanded
      ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M3.5 8.5h6l2-2h3.8c1 0 1.7.4 2.2 1.2l1 1.3H7.2a2 2 0 0 0-1.9 1.3L3.5 16z"/><path d="M3.5 8.5V6h6l2 2h8v2"/><path d="m5.4 10.5-1.9 7h15.6l1.8-7z"/></svg>'
      : icon("folder");
    const fileIcon = (name) => {
      if (/\.pdf$/i.test(name))
        return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M6 3.5h8l4 4V20H6z"/><path d="M14 3.5V8h4M8.5 12h7M8.5 15h5"/></svg>';
      if (/\.(png|jpe?g|gif|webp)$/i.test(name)) return icon("picture");
      return icon("note");
    };
    const renderNote = ({ entry, path }, depth) => {
      const selected = entry.key === state.selected;
      const name = entry.title || path.split("/").pop() || readerFocusCopy("未命名笔记", "Untitled note");
      return `<button class="obsidian-tree-row obsidian-note-row ${
        selected ? "selected" : ""
      }" type="button" role="treeitem" aria-selected="${String(
        selected
      )}" aria-level="${depth + 1}" data-select-entry="${esc(
        entry.key
      )}" style="--tree-indent:${depth * 16}px"><span class="obsidian-chevron" aria-hidden="true"></span><span class="obsidian-note-icon" aria-hidden="true">${fileIcon(
        name
      )}</span><span class="obsidian-node-name">${esc(name)}</span></button>`;
    };
    const renderFolder = (folder, depth) => {
      if (depth > 32) return "";
      const collapsed = obsidianFolderCollapsed(folder.path);
      const children = tree.foldersByParent.get(folder.path) || [];
      const notes = tree.notesByParent.get(folder.path) || [];
      const loaded = liveDeviceData.obsidianLoadedFolders.has(folder.path);
      const loading = liveDeviceData.obsidianFolderInflight.has(folder.path);
      const hasChildren = !loaded || children.length > 0 || notes.length > 0;
      const childMarkup = hasChildren && !collapsed
        ? children.map((child) => renderFolder(child, depth + 1)).join("") +
          notes.map((note) => renderNote(note, depth + 1)).join("") +
          (!loaded
            ? `<div class="obsidian-tree-loading" style="--tree-indent:${
                (depth + 1) * 16
              }px">${loading ? readerFocusCopy("正在读取…", "Loading…") : readerFocusCopy("展开后读取", "Expand to load")}</div>`
            : "")
        : "";
      return `<div class="obsidian-tree-branch" role="none"><button class="obsidian-tree-row obsidian-folder-row" type="button" role="treeitem" aria-expanded="${String(
        !collapsed
      )}" aria-level="${depth + 1}" data-toggle-obsidian-folder="${esc(
        folder.path
      )}" style="--tree-indent:${depth * 16}px"><span class="obsidian-chevron ${
        collapsed ? "collapsed" : ""
      }" aria-hidden="true">${icon("chevron")}</span><span class="obsidian-folder-icon" aria-hidden="true">${folderIcon(
        !collapsed
      )}</span><span class="obsidian-node-name">${esc(
        folder.name
      )}</span><span class="count">${parity.obsidianCounts.get(folder.path) ?? "—"}</span></button>${childMarkup ? `<div role="group">${childMarkup}</div>` : ""}</div>`;
    };
    const rootChildren = (tree.foldersByParent.get("") || [])
      .map((folder) => renderFolder(folder, 0))
      .join("") + (tree.notesByParent.get("") || []).map((note) => renderNote(note, 0)).join("");
    return `<div class="obsidian-browser"><div class="obsidian-tree-summary"><span>${readerFocusCopy("我的知识库", "My knowledge base")}</span><span>${knownCount(parity.deviceCounts.obsidian) ?? "—"} ${readerFocusCopy("个文件", "files")}</span></div><div class="obsidian-tree" role="tree" aria-label="${readerFocusCopy("我的知识库文件", "Knowledge base files")}">${rootChildren}</div></div>`;
  }

  function sourceCard(source) {
    const tags = (source.catalog_tags || [])
      .slice(0, 3)
      .map((tag) => badge(tag))
      .join("");
    const types = (source.content_types || ["article"])
      .slice(0, 2)
      .map((kind) => badge(contentTypeNames[kind] || kind, "teal"))
      .join("");
    const subscribedTitle = source.subscribed ? "退订" : "订阅";
    return `<article class="directory-card" data-source-card="${esc(
      source.id
    )}">
      ${sourceAvatar(source)}
      <div class="directory-copy">
        <div class="directory-title"><strong>${esc(
          source.title_override || source.name
        )}</strong>${source.recommended ? badge("推荐", "blue") : ""}${
      source.hidden ? badge("已隐藏", "amber") : ""
    }</div>
        <p>${esc(source.description || source.url)}</p><span class="directory-activity">${esc([source.url, source.language].filter(Boolean).join(" · "))}</span>
        <div class="directory-meta">${statusBadge(source)}${badge(
      categoryNames[source.category] || source.category
    )}${types}${tags}</div>
        <span class="directory-activity">${esc(sourceActivity(source))}</span>
      </div>
      <div class="directory-actions">
        <button class="icon-btn" type="button" data-preview-source="${esc(
          source.id
        )}" aria-label="预览${esc(source.name)}" title="预览">${icon(
      "external"
    )}</button>
        <button class="icon-btn ${
          source.subscribed ? "starred" : ""
        }" type="button" data-toggle-source="${esc(
      source.id
    )}" aria-label="${subscribedTitle}${esc(
      source.name
    )}" title="${subscribedTitle}" ${parity.sourceBusy.has(source.id) ? 'disabled aria-busy="true"' : ""}>${
      source.subscribed ? icon("check") : icon("plus")
    }</button>
        <button class="icon-btn" type="button" data-sync-source="${esc(
          source.id
        )}" aria-label="同步${esc(source.name)}" title="同步">${icon(
      "refresh"
    )}</button>
      </div>
    </article>`;
  }

  function sourcePreviewPanel() {
    const result = parity.sourcePreview;
    if (!result) return "";
    const source = (state.sources || []).find(item => item.id === result.source?.id) || result.source || {};
    const items = (result.preview || []).slice(0, 5);
    return `<aside class="source-preview-panel" aria-label="来源预览">
      <div class="source-preview-head"><div>${sourceAvatar(
        source
      )}<div><strong>${esc(source.name)}</strong><p>${esc(
      source.description || source.url
    )}</p></div></div><button class="icon-btn" type="button" data-close-source-preview aria-label="关闭预览" title="关闭预览">${icon(
      "close"
    )}</button></div>
      <div class="source-preview-list">${
        result.loading ? '<div class="empty-inline" role="status">正在读取预览…</div>' : result.error ? `<div class="empty-inline">${esc(result.error)}<button class="text-btn" data-preview-source="${esc(source.id)}">重试预览</button></div>` : items.length
          ? items
              .map(
                (item) =>
                  `<button type="button" data-preview-entry="${esc(
                    item.id
                  )}"><strong>${esc(item.title)}</strong><span>${esc(
                    formatDate(item.published_at)
                  )} · ${esc(
                    contentTypeNames[item.view_type] || contentTypeNames.article
                  )}</span><p>${esc(
                    item.summary_text || "暂无摘要"
                  )}</p></button>`
              )
              .join("")
          : `<div class="empty-inline">${source.subscribed ? "已订阅，暂未获取内容" : "尚未订阅，暂时没有预览"}${source.last_error ? `<p>${esc(text(source.last_error).slice(0, 500))}</p>` : ""}${source.subscribed ? `<button class="text-btn" data-sync-source="${esc(source.id)}">重试同步</button>` : ""}</div>`
      }</div>
      <p class="preview-origin">预览 ${items.length} 条 · ${esc(sourceActivity(source))}</p>
    </aside>`;
  }

  function directoryMarkup(sources) {
    if (parity.mode !== "category")
      return `<div class="directory-grid">${
        sources.map(sourceCard).join("") ||
        '<div class="empty-inline">没有匹配来源</div>'
      }</div>`;
    const grouped = new Map();
    for (const source of sources) {
      const category = source.category || "other";
      if (!grouped.has(category)) grouped.set(category, []);
      grouped.get(category).push(source);
    }
    if (!grouped.size) return '<div class="empty-inline">没有匹配来源</div>';
    return [...grouped.entries()]
      .map(
        ([category, entries]) =>
          `<section class="category-group"><h4>${esc(
            categoryNames[category] || category
          )}<span>${
            entries.length
          }</span></h4><div class="directory-grid">${entries
            .map(sourceCard)
            .join("")}</div></section>`
      )
      .join("");
  }

  function renderDiscover() {
    const observedCategories = [
      ...new Set(
        (state.sources || []).map((source) => source.category).filter(Boolean)
      ),
    ];
    const categories = [
      ...discoverCategories.filter((value) => value !== "all"),
      ...observedCategories
        .filter((value) => !discoverCategories.includes(value))
        .sort(),
    ];
    const languages = [
      ...new Set(
        (state.sources || [])
          .map((source) => text(source.language).toLowerCase())
          .filter(Boolean)
      ),
    ].sort();
    const classification = classifyDiscoverInput(parity.input);
    const query = (
      classification.kind === "keyword" ? parity.input : parity.query
    ).toLowerCase();
    let sources = uniqueCatalogSources(state.sources || []).filter((source) => {
      if (parity.category && source.category !== parity.category) return false;
      if (
        parity.language &&
        text(source.language).toLowerCase() !== parity.language
      )
        return false;
      if (
        parity.contentType !== "all" &&
        !(source.content_types || ["article"]).includes(parity.contentType)
      )
        return false;
      if (!query) return true;
      return [
        source.name,
        source.description,
        source.category,
        source.language,
        ...(source.catalog_tags || []),
      ]
        .join(" ")
        .toLowerCase()
        .includes(query);
    });
    sources = [...sources].sort((left, right) => {
      if (parity.mode === "trending")
        return (
          text(right.last_sync_at).localeCompare(text(left.last_sync_at)) ||
          Number(right.records_seen || 0) - Number(left.records_seen || 0) ||
          left.name.localeCompare(right.name, "zh-CN")
        );
      if (
        parity.mode === "recommended" &&
        left.recommended !== right.recommended
      )
        return left.recommended ? -1 : 1;
      return left.name.localeCompare(right.name, "zh-CN");
    });
    const packs = parity.packs
      .map((pack) => {
        const sources = pack.sources || [];
        const subscribed = sources.filter((source) => source.subscribed).length;
        const complete = sources.length > 0 && subscribed === sources.length;
        const busy = parity.packBusy.has(pack.id);
        const icons = sources
          .slice(0, 6)
          .map((source) => sourceAvatar(source, "pack-source-avatar"))
          .join("");
        return `<article class="pack-item ${complete ? "complete" : ""}">
          <div class="pack-head"><span class="pack-icon">${esc(
            pack.icon_text || "RSS"
          )}</span><span class="pack-copy"><strong>${esc(
          pack.name
        )}</strong><span>${esc(pack.description)}</span></span></div>
          <div class="pack-source-row"><span class="pack-source-icons">${icons}</span><span>${
          sources.length
        } 个来源</span></div>
          <div class="pack-footer"><span>${
            subscribed ? `${subscribed}/${sources.length} 已添加` : "尚未添加"
          }</span><button class="primary-btn pack-action" type="button" data-subscribe-pack="${esc(
          pack.id
        )}" aria-busy="${busy}" ${complete || busy ? "disabled" : ""}>${
          complete
            ? `${icon("check")}已添加`
            : busy
            ? "添加中"
            : `${icon("plus")}一键添加`
        }</button></div>
        </article>`;
      })
      .join("");
    const candidates = parity.discoveries
      .filter((item) => !item.confirmed_at)
      .map((item) => {
        const previewItems = (item.preview || []).slice(0, 3);
        const preview = previewItems
          .map((entry) =>
            publicURL(entry.url)
              ? `<li><button class="preview-link" type="button" data-open-discovery-article="${esc(
                  entry.url
                )}">${esc(entry.title)}</button></li>`
              : `<li>${esc(entry.title)}</li>`
          )
          .join("");
        const wechat = previewItems.some(
          (entry) => entry.source_type === "wechat"
        );
        const action =
          item.status === "verified"
            ? `<button class="primary-btn" type="button" data-confirm-discovery="${esc(
                item.id
              )}">订阅</button>`
            : wechat
            ? `${badge("已识别公众号", "green")}${
                publicURL(previewItems[0]?.url)
                  ? `<button class="text-btn" type="button" data-open-discovery-article="${esc(
                      previewItems[0].url
                    )}">打开文章</button>`
                  : ""
              }`
            : item.status === "pending"
            ? badge("待验证", "amber")
            : badge("未找到", "red");
        const avatar = wechat
          ? `<span class="discovery-avatar"><img src="${esc(
              publicURL(item.icon_url) ? item.icon_url : projectLogoDataURL
            )}" data-fallback-src="${esc(projectLogoDataURL)}" alt="" loading="lazy"></span>`
          : "";
        return `<div class="pending-item ${wechat ? "wechat-candidate" : ""}">${avatar}<div><strong>${esc(
          item.title || item.input_url
        )}</strong><p>${esc(
          item.feed_url || item.error_message || item.input_url
        )}</p>${
          preview ? `<ul class="preview-titles">${preview}</ul>` : ""
        }</div><div class="pending-actions">${action}</div></div>`;
      })
      .join("");
    const inputPlaceholder =
      parity.inputMode === "html"
        ? "输入公开网页地址"
        : parity.inputMode === "profile"
        ? "输入公开个人主页或公开 Feed"
        : "搜索关键词，或输入网站 / Feed / RSSHub route";
    const modeHint =
      parity.inputMode === "html"
        ? "仅发现网页公开声明的 Feed；动态网页转换需要自托管采集能力"
        : parity.inputMode === "profile"
        ? "只支持公开主页或公开 Feed；需要登录的平台不会绕过权限"
        : classification.label;
    const trendNote =
      parity.mode === "trending"
        ? `<div class="catalog-notice">${esc(
            parity.trendPolicy ||
              "趋势按本地 Focus Space 最近同步活动排序，不展示未经验证的订阅数"
          )}</div>`
        : "";
    const typeFilters = Object.entries(contentTypeNames)
      .map(
        ([value, label]) =>
          `<button class="filter-chip ${
            parity.contentType === value ? "active" : ""
          }" type="button" data-content-type="${value}">${label}</button>`
      )
      .join("");
    const categoryTiles = discoverCategories
      .map((value) => {
        const count =
          value === "all"
            ? (state.sources || []).length
            : (state.sources || []).filter(
                (source) => source.category === value
              ).length;
        const selected =
          value === "all" ? !parity.category : parity.category === value;
        return `<button class="category-tile ${
          selected ? "active" : ""
        }" type="button" data-catalog-category="${value}"><strong>${esc(
          categoryNames[value] || value
        )}</strong><span>${count}</span></button>`;
      })
      .join("");
    $("parity-content").innerHTML = `
      <form id="discovery-form" class="discover-search"><div class="discover-input-wrap">${icon(
        "search"
      )}<input id="feed-url" type="search" placeholder="${esc(
      inputPlaceholder
    )}" value="${esc(
      parity.input
    )}" maxlength="1000" autocomplete="off"><span class="input-kind ${
      classification.kind === "invalid" ? "invalid" : ""
    }" id="discover-input-kind">${esc(
      modeHint
    )}</span></div><button class="primary-btn" type="submit">${
      classification.kind === "keyword" || classification.kind === "empty"
        ? "搜索"
        : "发现来源"
    }</button></form>
      <div class="opml-row"><span class="opml-row-label">订阅库互通</span><button class="text-btn" id="opml-import-button" type="button">导入 OPML</button><button class="text-btn" id="opml-export-button" type="button" data-export-opml>导出 OPML</button><input id="opml-file-input" type="file" accept=".opml,.xml,application/xml,text/xml" hidden /></div>
      <div class="discovery-message" id="discover-error" role="status" aria-live="polite">${esc(
        parity.discoveryNotice
      )}</div>
      ${
        candidates
          ? `<section class="discover-section discovery-results"><div class="discover-section-head"><div><h3>待确认订阅</h3><p>验证成功后，请在这里确认订阅</p></div></div><div class="pending-list">${candidates}</div></section>`
          : ""
      }
      <div class="discover-mode-row"><div class="segmented-control" role="tablist"><button type="button" class="${
        parity.mode === "recommended" ? "active" : ""
      }" data-discover-mode="recommended">推荐</button><button type="button" class="${
      parity.mode === "trending" ? "active" : ""
    }" data-discover-mode="trending">趋势</button><button type="button" class="${
      parity.mode === "category" ? "active" : ""
    }" data-discover-mode="category">分类</button></div><div class="discover-shortcuts"><button class="text-btn ${
      parity.inputMode === "html" ? "active" : ""
    }" type="button" data-discover-capability="html">HTML → RSS</button><button class="text-btn" type="button" data-discover-capability="later">稍后处理</button><button class="text-btn ${
      parity.inputMode === "profile" ? "active" : ""
    }" type="button" data-discover-capability="profile">关注用户</button></div></div>
      <div class="discover-controls">
        <select class="field-control" id="catalog-category"><option value="">全部分类</option>${categories
          .map(
            (value) =>
              `<option value="${esc(value)}" ${
                value === parity.category ? "selected" : ""
              }>${esc(categoryNames[value] || value)}</option>`
          )
          .join("")}</select>
        <select class="field-control" id="catalog-language"><option value="">全部语言</option>${languages
          .map(
            (value) =>
              `<option value="${esc(value)}" ${
                value === parity.language ? "selected" : ""
              }>${esc(languageNames[value] || value)}</option>`
          )
          .join("")}</select>
        <button class="text-btn" type="button" data-refresh-catalog>${icon(
          "refresh"
        )}刷新目录</button>
      </div>
      <div class="content-type-filters" aria-label="内容类型">${typeFilters}</div>
      ${trendNote}
      ${
        parity.mode === "category"
          ? `<div class="category-taxonomy" aria-label="来源分类">${categoryTiles}</div>`
          : ""
      }
      ${
        parity.mode === "recommended" && packs
          ? `<section class="discover-section"><div class="discover-section-head"><div><h3>专题订阅</h3><p>一次添加一组精选公开来源</p></div></div><div class="pack-grid">${packs}</div></section>`
          : ""
      }
      <section class="discover-section"><div class="discover-section-head"><div><h3>来源目录</h3><p>${
        sources.length
      } 个公开来源</p></div></div>${directoryMarkup(sources)}</section>
      ${sourcePreviewPanel()}
      <p class="catalog-provenance">${esc(
        parity.catalogPolicy || "公开目录由 Zettlab 维护"
      )}</p>`;
  }

  function folderOptions(selected = "") {
    return `<option value="">未分组</option>${parity.folders
      .map(
        (folder) =>
          `<option value="${esc(folder.id)}" ${
            folder.id === selected ? "selected" : ""
          }>${esc(folder.name)}</option>`
      )
      .join("")}`;
  }

  function tagOptions() {
    return `<option value="">添加标签</option>${parity.tags
      .map((tag) => `<option value="${esc(tag.id)}">${esc(tag.name)}</option>`)
      .join("")}`;
  }

  function managerRow(source) {
    const tags = (source.tags || [])
      .map((name) => {
        const tag = parity.tags.find((item) => item.name === name);
        return `<span class="tag-chip">${esc(name)}${
          tag
            ? `<button type="button" data-remove-tag="${esc(
                tag.id
              )}" data-tag-source="${esc(source.id)}" aria-label="移除${esc(
                name
              )}">×</button>`
            : ""
        }</span>`;
      })
      .join("");
    const intervals = [
      [900, "15 分钟"],
      [3600, "1 小时"],
      [7200, "2 小时"],
      [21600, "6 小时"],
      [86400, "每天"],
    ]
      .map(
        ([value, label]) =>
          `<option value="${value}" ${
            Number(source.sync_interval_seconds || 7200) === value
              ? "selected"
              : ""
          }>${label}</option>`
      )
      .join("");
    const retentions = [
      [30, "保留 30 天"],
      [90, "保留 90 天"],
      [365, "保留 1 年"],
    ]
      .map(
        ([value, label]) =>
          `<option value="${value}" ${
            Number(source.retention_days || 90) === value ? "selected" : ""
          }>${label}</option>`
      )
      .join("");
    // 状态行对齐 Folo 的源信息展示：同步异常必须显式可见（红字 + 错误详情
    // tooltip），未读/条目/最近同步给管理动作提供依据。占位符永远诚实，不
    // 挂"加载中"。
    const unreadCount = Math.max(0, Number(source.unread_count) || 0);
    const entryTotal = Math.max(0, Number(source.entry_count) || 0);
    const errored =
      Boolean(text(source.last_error)) ||
      ["error", "partial"].includes(text(source.status));
    const renamed =
      text(source.title_override) && text(source.title_override) !== text(source.name);
    const statusLine = `<div class="source-meta-line">${
      errored
        ? `<span class="source-status-error" title="${esc(
            text(source.last_error) || readerFocusCopy("同步失败", "sync failed")
          )}">${readerFocusCopy("同步异常", "sync error")}</span>`
        : ""
    }<span>${readerFocusCopy(
      `未读 ${unreadCount}`,
      `${unreadCount} unread`
    )}</span><span>${readerFocusCopy(
      `共 ${entryTotal} 条`,
      `${entryTotal} entries`
    )}</span><span>${readerFocusCopy(
      `最近同步 ${managerRelativeTime(source.last_sync_at)}`,
      `synced ${managerRelativeTime(source.last_sync_at)}`
    )}</span></div>`;
    const renaming = parity.managerRenameSource === text(source.id);
    const titleBlock = renaming
      ? `<div class="manager-rename-row"><input class="field-control" id="rename-source-input" type="text" maxlength="120" value="${esc(
          source.title_override || source.name
        )}" aria-label="${readerFocusCopy(
          "重命名订阅",
          "Rename subscription"
        )}"><button class="text-btn" type="button" data-rename-save="${esc(
          source.id
        )}">${readerFocusCopy("保存", "Save")}</button><button class="text-btn" type="button" data-rename-cancel>${readerFocusCopy(
          "取消",
          "Cancel"
        )}</button></div>`
      : `<strong>${esc(source.title_override || source.name)}${
          renamed
            ? `<i class="manager-renamed-origin" title="${esc(
                source.name
              )}">${esc(source.name)}</i>`
            : ""
        }</strong><span>${esc(source.url)}</span>`;
    return `<div class="manager-row">
      <input type="checkbox" data-select-source="${esc(source.id)}" ${
      parity.selectedSources.has(source.id) ? "checked" : ""
    } aria-label="选择${esc(source.name)}">
      <div class="manager-source">${sourceAvatar(
        source
      )}<div class="manager-source-copy">${titleBlock}${statusLine}<div class="tag-line">${tags}<select class="mini-select" data-add-tag-source="${esc(
      source.id
    )}">${tagOptions()}</select></div><div class="source-settings-line"><select class="mini-select" data-sync-interval-source="${esc(
      source.id
    )}" aria-label="${esc(
      source.name
    )}同步频率">${intervals}</select><select class="mini-select" data-retention-source="${esc(
      source.id
    )}" aria-label="${esc(
      source.name
    )}保留时间">${retentions}</select><label class="toggle-cell"><input type="checkbox" data-auto-extract-source="${esc(
      source.id
    )}" ${
      source.auto_extract ? "checked" : ""
    }>自动提炼</label><label class="toggle-cell"><input type="checkbox" data-hide-source="${esc(
      source.id
    )}" ${source.hidden ? "checked" : ""}>侧栏隐藏</label></div></div></div>
      <select class="mini-select" data-folder-source="${esc(
        source.id
      )}">${folderOptions(source.folder_id || "")}</select>
      <label class="toggle-cell"><input type="checkbox" data-pin-source="${esc(
        source.id
      )}" ${source.pinned ? "checked" : ""}>置顶</label>
      <label class="toggle-cell"><input type="checkbox" data-mute-source="${esc(
        source.id
      )}" ${source.muted ? "checked" : ""}>静音</label>
      <div class="sort-actions"><button class="icon-btn" type="button" data-read-source="${esc(
        source.id
      )}" ${unreadCount ? "" : "disabled"} aria-label="${readerFocusCopy(
        "全部标为已读",
        "Mark all read"
      )}${esc(source.name)}" title="${readerFocusCopy(
        "把该来源条目全部标为已读（可撤销）",
        "Mark this source read (undoable)"
      )}">${icon("check")}</button><button class="icon-btn" type="button" data-rename-source="${esc(
        source.id
      )}" aria-label="${readerFocusCopy("重命名", "Rename")}${esc(
        source.name
      )}" title="${readerFocusCopy(
        "重命名订阅（留空恢复默认名称）",
        "Rename subscription (empty restores default)"
      )}">${icon("edit")}</button><button class="icon-btn" type="button" data-sort-source="${esc(
        source.id
      )}" data-sort-delta="-10" aria-label="上移" title="上移">↑</button><button class="icon-btn" type="button" data-sort-source="${esc(
      source.id
    )}" data-sort-delta="10" aria-label="下移" title="下移">↓</button><button class="icon-btn" type="button" data-sync-source="${esc(
      source.id
    )}" aria-label="立即同步${esc(source.name)}" title="立即同步">${icon(
      "refresh"
    )}</button><button class="icon-btn" type="button" data-open-site-source="${esc(
      source.id
    )}" aria-label="打开网站" title="打开网站">${icon(
      "external"
    )}</button><button class="icon-btn" type="button" data-copy-source-url="${esc(
      source.id
    )}" aria-label="复制订阅链接" title="复制订阅链接">${icon(
      "edit"
    )}</button><button class="icon-btn" type="button" data-toggle-source="${esc(
      source.id
    )}" aria-label="退订${esc(
      source.name
    )}" title="退订（可在已退订区恢复）">${icon(
      "close"
    )}</button></div>
    </div>`;
  }

  // managerSourceMatchesQuery is extracted so the 订阅管理 search box and the
  // 已退订 section share one filter; it stays pure for the flow test.
  function managerSourceMatchesQuery(source, query) {
    const needle = text(query).trim().toLowerCase();
    if (!needle) return true;
    const fields = [
      source.title_override,
      source.name,
      source.url,
      source.site_url,
      source.folder_name,
      (source.tags || []).join(" "),
    ];
    return fields.some((field) =>
      text(field).toLowerCase().includes(needle)
    );
  }

  // managerRelativeTime renders the manager row's 最近同步 stamp.  Empty or
  // unparseable input is an honest 从未同步, never a spinner.
  function managerRelativeTime(value) {
    const time = Date.parse(text(value));
    if (!text(value) || !Number.isFinite(time))
      return readerFocusCopy("从未同步", "never");
    const minutes = Math.max(0, Math.round((Date.now() - time) / 60000));
    if (minutes < 1) return readerFocusCopy("刚刚", "just now");
    if (minutes < 60)
      return readerFocusCopy(`${minutes} 分钟前`, `${minutes}m ago`);
    const hours = Math.round(minutes / 60);
    if (hours < 24) return readerFocusCopy(`${hours} 小时前`, `${hours}h ago`);
    const days = Math.round(hours / 24);
    if (days < 30) return readerFocusCopy(`${days} 天前`, `${days}d ago`);
    return new Date(time).toLocaleDateString();
  }

  // managerSortedSources keeps 手动 (sort_order) as the default and offers the
  // three read-only orderings Folo's settings table exposes; it never mutates
  // state.sources, only the rendered copy.
  function managerSortedSources(sources) {
    const list = [...sources];
    const mode = parity.managerSortMode;
    if (mode === "name") {
      list.sort((a, b) =>
        text(a.title_override || a.name).localeCompare(
          text(b.title_override || b.name),
          "zh-Hans-CN"
        )
      );
    } else if (mode === "unread") {
      list.sort(
        (a, b) => (Number(b.unread_count) || 0) - (Number(a.unread_count) || 0)
      );
    } else if (mode === "sync") {
      list.sort(
        (a, b) =>
          (Date.parse(text(b.last_sync_at)) || 0) -
          (Date.parse(text(a.last_sync_at)) || 0)
      );
    }
    return list;
  }

  function renderSubscriptions() {
    const query = text(parity.managerSourceQuery);
    const subscribedSources = (state.sources || []).filter(
      (source) => source.subscribed
    );
    const sources = subscribedSources.filter((source) =>
      managerSourceMatchesQuery(source, query)
    );
    const unsubscribed = (state.sources || []).filter(
      (source) =>
        !source.subscribed &&
        source.formerly_subscribed &&
        managerSourceMatchesQuery(source, query)
    );
    $("parity-content").innerHTML = `
      <div class="manager-toolbar">
        <label class="toggle-cell"><input id="select-all-sources" type="checkbox">全选</label>
        <select class="field-control" id="bulk-source-operation"><option value="pin">置顶</option><option value="unpin">取消置顶</option><option value="mute">静音</option><option value="unmute">取消静音</option><option value="move">移动分组</option><option value="unsubscribe">退订</option></select>
        <select class="field-control" id="bulk-source-folder">${folderOptions()}</select>
        <button class="text-btn" type="button" data-apply-source-bulk>应用</button>
        <select class="field-control" id="manager-sort-mode" aria-label="${readerFocusCopy(
          "订阅排序",
          "Sort subscriptions"
        )}"><option value="manual" ${
      parity.managerSortMode === "manual" ? "selected" : ""
    }>${readerFocusCopy("手动排序", "Manual")}</option><option value="name" ${
      parity.managerSortMode === "name" ? "selected" : ""
    }>${readerFocusCopy("按名称", "By name")}</option><option value="unread" ${
      parity.managerSortMode === "unread" ? "selected" : ""
    }>${readerFocusCopy("按未读", "By unread")}</option><option value="sync" ${
      parity.managerSortMode === "sync" ? "selected" : ""
    }>${readerFocusCopy("按最近同步", "By last sync")}</option></select>
        <span class="manager-spacer"></span>
        <input class="field-control manager-search" id="manager-source-search" type="search" placeholder="搜索订阅" value="${esc(
          query
        )}" aria-label="搜索订阅">${
      query
        ? `<span class="manager-search-count">${sources.length}/${subscribedSources.length}</span>`
        : ""
    }<button class="text-btn" type="button" data-export-opml>导出 OPML</button>
      </div>
      <div class="manager-list"><div class="manager-row header"><span></span><span>来源</span><span>分组</span><span>置顶</span><span>静音</span><span>排序</span></div>${
        managerSortedSources(sources).map(managerRow).join("") ||
        `<div class="empty-inline">${
          query ? "没有匹配的订阅" : "暂无订阅"
        }</div>`
      }</div>
      <section class="manager-unsubscribed" data-unsubscribed-section>
        <h4>已退订的订阅（${unsubscribed.length}）</h4>
        ${
          unsubscribed
            .map(
              (source) => `<div class="manager-unsubscribed-row" data-unsubscribed-source="${esc(
                source.id
              )}">${sourceAvatar(source)}<div class="manager-unsubscribed-copy"><strong>${esc(
                source.title_override || source.name
              )}</strong><span>${esc(source.url)}</span></div>
              <button class="text-btn" type="button" data-open-site-source="${esc(
                source.id
              )}">打开网站</button>
              <button class="text-btn" type="button" data-copy-source-url="${esc(
                source.id
              )}">复制订阅链接</button>
              <button class="primary-btn" type="button" data-toggle-source="${esc(
                source.id
              )}">重新订阅</button></div>`
            )
            .join("") ||
          '<div class="empty-inline">没有已退订的订阅；退订后的来源会保留在这里，随时可以重新订阅。</div>'
        }
      </section>
      <div class="create-grid">
        <input class="field-control" id="new-folder-name" maxlength="80" placeholder="新分组名称"><button class="text-btn" type="button" data-create-folder>新建分组</button>
        <input class="field-control" id="new-tag-name" maxlength="60" placeholder="新标签名称"><button class="text-btn" type="button" data-create-tag>新建标签</button>
      </div>
      ${
        parity.folders.length
          ? `<div class="create-grid folder-manage-row">
        <select class="field-control" id="folder-manage-select" aria-label="${readerFocusCopy(
          "选择分组",
          "Select folder"
        )}">${folderOptions(
          parity.folderDeleteArmed ? parity.folderDeleteTarget : ""
        )}</select>
        <input class="field-control" id="folder-rename-name" maxlength="80" placeholder="${readerFocusCopy(
          "分组新名称",
          "New folder name"
        )}"><button class="text-btn" type="button" data-rename-folder>${readerFocusCopy(
            "重命名分组",
            "Rename folder"
          )}</button>
        <button class="text-btn folder-delete-btn" type="button" data-delete-folder>${
          parity.folderDeleteArmed
            ? readerFocusCopy("确认删除（来源移入未分组）", "Confirm delete")
            : readerFocusCopy("删除分组", "Delete folder")
        }</button>
      </div>`
          : ""
      }`;
  }

  function resetEntryPagination() {
    const news = Array.isArray(state.dashboard?.news)
      ? state.dashboard.news
      : [];
    parity.entryBaseNews = boundReaderNews(news.map((item) => ({ ...item })));
    parity.entryAggregateSnapshot = null;
    parity.entryViewSnapshots.clear();
    parity.entryViewLoaded.clear();
    parity.entryViewErrors.clear();
    parity.entryViewTotals.clear();
    parity.entryViewLoading = "";
    parity.entryScopeSnapshots.clear();
    if (news.length !== parity.entryBaseNews.length)
      state.dashboard.news = parity.entryBaseNews.map((item) => ({ ...item }));
    parity.entryScope = "";
    parity.entryScopeError = "";
    // The dashboard projection and typed feed are separate SQL reads. Never
    // treat the snapshot length as an offset: concurrent inserts and tied
    // timestamps can make that offset skip rows. The first typed page
    // establishes the authoritative keyset cursor; mergePagedEntries removes
    // rows already present in the snapshot.
    parity.entryCursor = "0";
    parity.entryPagesRead = 0;
    parity.entryHasMore = true;
    parity.entryLoading = false;
    parity.entryLoadingOwnerToken = null;
    parity.entryLoadingView = "";
    parity.entryPageError = "";
    parity.entryScopeLoading = false;
    parity.detailLoading = false;
    if (state.selected === detailLoadingKey) state.selected = null;
    parity.entryRequestToken += 1;
    parity.videoInitialRequestStarted = false;
    rememberAggregateEntrySnapshot();
  }

  function rememberAggregateEntrySnapshot() {
    const markView = arguments.length > 0 && arguments[0] === true;
    const view = arguments.length > 1 ? arguments[1] : state.view;
    if (!state.dashboard || parity.entryScope) return;
    parity.entryAggregateSnapshot = {
      news: Array.isArray(state.dashboard.news)
        ? state.dashboard.news.map((item) => ({ ...item }))
        : [],
      cursor: text(parity.entryCursor),
      pagesRead: Number(parity.entryPagesRead) || 0,
      hasMore: Boolean(parity.entryHasMore),
      totalEntries: entryTotalForView(view),
    };
    const snapshot = parity.entryAggregateSnapshot;
    const key = text(view);
    // A dashboard snapshot is not proof that the typed aggregate endpoint was
    // read.  Only an explicit successful typed request (or a later pagination
    // page for an already-loaded view) may mark the view reusable.  Once a
    // view is marked loaded, ordinary preference/context rebinds can refresh
    // its bounded snapshot without issuing another network request.
    if (
      isAggregateEntryView(key) &&
      (markView || parity.entryViewLoaded.has(key))
    ) {
      parity.entryViewSnapshots.set(key, snapshot);
      parity.entryViewLoaded.add(key);
      parity.entryViewErrors.delete(key);
      const total = finiteEntryTotal(snapshot.totalEntries);
      if (total !== null) parity.entryViewTotals.set(key, total);
    }
  }

  function restoreAggregateEntrySnapshot() {
    const snapshot = parity.entryAggregateSnapshot;
    if (!snapshot || !Array.isArray(snapshot.news)) return false;
    if (state.dashboard)
      state.dashboard.news = snapshot.news.map((item) => ({ ...item }));
    parity.entryCursor = text(snapshot.cursor);
    parity.entryPagesRead = Number(snapshot.pagesRead) || 0;
    parity.entryHasMore = Boolean(snapshot.hasMore);
    return true;
  }

  // Keep the currently opened detail stable while the bounded dashboard
  // snapshot or a source-scoped page is being replaced. Only display fields
  // are retained and the cache is capped so a long reading session cannot
  // become an unbounded browser-side store.
  function cacheDetailEntry(entry) {
    if (!entry?.key || !entry.item) return;
    const item = { ...entry.item };
    for (const field of [
      "safe_html",
      "content",
      "content_text",
      "transcript",
      "report",
      "segments",
    ])
      delete item[field];
    parity.detailCache.delete(entry.key);
    parity.detailCache.set(entry.key, {
      ...entry,
      item,
      cached_at: Date.now(),
    });
    while (parity.detailCache.size > parity.detailCacheLimit) {
      const oldest = parity.detailCache.keys().next().value;
      if (!oldest) break;
      parity.detailCache.delete(oldest);
    }
  }

  window.__cacheReaderDetailEntry = cacheDetailEntry;
  // The base page owns optimistic item preferences. Let it re-apply the
  // bounded ordinary/protected reader windows immediately after a star or
  // read-later mutation instead of waiting for the next page or refresh.
  window.__rebindReaderNewsProjection = rebindReaderNewsProjection;

  // updateNewsCardLocally flips the visible card state without a full timeline
  // repaint, so m/s/l key presses and star clicks feel instant. Returns false
  // when no card is on screen (caller falls back to render()). View filters
  // that depend on the mutated field (starred / read-later views) still need
  // the repaint and bypass this fast path in the caller.
  function updateNewsCardLocally(key, field, value) {
    const card = document.querySelector(
      `[data-entry-key="${CSS.escape(key)}"]`
    );
    if (!card) return false;
    if (field === "read") card.classList.toggle("read", Boolean(value));
    if (field === "starred") {
      document
        .querySelectorAll(`[data-star-entry="${CSS.escape(key)}"]`)
        .forEach((button) => {
          const active = Boolean(value);
          button.classList.toggle("starred", active);
          const label = active
            ? readerFocusCopy("取消收藏", "Remove from favorites")
            : readerFocusCopy("收藏", "Add to favorites");
          button.setAttribute("aria-label", label);
          button.title = label;
          button.innerHTML = active ? icon("starFill") : icon("star");
        });
    }
    return true;
  }
  window.__updateReaderCardLocally = updateNewsCardLocally;

  function restoreCachedSelectedEntry() {
    const key = text(state.selected);
    if (!key || key === detailLoadingKey || !state.dashboard) return false;
    if (makeEntries(state.dashboard).some((entry) => entry.key === key))
      return true;
    const cached = parity.detailCache.get(key);
    if (!cached?.item) return false;
    const collections = {
      news: "news",
      recording: "recordings",
      evidence: "evidence",
      action: "actions",
      metric: "metrics",
      project: "projects",
      signal: "signals",
      decision: "decisions",
    };
    const collection = collections[text(cached.kind)];
    if (collection) {
      const items = Array.isArray(state.dashboard[collection])
        ? state.dashboard[collection]
        : [];
      if (!items.some((item) => item.id === cached.item.id)) {
        const nextItems = [cached.item, ...items];
        state.dashboard[collection] =
          collection === "news" ? boundReaderNews(nextItems) : nextItems;
        if (collection === "news" && Array.isArray(parity.entryBaseNews)) {
          parity.entryBaseNews = boundReaderNews([
            cached.item,
            ...parity.entryBaseNews,
          ]);
        }
      }
    }
    return makeEntries(state.dashboard).some((entry) => entry.key === key);
  }

  function detailLoadingMarkup() {
    return `<div class="detail-loading" data-detail-loading role="status" aria-live="polite" aria-busy="true" aria-label="正在打开文章">
      <div class="detail-loading-status"><span class="detail-loading-spinner" aria-hidden="true"></span><strong>正在打开文章</strong><span>正在读取来源内容</span></div>
      <div class="detail-loading-kicker"><span class="detail-skeleton detail-skeleton-icon"></span><span class="detail-skeleton detail-skeleton-kicker"></span><span class="detail-skeleton detail-skeleton-source"></span></div>
      <div class="detail-skeleton detail-skeleton-title"></div>
      <div class="detail-skeleton detail-skeleton-title detail-skeleton-title-short"></div>
      <div class="detail-skeleton detail-skeleton-meta"></div>
      <section class="detail-loading-overview" aria-hidden="true"><div class="detail-skeleton detail-skeleton-line"></div><div class="detail-skeleton detail-skeleton-line detail-skeleton-line-short"></div></section>
      <div class="detail-loading-body" aria-hidden="true"><div class="detail-skeleton detail-skeleton-line"></div><div class="detail-skeleton detail-skeleton-line"></div><div class="detail-skeleton detail-skeleton-line detail-skeleton-line-medium"></div><div class="detail-skeleton detail-skeleton-line"></div><div class="detail-skeleton detail-skeleton-line detail-skeleton-line-short"></div></div>
    </div>`;
  }

  function timelineSkeletonCardMarkup(kind) {
    if (kind === "video")
      return `<article class="timeline-skeleton-card is-video" aria-hidden="true">
        <span class="timeline-skeleton-media"></span>
        <div class="timeline-skeleton-video-copy"><span class="timeline-skeleton-line"></span><span class="timeline-skeleton-line is-medium"></span><span class="timeline-skeleton-line timeline-skeleton-meta"></span></div>
      </article>`;
    const social = kind === "social";
    return `<article class="timeline-skeleton-card ${social ? "is-social" : "is-article"}" aria-hidden="true">
      <span class="timeline-skeleton-line timeline-skeleton-avatar"></span>
      <div class="timeline-skeleton-copy"><div class="timeline-skeleton-head"><span class="timeline-skeleton-line"></span><span class="timeline-skeleton-line"></span></div><span class="timeline-skeleton-line is-wide"></span><span class="timeline-skeleton-line is-medium"></span><span class="timeline-skeleton-line timeline-skeleton-meta"></span></div>
    </article>`;
  }

  function timelineLoadingMarkup(mode = "scope") {
    const loadingMore = mode === "more";
    const videoLoading =
      ["video", "youtube"].includes(state.view) ||
      Boolean(videoProviderForSource(selectedCatalogSource()));
    const cards = videoLoading
      ? Array.from({ length: 8 }, () => timelineSkeletonCardMarkup("video")).join("")
      : `${timelineSkeletonCardMarkup("article")}${timelineSkeletonCardMarkup("social")}${timelineSkeletonCardMarkup("video")}`;
    return `<div class="timeline-loading ${loadingMore ? "is-more" : "is-scope"}" data-timeline-loading role="status" aria-live="polite" aria-busy="true" aria-label="${loadingMore ? "正在读取更多内容" : "正在读取来源内容"}">
      <div class="timeline-loading-status"><span class="timeline-loading-spinner" aria-hidden="true"></span><strong>${loadingMore ? "正在读取更多内容" : "正在读取来源内容"}</strong><span>${loadingMore ? "保留当前列表，稍候补充" : "只加载当前来源的最新内容"}</span></div>
      <div class="timeline-loading-cards ${videoLoading ? "video-grid" : ""}">${cards}</div>
    </div>`;
  }

  function entryScopeForView() {
    if (!state.view.startsWith("source:")) return "";
    // The source directory is an independent, lazy read.  A persisted source
    // view (or a click while that directory request is still in flight) can
    // therefore be valid before `state.sources` contains its row.  Returning
    // an empty scope here makes renderTimeline fall back to the aggregate
    // dashboard and, more importantly, prevents switchEntryScope from ever
    // issuing the typed `source_id` read.  Keep the source id as the routing
    // truth; the scoped endpoint and resolveSourceMetadata validate/resolve
    // the row before any source sync is enqueued.
    return text(state.view.slice(7));
  }

  function isAggregateEntryView(view = state.view) {
    return ["all", "rss", "video", "youtube", "picture", "starred", "later"].includes(text(view));
  }

  function finiteEntryTotal(value) {
    const number = Number(value);
    return Number.isFinite(number) && number >= 0 ? number : null;
  }

  // Resolve a total for one aggregate tab without falling back to another
  // tab's total.  The typed page and the independent counts endpoint both
  // use the same field names, while older hosts may only expose
  // total_entries/video_entries; those are used strictly as compatibility
  // fallbacks for the matching view.
  function entryTotalForView(view = state.view) {
    const key = text(view);
    const cached = finiteEntryTotal(parity.entryViewTotals.get(key));
    if (cached !== null) return cached;
    const counts = parity.counts || {};
    const dashboard = state.dashboard || {};
    const field = {
      all: "total_entries",
      rss: "rss_entries",
      video: "video_entries",
      youtube: "youtube_entries",
      picture: "picture_entries",
    }[key];
    if (!field) return null;
    const direct = finiteEntryTotal(counts[field]);
    if (direct !== null) return direct;
    const dashboardDirect = finiteEntryTotal(dashboard[field]);
    if (dashboardDirect !== null) return dashboardDirect;
    // Before reader_counts.get was available, RSS was represented only by
    // the all/video pair.  Derive it only as a last resort; never use the
    // video total as a YouTube total.
    if (key === "rss") {
      const all = finiteEntryTotal(counts.total_entries ?? dashboard.total_entries);
      const video = finiteEntryTotal(counts.video_entries ?? dashboard.video_entries);
      if (all !== null && video !== null) return Math.max(0, all - video);
    }
    return null;
  }

  function aggregateTotalField(view = state.view) {
    return {
      all: "total_entries",
      rss: "rss_entries",
      video: "video_entries",
      youtube: "youtube_entries",
      picture: "picture_entries",
    }[text(view)] || "";
  }

  // Client-side mirror of the server's picture predicate: a classified
  // picture entry or an image media attachment, never a video entry and never
  // an entry that only happens to carry an article thumbnail.
  function aggregatePictureNews(item) {
    if (!item || videoProviderForItem(item)) return false;
    const mime = text(item.media_mime).toLowerCase();
    return (
      text(item.view_type).toLowerCase() === "picture" ||
      mime.startsWith("image/")
    );
  }

  function aggregateEntryMatches(entry, view) {
    const item = entry?.item || entry;
    const isNews = entry?.kind === "news" || (!entry?.kind && item?.id);
    if (!isNews) return false;
    const provider = text(entry?.videoProvider) || videoProviderForItem(item);
    const isVideo = entry?.group === "video" || Boolean(provider);
    switch (text(view)) {
      case "starred":
        return Boolean(item.starred);
      case "later":
        return Boolean(item.read_later);
      case "rss":
        return !isVideo;
      case "video":
        return isVideo;
      case "youtube":
        return isVideo && provider === "youtube";
      case "picture":
        return !isVideo && aggregatePictureNews(item);
      case "all":
      default:
        return true;
    }
  }

  function loadedAggregateEntryCount(view = state.view, entries) {
    const source = Array.isArray(entries)
      ? entries
      : makeEntries(state.dashboard || {});
    return source.filter((entry) => aggregateEntryMatches(entry, view)).length;
  }

  // In-flight entry requests are owned by the view that started them.  A
  // navigation can happen before the old promise settles; invalidate that
  // promise and release both busy flags so the new view can paint immediately.
  // The request token is shared by aggregate and cursor-page reads, therefore
  // one increment invalidates every stale continuation without aborting the
  // underlying fetch.
  function cancelStaleReaderEntryLoad(view = state.view) {
    const target = text(view);
    const staleAggregate =
      Boolean(parity.entryViewLoading) && parity.entryViewLoading !== target;
    const stalePage =
      Boolean(parity.entryLoading) &&
      Boolean(parity.entryLoadingView) &&
      parity.entryLoadingView !== target;
    if (!staleAggregate && !stalePage) return false;
    parity.entryViewLoading = "";
    parity.entryLoading = false;
    parity.entryLoadingOwnerToken = null;
    parity.entryLoadingView = "";
    parity.entryRequestToken += 1;
    return true;
  }

  function restoreAggregateViewSnapshot(view = state.view) {
    const key = text(view);
    if (!parity.entryViewLoaded.has(key)) return false;
    const snapshot = parity.entryViewSnapshots.get(key);
    if (!snapshot || !Array.isArray(snapshot.news) || !state.dashboard)
      return false;
    state.dashboard.news = snapshot.news.map((item) => ({ ...item }));
    parity.entryAggregateSnapshot = snapshot;
    parity.entryScope = "";
    parity.entryScopeError = "";
    parity.entryCursor = text(snapshot.cursor);
    parity.entryPagesRead = Number(snapshot.pagesRead) || 0;
    parity.entryHasMore = Boolean(snapshot.hasMore);
    const total = finiteEntryTotal(snapshot.totalEntries);
    if (total !== null) parity.entryViewTotals.set(key, total);
    return true;
  }

  function aggregateEntryErrorMarkup(view, error) {
    const message = text(error, "内容暂时读取失败").slice(0, 180);
    return `<div class="empty-inline aggregate-entry-error" role="alert"><strong>内容暂时读取失败</strong><span>${esc(
      message
    )}</span><button class="text-btn" type="button" data-retry-aggregate="${esc(
      view
    )}">${icon("refresh")} 重试</button></div>`;
  }

  function entryPageErrorMarkup(error) {
    const message = text(error, "更多内容暂时读取失败").slice(0, 180);
    return `<div class="entry-page-more entry-page-error" role="alert"><span>${esc(
      message
    )}</span><button class="text-btn" type="button" data-retry-entries>${icon(
      "refresh"
    )} 重试</button></div>`;
  }

  // Aggregate views (all/rss/video/youtube) share the same bounded cursor
  // endpoint, but each view owns its first page. Fetch only after that view is
  // entered and discard stale responses when the user navigates away.
  async function loadAggregateEntriesOnDemand(
    view = state.view,
    navigationToken = parity.navigationToken
  ) {
    const target = text(view);
    // The request may be queued from a capture-phase click.  Reject it if a
    // newer navigation has superseded that click, or if the base handler has
    // not committed the requested view yet.
    if (
      !isAggregateEntryView(target) ||
      !state.dashboard ||
      navigationToken !== parity.navigationToken ||
      state.view !== target
    )
      return;
    if (parity.entryViewLoading === target) return;
    if (restoreAggregateViewSnapshot(target)) {
      renderReaderProjectionOnly();
      return;
    }
    // A direct/programmatic navigation may call this while another aggregate
    // request is still settling.  Do not let the previous promise retain the
    // busy state or merge rows into the new view.
    cancelStaleReaderEntryLoad(target);
    const token = ++parity.entryRequestToken;
    const fallbackNews = Array.isArray(state.dashboard.news)
      ? state.dashboard.news.map((item) => ({ ...item }))
      : [];
    parity.entryViewLoading = target;
    parity.entryViewErrors.delete(target);
    parity.entryPageError = "";
    parity.entryScope = "";
    parity.entryCursor = "0";
    parity.entryPagesRead = 0;
    parity.entryHasMore = true;
    parity.entryLoading = false;
    parity.entryLoadingOwnerToken = null;
    parity.entryLoadingView = "";
    // The first aggregate video page is loaded by this function.  Mark it as
    // started so renderTimeline does not immediately issue a second page-0
    // request through the legacy top-level-video branch.
    if (["video", "youtube"].includes(target))
      parity.videoInitialRequestStarted = true;
    state.dashboard.news = [];
    renderReaderProjectionOnly();
    try {
      const params = new URLSearchParams({
        limit: String(parity.entryPageSize),
        cursor: "0",
      });
      if (["starred", "later"].includes(target)) params.set("saved", target === "starred" ? "starred" : "read_later");
      if (["rss", "video", "youtube", "picture"].includes(target))
        params.set(
          "view_type",
          target === "rss" ? "rss" : target === "youtube" ? "youtube" : target === "picture" ? "picture" : "video"
        );
      const result = await request(`./api/entries?${params.toString()}`);
      if (
        token !== parity.entryRequestToken ||
        navigationToken !== parity.navigationToken ||
        state.view !== target
      )
        return;
      replacePagedEntries(result.entries || []);
      parity.entryPagesRead = 1;
      parity.entryCursor = text(result.next_cursor_token || result.next_cursor);
      parity.entryHasMore = Boolean(
        parity.entryCursor &&
          entryRemotePageRemaining(result.total_entries) &&
          parity.entryPagesRead < readerEntryPageLimit
      );
      const total = finiteEntryTotal(result.total_entries);
      if (total !== null) {
        parity.entryViewTotals.set(target, total);
        const field = aggregateTotalField(target);
        if (field && state.dashboard) state.dashboard[field] = total;
        if (field && parity.counts) parity.counts[field] = total;
      }
      rememberAggregateEntrySnapshot(true, target);
    } catch (error) {
      if (
        token === parity.entryRequestToken &&
        navigationToken === parity.navigationToken &&
        state.view === target
      ) {
        state.dashboard.news = fallbackNews;
        parity.entryViewErrors.set(
          target,
          text(error?.message, "内容暂时读取失败")
        );
        showError(error.message || "内容暂时读取失败");
        // 冷启动紧跟应用唤醒：首个聚合页请求可能正好撞上唤醒 503。同视图
        // 仍在前台时静默重试一次（有界），不再强迫用户手动点"重试"。
        const autoRetries = parity.entryViewAutoRetries.get(target) || 0;
        if (autoRetries < 1 && !document.hidden) {
          parity.entryViewAutoRetries.set(target, autoRetries + 1);
          window.setTimeout(() => {
            if (state.view !== target || parity.entryViewLoading === target) return;
            void loadAggregateEntriesOnDemand(target);
          }, 2000);
        }
      }
    } finally {
      if (
        token === parity.entryRequestToken &&
        navigationToken === parity.navigationToken &&
        state.view === target
      ) {
        parity.entryViewLoading = "";
        renderReaderProjectionOnly();
      }
    }
  }

  function knownCount(value) {
    if (value === null || value === undefined || typeof value === "boolean" || (typeof value === "string" && !value.trim())) return null;
    const count = Number(value);
    return Number.isSafeInteger(count) && count >= 0 ? count : null;
  }

  function sourceUnreadTotal(source) {
    if (!parity.counts?.source_counts_available) return null;
    if (parity.counts.source_counts_truncated && !parity.counts.by_source?.[source.id]) return null;
    const row = parity.counts.by_source?.[source.id];
    return row ? knownCount(row.unread_entries) : knownCount(source.unread_count);
  }

  async function loadDeviceCounts(retryBudget = 0) {
    if (parity.deviceCountsLoading) return parity.deviceCountsLoading;
    parity.deviceCountsLoading = Promise.allSettled([
      request(deviceDataPath("meetings/count"), { timeoutMs: 8000 }).then(value => {
        if (value.capabilities?.counts_v1 && value.complete && knownCount(value.total) !== null)
          parity.deviceCounts.meeting = value.total;
      }),
      loadObsidianCounts(""),
    ]).then(results => {
      const failed = results.some(result => result.status === "rejected");
      parity.deviceCounts.stale = parity.deviceCounts.obsidianStale || failed;
      renderNav();
      if (liveDeviceDataView()) renderTimeline();
      // 冷启动紧跟代理唤醒窗口：首个 counts 请求可能被唤醒 503 吃掉且没有
      // 后续事件再触发。给启动路径一份有界退避重试；进入设备视图等用户
      // 行为本来就会重新调度，不需要重试预算。
      if (failed && retryBudget > 0 && !document.hidden) {
        window.setTimeout(() => { void loadDeviceCounts(retryBudget - 1); }, 8000);
      }
    }).finally(() => { parity.deviceCountsLoading = null; });
    return parity.deviceCountsLoading;
  }

  async function loadObsidianCounts(path) {
    const value = await request(deviceDataPath("obsidian/counts", { path }), { timeoutMs: 8000 });
    if (!value.capabilities?.counts_v1) return;
    if (knownCount(value.total) !== null) {
      parity.obsidianCounts.set(path, value.total);
      if (!path) parity.deviceCounts.obsidian = value.total;
    }
    for (const [key, count] of Object.entries(value.children || {}))
      if (knownCount(count) !== null) parity.obsidianCounts.set(key, count);
    parity.deviceCounts.obsidianStale = !value.complete;
  }

  async function loadEntryCounts(silent = true) {
    if (parity.countsLoading) { parity.countsRefreshPending = true; return parity.counts; }
    parity.countsLoading = true;
    try {
      const value = await request("./api/reader/counts", { timeoutMs: 5000 });
      const body = value?.data && typeof value.data === "object" ? value.data : value;
      const payload = body?.counts && typeof body.counts === "object" ? body.counts : body;
      if (!payload || typeof payload !== "object") throw new Error("invalid counts");
      parity.countsStale = false;
      const bySource = { ...(payload.by_source || {}) };
      const hasAuthoritativeSourceCounts =
        Array.isArray(payload.source_counts) ||
        (payload.by_source && typeof payload.by_source === "object");
      // The device bounds the per-source response so a pathological catalog
      // cannot allocate an unbounded browser map.  A truncated response is
      // useful for rows it contains, but it is not authoritative for sources
      // omitted after the cap.  Keep their previous values until a complete
      // response arrives; treating them as zero makes populated sources look
      // empty whenever the catalog exceeds the server-side bound.
      const sourceCountsTruncated =
        payload.source_counts_truncated === true ||
        payload.source_counts_truncated === 1 ||
        payload.source_counts_truncated === "1";
      if (sourceCountsTruncated) {
        for (const [id, oldCount] of Object.entries(parity.counts?.by_source || {}))
          if (!bySource[id]) bySource[id] = oldCount;
        parity.countsStale = true;
      }
      if (Array.isArray(payload.source_counts))
        payload.source_counts.forEach((row) => {
          if (row?.source_id) bySource[row.source_id] = row;
        });
      // Preserve whether the response actually carried a per-source
      // projection. `by_source` is normalized to an object for callers, so
      // checking only its type later would incorrectly treat an older
      // aggregate-only response as authoritative and turn every catalog row
      // into a false zero.
      parity.counts = {
        ...payload,
        by_source: bySource,
        source_counts_available: hasAuthoritativeSourceCounts,
      };
      for (const [view, field] of Object.entries({
        all: "total_entries",
        rss: "rss_entries",
        video: "video_entries",
        youtube: "youtube_entries",
      })) {
        const total = finiteEntryTotal(payload[field]);
        if (total !== null) parity.entryViewTotals.set(view, total);
      }
      if (Array.isArray(state.sources))
        state.sources = state.sources.map((source) => {
          const row = bySource[source.id];
          if (!hasAuthoritativeSourceCounts) return source;
          const entryCount = knownCount(row?.total_entries);
          const unreadCount = knownCount(row?.unread_entries);
          return {
            ...source,
            // A missing row is authoritative zero (the endpoint groups only
            // sources with visible entries). Invalid row values retain the
            // previous value for compatibility with older hosts.
            entry_count: row && Number.isFinite(entryCount) && entryCount >= 0
              ? entryCount
              : row
              ? source.entry_count
              : sourceCountsTruncated
              ? source.entry_count
              : 0,
            unread_count: row && Number.isFinite(unreadCount) && unreadCount >= 0
              ? unreadCount
              : row
              ? source.unread_count
              : sourceCountsTruncated
              ? source.unread_count
              : 0,
          };
        });
      if (state.dashboard) {
        for (const key of [
          "total_entries",
          "unread_entries",
          "video_entries",
          "rss_entries",
          "youtube_entries",
        ]) {
          if (Number.isFinite(Number(payload[key]))) state.dashboard[key] = Number(payload[key]);
        }
      }
      renderNav();
      renderSources();
    } catch (error) {
      parity.countsStale = true;
      // Older hosts have no counts endpoint. Keep rendering from the
      // dashboard/source projection instead of turning an optional request
      // into a startup failure.
      if (!parity.counts) {
        const dashboard = state.dashboard || {};
        parity.counts = {
          total_entries: Number(dashboard.total_entries) || 0,
          unread_entries: Number(dashboard.unread_entries) || 0,
          video_entries: Number(dashboard.video_entries) || 0,
        };
      }
      renderNav();
      if (!silent) showError(error.message || "计数暂时读取失败");
    } finally {
      parity.countsLoading = false;
      if (parity.countsRefreshPending) { parity.countsRefreshPending = false; void loadEntryCounts(true); }
    }
    return parity.counts;
  }

  // The base bootstrap and its bounded warmup loop can refresh count badges
  // without knowing the parity module's private state. The function is
  // intentionally read-only and returns the in-flight/result promise so a
  // caller can choose not to block rendering.
  window.__plaudRefreshReaderCounts = () => Promise.allSettled([loadEntryCounts(true), loadDeviceCounts()]);

  // A source sync changes only the source projection and its independent
  // counters. Keep this refresh deliberately narrower than loadParityData:
  // the latter also fetches packs, folders, tags, context, settings and sync
  // history, which makes a source click look like a full-page reload and can
  // replace the reader shell while the user is browsing. Share one in-flight
  // promise so a click, a monitor completion and the warmup loop cannot create
  // a burst of identical catalog/count requests.
  let sourceProjectionRefreshPromise = null;
  async function refreshSourceMetadataAndCounts(silent = true) {
    if (sourceProjectionRefreshPromise) return sourceProjectionRefreshPromise;
    sourceProjectionRefreshPromise = (async () => {
      const [sourcesResult] = await Promise.allSettled([
        request("./api/sources?include_counts=0", { timeoutMs: 5000 }),
        loadEntryCounts(silent),
      ]);
      if (sourcesResult.status === "fulfilled") {
        const value = sourcesResult.value;
        const body = value?.data && typeof value.data === "object" ? value.data : value;
        const nextSources = Array.isArray(body?.sources) ? body.sources : null;
        if (nextSources) {
          const currentSources = Array.isArray(state.sources) ? state.sources : [];
          // An empty catalog can be returned briefly while the device worker is
          // creating its first source projection. Do not erase a known local
          // directory on that transient response; a later warmup will reconcile
          // it without forcing the reader back to the home view.
          if (nextSources.length || !currentSources.length)
            state.sources = window.__plaudMergeSourceCatalogCounts
              ? window.__plaudMergeSourceCatalogCounts(
                  currentSources,
                  nextSources,
                  false
                )
              : nextSources;
          renderSources();
          renderNav();
          updateSourceBar();
        }
      } else if (!silent) {
        showError("来源目录暂时未载入，可稍后重试");
      }
      return {
        sources: state.sources,
        counts: parity.counts,
      };
    })().finally(() => {
      sourceProjectionRefreshPromise = null;
    });
    return sourceProjectionRefreshPromise;
  }

  function normalizePagedEntry(item) {
    if (!item || !item.id) return null;
    const blocks = Array.isArray(item.blocks) ? item.blocks : item.article_blocks;
    return {
      id: item.id,
      external_id: item.external_id || item.id,
      title: item.title || "未命名内容",
      url: item.url || "",
      source: item.source || "未知来源",
      source_id: item.source_id || "",
      origin: item.origin || "agent",
      source_type: item.source_type || "rss",
      view_type: item.view_type || "article",
      author: item.author || "",
      published_at: item.published_at || "",
      updated_at: item.updated_at || "",
      revision: Number(item.revision) || 0,
      summary_text: item.summary_text || "",
      content_format: item.content_format || "text",
      safe_html: item.safe_html || "",
      thumbnail_url: item.thumbnail_url || "",
      media_url: item.media_url || "",
      media_mime: item.media_mime || "",
      duration_seconds: item.duration_seconds || 0,
      transcript: text(item.transcript),
      content_hash: item.content_hash || "",
      fetched_at: item.fetched_at || "",
      permission_scope: item.permission_scope || "",
      source_level: item.source_level || "",
      failure_reason: item.failure_reason || "",
      ai_overview: item.ai_overview || null,
      // Article blocks are optional so older hosts continue to render the
      // existing full-text projection. Keep the structured citations on the
      // item when a newer host provides them.
      blocks_version: text(item.blocks_version || item.article_blocks_version),
      blocks_hash: text(item.blocks_hash || item.article_blocks_hash),
      blocks_truncated: Boolean(
        item.blocks_truncated ?? item.article_blocks_truncated
      ),
      blocks: Array.isArray(blocks) ? blocks : [],
      citations: Array.isArray(item.citations) ? item.citations : [],
    };
  }

  // Generated apps do not receive a device JWT. App Host exposes this small,
  // capability-gated read surface instead. Keep the projection bounded and
  // retain the last successful result when an older host or a temporary device
  // read is unavailable.
  const liveDeviceData = {
    loadedAt: 0,
    inflight: null,
    meetingDetails: new Map(),
    meetingNextOffset: 0,
    meetingHasMore: false,
    meetingLoading: false,
    obsidianDetails: new Map(),
    obsidianLoadedFolders: new Set(),
    obsidianFolderInflight: new Map(),
  };
  const fullTextCacheLimit = 4;
  // IndexedDB keeps opened article projections across reloads without putting
  // large HTML strings into the localStorage quota. This is a browser cache,
  // not the source of truth: AC entry_full_texts remains authoritative.
  const fullTextBrowserCacheLimit = 16;
  const fullTextAttemptLimit = 64;
  const fullTextBrowserCacheTTL = 30 * 24 * 60 * 60 * 1000;
  const fullTextBrowserMaxBytes = 2 * 1024 * 1024;
  const fullTextBrowserDBName = "plaud-reader-cache-v2";
  const fullTextBrowserStoreName = "fulltext";
  let fullTextBrowserDBPromise = null;

  let fullTextNamespacePromise = null;
  async function fullTextCacheKey(entry) {
    const id = text(entry?.item?.id);
    if (!id) return "";
    if (!fullTextNamespacePromise) fullTextNamespacePromise = request("./api/capabilities", { timeoutMs: 5000 })
      .then((value) => /^[a-f0-9]{64}$/.test(value.cache_namespace) ? value.cache_namespace : "")
      .catch(() => "");
    const scope = await fullTextNamespacePromise;
    return scope ? `${window.location.pathname}:${scope}:${id}` : "";
  }

  function openFullTextBrowserDB() {
    if (!window.indexedDB) return Promise.resolve(null);
    if (fullTextBrowserDBPromise) return fullTextBrowserDBPromise;
    fullTextBrowserDBPromise = new Promise((resolve) => {
      let request;
      try {
        request = window.indexedDB.open(fullTextBrowserDBName, 1);
      } catch {
        resolve(null);
        return;
      }
      request.onupgradeneeded = () => {
        const db = request.result;
        if (!db.objectStoreNames.contains(fullTextBrowserStoreName)) {
          const store = db.createObjectStore(fullTextBrowserStoreName, {
            keyPath: "entry_id",
          });
          store.createIndex("cached_at", "cached_at");
        }
      };
      request.onsuccess = () => {
        const db = request.result;
        db.onversionchange = () => db.close();
        resolve(db);
      };
      request.onerror = () => resolve(null);
      request.onblocked = () => resolve(null);
    }).catch(() => null);
    return fullTextBrowserDBPromise;
  }

  function browserFullTextPayload(entry, value) {
    const blocks = Array.isArray(value?.blocks)
      ? value.blocks
      : value?.article_blocks;
    const payload = {
      entry_id: text(entry?.item?.id),
      content_hash: text(value?.content_hash || entry?.item?.content_hash),
      content_text: text(value?.content_text).slice(0, 32000),
      content_html: text(value?.content_html),
      content_format: text(value?.content_format, "text"),
      // Keep the structured projection with the browser cache. Older cache
      // records may omit these fields; citation reads fall back to AC so a
      // stale cache can never hide a paragraph anchor.
      blocks_version: text(value?.blocks_version || value?.article_blocks_version),
      blocks_hash: text(value?.blocks_hash || value?.article_blocks_hash),
      blocks: Array.isArray(blocks) ? blocks.slice(0, 500) : [],
      blocks_truncated: Boolean(
        value?.blocks_truncated ?? value?.article_blocks_truncated
      ),
      image_count: Number(value?.image_count) || 0,
      lead_image_index: Number.isInteger(Number(value?.lead_image_index))
        ? Number(value.lead_image_index)
        : -1,
      fetched_at: text(value?.fetched_at),
      status: text(value?.status, "ok"),
      source_url: text(value?.source_url || entry?.item?.url),
      cached: true,
    };
    if (!payload.entry_id || !payload.content_text) return null;
    try {
      if (JSON.stringify(payload).length > fullTextBrowserMaxBytes) return null;
    } catch {
      return null;
    }
    return payload;
  }

  async function readBrowserFullText(entry) {
    const id = await fullTextCacheKey(entry);
    if (!id) return null;
    const db = await openFullTextBrowserDB();
    if (!db) return null;
    return new Promise((resolve) => {
      let transaction;
      try {
        transaction = db.transaction(fullTextBrowserStoreName, "readwrite");
        const store = transaction.objectStore(fullTextBrowserStoreName);
        const request = store.get(id);
        request.onsuccess = () => {
          const record = request.result;
          const hash = text(entry.item.content_hash);
          const expired =
            !record ||
            !record.value ||
            !text(record.value.content_text) ||
            text(record.value.status) === "summary" ||
            articleContentLooksBlocked(record.value) ||
            Date.now() - Number(record.cached_at || 0) > fullTextBrowserCacheTTL;
          const recordHash = text(record?.content_hash);
          const revisionChanged = Boolean(hash || recordHash) && recordHash !== hash;
          if (expired || revisionChanged) {
            if (record) store.delete(id);
            resolve(null);
            return;
          }
          resolve(record.value);
        };
        request.onerror = () => resolve(null);
      } catch {
        resolve(null);
      }
    });
  }

  async function writeBrowserFullText(entry, value) {
    const recordValue = browserFullTextPayload(entry, value);
    const cacheKey = await fullTextCacheKey(entry);
    if (!recordValue || !cacheKey) return;
    const db = await openFullTextBrowserDB();
    if (!db) return;
    await new Promise((resolve) => {
      let transaction;
      try {
        transaction = db.transaction(fullTextBrowserStoreName, "readwrite");
        const store = transaction.objectStore(fullTextBrowserStoreName);
        store.put({
          entry_id: cacheKey,
          content_hash: recordValue.content_hash,
          cached_at: Date.now(),
          value: recordValue,
        });
        const all = store.getAll();
        all.onsuccess = () => {
          const rows = Array.isArray(all.result) ? all.result : [];
          rows
            .sort((left, right) => Number(right.cached_at) - Number(left.cached_at))
            .slice(fullTextBrowserCacheLimit)
            .forEach((row) => store.delete(row.entry_id));
        };
        transaction.oncomplete = () => resolve();
        transaction.onerror = () => resolve();
        transaction.onabort = () => resolve();
      } catch {
        resolve();
      }
    });
  }

  function deviceDataPath(path, params = {}) {
    const query = new URLSearchParams(params);
    return `./_zettlab/device-data/v1/${path}${query.toString() ? `?${query}` : ""}`;
  }

  function deviceMeetingDate(value, fallback = "") {
    const seconds = Number(value);
    if (!Number.isFinite(seconds) || seconds <= 0) return fallback;
    return new Date(seconds > 100000000000 ? seconds : seconds * 1000).toISOString();
  }

  function normalizeDeviceMeeting(item) {
    if (!item?.id) return null;
    const rawStatus = text(item.status).toLowerCase();
    const status = ["done", "completed", "summarized", "ready"].includes(rawStatus)
      ? "done"
      : rawStatus || "processing";
    return {
      id: text(item.id),
      title: text(item.title, "未命名会议"),
      status,
      duration_seconds: Number(item.duration_seconds) || 0,
      started_at: deviceMeetingDate(item.started_at, text(item.created_at)),
      created_at: text(item.created_at),
      updated_at: text(item.updated_at),
      speaker_count: Number(item.speaker_count) || 0,
      speaker_names:
        item.speaker_names && typeof item.speaker_names === "object"
          ? item.speaker_names
          : {},
      audio_ref: text(item.audio_ref),
      summary: text(item.summary),
      transcript: text(item.transcript),
      transcript_truncated: Boolean(item.transcript_truncated),
      segments: (Array.isArray(item.segments) ? item.segments : [])
        .map((segment) => ({
          speaker: text(segment?.speaker),
          start: Number(segment?.start ?? segment?.start_seconds) || 0,
          end: Number(segment?.end ?? segment?.end_seconds) || 0,
          text: text(segment?.text),
        }))
        .filter((segment) => segment.text),
      segments_truncated: Boolean(item.segments_truncated),
      per_speaker_summary: text(item.per_speaker_summary),
      report:
        typeof item.report === "string"
          ? item.report
          : item.report && typeof item.report === "object"
          ? JSON.stringify(item.report)
          : "",
      meeting_stage: text(item.meeting_stage || item.stage),
      error_message: text(item.error_message || item.error),
      live_device: true,
      source_type: "meeting",
    };
  }

  function normalizeObsidianEntry(item) {
    const format = text(item?.type).toLowerCase();
    if (!item?.path || !["markdown", "text", "pdf", "image"].includes(format))
      return null;
    const path = text(item.path);
    return {
      id: `live-obsidian:${path}`,
      title: text(item.name, path.split("/").pop() || "未命名文件"),
      locator: path,
      quote:
        format === "pdf"
          ? "来自 Obsidian 的实时 PDF"
          : format === "image"
          ? "来自 Obsidian 的实时图片"
          : format === "text"
          ? "来自 Obsidian 的实时文本"
          : "来自 Obsidian 的实时笔记",
      source_type: "obsidian",
      content_format: format,
      asset_url:
        format === "pdf" || format === "image"
          ? deviceDataPath("obsidian/asset", { path })
          : "",
      size: Number(item.size) || 0,
      observed_at: text(item.modified_at),
      modified_at: text(item.modified_at),
      live_device: true,
    };
  }

  function normalizeObsidianFolder(item, parentPath = "") {
    if (!item || item.type !== "directory" || !item.path) return null;
    const path = text(item.path).replace(/^\/+|\/+$/g, "");
    if (!path) return null;
    return {
      name: text(item.name, path.split("/").pop() || "未命名目录"),
      path,
      parent_path: text(parentPath).replace(/^\/+|\/+$/g, ""),
      modified_at: text(item.modified_at),
    };
  }

  async function loadLiveMeetings(offset = 0) {
    const result = await request(deviceDataPath("meetings", { limit: 50, offset }), { timeoutMs: 7000 });
    const rows = (result.meetings || []).map(normalizeDeviceMeeting).filter(Boolean);
    liveDeviceData.meetingNextOffset = offset + rows.length;
    liveDeviceData.meetingHasMore = Boolean(result.has_more && rows.length);
    return rows;
  }

  async function loadMoreMeetings() {
    if (liveDeviceData.meetingLoading || !liveDeviceData.meetingHasMore) return;
    liveDeviceData.meetingLoading = true;
    renderTimeline();
    try {
      const rows = await loadLiveMeetings(liveDeviceData.meetingNextOffset);
      const merged = new Map((state.liveMeetings || []).map(row => [row.id, row]));
      rows.forEach(row => merged.set(row.id, row));
      // Keep the most recent browsing window bounded, like article pages.
      state.liveMeetings = [...merged.values()].slice(-1000);
      parity.deviceListError = "";
    } catch (error) {
      parity.deviceListError = text(error.message, "会议读取失败，请重试");
    } finally {
      liveDeviceData.meetingLoading = false;
      if (liveDeviceDataView()) { $("timeline").dataset.renderFingerprint = ""; renderTimeline(); }
    }
  }
  document.addEventListener("click", event => {
    if (event.target.closest("[data-load-more-meetings]")) void loadMoreMeetings();
    if (event.target.closest("[data-retry-device-list]")) scheduleLiveDeviceDataRefresh(true, false);
    if (event.target.closest("[data-retry-reader]")) void refresh(false);
    const pictureTrigger = event.target.closest("[data-open-picture]");
    if (pictureTrigger) {
      event.preventDefault();
      event.stopPropagation();
      openPictureLightbox(pictureTrigger.getAttribute("data-open-picture") || "");
    }
  });

  async function loadMoreMeetings() {
    if (liveDeviceData.meetingLoading || !liveDeviceData.meetingHasMore) return;
    liveDeviceData.meetingLoading = true;
    renderTimeline();
    try {
      const rows = await loadLiveMeetings(liveDeviceData.meetingNextOffset);
      const merged = new Map((state.liveMeetings || []).map(row => [row.id, row]));
      rows.forEach(row => merged.set(row.id, row));
      // Keep the most recent browsing window bounded, like article pages.
      state.liveMeetings = [...merged.values()].slice(-1000);
      parity.deviceListError = "";
    } catch (error) {
      parity.deviceListError = text(error.message, "会议读取失败，请重试");
    } finally {
      liveDeviceData.meetingLoading = false;
      if (liveDeviceDataView()) { $("timeline").dataset.renderFingerprint = ""; renderTimeline(); }
    }
  }
  function mergeLiveObsidianFolderSnapshot(path, result) {
    const normalized = normalizeObsidianPath(path);
    const notes = [];
    const folders = [];
    for (const item of Array.isArray(result?.entries) ? result.entries : []) {
      if (item.type === "directory") {
        const folder = normalizeObsidianFolder(item, normalized);
        if (folder) folders.push(folder);
        continue;
      }
      const note = normalizeObsidianEntry(item);
      if (note) notes.push(note);
    }
    const existingNotes = Array.isArray(state.liveObsidian)
      ? state.liveObsidian
      : [];
    state.liveObsidian = existingNotes
      .filter(
        (entry) =>
          obsidianPathParent(entry.locator || entry.path) !== normalized
      )
      .concat(notes);
    state.liveObsidian.forEach((entry) => {
      const cached = liveDeviceData.obsidianDetails.get(entry.locator);
      if (cached?.content) entry.content = cached.content;
      if (cached?.asset_url) entry.asset_url = cached.asset_url;
    });
    const existingFolders = Array.isArray(state.liveObsidianFolders)
      ? state.liveObsidianFolders
      : [];
    state.liveObsidianFolders = existingFolders
      .filter((folder) => normalizeObsidianPath(folder.parent_path) !== normalized)
      .concat(folders);
    liveDeviceData.obsidianLoadedFolders.add(normalized);
    return true;
  }

  function loadLiveObsidianFolder(path, force = false) {
    const normalized = normalizeObsidianPath(path);
    if (!force && liveDeviceData.obsidianLoadedFolders.has(normalized))
      return Promise.resolve(false);
    const current = liveDeviceData.obsidianFolderInflight.get(normalized);
    if (current) return current;
    const pending = (async () => {
      const entries = [];
      let cursor = "";
      let restarted = false;
      for (let page = 0; page < 164; page++) {
        let result;
        try {
          result = await request(deviceDataPath("obsidian/list", { path: normalized, limit: 200, ...(cursor ? { cursor } : {}) }), { timeoutMs: 7000 });
        } catch (error) {
          if (error.status === 410 && cursor && !restarted) {
            restarted = true; cursor = ""; entries.length = 0; page = -1; continue;
          }
          throw error;
        }
        if (entries.length + (result.entries || []).length > 32768) throw new Error("目录超过读取上限，请缩小范围");
      entries.push(...(result.entries || []));
        cursor = text(result.next_cursor);
        if (!result.has_more || !cursor) {
          mergeLiveObsidianFolderSnapshot(normalized, { entries });
          void loadObsidianCounts(normalized).then(() => { if (liveDeviceDataView()) renderTimeline(); }).catch(() => {});
          return true;
        }
      }
      throw new Error("目录超过读取上限，请缩小范围");
    })().finally(() => { liveDeviceData.obsidianFolderInflight.delete(normalized); });
    liveDeviceData.obsidianFolderInflight.set(normalized, pending);
    return pending;
  }

  async function restoreExpandedObsidianFolders(force = false) {
    const visited = new Set();
    for (let depth = 0; depth < 64; depth++) {
      const folders = (state.liveObsidianFolders || []).filter(folder =>
        !obsidianFolderCollapsed(folder.path) && !visited.has(folder.path)).slice(0, Math.max(0, 200 - visited.size));
      if (!folders.length) break;
      for (let i = 0; i < folders.length; i += 4) {
        await Promise.allSettled(folders.slice(i, i + 4).map(folder => {
          visited.add(folder.path);
          return loadLiveObsidianFolder(folder.path, force);
        }));
      }
      if (visited.size >= 200) break;
    }
  }

  function loadLiveObsidian(force = false) {
    // Initial entry reads one bounded root page. Descendants are loaded only
    // when their disclosure row is expanded, and repeated clicks share the
    // same in-flight request.
    return loadLiveObsidianFolder("", force).then((changed) => {
      return restoreExpandedObsidianFolders(force).then(() => changed);
    });
  }

  function loadLiveDeviceData(force = false) {
    // Meetings and the Obsidian vault are a session snapshot. The previous
    // one-minute TTL recursively walked the complete vault every minute (one
    // HTTP request per folder), even when nothing on screen requested a
    // refresh. Keep the snapshot for this app session and invalidate it only
    // from the explicit refresh control below.
    if (!force && liveDeviceData.loadedAt > 0)
      return Promise.resolve(false);
    if (liveDeviceData.inflight) return liveDeviceData.inflight;
    const settings = parity.settings;
    liveDeviceData.inflight = Promise.allSettled([
      loadLiveMeetings(),
      settings?.obsidian_enabled === false
        ? Promise.resolve(false)
        : loadLiveObsidian(force),
    ])
      .then(([meetings, obsidian]) => {
        let changed = false;
        if (meetings.status === "fulfilled") {
          state.liveMeetings = meetings.value;
          changed = true;
        }
        if (obsidian.status === "fulfilled") {
          changed = Boolean(obsidian.value) || changed;
        }
        liveDeviceData.loadedAt = changed ? Date.now() : 0;
        if (meetings.status === "rejected" && ["meeting", "recording", "source:my-recordings", "source:device-meetings"].includes(state.view))
          throw meetings.reason;
        if (obsidian.status === "rejected" && (state.view === "obsidian" || state.view.startsWith("obsidian-folder:")))
          throw obsidian.reason;
        return changed;
      })
      .finally(() => {
        liveDeviceData.inflight = null;
      });
    return liveDeviceData.inflight;
  }

  function liveDeviceDataView(view = state.view) {
    return [
      "meeting",
      "recording",
      "obsidian",
      "source:my-recordings",
      "source:device-meetings",
      "source:obsidian",
    ].includes(text(view)) || text(view).startsWith("obsidian-folder:");
  }

  function scheduleLiveDeviceDataRefresh(
    force = false,
    silent = true,
    view = state.view
  ) {
    // Cloud feeds must not trigger a meeting or filesystem scan. Device data
    // is loaded only when the user enters a view that can display it.
    if (!force && !liveDeviceDataView(view)) return;
    parity.deviceListError = "";
    void loadLiveDeviceData(force)
      .then((changed) => {
        if (changed && state.view === view && state.dashboard && !document.hidden) render();
      })
      .catch((error) => {
        if (state.view !== view) return;
        parity.deviceListError = text(error?.message, "设备内容暂时读取失败");
        if (liveDeviceDataView()) renderTimeline();
        if (!silent && liveDeviceDataView(state.view))
          showError(error?.message || "设备内容暂时读取失败，可继续查看云端文章");
      });
  }

  async function loadLiveMeetingDetail(id) {
    const key = text(id);
    if (!key) return null;
    if (liveDeviceData.meetingDetails.has(key)) return liveDeviceData.meetingDetails.get(key);
    const result = await request(deviceDataPath(`meetings/${encodeURIComponent(key)}`), {
      timeoutMs: 7000,
    });
    const meeting = normalizeDeviceMeeting(result?.meeting || {});
    if (meeting) {
      meeting.transcript = text(result?.meeting?.transcript);
      meeting.summary = text(result?.meeting?.summary);
      meeting.per_speaker_summary = text(result?.meeting?.per_speaker_summary);
      meeting.report = text(result?.meeting?.report);
      liveDeviceData.meetingDetails.set(key, meeting);
    }
    return meeting;
  }

  async function loadLiveObsidianDetail(path) {
    const key = text(path);
    if (!key) return null;
    if (liveDeviceData.obsidianDetails.has(key)) {
      const cached = liveDeviceData.obsidianDetails.get(key);
      const current = state.liveObsidian.find((item) => item.locator === key);
      if (current && cached?.content) current.content = cached.content;
      return current || cached;
    }
    const entry = state.liveObsidian.find((item) => item.locator === key);
    const format = text(entry?.content_format).toLowerCase();
    if (["pdf", "image"].includes(format)) {
      if (entry) {
        entry.asset_url = deviceDataPath("obsidian/asset", { path: key });
        liveDeviceData.obsidianDetails.set(key, entry);
      }
      return entry || null;
    }
    const result = await request(deviceDataPath("obsidian/read", { path: key }), {
      timeoutMs: 7000,
    });
    const content = text(result?.content);
    if (entry) {
      entry.content = content;
      liveDeviceData.obsidianDetails.set(key, entry);
    }
    return entry || null;
  }

  window.__plaudLiveMeetingDetail = loadLiveMeetingDetail;
  window.__plaudLiveObsidianDetail = loadLiveObsidianDetail;

  function replacePagedEntries(items) {
    if (!state.dashboard) return;
    state.dashboard.news = boundReaderNews(
      (Array.isArray(items) ? items : []).map(normalizePagedEntry).filter(Boolean)
    );
  }

  function mergeReaderEntry(existing, incoming) {
    const oldHash = text(existing?.content_hash);
    const newHash = text(incoming?.content_hash);
    const oldUpdated = Date.parse(
      existing?.updated_at || existing?.fetched_at || ""
    ) || 0;
    const newUpdated = Date.parse(
      incoming?.updated_at || incoming?.fetched_at || ""
    ) || 0;
    const oldRevision = Number(existing?.revision) || 0;
    const newRevision = Number(incoming?.revision) || 0;
    // A repeated ID is not necessarily the same revision: feed workers can
    // enrich an article after the first dashboard snapshot. Prefer the newer
    // typed-page representation, while retaining full text that a non-full
    // page intentionally omitted.
    if (
      !existing ||
      (newHash && oldHash && newHash !== oldHash) ||
      (newHash && !oldHash) ||
      newUpdated > oldUpdated ||
      newRevision > oldRevision
    ) {
      const merged = { ...existing, ...incoming };
      if (!incoming.safe_html && existing?.safe_html)
        merged.safe_html = existing.safe_html;
      if (!incoming.ai_overview && existing?.ai_overview)
        merged.ai_overview = existing.ai_overview;
      // A newer list row can still be projection-light. Do not let that row
      // erase anchors already hydrated for the same article revision.
      if (
        (!newHash || !oldHash || newHash === oldHash) &&
        (!Array.isArray(incoming.blocks) || !incoming.blocks.length) &&
        Array.isArray(existing?.blocks) &&
        existing.blocks.length
      ) {
        merged.blocks = existing.blocks;
        merged.blocks_version = existing.blocks_version;
        merged.blocks_hash = existing.blocks_hash;
        merged.blocks_truncated = existing.blocks_truncated;
      }
      if (
        (!newHash || !oldHash || newHash === oldHash) &&
        (!Array.isArray(incoming.citations) || !incoming.citations.length) &&
        Array.isArray(existing?.citations) &&
        existing.citations.length
      )
        merged.citations = existing.citations;
      return merged;
    }
    // The list endpoint intentionally omits heavy article projections, while
    // a later page or an inline refresh may carry the structured ArticleBlock
    // anchors. Keep those optional fields even when the display row itself is
    // not newer; otherwise a summary snapshot silently drops block IDs and a
    // citation can no longer land on its paragraph after the merge.
    const merged = { ...existing };
    if (
      Array.isArray(incoming.blocks) &&
      incoming.blocks.length &&
      (!Array.isArray(existing.blocks) || !existing.blocks.length)
    ) {
      merged.blocks = incoming.blocks;
      merged.blocks_version = incoming.blocks_version || existing.blocks_version;
      merged.blocks_hash = incoming.blocks_hash || existing.blocks_hash;
      merged.blocks_truncated = incoming.blocks_truncated ?? existing.blocks_truncated;
    }
    if (
      Array.isArray(incoming.citations) &&
      incoming.citations.length &&
      (!Array.isArray(existing.citations) || !existing.citations.length)
    )
      merged.citations = incoming.citations;
    if (!incoming.safe_html && existing?.safe_html)
      merged.safe_html = existing.safe_html;
    if (!incoming.ai_overview && existing?.ai_overview)
      merged.ai_overview = existing.ai_overview;
    return merged;
  }

  function mergePagedEntries(items) {
    if (!state.dashboard || !Array.isArray(items) || !items.length) {
      const status = readerNewsWindowStatus(state.dashboard?.news || []);
      return { added: 0, truncated: false, ...status };
    }
    const merged = new Map(
      (state.dashboard.news || []).map((item) => [item.id, item])
    );
    let added = 0;
    for (const raw of items) {
      const item = normalizePagedEntry(raw);
      if (!item) continue;
      if (!merged.has(item.id)) added += 1;
      merged.set(item.id, mergeReaderEntry(merged.get(item.id), item));
    }
    const bounded = boundReaderNews([...merged.values()]);
    state.dashboard.news = bounded;
    const status = readerNewsWindowStatus(bounded);
    return {
      added,
      truncated: bounded.length < merged.size,
      ...status,
    };
  }

  function savedNewsProjectionComplete() {
    return ["starred", "later"].includes(state.view) &&
      parity.entryViewLoaded.has(state.view) && !parity.entryHasMore;
  }

  async function loadMoreEntries() {
    // IntersectionObserver callbacks can already be queued when navigation
    // leaves a reader projection. Never let one of those callbacks issue a
    // late feed request or merge rows while the Focus workspace owns the
    // shell.
    if (
      focusV3Views.has(state.view) ||
      document.body.classList.contains("focus-v3-active") ||
      savedNewsProjectionComplete()
    )
      return;
    // Typed cursor compatibility: ./api/entries?limit=${parity.entryPageSize}&cursor=
    if (
      parity.entryLoading ||
      parity.entryScopeLoading ||
      !parity.entryHasMore ||
      parity.entryPagesRead >= readerEntryPageLimit
    ) {
      if (parity.entryPagesRead >= readerEntryPageLimit) {
        parity.entryHasMore = false;
        renderTimeline();
      }
      return;
    }
    parity.entryLoading = true;
    parity.entryPageError = "";
    // Keep the existing DOM and scroll position while the next page is in
    // flight. Replacing the whole timeline here used to cause two extra full
    // renders per page and made the list feel stuck on slower boards.
    const loadMoreButton = document.querySelector(
      "[data-load-more-entries]"
    );
    if (loadMoreButton) {
      loadMoreButton.disabled = true;
      loadMoreButton.setAttribute("aria-busy", "true");
      loadMoreButton.textContent = "正在读取更多内容…";
    }
    // The button is updated in place to preserve scroll position. Invalidate
    // the render fingerprint so the completion path cannot skip the first
    // idle render and leave this button permanently disabled.
    document
      .querySelector("#timeline")
      ?.removeAttribute("data-render-fingerprint");
    const requestToken = parity.entryRequestToken;
    const requestScope = parity.entryScope;
    parity.entryLoadingView = text(state.view);
    parity.entryLoadingOwnerToken = requestToken;
    try {
      const cursor = parity.entryCursor || "0";
      const params = new URLSearchParams({
        limit: String(parity.entryPageSize),
        cursor,
      });
      if (parity.entryScope) params.set("source_id", parity.entryScope);
      if (["starred", "later"].includes(state.view)) params.set("saved", state.view === "starred" ? "starred" : "read_later");
      const scopedSource = (state.sources || []).find(
        (source) => source.id === parity.entryScope
      );
      if (["rss", "video", "youtube", "picture"].includes(state.view)) {
        params.set(
          "view_type",
          state.view === "rss"
            ? "rss"
            : state.view === "youtube"
            ? "youtube"
            : state.view === "picture"
            ? "picture"
            : "video"
        );
      } else if (videoProviderForSource(scopedSource)) {
        params.set("view_type", "video");
      }
      const result = await request(`./api/entries?${params.toString()}`);
      if (
        requestToken !== parity.entryRequestToken ||
        requestScope !== parity.entryScope
      ) {
        // A dashboard refresh or scope switch may invalidate this response.
        // Only the request that currently owns the busy flag may release it;
        // a newer request for the same scope must keep its loading state.
        if (
          requestScope === parity.entryScope &&
          parity.entryLoadingOwnerToken === requestToken &&
          !parity.entryScopeLoading
        ) {
          parity.entryLoading = false;
          parity.entryLoadingOwnerToken = null;
          parity.entryLoadingView = "";
          renderTimeline();
        }
        return;
      }
      const mergeResult = mergePagedEntries(result.entries || []);
      const pageTotal = finiteEntryTotal(result.total_entries);
      if (pageTotal !== null && isAggregateEntryView(state.view)) {
        parity.entryViewTotals.set(text(state.view), pageTotal);
        const field = aggregateTotalField(state.view);
        if (field && state.dashboard) state.dashboard[field] = pageTotal;
        if (field && parity.counts) parity.counts[field] = pageTotal;
      }
      parity.entryPagesRead += 1;
      const nextCursor = text(result.next_cursor_token || result.next_cursor);
      // A broken/old host can echo the same cursor. Stop rather than polling
      // the last page forever; a later dashboard refresh can reopen paging.
      parity.entryCursor = nextCursor;
      // Do not evict the visible tail when the local projection reaches the
      // device retention ceiling. Keeping the existing rows preserves the
      // scroll anchor; the authoritative cursor still remains available for a
      // later refresh after the dashboard snapshot changes.
      parity.entryHasMore = Boolean(
        nextCursor &&
          nextCursor !== cursor &&
          (parity.contextMembershipsReady
            ? !mergeResult.windowsFull
            : state.dashboard.news.length < readerNewsTotalLimit) &&
          entryRemotePageRemaining(result.total_entries) &&
          parity.entryPagesRead < readerEntryPageLimit
      );
      if (!parity.entryScope)
        rememberAggregateEntrySnapshot(true, state.view);
      parity.entryLoading = false;
      parity.entryLoadingOwnerToken = null;
      parity.entryLoadingView = "";
      // Pagination only changes the reader projection. Avoid rebuilding the
      // navigation, source directory, catalog, and overview on every page;
      // refresh the detail pane only when the selected entry needs it.
      renderTimeline();
      if (state.selected) renderDetail();
    } catch (error) {
      if (
        requestToken !== parity.entryRequestToken ||
        requestScope !== parity.entryScope
      ) {
        if (
          requestScope === parity.entryScope &&
          parity.entryLoadingOwnerToken === requestToken &&
          !parity.entryScopeLoading
        ) {
          parity.entryLoading = false;
          parity.entryLoadingOwnerToken = null;
          parity.entryLoadingView = "";
          renderTimeline();
        }
        return;
      }
      parity.entryLoading = false;
      parity.entryLoadingOwnerToken = null;
      parity.entryLoadingView = "";
      // Stop the sentinel from immediately reissuing the same failed cursor
      // request.  The cursor is retained and the explicit retry action below
      // can resume it when the user is ready.
      parity.entryHasMore = false;
      parity.entryPageError = text(error?.message, "更多内容暂时读取失败");
      renderTimeline();
      showError(error.message || "更多内容暂时读取失败");
    }
  }

  function sourceCanLoadHistory(source) {
    return Boolean(
      source?.status === "partial" &&
        (source.id.startsWith("bilibili-") ||
          source.id.startsWith("wechat-") &&
            text(source.url).includes("jintiankansha.me/column/"))
    );
  }

  async function loadMoreSourceHistory(sourceID) {
    const id = text(sourceID);
    if (!id || parity.sourceHistoryLoading.has(id)) return;
    parity.sourceHistoryLoading.add(id);
    renderTimeline();
    setSourceBusy(sourceID, true);
    try {
      await jsonRequest("./api/sync", "POST", { source_id: id });
      await monitorSourceSync(id);
    } catch (error) {
      showError(error.message || "更早文章暂时读取失败");
    } finally {
      parity.sourceHistoryLoading.delete(id);
      renderTimeline();
    }
  }

  function observeEntryPagination() {
    parity.entryObserver?.disconnect();
    const timeline = $("timeline");
    const entrySentinel = timeline?.querySelector("[data-entry-page-sentinel]");
    const sourceSentinel = timeline?.querySelector("[data-source-history-sentinel]");
    if (!timeline || (!entrySentinel && !sourceSentinel)) return;
    const observer = new IntersectionObserver(
      (entries) => {
        // An IntersectionObserver may deliver a callback that was queued just
        // before disconnect(). Ignore that callback once a newer projection
        // owns the sentinel; otherwise an old article/source view can start a
        // request for the newly selected view during navigation.
        if (parity.entryObserver !== observer) return;
        for (const entry of entries) {
          if (!entry.isIntersecting) continue;
          if (entry.target === sourceSentinel) void loadMoreSourceHistory(parity.entryScope);
          else void loadMoreEntries();
        }
      },
      { root: timeline, rootMargin: "0px 0px 320px 0px", threshold: 0.01 }
    );
    parity.entryObserver = observer;
    if (entrySentinel && parity.entryHasMore) observer.observe(entrySentinel);
    if (sourceSentinel && sourceCanLoadHistory(selectedCatalogSource())) observer.observe(sourceSentinel);
  }

  function rememberEntryScopeSnapshot(scope = parity.entryScope) {
    const key = text(scope);
    if (!key || !Array.isArray(state.dashboard?.news)) return;
    parity.entryScopeSnapshots.set(key, {
      news: state.dashboard.news.map((item) => ({ ...item })),
      cursor: parity.entryCursor,
      pagesRead: parity.entryPagesRead,
      hasMore: parity.entryHasMore,
    });
  }

  function invalidateReaderProjectionRequests() {
    if (parity.entryScope && !parity.entryScopeLoading)
      rememberEntryScopeSnapshot();
    if (!parity.entryScope) rememberAggregateEntrySnapshot();
    const restoredAggregate = restoreAggregateEntrySnapshot();
    const aggregateNews = restoredAggregate
      ? state.dashboard?.news || []
      : Array.isArray(parity.entryBaseNews)
      ? parity.entryBaseNews.map((item) => ({ ...item }))
      : Array.isArray(state.dashboard?.news)
      ? state.dashboard.news.map((item) => ({ ...item }))
      : [];
    if (!restoredAggregate) {
      if (!Array.isArray(parity.entryBaseNews))
        parity.entryBaseNews = aggregateNews.map((item) => ({ ...item }));
      if (state.dashboard) state.dashboard.news = aggregateNews;
    }
    parity.entryRequestToken += 1;
    parity.entryLoading = false;
    parity.entryLoadingOwnerToken = null;
    parity.entryLoadingView = "";
    parity.entryViewLoading = "";
    parity.entryPageError = "";
    parity.entryScopeLoading = false;
    parity.entryScope = "";
    if (!restoredAggregate) {
      parity.entryCursor = "0";
      parity.entryPagesRead = 0;
      const aggregateTotal = entryTotalForView(state.view);
      const aggregateLoadedCount = loadedAggregateEntryCount(
        state.view,
        aggregateNews
      );
      parity.entryHasMore =
        aggregateLoadedCount < readerNewsTotalLimit &&
        (Number.isFinite(aggregateTotal)
          ? aggregateTotal > aggregateLoadedCount
          : aggregateLoadedCount >= parity.entryPageSize);
    }
    parity.detailLoading = false;
    parity.videoInitialRequestStarted = false;
    parity.entryObserver?.disconnect();
    parity.entryObserver = null;
    parity.xFeedObserver?.takeRecords?.();
    parity.xFeedObserver?.disconnect();
    parity.xFeedObserver = null;
    parity.xFeedHydrationQueue.length = 0;
    parity.xFeedHydrationQueued.clear();
    parity.xFeedAvatarEntryID = "";
  }

  function restoreEntryScopeSnapshot(scope) {
    const snapshot = parity.entryScopeSnapshots.get(text(scope));
    if (!snapshot || !Array.isArray(snapshot.news)) return false;
    state.dashboard.news = snapshot.news.map((item) => ({ ...item }));
    parity.entryCursor = text(snapshot.cursor);
    parity.entryPagesRead = Number(snapshot.pagesRead) || 0;
    parity.entryHasMore = Boolean(snapshot.hasMore);
    return true;
  }

  function renderReaderProjectionOnly() {
    renderNav();
    renderTimeline();
    if (state.selected) renderDetail();
    else renderOverview();
    updateSourceBar();
  }

  async function switchEntryScope(scope, options = {}) {
    const target = text(scope);
    if (
      !state.dashboard ||
      (!options.force && target === parity.entryScope && !parity.entryScopeLoading)
    )
      return;
    const previousScope = parity.entryScope;
    if (previousScope && previousScope !== target)
      rememberEntryScopeSnapshot(previousScope);
    if (!previousScope && target) rememberAggregateEntrySnapshot();
    const token = ++parity.entryRequestToken;
    parity.entryViewLoading = "";
    parity.entryLoading = false;
    parity.entryLoadingOwnerToken = null;
    parity.entryLoadingView = "";
    parity.entryPageError = "";
    parity.entryScopeError = "";
    parity.entryScope = target;
    parity.entryCursor = "0";
    parity.entryPagesRead = 0;
    // A bounded snapshot may be shorter than one page while older rows still
    // exist. Let the typed endpoint return the authoritative next_cursor.
    parity.entryHasMore = true;
    parity.entryLoading = false;
    parity.entryLoadingOwnerToken = null;
    const restored = target ? restoreEntryScopeSnapshot(target) : false;
    parity.entryScopeLoading = Boolean(target);
    if (restored) parity.entryScopeLoading = false;
    // Preserve the open detail while the scoped list is loading. The detail
    // panel shows a skeleton rather than falling back to the overview.
    parity.detailLoading = true;
    if (!target) {
      parity.entryScopeLoading = false;
      if (!restoreAggregateEntrySnapshot()) {
        state.dashboard.news = (parity.entryBaseNews || []).map((item) => ({
          ...item,
        }));
      }
      parity.detailLoading = false;
      renderReaderProjectionOnly();
      return;
    }
    // Revisited sources paint their bounded local snapshot immediately. A
    // first visit limits the loading state to the timeline; the navigation
    // and surrounding shell stay mounted.
    if (restored) {
      parity.detailLoading = false;
      renderReaderProjectionOnly();
    }
    // The source directory is loaded independently from the first reader
    // paint.  If the user clicks a source immediately, resolve just this
    // source before deciding whether an empty page should enqueue its sync;
    // otherwise the first click can observe an empty `state.sources` and
    // permanently skip hydration until a manual refresh.
    let scopedSource = sourceFromState(target);
    if (!scopedSource) {
      scopedSource = await resolveSourceMetadata(target);
    } else if (
      !scopedSource.subscribed &&
      text(scopedSource.collection_mode).toLowerCase() === "source_gateway" &&
      sourceMetadataRefreshDue(target)
    ) {
      // A first-install local row can briefly exist before the server
      // subscription projection is materialized. Re-read only this source so
      // a click made during that race can observe the newly subscribed state;
      // an explicit unsubscribe remains non-syncing because the refreshed row
      // is checked again below.
      scopedSource = (await resolveSourceMetadata(target, true)) || scopedSource;
    }
    if (token !== parity.entryRequestToken || entryScopeForView() !== target)
      return;
    try {
      const params = new URLSearchParams({
        limit: String(parity.entryPageSize),
        cursor: "0",
        source_id: target,
      });
      if (["rss", "video", "youtube", "picture"].includes(state.view)) {
        params.set(
          "view_type",
          state.view === "rss"
            ? "rss"
            : state.view === "youtube"
            ? "youtube"
            : state.view === "picture"
            ? "picture"
            : "video"
        );
      } else if (videoProviderForSource(scopedSource)) {
        params.set("view_type", "video");
      }
      const result = await request(`./api/entries?${params.toString()}`, {
        timeoutMs: sourceEntryRequestTimeoutMs,
      });
      if (token !== parity.entryRequestToken || entryScopeForView() !== target)
        return;
      const pageEntries = Array.isArray(result.entries) ? result.entries : [];
      replacePagedEntries(pageEntries);
      // Navigation only reads the existing local snapshot. Explicit refresh
      // controls own sync enqueueing so opening a source never blocks on a
      // network fetch or silently starts a background job.
      // A source-scoped page has its own total. Never write it into the
      // aggregate video/YouTube/RSS counters; the independent counts endpoint
      // owns those values and the source badge is updated separately.
      // The scoped request above is the first page already consumed. Count it
      // toward the same hard cursor budget used by loadMoreEntries so a source
      // view cannot make one extra request past the device's 10,000-row cursor
      // ceiling.
      parity.entryPagesRead = 1;
      if (
        state.selected &&
        !makeEntries(state.dashboard || {}).some(
          (entry) => entry.key === state.selected
        )
      )
        state.selected = null;
      parity.entryCursor = text(result.next_cursor_token || result.next_cursor);
      parity.entryHasMore = Boolean(
        parity.entryCursor &&
          entryRemotePageRemaining(result.total_entries) &&
          parity.entryPagesRead < readerEntryPageLimit
      );
      rememberEntryScopeSnapshot(target);
    } catch (error) {
      if (token === parity.entryRequestToken) {
        // The cloud source can be visible in the lightweight catalog before
        // its server-side feed state has been reconciled.  Treat that typed
        // 503 as a recoverable first-hydration state: enqueue the same
        // per-source sync used by an authoritative empty page, keep the
        // surrounding reader mounted, and let monitorSourceSync reload this
        // source when the local projection is ready.  Other 503s and all
        // non-retryable errors retain the normal error path.
        parity.entryScopeError = text(
          error?.message,
          readerFocusCopy("来源内容暂时读取失败", "Source content is temporarily unavailable")
        );
        if (
          error?.status === 503 &&
          scopedSource?.subscribed &&
          sourceSupportsOnDemandSync(scopedSource)
        ) {
          showToast(`${scopedSource.name || readerFocusCopy("来源", "Source")} ${readerFocusCopy("暂无本地快照，请点击刷新同步", "has no local snapshot yet; use Refresh to sync")}`);
        } else {
          showError(parity.entryScopeError);
        }
      }
    } finally {
      if (token === parity.entryRequestToken) {
        parity.entryScopeLoading = false;
        parity.detailLoading = false;
        renderReaderProjectionOnly();
      }
    }
  }

  async function reloadSourceScopeAfterSync(sourceID) {
    const target = text(sourceID);
    if (!target || !state.dashboard || state.view !== `source:${target}`)
      return;
    // A pending sync may have produced an empty first page. Do not restore
    // that page on the forced read; the newly completed local sync is now the
    // authoritative source projection.
    parity.entryScopeSnapshots.delete(target);
    await switchEntryScope(target, { force: true });
  }

  function readerFocusIdempotencyKey(action, revision, subject = "") {
    // The key is generated by the UI once per mutation and sent in the
    // transport header. It is deliberately kept out of the typed JSON body.
    const suffix = text(subject).replace(/[^A-Za-z0-9._:-]/g, "_").slice(0, 96);
    return `reader-focus:${action}:${Number(revision) || 0}:${suffix}:${uuidV4()}`.slice(0, 200);
  }

  async function loadReaderFocusState(silent = false) {
    if (parity.readerFocusLoading) return parity.readerFocus;
    parity.readerFocusLoading = true;
    try {
      const value = await request("./api/reader-focus", { timeoutMs: 7000 });
      parity.readerFocus = value && typeof value === "object" ? value : null;
      parity.readerFocusMessage = "";
      return parity.readerFocus;
    } catch (error) {
      // Older board snapshots do not expose this optional endpoint. Keep the
      // rest of settings usable and show an inline, recoverable message.
      parity.readerFocus = null;
      parity.readerFocusMessage = error?.message || readerFocusCopy("当前焦点设置暂时不可用", "Current focus settings are temporarily unavailable");
      if (!silent) showError(parity.readerFocusMessage);
      return null;
    } finally {
      parity.readerFocusLoading = false;
    }
  }

  function readerFocusTopicStatus(topic) {
    const status = text(topic?.bootstrap_status || "pending").toLowerCase();
    if (topic?.enabled === false)
      return { label: readerFocusCopy("已暂停", "Paused"), kind: "" };
    if (status === "done")
      return { label: readerFocusCopy("已完成首次分析", "Initial analysis complete"), kind: "green" };
    if (status === "active")
      return { label: readerFocusCopy("正在准备", "Preparing"), kind: "amber" };
    if (status === "skipped")
      return { label: readerFocusCopy("仅推荐", "Recommendations only"), kind: "" };
    return { label: readerFocusCopy("待选择首次分析", "Initial analysis not selected"), kind: "amber" };
  }

  function renderReaderFocusSettings() {
    const root = parity.readerFocus;
    if (parity.readerFocusLoading && !root)
      return `<section class="settings-section reader-focus-settings" data-reader-focus-settings><div class="settings-section-head"><h3>${esc(readerFocusCopy("当前焦点", "Current focus"))}</h3></div><p class="settings-hint" aria-busy="true">${esc(readerFocusCopy("正在读取当前焦点…", "Loading current focus…"))}</p></section>`;
    if (!root || typeof root !== "object")
      return `<section class="settings-section reader-focus-settings is-unavailable" data-reader-focus-settings><div class="settings-section-head"><h3>${esc(readerFocusCopy("当前焦点", "Current focus"))}</h3></div><p class="settings-hint">${esc(parity.readerFocusMessage || readerFocusCopy("当前看板暂不支持焦点设置", "Focus settings are not available on this board"))}</p><button class="text-btn" type="button" data-reader-focus-reload>${esc(readerFocusCopy("重新读取", "Reload"))}</button></section>`;

    const topics = Array.isArray(root.topics) ? root.topics.slice(0, 8) : [];
    const policy = root.policy && typeof root.policy === "object" ? root.policy : {};
    const revision = Number(root.revision) || 1;
    const busyAttr = parity.readerFocusBusy ? " disabled aria-busy=\"true\"" : "";
    const topicRows = topics.length
      ? topics.map((topic) => {
          const topicID = text(topic.id).slice(0, 128);
          const status = readerFocusTopicStatus(topic);
          return `<div class="reader-focus-topic-row" data-reader-focus-topic="${esc(topicID)}">
            <div class="reader-focus-topic-fields">
              <label class="settings-field"><span>${esc(readerFocusCopy("焦点名称", "Focus name"))}</span><input data-reader-focus-label type="text" maxlength="120" value="${esc(text(topic.label))}" /></label>
              <label class="settings-field"><span>${esc(readerFocusCopy("检索词 / 别名", "Search terms / aliases"))}</span><input data-reader-focus-terms type="text" maxlength="2048" value="${esc((Array.isArray(topic.terms) ? topic.terms : []).join(", "))}" /></label>
            </div>
            <div class="reader-focus-topic-foot"><span class="badge ${esc(status.kind)}">${esc(status.label)}</span><span class="settings-hint">${esc(readerFocusCopy("最多 32 个关键词", "Up to 32 keywords"))}</span><span class="reader-focus-topic-actions"><button class="text-btn" type="button" data-reader-focus-save="${esc(topicID)}"${busyAttr}>${esc(readerFocusCopy("保存", "Save"))}</button><button class="text-btn" type="button" data-reader-focus-toggle="${esc(topicID)}"${busyAttr}>${esc(topic.enabled === false ? readerFocusCopy("恢复", "Resume") : readerFocusCopy("暂停", "Pause"))}</button><button class="icon-btn danger" type="button" data-reader-focus-remove="${esc(topicID)}"${busyAttr} aria-label="${esc(readerFocusCopy("删除主题", "Delete topic"))}" title="${esc(readerFocusCopy("删除主题", "Delete topic"))}">${icon("trash")}</button></span></div>
          </div>`;
        }).join("")
      : `<p class="settings-hint">${esc(readerFocusCopy("还没有设置当前焦点。先添加一个焦点，系统只会在确认后消耗分析额度。", "No current focus yet. Add one; analysis only starts after your confirmation."))}</p>`;
    const mode = text(policy.mode, "recommend_only");
    const modeLabel = mode === "auto_bootstrap"
      ? readerFocusCopy("已按主题确认历史分析", "Backlog analysis confirmed per topic")
      : mode === "auto_new"
        ? readerFocusCopy("已按主题确认后续新内容", "Future analysis confirmed per topic")
        : readerFocusCopy("仅推荐，等待主题确认", "Recommendations only; waiting for topic confirmation");
    const modeOptions = `<option value="${esc(mode)}" selected disabled>${esc(`${modeLabel} · ${readerFocusCopy("每个主题由 Chat 确认", "Each topic is confirmed in Chat")}`)}</option>`;
    const fullTextMode = text(policy.full_text_mode, "summary");
    return `<section class="settings-section reader-focus-settings" data-reader-focus-settings>
      <div class="settings-section-head"><div><h3>${esc(readerFocusCopy("当前焦点", "Current focus"))}</h3><p class="settings-hint">${esc(readerFocusCopy("本地先筛选未读内容；只有你确认后才会调用模型。", "Unread items are filtered locally; the model is called only after you confirm."))}</p></div><span class="reader-focus-revision">v${revision}</span></div>
      ${parity.readerFocusMessage ? `<p class="reader-focus-inline-message" role="status">${esc(parity.readerFocusMessage)}</p>` : ""}
      <div class="reader-focus-topic-list">${topicRows}</div>
      <div class="reader-focus-add" id="reader-focus-topic-form"><div class="settings-field"><label for="reader-focus-new-label">${esc(readerFocusCopy("新增焦点", "Add focus"))}</label><input id="reader-focus-new-label" name="reader_focus_new_label" type="text" maxlength="120" placeholder="${esc(readerFocusCopy("例如：AI Computer", "e.g. AI Computer"))}" required /></div><div class="settings-field"><label for="reader-focus-new-terms">${esc(readerFocusCopy("检索词 / 别名（逗号分隔）", "Search terms / aliases (comma separated)"))}</label><input id="reader-focus-new-terms" name="reader_focus_new_terms" type="text" maxlength="2048" placeholder="${esc(readerFocusCopy("AI PC，agent computer", "AI PC, agent computer"))}" /></div><button class="text-btn" type="button" data-reader-focus-add${busyAttr}>${esc(readerFocusCopy("添加焦点", "Add focus"))}</button></div>
      <div class="reader-focus-policy" id="reader-focus-policy-form"><div class="settings-field"><label for="reader-focus-mode">${esc(readerFocusCopy("分析模式", "Analysis mode"))}</label><select id="reader-focus-mode" name="reader_focus_mode"${busyAttr}>${modeOptions}</select></div><div class="settings-field"><label for="reader-focus-daily-limit">${esc(readerFocusCopy("每日文章上限", "Daily article limit"))}</label><input id="reader-focus-daily-limit" name="reader_focus_daily_limit" type="number" min="1" max="10" step="1" value="${readerFocusNumber(policy.daily_article_limit, 10)}"${busyAttr} /></div><div class="settings-field"><label for="reader-focus-token-budget">${esc(readerFocusCopy("每日 Token 预算", "Daily token budget"))}</label><input id="reader-focus-token-budget" name="reader_focus_token_budget" type="number" min="1000" max="12000" step="500" value="${readerFocusNumber(policy.daily_token_budget, 8000)}"${busyAttr} /></div><div class="settings-field"><label for="reader-focus-full-text">${esc(readerFocusCopy("输入范围", "Input scope"))}</label><select id="reader-focus-full-text" name="reader_focus_full_text"${busyAttr}><option value="summary" ${fullTextMode === "summary" ? "selected" : ""}>${esc(readerFocusCopy("标题 + 来源摘要（默认）", "Title + source summary (default)"))}</option><option value="explicit_full_text" ${fullTextMode === "explicit_full_text" ? "selected" : ""}>${esc(readerFocusCopy("仅在明确要求时读取全文", "Read full text only when explicitly requested"))}</option></select></div><button class="primary-btn" type="button" data-reader-focus-save-policy${busyAttr}>${esc(readerFocusCopy("保存关注设置", "Save focus settings"))}</button></div>
      <p class="settings-hint">${esc(readerFocusCopy("每日上限 10 篇、单轮上限 5 篇；预算不足时自动留到下一轮。", "Up to 10 articles per day and 5 per run; unfinished work stays for the next run when the budget is reached."))}</p>
    </section>`;
  }

  function readerFocusTermsFromInput(value) {
    return [...new Set(
      text(value)
        .split(/[，,\n]/)
        .map((item) => item.trim())
        .filter(Boolean)
    )].slice(0, 32);
  }

  async function mutateReaderFocus(action, body, subject = "") {
    const current = parity.readerFocus;
    if (!current || parity.readerFocusBusy) return false;
    const expectedRevision = Number(current.revision) || 1;
    parity.readerFocusBusy = true;
    parity.readerFocusMessage = readerFocusCopy("正在保存关注设置…", "Saving followed-topic settings…");
    renderSettings();
    try {
      const result = await request("./api/app-data/v1/operations/reader_focus.update", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": readerFocusIdempotencyKey(action, expectedRevision, subject),
        },
        body: JSON.stringify({
          schema_version: 1,
          expected_revision: expectedRevision,
          action,
          ...body,
        }),
        timeoutMs: 8000,
      });
      parity.readerFocus = result && typeof result === "object" ? result : parity.readerFocus;
      parity.readerFocusMessage = readerFocusCopy("关注设置已保存", "Followed-topic settings saved");
      renderSettings();
      // Refresh the optional projection without replacing the current detail.
      void refresh(true);
      return true;
    } catch (error) {
      parity.readerFocusMessage = error?.message || readerFocusCopy("关注设置没有保存", "Followed-topic settings were not saved");
      if (String(error?.message || "").includes("变化") || String(error?.message || "").toLowerCase().includes("conflict")) {
        await loadReaderFocusState(true);
        parity.readerFocusMessage = readerFocusCopy("设置已被其他操作更新，请检查后重试", "Settings changed elsewhere. Review and try again");
      }
      renderSettings();
      return false;
    } finally {
      parity.readerFocusBusy = false;
      if (parity.tab === "settings") renderSettings();
    }
  }

  function readerFocusTopicPayload(row, enabled) {
    if (!row) return null;
    const label = text(row.querySelector("[data-reader-focus-label]")?.value);
    const terms = readerFocusTermsFromInput(row.querySelector("[data-reader-focus-terms]")?.value);
    if (!label || !terms.length) {
      parity.readerFocusMessage = readerFocusCopy("主题名称和至少一个关键词不能为空", "A topic name and at least one keyword are required");
      renderSettings();
      return null;
    }
    const topic = { id: text(row.dataset.readerFocusTopic), label, terms };
    if (typeof enabled === "boolean") topic.enabled = enabled;
    return topic;
  }

  async function saveReaderFocusTopic(topicID) {
    if (parity.readerFocusBusy) return;
    const row = document.querySelector(`[data-reader-focus-topic="${CSS.escape(topicID)}"]`);
    const topic = readerFocusTopicPayload(row);
    if (!topic) return;
    await mutateReaderFocus("upsert_topic", { topic }, topicID);
  }

  async function toggleReaderFocusTopic(topicID) {
    if (parity.readerFocusBusy) return;
    const topic = (parity.readerFocus?.topics || []).find((item) => text(item.id) === text(topicID));
    const row = document.querySelector(`[data-reader-focus-topic="${CSS.escape(topicID)}"]`);
    const payload = readerFocusTopicPayload(row, topic?.enabled === false);
    if (!payload) return;
    await mutateReaderFocus("upsert_topic", { topic: payload }, topicID);
  }

  async function removeReaderFocusTopic(topicID) {
    if (parity.readerFocusBusy || !text(topicID)) return;
    await mutateReaderFocus("remove_topic", { topic: { id: text(topicID) } }, topicID);
  }

  async function addReaderFocusTopic() {
    if (parity.readerFocusBusy) return;
    const labelNode = $("reader-focus-new-label");
    const termsNode = $("reader-focus-new-terms");
    const label = text(labelNode?.value);
    const terms = readerFocusTermsFromInput(termsNode?.value || label);
    if (!label || !terms.length) {
      parity.readerFocusMessage = readerFocusCopy("主题名称和至少一个关键词不能为空", "A topic name and at least one keyword are required");
      renderSettings();
      return;
    }
    const saved = await mutateReaderFocus("upsert_topic", { topic: { label, terms, enabled: true } }, label);
    if (saved) {
      const nextLabel = $("reader-focus-new-label");
      const nextTerms = $("reader-focus-new-terms");
      if (nextLabel) nextLabel.value = "";
      if (nextTerms) nextTerms.value = "";
    }
  }

  async function saveReaderFocusPolicy() {
    if (parity.readerFocusBusy || !parity.readerFocus) return;
    const dailyArticleLimit = Math.min(10, Math.max(1, Number($("reader-focus-daily-limit")?.value) || 10));
    const dailyTokenBudget = Math.min(12000, Math.max(1000, Number($("reader-focus-token-budget")?.value) || 8000));
    const fullTextMode = text($("reader-focus-full-text")?.value, "summary");
    await mutateReaderFocus("set_policy", {
      policy: {
        daily_article_limit: Math.round(dailyArticleLimit),
        daily_token_budget: Math.round(dailyTokenBudget),
        full_text_mode: fullTextMode,
      },
    }, "policy");
  }

  function renderSettings() {
    const settings = parity.settings || {
      brief_enabled: true,
      brief_hour: 8,
      brief_minute: 0,
      brief_weekdays: [1, 2, 3, 4, 5],
      timezone: "device",
      auto_extract_recordings: true,
      auto_generate_article_overviews: true,
      obsidian_enabled: true,
      notify_overdue: true,
      notify_high_risk: true,
      notify_blockers: true,
      notify_source_failures: true,
      notify_urgent: true,
    };
    const weekdays = ["日", "一", "二", "三", "四", "五", "六"]
      .map(
        (label, day) =>
          `<label class="toggle-cell"><input type="checkbox" name="brief_weekdays" value="${day}" ${
            (settings.brief_weekdays || []).includes(day) ? "checked" : ""
          }>周${label}</label>`
      )
      .join("");
    const checkbox = (name, label) =>
      `<label class="settings-check"><input type="checkbox" name="${name}" ${
        settings[name] ? "checked" : ""
      }><span>${label}</span></label>`;
    $("parity-content").innerHTML = `
      <form id="settings-form" class="settings-form">
        <div class="settings-intro"><strong>当前设备设置</strong><span>数据和提醒只保存在这台设备；也可以在 Chat 中用同一组设置操作修改。</span></div>
        <section class="settings-section"><h3>Morning Brief</h3>
          <label class="settings-check"><input type="checkbox" name="brief_enabled" ${
            settings.brief_enabled ? "checked" : ""
          }><span>启用每日简报</span></label>
          <div class="settings-inline"><label>时间 <input class="field-control" type="number" name="brief_hour" min="0" max="23" value="${Number(
            settings.brief_hour ?? 8
          )}"> : <input class="field-control" type="number" name="brief_minute" min="0" max="59" value="${Number(
      settings.brief_minute ?? 0
    )}"></label><span class="settings-hint">设备时区：${esc(
      settings.timezone || "device"
    )}</span></div>
          <div class="settings-weekdays"><span class="settings-label">生成日</span>${weekdays}</div>
        </section>
        <section class="settings-section"><h3>自动处理与提醒</h3>
          ${checkbox(
            "auto_extract_recordings",
            "新录音自动提取行动、决策、风险和重要信息"
          )}
          ${checkbox(
            "auto_generate_article_overviews",
            "新文章自动生成 AI 概览"
          )}
          ${checkbox(
            "obsidian_enabled",
            "允许 Chat 按已授权的 Obsidian 范围读取并写入看板"
          )}
          <div class="settings-check-grid">${checkbox(
            "notify_overdue",
            "逾期行动"
          )}${checkbox("notify_high_risk", "高风险")}${checkbox(
      "notify_blockers",
      "阻塞项"
    )}${checkbox("notify_source_failures", "来源失败")}${checkbox(
      "notify_urgent",
      "紧急事项"
          )}</div>
        </section>
        ${renderReaderFocusSettings()}
        <div class="settings-actions"><button class="primary-btn" type="submit" data-save-settings>保存设置</button><span class="settings-hint">默认配置可以随时由你取消或在 Chat 中修改。</span></div>
      </form>`;
  }

  async function saveSettings() {
    const form = $("settings-form");
    if (!form) return;
    const value = (name) => form.elements.namedItem(name);
    const integer = (name, fallback) => {
      const parsed = Number(value(name)?.value);
      return Number.isInteger(parsed) ? parsed : fallback;
    };
    const payload = {
      brief_enabled: Boolean(value("brief_enabled")?.checked),
      brief_hour: integer("brief_hour", 8),
      brief_minute: integer("brief_minute", 0),
      brief_weekdays: [
        ...form.querySelectorAll('input[name="brief_weekdays"]:checked'),
      ].map((node) => Number(node.value)),
      auto_extract_recordings: Boolean(
        value("auto_extract_recordings")?.checked
      ),
      auto_generate_article_overviews: Boolean(
        value("auto_generate_article_overviews")?.checked
      ),
      obsidian_enabled: Boolean(value("obsidian_enabled")?.checked),
      notify_overdue: Boolean(value("notify_overdue")?.checked),
      notify_high_risk: Boolean(value("notify_high_risk")?.checked),
      notify_blockers: Boolean(value("notify_blockers")?.checked),
      notify_source_failures: Boolean(value("notify_source_failures")?.checked),
      notify_urgent: Boolean(value("notify_urgent")?.checked),
    };
    try {
      parity.settings = await jsonRequest("./api/settings", "PATCH", payload);
      renderSettings();
      showToast("设备设置已保存");
    } catch (error) {
      showError(error.message || "设备设置没有保存");
    }
  }

  renderCatalog = function renderParityCatalog() {
    if (!$("parity-content")) return;
    document.querySelectorAll("[data-parity-tab]").forEach((node) => {
      const active = node.dataset.parityTab === parity.tab;
      node.classList.toggle("active", active);
      node.setAttribute("aria-selected", String(active));
      node.tabIndex = active ? 0 : -1;
    });
    $("parity-content").setAttribute(
      "aria-labelledby",
      `source-tab-${parity.tab}`
    );
    if (parity.tab === "subscriptions") renderSubscriptions();
    else if (parity.tab === "settings") {
      renderSettings();
      if (!parity.readerFocus && !parity.readerFocusLoading)
        void loadReaderFocusState().then(() => {
          if (parity.tab === "settings") renderSettings();
        });
    }
    else renderDiscover();
  };

  function setSourceBusy(sourceID, busy) {
    if (busy) parity.sourceBusy.add(sourceID); else parity.sourceBusy.delete(sourceID);
    document.querySelectorAll(`[data-toggle-source="${CSS.escape(sourceID)}"], [data-sync-source="${CSS.escape(sourceID)}"]`).forEach(button => {
      button.disabled = busy;
      button.setAttribute("aria-busy", String(busy));
    });
  }

  async function toggleSource(sourceID) {
    if (parity.sourceBusy.has(sourceID)) return;
    const source = (state.sources || []).find((item) => item.id === sourceID);
    if (!source) return;
    const enabled = !source.subscribed;
    setSourceBusy(sourceID, true);
    try {
      await jsonRequest("./api/subscriptions", enabled ? "POST" : "DELETE", {
        source_id: sourceID,
        enabled,
      });
      source.subscribed = enabled;
      parity.selectedSources.delete(sourceID);
      renderNav();
      renderCatalog();
      await loadParityData();
      showToast(enabled ? `已订阅 ${source.name}` : `已退订 ${source.name}`);
      if (enabled) void monitorSourceSync(sourceID);
    } catch (error) {
      showError(error.message || "订阅状态没有保存");
    } finally { setSourceBusy(sourceID, false); }
  }

  async function syncSource(sourceID) {
    if (parity.sourceBusy.has(sourceID)) return;
    const source = (state.sources || []).find((item) => item.id === sourceID);
    if (!source) return;
    try {
      await jsonRequest("./api/sync", "POST", { source_id: sourceID });
      // Reflect the lightweight source/count projection immediately. The
      // monitor below owns status polling and reloads the typed source page
      // once the local sync has actually completed.
      await refreshSourceMetadataAndCounts(true);
      showToast(`已请求同步 ${source.name}；内容会保存到本地 Focus Space`);
      void monitorSourceSync(sourceID);
    } catch (error) {
      lazySourceLoading.delete(sourceID);
      // If the enqueue request itself failed, allow the next source click or
      // retry button to enqueue immediately instead of inheriting a stale
      // timestamp from the failed attempt.
      lazySourceLoadAt.delete(sourceID);
      if (state.view === `source:${sourceID}`) renderTimeline();
      showError(error.message || "同步请求没有保存");
    } finally { setSourceBusy(sourceID, false); }
  }

  // The inline source catalog lives in index.html and is loaded before this
  // parity module. Delegate its actions to the typed implementation once it is
  // available; keeping the hook on window avoids a second ad-hoc fetch path.
  window.__plaudToggleReaderSource = toggleSource;
  window.__plaudSyncReaderSource = syncSource;

  const lazySourceLoadAt = new Map();
  const lazySourceLoading = new Set();
  // A source row can be clicked before the inline bootstrap has completed its
  // lightweight catalog request.  Resolve only that row instead of falling
  // back to an all-source fetch (or silently skipping the first sync).
  const sourceMetadataLoads = new Map();
  // A persisted source route can render several times while startup
  // reconciliation catches up. Throttle forced single-source metadata reads
  // so that race recovery cannot become a request loop; keep the map bounded
  // for catalogs with many sources.
  const sourceMetadataRefreshAt = new Map();
  const sourceMetadataRefreshTTL = 15_000;

  function sourceMetadataRefreshDue(sourceID) {
    const id = text(sourceID);
    if (!id) return false;
    const now = Date.now();
    const last = Number(sourceMetadataRefreshAt.get(id) || 0);
    if (now - last < sourceMetadataRefreshTTL) return false;
    sourceMetadataRefreshAt.set(id, now);
    if (sourceMetadataRefreshAt.size > 256) {
      const oldest = sourceMetadataRefreshAt.keys().next().value;
      if (oldest) sourceMetadataRefreshAt.delete(oldest);
    }
    return true;
  }

  function sourceSupportsOnDemandSync(source) {
    return ["source_gateway", "app", "hybrid"].includes(
      text(source?.collection_mode).toLowerCase()
    );
  }

  function sourceFromState(sourceID) {
    const id = text(sourceID);
    return (state.sources || []).find((source) => text(source?.id) === id) || null;
  }

  async function resolveSourceMetadata(sourceID, force = false) {
    const id = text(sourceID);
    if (!id) return null;
    const known = sourceFromState(id);
    if (known && !force) return known;
    const pending = sourceMetadataLoads.get(id);
    if (pending) return pending;
    const promise = request(
      `./api/sources?source_id=${encodeURIComponent(id)}&include_counts=0`,
      { timeoutMs: 5000 }
    )
      .then((payload) => {
        const body = payload?.data && typeof payload.data === "object" ? payload.data : payload;
        const rows = Array.isArray(body?.sources) ? body.sources : [];
        const source = rows.find((item) => text(item?.id) === id) || null;
        if (!source) return null;
        const current = Array.isArray(state.sources) ? state.sources : [];
        const merged = window.__plaudMergeSourceCatalogCounts
          ? window.__plaudMergeSourceCatalogCounts(current, [source], false)
          : [source];
        const byID = new Map(current.filter((item) => item?.id).map((item) => [item.id, item]));
        for (const item of merged) byID.set(item.id, item);
        state.sources = [...byID.values()];
        // This is a metadata-only update.  Keep the active timeline and
        // detail mounted; only the navigation badges need repainting.
        renderSources();
        renderNav();
        updateSourceBar();
        return sourceFromState(id) || source;
      })
      .catch(() => null)
      .finally(() => {
        sourceMetadataLoads.delete(id);
      });
    sourceMetadataLoads.set(id, promise);
    return promise;
  }

  function enqueueSourceSyncOnce(sourceID) {
    const id = text(sourceID);
    const source = (state.sources || []).find((item) => item.id === id);
    if (!id || !source || !source.subscribed || !sourceSupportsOnDemandSync(source))
      return false;
    const now = Date.now();
    const last = Number(lazySourceLoadAt.get(id) || 0);
    if (now - last < 60_000 || sourceLastRequest(id)?.status === "pending")
      return false;
    lazySourceLoadAt.set(id, now);
    lazySourceLoading.add(id);
    void syncSource(id);
    return true;
  }

  function sourceHasLocalEntries(source) {
    const id = text(source?.id);
    if (!id) return null;
    const counts = parity.counts;
    const bySource = counts?.by_source;
    const hasAuthoritativeRows =
      counts?.source_counts_available === true ||
      Array.isArray(counts?.source_counts);
    const truncated =
      counts?.source_counts_truncated === true ||
      counts?.source_counts_truncated === 1 ||
      counts?.source_counts_truncated === "1";
    if (hasAuthoritativeRows && !truncated) {
      const row = bySource?.[id];
      return Number(row?.total_entries ?? row?.entry_count) > 0;
    }
    // A light catalog has an explicit zero placeholder, not an authoritative
    // empty result.  Reuse any row already mounted in the bounded reader
    // projection; otherwise return `null` so the source-scoped GET below can
    // decide whether a sync is needed instead of eagerly POSTing one.
    if (
      makeEntries(state.dashboard || {}).some(
        (entry) => entry.kind === "news" && sourceIDForEntry(entry) === id
      )
    )
      return true;
    const catalogCount = Number(source?.entry_count);
    return Number.isFinite(catalogCount) && catalogCount > 0 ? true : null;
  }

  async function loadSourceOnDemand(sourceID) {
    const id = text(sourceID);
    let source = sourceFromState(id);
    if (!source) {
      source = await resolveSourceMetadata(id);
    } else if (
      !source.subscribed &&
      text(source.collection_mode).toLowerCase() === "source_gateway" &&
      sourceMetadataRefreshDue(id)
    ) {
      source = (await resolveSourceMetadata(id, true)) || source;
    }
    if (!source || !source.subscribed || !sourceSupportsOnDemandSync(source)) return;
    const hasLocalEntries = sourceHasLocalEntries(source);
    // A healthy status or an old `last_sync_at` only describes the source
    // configuration; it does not prove that this app profile has a local
    // article row to render.  Treat every source mode uniformly: if the
    // selected source has zero local rows, enqueue its bounded first sync.
    // This is especially important after a fresh install, where source
    // metadata may already be `ok` while the local projection is empty.
    const locallyAvailable = hasLocalEntries;
    // `null` means the source catalog is the lightweight projection and the
    // independent counts endpoint has not established a zero yet.  Do not
    // enqueue a sync before the source-scoped read has had a chance to use
    // the local cache / gateway head path.
    if (locallyAvailable === null) return;
    // Navigation reads the existing local projection only. Refreshing an
    // already hydrated source here used to start a second dashboard refresh,
    // rebuild the whole shell, and jump the user back through the home view.
    // Explicit refresh remains available from the source toolbar.
    if (locallyAvailable) return;
    // Navigation is read-only. Source sync is owned by explicit refresh
    // controls and the background server scheduler.
  }

  async function subscribePack(packID) {
    const pack = parity.packs.find((item) => item.id === packID);
    if (
      !pack ||
      parity.packBusy.has(packID) ||
      (pack.sources || []).every((source) => source.subscribed)
    )
      return;
    parity.packBusy.add(packID);
    renderDiscoverPreservingScroll();
    try {
      const result = await jsonRequest("./api/packs", "POST", {
        pack_id: packID,
      });
      await loadParityData();
      showToast(
        `已添加 ${pack.name} · ${(result.source_ids || []).length} 个来源`
      );
    } catch (error) {
      showError(error.message || "订阅包没有保存");
    } finally {
      parity.packBusy.delete(packID);
      if ($("parity-content")) renderDiscoverPreservingScroll();
    }
  }

  function renderDiscoverPreservingScroll() {
    const panel = $("parity-content");
    const scrollTop = panel?.scrollTop || 0;
    renderDiscover();
    if (panel) panel.scrollTop = scrollTop;
  }

  async function submitDiscovery() {
    const input = $("feed-url");
    const errorNode = $("discover-error");
    const submitButton = input?.form?.querySelector('button[type="submit"]');
    const raw = text(input?.value);
    const classification = classifyDiscoverInput(raw);
    if (!raw || classification.kind === "invalid" || !classification.valid) {
      errorNode.textContent =
        classification.label || "请输入关键词、网站、Feed 或 RSSHub route";
      return;
    }
    if (
      (parity.inputMode === "html" || parity.inputMode === "profile") &&
      classification.kind === "keyword"
    ) {
      errorNode.textContent =
        parity.inputMode === "html"
          ? "HTML → RSS 需要公开网页地址"
          : "关注用户需要公开主页或 Feed 地址";
      return;
    }
    parity.discoveryNotice = "";
    errorNode.textContent =
      classification.kind === "keyword"
        ? "正在搜索来源…"
        : "正在验证来源，请稍候…";
    if (submitButton) {
      submitButton.disabled = true;
      submitButton.setAttribute("aria-busy", "true");
    }
    if (classification.kind === "keyword") {
      try {
        const result = await jsonRequest("./api/discoveries", "POST", {
          input: raw,
        });
        parity.query = result.query || raw;
        parity.input = raw;
        renderCatalog();
        $("feed-url")?.focus();
        showToast(`找到 ${(result.sources || []).length} 个目录来源`);
      } catch (error) {
        errorNode.textContent = error.message || "目录搜索失败";
      } finally {
        if (submitButton?.isConnected) {
          submitButton.disabled = false;
          submitButton.removeAttribute("aria-busy");
        }
      }
      return;
    }
    try {
      const result = await jsonRequest("./api/discoveries", "POST", {
        input: raw,
      });
      parity.input = "";
      parity.query = "";
      parity.inputMode = "standard";
      parity.discoveryNotice =
        result.status === "verified"
          ? `已找到 ${
              result.title || result.normalized_url || "来源"
            }，请确认订阅`
          : result.input_kind === "wechat_article" && result.title
          ? `已识别公众号 ${result.title}；文章可直接查看，持续订阅服务暂不可用`
          : `暂时无法验证：${result.error_message || "来源未返回可解析内容"}`;
      await loadParityData();
      if (result.status === "verified") {
        showToast(
          result.input_kind === "rsshub_route"
            ? "RSSHub route 已验证，可确认订阅"
            : "来源已验证，可确认订阅"
        );
      } else {
        showToast(
          result.input_kind === "wechat_article" && result.title
            ? "已识别公众号和文章；持续订阅源将在验证通过后开放"
            : result.requires_agent
            ? "公开来源暂未验证；可稍后重试或在 Chat 中让 Agent 处理"
            : "来源发现结果已保存"
        );
      }
    } catch (error) {
      parity.discoveryNotice = error.message || "来源发现请求没有保存";
      errorNode.textContent = parity.discoveryNotice;
    } finally {
      if (submitButton?.isConnected) {
        submitButton.disabled = false;
        submitButton.removeAttribute("aria-busy");
      }
    }
  }

  async function previewSource(sourceID) {
    if (parity.sourcePreview?.loading && parity.sourcePreview.source?.id === sourceID) return;
    const pending = { source: (state.sources || []).find(item => item.id === sourceID) || {id: sourceID}, loading: true };
    parity.sourcePreview = pending;
    renderCatalog();
    try {
      const result = await request(`./api/sources/preview?source_id=${encodeURIComponent(sourceID)}`, {timeoutMs: 15000});
      if (parity.sourcePreview !== pending) return;
      parity.sourcePreview = result;
    } catch (error) {
      if (parity.sourcePreview !== pending) return;
      parity.sourcePreview = {...pending, loading: false, error: error.message || "来源预览暂时不可用"};
    }
    renderCatalog();
    $("parity-content")?.querySelector(".source-preview-panel")?.scrollIntoView({block: "nearest"});
  }

  function activateDiscoverCapability(capability) {
    if (capability === "later") {
      closeDiscover();
      state.view = "later";
      state.selected = null;
      render();
      return;
    }
    parity.inputMode =
      parity.inputMode === capability ? "standard" : capability;
    parity.input = "";
    parity.query = "";
    renderCatalog();
    $("feed-url")?.focus();
  }

  async function confirmDiscovery(id) {
    const key = `discovery:${id}`;
    if (parity.sourceBusy.has(key)) return;
    parity.sourceBusy.add(key);
    document.querySelectorAll(`[data-confirm-discovery="${CSS.escape(id)}"]`).forEach(button => { button.disabled = true; button.textContent = "正在订阅…"; });
    try {
      const source = await jsonRequest("./api/discoveries", "PATCH", { id });
      await loadParityData();
      await refresh();
      showToast(`已订阅 ${source.name}`);
      void monitorSourceSync(source.id);
    } catch (error) {
      showError(error.message || "来源确认没有保存");
    } finally { parity.sourceBusy.delete(key); renderCatalog(); }
  }

  async function patchSubscription(sourceID, payload) {
    try {
      await jsonRequest("./api/subscriptions", "PATCH", {
        source_id: sourceID,
        ...payload,
      });
      await loadParityData(true);
    } catch (error) {
      showError(error.message || "订阅设置没有保存");
    }
  }

  async function patchSourceSettings(sourceID, payload) {
    try {
      await jsonRequest("./api/sources", "PATCH", { id: sourceID, ...payload });
      await loadParityData(true);
      showToast("来源设置已保存");
    } catch (error) {
      showError(error.message || "来源设置没有保存");
    }
  }

  async function applyBulkSources() {
    const sourceIDs = [...parity.selectedSources];
    if (!sourceIDs.length) {
      showError("请先选择来源");
      return;
    }
    const operation = $("bulk-source-operation").value;
    const folderID = $("bulk-source-folder").value;
    try {
      await jsonRequest("./api/subscriptions/bulk", "POST", {
        source_ids: sourceIDs,
        operation,
        folder_id: folderID,
      });
      parity.selectedSources.clear();
      await loadParityData();
      showToast("批量设置已保存");
    } catch (error) {
      showError(error.message || "批量设置没有保存");
    }
  }

  async function createFolder() {
    const input = $("new-folder-name");
    const name = text(input?.value);
    if (!name) return;
    try {
      await jsonRequest("./api/folders", "POST", {
        name,
        sort_order: parity.folders.length * 10,
      });
      await loadParityData();
      showToast("分组已创建");
    } catch (error) {
      showError(error.message || "分组没有创建");
    }
  }

  // ---- 订阅管理对齐 Folo：重命名订阅 / 源级全部已读 / 分组重命名与删除 ----

  function beginRenameSource(sourceID) {
    parity.managerRenameSource = text(sourceID);
    if (parity.tab === "subscriptions") renderCatalog();
    const input = $("rename-source-input");
    if (input) {
      input.focus();
      try {
        input.select();
      } catch (_) {}
    }
  }

  function cancelRenameSource() {
    if (!parity.managerRenameSource) return;
    parity.managerRenameSource = "";
    if (parity.tab === "subscriptions") renderCatalog();
  }

  async function saveRenameSource(sourceID) {
    const input = $("rename-source-input");
    const value = text(input?.value).trim();
    const source = sourceByID(sourceID);
    if (!input || !source) return;
    // 改回默认名时清掉 override（PATCH 空串），而不是跳过保存——否则旧的
    // title_override 会一直顶着默认名显示。
    const override = value === text(source.name) ? "" : value;
    await patchSubscription(sourceID, { title_override: override });
    parity.managerRenameSource = "";
    if (parity.tab === "subscriptions") renderCatalog();
    showToast(readerFocusCopy("订阅名称已保存", "Subscription renamed"));
  }

  // markSourceReadCollect walks the per-source entry pages (same
  // ./api/entries?source_id= contract as the source view; the server caps
  // limit at 50) and collects unread keys.  Bounded: 6 pages × 50 = at most
  // 300 keys, i.e. two /api/items/bulk batches below.
  async function markSourceReadCollect(sourceID) {
    const keys = [];
    let cursor = "0";
    for (let page = 0; page < 6 && keys.length < 300; page += 1) {
      const params = new URLSearchParams({
        source_id: sourceID,
        limit: "50",
        cursor,
      });
      const result = await jsonRequest(
        `./api/entries?${params.toString()}`,
        "GET"
      );
      const entries = Array.isArray(result?.entries) ? result.entries : [];
      for (const entry of entries) {
        if (!entry?.read && entry?.id) keys.push(`news:${entry.id}`);
      }
      const next = text(result?.next_cursor_token || result?.next_cursor);
      if (!next || !entries.length) break;
      cursor = next;
    }
    return keys;
  }

  async function undoMarkSourceRead(sourceID, keys) {
    try {
      for (let index = 0; index < keys.length; index += 200) {
        await jsonRequest("./api/items/bulk", "PATCH", {
          keys: keys.slice(index, index + 200),
          read: false,
        });
      }
      await Promise.allSettled([loadParityData(true), loadEntryCounts(true)]);
      showToast(readerFocusCopy(`已恢复 ${keys.length} 条未读`, `Restored ${keys.length} unread`));
    } catch (error) {
      showError(error.message || readerFocusCopy("撤销没有保存", "Undo failed"));
      await Promise.allSettled([loadParityData(true), loadEntryCounts(true)]);
    }
  }

  async function markSourceRead(sourceID) {
    const source = sourceByID(sourceID);
    if (!source) return;
    const busyKey = `read:${sourceID}`;
    if (parity.sourceBusy.has(busyKey)) return;
    parity.sourceBusy.add(busyKey);
    let keys = [];
    try {
      keys = await markSourceReadCollect(sourceID);
      if (!keys.length) {
        showToast(readerFocusCopy("该来源没有未读条目", "No unread entries"));
        return;
      }
      for (let index = 0; index < keys.length; index += 200) {
        await jsonRequest("./api/items/bulk", "PATCH", {
          keys: keys.slice(index, index + 200),
          read: true,
        });
      }
      await Promise.allSettled([loadParityData(true), loadEntryCounts(true)]);
      const capped =
        Number(source.unread_count) > keys.length
          ? readerFocusCopy(`（源共 ${source.unread_count} 条未读）`, ` (${source.unread_count} total)`)
          : "";
      showToastAction(
        readerFocusCopy(`已标记 ${keys.length} 条已读${capped}`, `Marked ${keys.length} read${capped}`),
        readerFocusCopy("撤销", "Undo"),
        () => void undoMarkSourceRead(sourceID, keys)
      );
    } catch (error) {
      showError(error.message || readerFocusCopy("批量已读没有保存", "Mark read failed"));
      await Promise.allSettled([loadParityData(true), loadEntryCounts(true)]);
    } finally {
      parity.sourceBusy.delete(busyKey);
    }
  }

  async function renameFolder() {
    const select = $("folder-manage-select");
    const input = $("folder-rename-name");
    const folderID = text(select?.value);
    const name = text(input?.value).trim();
    if (!folderID) {
      showError(readerFocusCopy("请先选择要重命名的分组", "Select a folder first"));
      return;
    }
    if (!name) {
      showError(readerFocusCopy("请输入分组新名称", "Enter a new folder name"));
      return;
    }
    try {
      await jsonRequest("./api/folders", "PATCH", {
        id: folderID,
        name,
        sort_order: parity.folders.find((item) => item.id === folderID)?.sort_order || 0,
      });
      parity.folderDeleteArmed = false;
      await loadParityData();
      showToast(readerFocusCopy("分组已重命名", "Folder renamed"));
    } catch (error) {
      showError(error.message || readerFocusCopy("分组没有重命名", "Rename failed"));
    }
  }

  // 删除分组走两击确认：第一击只武装按钮并锁定目标（重绘会重置下拉框，
  // 目标必须存在 parity 状态里），5 秒无第二击自动解除。后端把组内来源移回
  // 未分组，来源与条目都不丢。
  //
  // 同击回声：订阅管理的点击同时走 modal 与 document 两层委托（幂等操作双
  // 挂载是既有模式），非幂等的 arm/confirm 状态机必须用武装时间戳挡掉同一次
  // click 的第二次调用——400ms 内的"确认"视为回声忽略，真人二次点击远慢于
  // 这个间隔。
  async function deleteFolder() {
    const select = $("folder-manage-select");
    if (!parity.folderDeleteArmed) {
      const folderID = text(select?.value);
      if (!folderID) {
        showError(readerFocusCopy("请先选择要删除的分组", "Select a folder first"));
        return;
      }
      parity.folderDeleteTarget = folderID;
      parity.folderDeleteArmed = true;
      parity.folderDeleteArmedAt = Date.now();
      if (parity.tab === "subscriptions") renderCatalog();
      window.setTimeout(() => {
        if (!parity.folderDeleteArmed) return;
        parity.folderDeleteArmed = false;
        parity.folderDeleteTarget = "";
        if (parity.tab === "subscriptions") renderCatalog();
      }, 5000);
      return;
    }
    if (Date.now() - parity.folderDeleteArmedAt < 400) return;
    // 确认击：用武装时锁定的目标，重绘后的下拉框值不可信。
    const target = parity.folderDeleteTarget || text(select?.value);
    parity.folderDeleteArmed = false;
    parity.folderDeleteTarget = "";
    if (!target) {
      showError(readerFocusCopy("请先选择要删除的分组", "Select a folder first"));
      if (parity.tab === "subscriptions") renderCatalog();
      return;
    }
    try {
      await jsonRequest("./api/folders", "DELETE", { id: target });
      await loadParityData();
      showToast(readerFocusCopy("分组已删除，来源已移入未分组", "Folder deleted; feeds moved to default"));
    } catch (error) {
      showError(error.message || readerFocusCopy("分组没有删除", "Delete failed"));
    }
  }

  async function createTag() {
    const input = $("new-tag-name");
    const name = text(input?.value);
    if (!name) return;
    try {
      await jsonRequest("./api/tags", "POST", { name, color: "#64748b" });
      await loadParityData();
      showToast("标签已创建");
    } catch (error) {
      showError(error.message || "标签没有创建");
    }
  }

  async function setTag(sourceID, tagID, enabled) {
    if (!tagID) return;
    try {
      await jsonRequest("./api/tags", "PATCH", {
        id: tagID,
        source_id: sourceID,
        enabled,
      });
      await loadParityData(true);
    } catch (error) {
      showError(error.message || "标签没有保存");
    }
  }

  function onModalClick(event) {
    const tab = event.target.closest("[data-parity-tab]");
    if (tab) {
      parity.tab = tab.dataset.parityTab;
      renderCatalog();
      return;
    }
    const mode = event.target.closest("[data-discover-mode]");
    if (mode) {
      parity.mode = mode.dataset.discoverMode;
      parity.sourcePreview = null;
      renderCatalog();
      return;
    }
    const contentType = event.target.closest("[data-content-type]");
    if (contentType) {
      parity.contentType = contentType.dataset.contentType;
      parity.sourcePreview = null;
      renderCatalog();
      return;
    }
    const category = event.target.closest("[data-catalog-category]");
    if (category) {
      parity.category =
        category.dataset.catalogCategory === "all"
          ? ""
          : category.dataset.catalogCategory;
      parity.sourcePreview = null;
      renderCatalog();
      return;
    }
    const capability = event.target.closest("[data-discover-capability]");
    if (capability)
      return activateDiscoverCapability(capability.dataset.discoverCapability);
    if (event.target.closest("[data-reader-focus-reload]")) {
      return void loadReaderFocusState().then(() => {
        if (parity.tab === "settings") renderSettings();
      });
    }
    const addFocus = event.target.closest("[data-reader-focus-add]");
    if (addFocus) return void addReaderFocusTopic();
    const saveFocus = event.target.closest("[data-reader-focus-save]");
    if (saveFocus) return void saveReaderFocusTopic(saveFocus.dataset.readerFocusSave);
    const toggleFocus = event.target.closest("[data-reader-focus-toggle]");
    if (toggleFocus) return void toggleReaderFocusTopic(toggleFocus.dataset.readerFocusToggle);
    const removeFocus = event.target.closest("[data-reader-focus-remove]");
    if (removeFocus) return void removeReaderFocusTopic(removeFocus.dataset.readerFocusRemove);
    if (event.target.closest("[data-reader-focus-save-policy]"))
      return void saveReaderFocusPolicy();
    const toggle = event.target.closest("[data-toggle-source]");
    if (toggle) return void toggleSource(toggle.dataset.toggleSource);
    const preview = event.target.closest("[data-preview-source]");
    if (preview) return void previewSource(preview.dataset.previewSource);
    if (event.target.closest("[data-close-source-preview]")) {
      parity.sourcePreview = null;
      renderCatalog();
      return;
    }
    const previewEntry = event.target.closest("[data-preview-entry]");
    if (previewEntry) {
      const key = `news:${previewEntry.dataset.previewEntry}`;
      if (
        makeEntries(state.dashboard || {}).some((entry) => entry.key === key)
      ) {
        closeDiscover();
        selectEntry(key);
      } else {
        showToast("该预览尚未进入当前内容流");
      }
      return;
    }
    const sync = event.target.closest("[data-sync-source]");
    if (sync) return void syncSource(sync.dataset.syncSource);
    const pack = event.target.closest("[data-subscribe-pack]");
    if (pack) return void subscribePack(pack.dataset.subscribePack);
    const confirm = event.target.closest("[data-confirm-discovery]");
    if (confirm) return void confirmDiscovery(confirm.dataset.confirmDiscovery);
    const article = event.target.closest("[data-open-discovery-article]");
    if (article && publicURL(article.dataset.openDiscoveryArticle || "")) {
      window.open(
        article.dataset.openDiscoveryArticle,
        "_blank",
        "noopener,noreferrer"
      );
      return;
    }
    if (event.target.closest("[data-refresh-catalog]"))
      return void loadParityData();
    if (event.target.closest("[data-apply-source-bulk]"))
      return void applyBulkSources();
    if (event.target.closest("[data-create-folder]"))
      return void createFolder();
    if (event.target.closest("[data-create-tag]")) return void createTag();
    const removeTag = event.target.closest("[data-remove-tag]");
    if (removeTag)
      return void setTag(
        removeTag.dataset.tagSource,
        removeTag.dataset.removeTag,
        false
      );
    const sort = event.target.closest("[data-sort-source]");
    if (sort) {
      const source = (state.sources || []).find(
        (item) => item.id === sort.dataset.sortSource
      );
      if (source)
        return void patchSubscription(source.id, {
          sort_order:
            Number(source.sort_order || 0) + Number(sort.dataset.sortDelta),
        });
    }
    const renameEntry = event.target.closest("[data-rename-source]");
    if (renameEntry) return beginRenameSource(renameEntry.dataset.renameSource);
    if (event.target.closest("[data-rename-cancel]"))
      return cancelRenameSource();
    const renameSave = event.target.closest("[data-rename-save]");
    if (renameSave) return void saveRenameSource(renameSave.dataset.renameSave);
    const readSource = event.target.closest("[data-read-source]");
    if (readSource) return void markSourceRead(readSource.dataset.readSource);
    if (event.target.closest("[data-rename-folder]"))
      return void renameFolder();
    if (event.target.closest("[data-delete-folder]"))
      return void deleteFolder();
  }

  function restoreModalFocus() {
    const target = parity.modalReturnFocus;
    parity.modalReturnFocus = null;
    if (target?.isConnected) window.setTimeout(() => target.focus(), 0);
  }

  function onModalKeydown(event) {
    if (event.key === "Escape") {
      event.preventDefault();
      closeDiscover();
      restoreModalFocus();
      return;
    }
    const current = event.target.closest("[data-parity-tab]");
    if (
      !current ||
      !["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)
    )
      return;
    const tabs = [...document.querySelectorAll("[data-parity-tab]")];
    const index = tabs.indexOf(current);
    const nextIndex =
      event.key === "Home"
        ? 0
        : event.key === "End"
        ? tabs.length - 1
        : (index + (event.key === "ArrowRight" ? 1 : -1) + tabs.length) %
          tabs.length;
    event.preventDefault();
    parity.tab = tabs[nextIndex].dataset.parityTab;
    renderCatalog();
    tabs[nextIndex].focus();
  }

  function onModalChange(event) {
    const target = event.target;
    if (target.id === "catalog-category") {
      parity.category = target.value;
      parity.sourcePreview = null;
      renderCatalog();
    } else if (target.id === "catalog-language") {
      parity.language = target.value;
      parity.sourcePreview = null;
      renderCatalog();
    } else if (target.id === "select-all-sources") {
      parity.selectedSources.clear();
      if (target.checked)
        (state.sources || [])
          .filter((source) => source.subscribed)
          .forEach((source) => parity.selectedSources.add(source.id));
      renderCatalog();
    } else if (target.matches("[data-select-source]")) {
      if (target.checked)
        parity.selectedSources.add(target.dataset.selectSource);
      else parity.selectedSources.delete(target.dataset.selectSource);
    } else if (target.matches("[data-folder-source]")) {
      void patchSubscription(target.dataset.folderSource, {
        folder_id: target.value,
      });
    } else if (target.matches("[data-pin-source]")) {
      void patchSubscription(target.dataset.pinSource, {
        pinned: target.checked,
      });
    } else if (target.matches("[data-mute-source]")) {
      void patchSubscription(target.dataset.muteSource, {
        muted: target.checked,
      });
    } else if (target.matches("[data-add-tag-source]")) {
      void setTag(target.dataset.addTagSource, target.value, true);
    } else if (target.matches("[data-sync-interval-source]")) {
      void patchSourceSettings(target.dataset.syncIntervalSource, {
        sync_interval_seconds: Number(target.value),
      });
    } else if (target.matches("[data-retention-source]")) {
      void patchSourceSettings(target.dataset.retentionSource, {
        retention_days: Number(target.value),
      });
    } else if (target.matches("[data-auto-extract-source]")) {
      void patchSourceSettings(target.dataset.autoExtractSource, {
        auto_extract: target.checked,
      });
    } else if (target.matches("[data-hide-source]")) {
      void patchSourceSettings(target.dataset.hideSource, {
        hidden: target.checked,
      });
    }
  }

  function onModalInput(event) {
    if (event.target.id !== "feed-url") return;
    parity.input = event.target.value;
    parity.discoveryNotice = "";
    const classification = classifyDiscoverInput(parity.input);
    parity.query = classification.kind === "keyword" ? parity.input : "";
    parity.sourcePreview = null;
    renderCatalog();
    const next = $("feed-url");
    if (next) {
      next.focus();
      next.setSelectionRange(next.value.length, next.value.length);
    }
  }

  function onModalSubmit(event) {
    event.preventDefault();
    if (event.target.id === "discovery-form") void submitDiscovery();
    else if (event.target.id === "settings-form") void saveSettings();
  }

  filteredEntries = function filteredParityEntries() {
    if (state.view.startsWith("obsidian-folder:")) {
      const prefix = obsidianFolderFromView(state.view);
      let entries = makeEntries(state.dashboard || {}).filter(
        (entry) =>
          entry.group === "obsidian" &&
          obsidianPathMatches(entry.item?.locator || entry.item?.path, prefix)
      );
      const query = state.query.toLowerCase();
      if (query)
        entries = entries.filter((entry) =>
          [entry.title, entry.summary, entry.source, entry.type].some((value) =>
            text(value).toLowerCase().includes(query)
          )
        );
      return entries;
    }
    if (!["later", "context", "picture"].includes(state.view))
      return baseFilteredEntries();
    let entries = makeEntries(state.dashboard || {});
    if (state.view === "picture") {
      // The base projection has no picture branch; apply the same predicate
      // the server used for the typed page so client-side rebinds (search,
      // preference flips, snapshot restore) keep the wall exact.
      entries = entries.filter(
        (entry) =>
          entry.kind === "news" && aggregatePictureNews(entry.item)
      );
    } else if (state.view === "later")
      entries = entries.filter((entry) => pref(entry).read_later);
    else entries = entries.filter((entry) => contextIsActive(entry));
    const query = state.query.toLowerCase();
    if (query)
      entries = entries.filter((entry) =>
        [entry.title, entry.summary, entry.source, entry.type].some((value) =>
          text(value).toLowerCase().includes(query)
        )
      );
    return entries;
  };

  entryCounts = function parityEntryCounts() {
    const counts = { ...baseEntryCounts(), later: 0, context: 0 };
    // RSS is a total-content view. Do not display the sum of per-source
    // unread badges here; it makes the navigation count disagree with the
    // paginated list and hides the real backlog size.
    const loadedNewsEntries = makeEntries(state.dashboard || {}).filter(
      (entry) => entry.kind === "news"
    );
    const loadedNews = loadedNewsEntries.length;
    const loadedVideoCount = loadedNewsEntries.filter(
      (entry) => entry.group === "video"
    ).length;
    const videoTotal = entryTotalForView("video");
    counts.video = videoTotal === null
      ? loadedVideoCount
      : Math.max(loadedVideoCount, videoTotal);
    const loadedYouTubeCount = loadedNewsEntries.filter(
      (entry) => entry.group === "video" && entry.videoProvider === "youtube"
    ).length;
    const youtubeTotal = entryTotalForView("youtube");
    counts.youtube = youtubeTotal === null
      ? loadedYouTubeCount
      : Math.max(loadedYouTubeCount, youtubeTotal);
    counts.bilibili = loadedNewsEntries.filter(
      (entry) => entry.group === "video" && entry.videoProvider === "bilibili"
    ).length;
    const rssTotal = entryTotalForView("rss");
    if (rssTotal !== null || loadedNews)
      counts.rss = Math.max(
        loadedNewsEntries.filter((entry) => entry.group !== "video").length,
        rssTotal === null ? 0 : rssTotal
      );
    const pictureTotal = entryTotalForView("picture");
    const loadedPictureCount = loadedNewsEntries.filter(
      (entry) => entry.kind === "news" && aggregatePictureNews(entry.item)
    ).length;
    if (pictureTotal !== null || loadedPictureCount)
      counts.picture = Math.max(
        loadedPictureCount,
        pictureTotal === null ? 0 : pictureTotal
      );
    for (const entry of makeEntries(state.dashboard || {})) {
      const preference = pref(entry);
      if (preference.read_later) counts.later++;
      if (contextIsActive(entry)) counts.context++;
    }
    return counts;
  };

  function readerFocusCopy(zh, en) {
    return window.plaudI18n?.locale === "zh-CN" ? zh : en;
  }

  function readerFocusNumber(value, fallback = 0) {
    const number = Number(value);
    return Number.isFinite(number) && number >= 0 ? Math.round(number) : fallback;
  }

  function readerFocusURL(raw) {
    const value = text(raw);
    if (!value || value.length > 2048) return "";
    try {
      const url = new URL(value, window.location.href);
      if (!["http:", "https:"].includes(url.protocol)) return "";
      return url.href;
    } catch {
      return "";
    }
  }

  function readerFocusEntryKey(item, dashboard) {
    const entryID = text(item?.entry_id);
    if (!entryID) return "";
    const entries = makeEntries(dashboard || {});
    return (
      entries.find(
        (entry) =>
          entry.key === entryID ||
          entry.id === entryID ||
          entry.item?.id === entryID ||
          entry.key === `news:${entryID}`
      )?.key || ""
    );
  }

  function readerFocusStatus(status) {
    const value = text(status, "pending").toLowerCase();
    if (["ready", "ok", "completed"].includes(value))
      return {
        label: readerFocusCopy("已分析", "Analyzed"),
        kind: "green",
      };
    if (["failed", "error"].includes(value))
      return {
        label: readerFocusCopy("分析失败", "Analysis failed"),
        kind: "red",
      };
    if (["budget_limited", "budget", "limit"].includes(value))
      return {
        label: readerFocusCopy("已达预算上限", "Daily budget reached"),
        kind: "red",
      };
    if (["queued", "running", "pending"].includes(value))
      return {
        label: readerFocusCopy("排队中", "Queued"),
        kind: "amber",
      };
    return {
      label: readerFocusCopy("待分析", "Pending analysis"),
      kind: "amber",
    };
  }

  function readerFocusLifecycleStatus(status) {
    const value = text(status).toLowerCase();
    if (["ready", "ok", "completed", "enabled", "active", "auto_new"].includes(value))
      return readerFocusCopy("自动预分析已开启", "Daily analysis is enabled");
    if (["recommend_only", "skipped", "disabled", "off", "none"].includes(value))
      return readerFocusCopy("仅生成推荐，不自动分析", "Recommendations only; automatic analysis is off");
    if (["failed", "error"].includes(value))
      return readerFocusCopy("预分析暂时不可用，仍可浏览推荐", "Analysis is unavailable; recommendations remain available");
    if (["pending", "queued", "running", "auto_bootstrap"].includes(value))
      return readerFocusCopy("预分析排队中，未完成内容会留到下一轮", "Analysis is queued; unfinished items stay for the next run");
    if (value === "done")
      return readerFocusCopy("首次预分析已完成，后续只处理新内容", "Initial analysis is complete; future runs cover new content only");
    return value
      ? readerFocusCopy("正在准备当前焦点推荐", "Preparing current-focus recommendations")
      : "";
  }

  const decisionRunActiveStatuses = new Set([
    "queued",
    "collecting_context",
    "running_ai",
    "applying",
  ]);

  function decisionRunNumber(...values) {
    for (const value of values) {
      const number = Number(value);
      if (Number.isFinite(number) && number >= 0) return number;
    }
    return null;
  }

  function normalizeDecisionRunStatus(value, run = {}) {
    const status = text(value).toLowerCase().replace(/[\s-]+/g, "_");
    if (["created", "pending", "claimed", "queued"].includes(status))
      return status === "claimed" ? "running_ai" : "queued";
    if (["collecting", "context", "collecting_evidence"].includes(status))
      return "collecting_context";
    if (["running", "analyzing", "analysing", "generating"].includes(status))
      return "running_ai";
    if (["clarify", "clarifying", "waiting_for_clarification", "waiting_for_answer"].includes(status))
      return "waiting_clarification";
    if (["apply", "saving", "persisting"].includes(status)) return "applying";
    if (["ready", "done", "applied", "partial", "success"].includes(status))
      return "completed";
    if (["budget", "budget_limited", "credits", "insufficient_credits", "quota_exceeded"].includes(status))
      return "paused_insufficient_credits";
    if (["cancelled", "canceled"].includes(status)) return "cancelled";
    if (status === "failed" || status === "error")
      return run.retryable === false || run.can_retry === false
        ? "failed_terminal"
        : "failed_retryable";
    return status;
  }

  function normalizeDecisionQuestion(value) {
    if (!value || typeof value !== "object") return null;
    const prompt = text(
      value.prompt || value.question || value.text || value.title || value.label
    ).slice(0, 1000);
    const rawOptions = Array.isArray(value.options)
      ? value.options
      : Array.isArray(value.choices)
        ? value.choices
        : [];
    const options = rawOptions
      .map((option, index) => {
        if (typeof option === "string")
          return { id: option.slice(0, 160), label: option.slice(0, 500), detail: "" };
        if (!option || typeof option !== "object") return null;
        const id = text(
          option.id || option.option_id || option.value || option.key,
          `option-${index + 1}`
        ).slice(0, 160);
        const label = text(
          option.label || option.title || option.text || option.value,
          id
        ).slice(0, 500);
        return {
          id,
          label,
          detail: text(option.detail || option.description || option.help_text).slice(0, 700),
        };
      })
      .filter(Boolean)
      .slice(0, 12);
    const type = text(value.type || value.input_type || value.answer_type).toLowerCase();
    const multiple = type === "multi" || Boolean(
      value.multiple || value.allow_multiple || ["multiple", "multi_choice", "checkbox"].includes(type)
    );
    const allowsText = type === "text" || Boolean(
      value.allow_text || value.allows_text || value.free_text || (!options.length && type !== "scale")
    );
    const id = text(value.id || value.question_id || value.clarification_id).slice(0, 160);
    if (!id || !prompt) return null;
    const rawAnswer = value.answer && typeof value.answer === "object" ? value.answer : null;
    const answer = rawAnswer
      ? {
          type: text(rawAnswer.type || type).toLowerCase(),
          value: rawAnswer.value,
          revision: Math.max(0, Number(rawAnswer.revision || value.answer_revision) || 0),
        }
      : null;
    return {
      id,
      prompt,
      detail: text(value.detail || value.description || value.help_text).slice(0, 1000),
      type: type || (options.length ? (multiple ? "multi_choice" : "single_choice") : "text"),
      multiple,
      allowsText,
      textLabel: text(value.text_label || value.other_label).slice(0, 120),
      placeholder: text(value.placeholder).slice(0, 240),
      required: value.required !== false,
      revision: Math.max(0, Number(value.revision || value.question_revision) || 0),
      answerRevision: Math.max(0, Number(value.answer_revision) || 0),
      status: text(value.status).toLowerCase(),
      answer,
      maxSelect: Math.max(0, Number(value.max_select) || 0),
      scaleMin: Number.isInteger(Number(value.scale_min)) ? Number(value.scale_min) : 1,
      scaleMax: Number.isInteger(Number(value.scale_max)) ? Number(value.scale_max) : 5,
      options,
    };
  }

  function normalizeDecisionRunResponse(value, focusID = "") {
    if (!value || typeof value !== "object") return null;
    const root = value.data && typeof value.data === "object" ? value.data : value;
    const run = root.run && typeof root.run === "object"
      ? root.run
      : root.decision_run && typeof root.decision_run === "object"
        ? root.decision_run
        : root.current_run && typeof root.current_run === "object"
          ? root.current_run
          : root;
    const question = normalizeDecisionQuestion(
      root.current_question || run.current_question || root.clarification || run.clarification
    );
    const questions = (Array.isArray(root.questions) ? root.questions : Array.isArray(run.questions) ? run.questions : [])
      .map((item) => normalizeDecisionQuestion(item))
      .filter(Boolean);
    const status = normalizeDecisionRunStatus(
      run.status || root.status || (question ? "waiting_clarification" : ""),
      run
    );
    const runID = text(run.run_id || run.id || root.run_id).slice(0, 160);
    const resolvedFocusID = text(run.focus_id || root.focus_id || focusID).slice(0, 160);
    const capabilities = root.capabilities && typeof root.capabilities === "object"
      ? root.capabilities
      : run.capabilities && typeof run.capabilities === "object"
        ? run.capabilities
        : {};
    const canStart = Boolean(root.can_start ?? run.can_start ?? capabilities.start);
    if (!runID && !status && !question && !canStart) return null;

    const progress = root.progress && typeof root.progress === "object"
      ? root.progress
      : run.progress && typeof run.progress === "object"
        ? run.progress
        : {};
    const completed = decisionRunNumber(
      progress.answered,
      progress.completed,
      progress.processed,
      progress.current,
      run.processed_count,
      root.processed_count
    );
    const total = decisionRunNumber(
      progress.total,
      progress.count,
      run.candidate_count,
      root.candidate_count
    );
    let percent = decisionRunNumber(
      progress.percent,
      progress.percentage,
      run.progress_percent,
      root.progress_percent
    );
    const ratio = decisionRunNumber(progress.ratio, run.progress_ratio, root.progress_ratio);
    if (ratio !== null) percent = ratio <= 1 ? ratio * 100 : ratio;
    else if (percent === null && total && completed !== null)
      percent = (completed / total) * 100;
    if (percent !== null) percent = Math.min(100, Math.max(0, percent));

    const usage = root.usage && typeof root.usage === "object"
      ? root.usage
      : run.usage && typeof run.usage === "object"
        ? run.usage
        : {};
    const actualInput = decisionRunNumber(
      usage.input_tokens,
      run.input_tokens,
      run.actual_input_tokens,
      root.actual_input_tokens
    );
    const actualOutput = decisionRunNumber(
      usage.output_tokens,
      run.output_tokens,
      run.actual_output_tokens,
      root.actual_output_tokens
    );
    const credits = root.credits && typeof root.credits === "object"
      ? root.credits
      : run.credits && typeof run.credits === "object"
        ? run.credits
        : {};
    const error = root.error && typeof root.error === "object"
      ? root.error
      : run.error && typeof run.error === "object"
        ? run.error
        : {};
    return {
      raw: root,
      runID,
      focusID: resolvedFocusID,
      analysisRunID: text(run.analysis_run_id || root.analysis_run_id).slice(0, 160),
      decisionID: text(run.decision_id || root.decision_id).slice(0, 160),
      decisionMode: text(run.thinking_template_id || root.thinking_template_id || run.decision_mode || root.decision_mode, "guided").toLowerCase(),
      status,
      phase: text(
        progress.label || progress.phase_label || run.phase_label || run.phase || root.phase
      ).slice(0, 240),
      message: text(
        progress.message || run.status_message || root.message || root.status_message
      ).slice(0, 1000),
      completed,
      total,
      percent,
      question,
      questions,
      revision: Math.max(0, Number(run.revision || root.revision) || 0),
      focusRevision: Math.max(0, Number(run.focus_revision || root.focus_revision) || 0),
      actualInput,
      actualOutput,
      errorCode: text(error.code || run.error_code || root.error_code).slice(0, 100),
      errorMessage: text(error.message || run.error_message || root.error_message).slice(0, 1000),
      creditMessage: text(
        credits.message || run.credit_message || root.credit_message
      ).slice(0, 1000),
      canStart,
      canResume:
        status !== "cancelled" &&
        Boolean(root.can_resume ?? run.can_resume ?? capabilities.resume),
      canRetry: Boolean(root.can_retry ?? run.can_retry ?? capabilities.retry ?? run.retryable),
      canCancel: Boolean(root.can_cancel ?? run.can_cancel ?? capabilities.cancel),
      updatedAt: text(run.updated_at || root.updated_at).slice(0, 80),
    };
  }

  function decisionRunStatusInfo(run) {
    switch (run?.status) {
      case "queued":
        return { label: readerFocusCopy("等待开始", "Queued"), tone: "active", active: true };
      case "collecting_context":
        return { label: readerFocusCopy("正在整理资料", "Collecting context"), tone: "active", active: true };
      case "running_ai":
        return { label: readerFocusCopy("正在分析", "Analyzing"), tone: "active", active: true };
      case "waiting_clarification":
        return { label: readerFocusCopy("需要你的确认", "Needs your input"), tone: "attention" };
      case "applying":
        return { label: readerFocusCopy("正在保存结果", "Saving results"), tone: "active", active: true };
      case "completed":
        return { label: readerFocusCopy("分析完成", "Analysis complete"), tone: "success" };
      case "paused_insufficient_credits":
        return { label: readerFocusCopy("额度不足，已暂停", "Paused: insufficient credits"), tone: "attention" };
      case "failed_retryable":
        return { label: readerFocusCopy("分析中断", "Analysis interrupted"), tone: "danger" };
      case "failed_terminal":
        return { label: readerFocusCopy("分析失败", "Analysis failed"), tone: "danger" };
      case "cancelled":
        return { label: readerFocusCopy("已取消", "Cancelled"), tone: "neutral" };
      case "superseded":
        return { label: readerFocusCopy("已有新版本", "Superseded"), tone: "neutral" };
      case "not_started":
      case "idle":
        return { label: readerFocusCopy("尚未分析", "Not started"), tone: "neutral" };
      default:
        return { label: readerFocusCopy("正在同步状态", "Syncing status"), tone: "neutral" };
    }
  }

  function decisionRunDraftKey(focusID, questionID) {
    return `${text(focusID)}\u0000${text(questionID)}`;
  }

  function decisionRunQuestionMarkup(run, focusID, busy) {
    const answeredQuestion = !run?.question && Array.isArray(run?.questions)
      ? [...run.questions].reverse().find((item) => item?.answer)
      : null;
    const question = run?.question || answeredQuestion;
    if (!question) {
      return run?.status === "waiting_clarification"
        ? `<p class="decision-run-waiting-copy">${esc(readerFocusCopy("问题正在同步，已保留当前分析进度。", "The question is syncing; current progress is preserved."))}</p>`
        : "";
    }
    const fieldName = `decision-answer-${question.id.replace(/[^A-Za-z0-9_-]/g, "-")}`;
    const editing = Boolean(!run?.question && answeredQuestion?.answer);
    const savedValue = question.answer?.value;
    const savedOptionIDs = Array.isArray(savedValue)
      ? savedValue.map((value) => String(value))
      : ["choice", "scale"].includes(question.type) && savedValue !== undefined && savedValue !== null
        ? [String(savedValue)]
        : [];
    const savedText = question.type === "text" && typeof savedValue === "string" ? savedValue : "";
    const draft = parity.decisionRunDrafts.get(decisionRunDraftKey(focusID, question.id)) || {
      optionIDs: savedOptionIDs,
      answerText: savedText,
    };
    const selected = new Set(Array.isArray(draft.optionIDs) ? draft.optionIDs : savedOptionIDs);
    const optionMarkup = question.options.length
      ? `<div class="decision-run-options" role="${question.multiple ? "group" : "radiogroup"}" aria-label="${esc(question.prompt)}">${question.options
          .map((option) => `<label class="decision-run-option"><input type="${question.multiple ? "checkbox" : "radio"}" name="${esc(fieldName)}" value="${esc(option.id)}"${selected.has(option.id) ? " checked" : ""}${busy ? " disabled" : ""}><span><strong>${esc(option.label)}</strong>${option.detail ? `<small>${esc(option.detail)}</small>` : ""}</span></label>`)
          .join("")}</div>`
      : "";
    const scaleMarkup = question.type === "scale"
      ? `<div class="decision-run-scale" role="radiogroup" aria-label="${esc(question.prompt)}">${Array.from(
          { length: Math.max(0, Math.min(11, question.scaleMax - question.scaleMin + 1)) },
          (_, index) => question.scaleMin + index
        ).map((value) => `<label><input type="radio" name="${esc(fieldName)}" value="${value}"${selected.has(String(value)) ? " checked" : ""}${busy ? " disabled" : ""}><span>${value}</span></label>`).join("")}</div>`
      : "";
    const textMarkup = question.allowsText
      ? `<label class="decision-run-text"><span>${esc(question.textLabel || (question.options.length ? readerFocusCopy("补充说明（可选）", "Additional context (optional)") : readerFocusCopy("你的回答", "Your answer")))}</span><textarea data-decision-answer-text maxlength="2000"${question.options.length || !question.required ? "" : " required"}${busy ? " disabled" : ""} placeholder="${esc(question.placeholder || readerFocusCopy("输入你的判断或补充信息", "Add your answer or context"))}">${esc(text(draft.answerText).slice(0, 2000))}</textarea></label>`
      : "";
    return `<form class="decision-run-question${editing ? " is-editing" : ""}" data-decision-run-answer data-focus-id="${esc(focusID)}" data-run-id="${esc(run.runID)}" data-run-revision="${run.revision}" data-question-id="${esc(question.id)}" data-question-type="${esc(question.type)}" data-question-revision="${question.revision}" data-answer-revision="${question.answerRevision}" data-question-max-select="${question.maxSelect}" data-question-required="${question.required ? "1" : "0"}"><div class="decision-run-question-head"><span>${esc(editing ? readerFocusCopy("已保存的澄清", "Saved clarification") : readerFocusCopy("需要澄清", "Clarification"))}</span><strong>${esc(question.prompt)}</strong>${question.detail ? `<p>${esc(question.detail)}</p>` : ""}${editing ? `<p>${esc(readerFocusCopy("修改后会使旧分析失效，并根据新回答重新分析。", "Changing this answer invalidates the old analysis and starts a new one."))}</p>` : ""}</div>${optionMarkup}${scaleMarkup}${textMarkup}<div class="decision-run-question-foot"><span class="decision-run-inline-message" data-decision-run-inline-message role="status"></span><button class="primary-btn" type="submit"${busy ? " disabled aria-busy=\"true\"" : ""}>${esc(busy ? readerFocusCopy("正在提交…", "Submitting…") : editing ? readerFocusCopy("修改并重新分析", "Update and reanalyze") : readerFocusCopy("确认并继续", "Confirm and continue"))}${icon(editing ? "refresh" : "arrow")}</button></div></form>`;
  }

  function decisionRunModeLabel(value) {
    switch (text(value, "guided").toLowerCase()) {
      case "rational":
        return readerFocusCopy("理性分析", "Rational");
      case "bounded_rationality":
        return readerFocusCopy("有限理性", "Bounded rationality");
      case "first_principles":
        return readerFocusCopy("第一性原理", "First principles");
      default:
        return readerFocusCopy("引导式", "Guided");
    }
  }

  function decisionRunPanelMarkup(focus, analysis, dashboard) {
    const focusID = text(focus?.id);
    if (!focusID) return "";
    const embedded = dashboard?.decision_run || analysis?.decision_run || analysis?.current_run;
    const run = parity.decisionRuns.get(focusID) || normalizeDecisionRunResponse(embedded, focusID);
    if (!run) {
      if (text(parity.selectedFocusID) !== focusID || parity.focusPanel !== "workspace") return "";
      const loadError = parity.decisionRunErrors.get(focusID);
      const message = loadError
        ? readerFocusCopy("暂时无法读取决策状态，点击“决策”重试。", "Decision status is unavailable. Select Decision to retry.")
        : readerFocusCopy("正在读取决策状态…", "Loading decision status…");
      return `<section class="decision-run-panel is-neutral" data-decision-run-panel data-focus-id="${esc(focusID)}" aria-live="polite"><div class="decision-run-head"><span class="decision-run-state-icon${loadError ? "" : " is-active"}" aria-hidden="true">${loadError ? icon("decision") : ""}</span><div><span>${esc(readerFocusCopy("决策分析", "Decision analysis"))}</span><strong>${esc(message)}</strong></div></div></section>`;
    }
    const latestAnalysisRunID = text(analysis?.run_id).slice(0, 160);
    const latestAnalysisRun = (Array.isArray(analysis?.runs) ? analysis.runs : []).find(
      (item) => text(item?.run_id || item?.id) === latestAnalysisRunID
    );
    // A guided decision run can be created before the Agent applies a focus
    // analysis. Once an applied analysis exists, an unrelated empty
    // clarification run is stale and its "no materials" question is false.
    // Do not let that stale run mask the real decision result or block the
    // feedback controls; a run linked to the applied analysis remains visible.
    const reusableAnalysisStatus = text(
      latestAnalysisRun?.status || analysis?.status
    ).toLowerCase();
    const analysisFocusRevision = Number(latestAnalysisRun?.focus_revision) || 0;
    const analysisMatchesFocus =
      analysisFocusRevision <= 0 || analysisFocusRevision === Number(focus.revision);
    const analysisRunID = ["applied", "partial", "completed"].includes(reusableAnalysisStatus) && analysisMatchesFocus
      ? latestAnalysisRunID
      : "";
    if (analysisRunID && run.status === "waiting_clarification" && !run.analysisRunID)
      return "";
    const info = decisionRunStatusInfo(run);
    const busy = parity.decisionRunBusy.has(focusID);
    const message = parity.decisionRunMessages.get(focusID);
    const loadError = parity.decisionRunErrors.get(focusID);
    const processed = run.completed !== null && run.total !== null && run.total > 0
      ? `${readerFocusCopy("澄清", "Clarification")} ${Math.round(run.completed)}/${Math.round(run.total)}`
      : "";
    const clarificationProgress = [
      "waiting_clarification",
      "paused_insufficient_credits",
      "failed_retryable",
      "failed_terminal",
    ].includes(run.status) && run.total > 0;
    const displayPercent = clarificationProgress ? run.percent : null;
    const progressCopy = run.phase || run.message || (
      run.status === "waiting_clarification"
        ? readerFocusCopy("等待你回答当前问题", "Waiting for your answer")
        : info.active
          ? info.label
          : readerFocusCopy("已保留当前阶段", "Current stage is preserved")
    );
    const progressMarkup = !["not_started", "idle", "completed"].includes(run.status) && (info.active || displayPercent !== null || processed)
      ? `<div class="decision-run-progress"><div class="decision-run-progress-track" role="progressbar" aria-label="${esc(clarificationProgress ? readerFocusCopy("澄清进度", "Clarification progress") : readerFocusCopy("决策运行状态", "Decision run status"))}"${displayPercent !== null ? ` aria-valuemin="0" aria-valuemax="100" aria-valuenow="${Math.round(displayPercent)}"` : ' aria-busy="true"'}><i${displayPercent !== null ? ` style="width:${displayPercent}%"` : ""}></i></div><div class="decision-run-progress-meta"><span>${esc(progressCopy)}</span>${processed ? `<span>${esc(processed)}</span>` : ""}</div></div>`
      : "";
    let notice = "";
    let action = "";
    // A claimed/failed/superseded analysis run is not a reusable decision
    // input. Starting a fresh decision without it lets the bounded Agent claim
    // the current evidence set instead of being pinned to an expired run.
    const linkedDecision = analysisRunID ? (Array.isArray(analysis?.decisions) ? analysis.decisions : []).find(
      (item) =>
        item &&
        text(item.analysis_run_id) === analysisRunID &&
        text(item.feedback_status, item.status).toLowerCase() !== "rejected"
    ) : null;
    const linkedDecisionID = text(linkedDecision?.id).slice(0, 160);
    const startAction = (label, iconName = "sparkles", freshAnalysis = false) =>
      `<button class="text-btn decision-run-resume" type="button" data-decision-run-configure data-focus-id="${esc(focusID)}" data-focus-revision="${Number(focus.revision) || 0}" data-decision-mode="${esc(focus.thinking_template_id || run.decisionMode || "guided")}"${run.runID ? ` data-previous-run-id="${esc(run.runID)}" data-previous-run-revision="${Number(run.revision) || 0}"` : ""}${!freshAnalysis && analysisRunID ? ` data-analysis-run-id="${esc(analysisRunID)}"` : ""}${!freshAnalysis && linkedDecisionID ? ` data-decision-id="${esc(linkedDecisionID)}"` : ""}${busy ? ' disabled aria-busy="true"' : ""}>${icon(iconName)}${esc(label)}</button>`;
    const cancelAction = () =>
      `<button class="text-btn decision-run-cancel" type="button" data-decision-run-action="cancel" data-focus-id="${esc(focusID)}" data-run-id="${esc(run.runID)}" data-run-revision="${Number(run.revision) || 0}"${busy ? " disabled aria-busy=\"true\"" : ""}>${icon("close")}${esc(readerFocusCopy("停止本轮分析", "Stop analysis"))}</button>`;
    if (decisionRunActiveStatuses.has(run.status) && run.canCancel) {
      action = cancelAction();
    } else if (run.status === "paused_insufficient_credits") {
      notice = `<div class="decision-run-notice is-attention"><strong>${esc(readerFocusCopy("进度已保存", "Progress saved"))}</strong><p>${esc(run.creditMessage || readerFocusCopy("补充额度后可从当前步骤继续，不会重新处理已完成的内容。", "Add credits to continue from this step; completed work will not be repeated."))}</p></div>`;
      if (run.canResume)
        action = `<button class="text-btn decision-run-resume" type="button" data-decision-run-action="resume" data-focus-id="${esc(focusID)}" data-run-id="${esc(run.runID)}" data-run-revision="${run.revision}"${busy ? " disabled aria-busy=\"true\"" : ""}>${icon("refresh")}${esc(readerFocusCopy("继续分析", "Resume analysis"))}</button>`;
    } else if (["failed_retryable", "failed_terminal"].includes(run.status)) {
      notice = `<div class="decision-run-notice is-danger"><strong>${esc(readerFocusCopy("本次分析没有完成", "This analysis did not finish"))}</strong><p>${esc(run.errorMessage || readerFocusCopy("当前结果和已保存进度不会被清空。", "Existing results and saved progress are preserved."))}${run.errorCode ? ` · ${esc(run.errorCode)}` : ""}</p></div>`;
      if (run.status === "failed_retryable" && (run.canRetry || run.canResume))
        action = `<button class="text-btn decision-run-resume" type="button" data-decision-run-action="resume" data-focus-id="${esc(focusID)}" data-run-id="${esc(run.runID)}" data-run-revision="${run.revision}"${busy ? " disabled aria-busy=\"true\"" : ""}>${icon("refresh")}${esc(readerFocusCopy("重试当前步骤", "Retry current step"))}</button>`;
      else if (run.status === "failed_terminal" && run.canStart)
        action = startAction(readerFocusCopy("重新开始", "Start again"), "refresh", true);
    } else if (run.status === "superseded" && run.canStart) {
      notice = `<div class="decision-run-notice"><strong>${esc(readerFocusCopy("焦点资料已更新", "Focus materials changed"))}</strong><p>${esc(readerFocusCopy("旧分析已安全停用；重新开始会使用当前版本的全部资料。", "The old run was retired safely; start again to use all materials from the current version."))}</p></div>`;
      action = startAction(readerFocusCopy("使用新资料重新分析", "Analyze current materials"), "refresh", true);
    } else if (run.status === "cancelled" && run.canStart) {
      notice = `<div class="decision-run-notice"><strong>${esc(readerFocusCopy("本轮分析已结束", "This run has ended"))}</strong><p>${esc(readerFocusCopy("重新开始会创建新一轮分析，不会恢复已取消的运行。", "Restarting creates a new run; the cancelled run will not be resumed."))}</p></div>`;
      action = startAction(readerFocusCopy("重新开始", "Start again"), "refresh", true);
    } else if (run.status === "completed" && run.canStart) {
      action = startAction(readerFocusCopy("重新分析", "Analyze again"), "refresh", true);
    } else if (["not_started", "idle"].includes(run.status) && run.canStart) {
      action = startAction(readerFocusCopy("开始分析", "Start analysis"));
    }
    const inlineState = message || (loadError && run.runID ? readerFocusCopy("状态更新暂时中断，将自动重试。", "Status update paused; it will retry automatically.") : "");
    const showQuestions = !["completed", "cancelled", "failed_terminal"].includes(run.status);
    const completedMeta = run.status === "completed"
      ? [decisionRunModeLabel(run.decisionMode), run.updatedAt ? formatDateByLocale(run.updatedAt, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) : ""].filter(Boolean).join(" · ")
      : "";
    return `<section class="decision-run-panel is-${esc(info.tone)}${run.status === "completed" ? " is-complete" : ""}" data-decision-run-panel data-focus-id="${esc(focusID)}" aria-live="polite"${info.active ? ' aria-busy="true"' : ""}><div class="decision-run-head"><span class="decision-run-state-icon${info.active ? " is-active" : ""}" aria-hidden="true">${info.active ? "" : icon(run.status === "completed" ? "check" : "decision")}</span><div><span>${esc(run.status === "completed" ? readerFocusCopy("分析记录", "Analysis record") : readerFocusCopy("决策分析", "Decision analysis"))}</span><strong>${esc(run.status === "completed" ? completedMeta || info.label : info.label)}</strong></div>${action}</div>${progressMarkup}${notice}${showQuestions ? decisionRunQuestionMarkup(run, focusID, busy) : ""}${inlineState ? `<p class="decision-run-state-message" role="status">${esc(inlineState)}</p>` : ""}</section>`;
  }

  function readerFocusOverviewText(item) {
    const overview = item?.overview;
    if (!overview) return "";
    if (typeof overview === "string") return text(overview).slice(0, 500);
    return text(overview.summary || overview.impact).slice(0, 500);
  }

  function focusResultKindLabel(kind) {
    const value = text(kind).toLowerCase();
    if (value === "action") return readerFocusCopy("行动", "Action");
    if (value === "decision") return readerFocusCopy("决策", "Decision");
    return readerFocusCopy("信号", "Signal");
  }

  function focusResultStatusLabel(status) {
    switch (text(status, "draft").toLowerCase()) {
      case "accepted":
        return readerFocusCopy("已接受", "Accepted");
      case "rejected":
        return readerFocusCopy("已拒绝", "Rejected");
      case "snoozed":
        return readerFocusCopy("稍后处理", "Snoozed");
      case "edited":
        return readerFocusCopy("已修改，待确认", "Edited, needs review");
      default:
        return readerFocusCopy("待确认", "Needs review");
    }
  }

  function focusIntentLabel(value) {
    return ({
      decide: readerFocusCopy("做决定", "Decide"),
      plan: readerFocusCopy("制订计划", "Plan"),
      create: readerFocusCopy("创作内容", "Create"),
      monitor: readerFocusCopy("持续关注", "Monitor"),
    })[text(value)] || readerFocusCopy("做决定", "Decide");
  }

  function focusIntentIcon(value) {
    return ({ decide: "decision", plan: "action", create: "edit", monitor: "signal" })[text(value)] || "decision";
  }

  function focusThinkingTemplateLabel(value) {
    return decisionRunModeLabel(value);
  }

  function focusBlueprintID(intentKind) {
    const value = ["decide", "plan", "create", "monitor"].includes(text(intentKind))
      ? text(intentKind)
      : "decide";
    return `${value}.v1`;
  }

  function focusDefaultThinkingTemplate(intentKind) {
    return ({ decide: "rational", plan: "bounded_rationality", create: "first_principles", monitor: "guided" })[text(intentKind)] || "guided";
  }

  function recommendFocusIntent(value) {
    const normalized = text(value).toLocaleLowerCase();
    if (/(持续|关注|追踪|跟踪|监控|观察|watch|monitor|track)/u.test(normalized)) return "monitor";
    if (/(写|文章|论文|acm|创作|大纲|brief|报告|脚本|create|write)/u.test(normalized)) return "create";
    if (/(旅行|旅游|行程|计划|规划|路线|实施|项目|里程碑|trip|travel|plan)/u.test(normalized)) return "plan";
    return "decide";
  }

  function focusAnalysisSnapshotKey(focusID, runID) {
    return `${text(focusID)}\u0000${text(runID)}`;
  }

  function focusAnalysisForPanel(focusID) {
    const id = text(focusID);
    const runID = text(parity.focusAnalysisRunSelections.get(id));
    return runID
      ? parity.focusAnalysisSnapshots.get(focusAnalysisSnapshotKey(id, runID)) || parity.focusAnalyses.get(id) || {}
      : parity.focusAnalyses.get(id) || {};
  }

  function focusWorkProducts(analysis) {
    return (Array.isArray(analysis?.work_products) ? analysis.work_products : [])
      .filter((item) => item && text(item.kind, "work_product") === "work_product");
  }

  function focusReviewAvailable(focus, analysis) {
    const acceptedDecision = (analysis?.decisions || []).some((item) => text(item.feedback_status) === "accepted");
    const now = Date.now();
    const dueAction = (analysis?.actions || []).some((item) => {
      if (text(item.feedback_status) === "rejected") return false;
      const data = focusResultData(item);
      if (text(item.status) === "done" || data.completed === true) return true;
      const due = Date.parse(text(data.due_at));
      return Number.isFinite(due) && due <= now;
    });
    return acceptedDecision || dueAction || ["executing", "done"].includes(text(focus?.workflow_state));
  }

  function focusResultItems(analysis) {
    if (!analysis || typeof analysis !== "object") return [];
    const runID = text(analysis.run_id);
    const sameRun = (item) =>
      item && (!runID || text(item.analysis_run_id) === runID);
    const source = Array.isArray(analysis.results)
      ? analysis.results
      : [
          ...(Array.isArray(analysis.signals) ? analysis.signals : []),
          ...(Array.isArray(analysis.actions) ? analysis.actions : []),
          ...(Array.isArray(analysis.decisions) ? analysis.decisions : []),
        ];
    return source.filter(sameRun).slice(0, 9);
  }

  function focusResultData(item) {
    return item?.data && typeof item.data === "object" ? item.data : item || {};
  }

  function focusResultSourceIDs(item) {
    const data = focusResultData(item);
    const values = Array.isArray(item?.source_ids)
      ? item.source_ids
      : Array.isArray(data.source_ids)
        ? data.source_ids
        : [];
    return values.map((value) => text(value).slice(0, 80)).filter(Boolean).slice(0, 3);
  }

  function focusAnalysisEvidenceRows() {
    const evidence = state.dashboard?.focus_analysis?.evidence;
    return Array.isArray(evidence) ? evidence : [];
  }

  function normalizeFocusSourceLocator(value) {
    if (!value) return null;
    if (typeof value === "string") {
      const raw = text(value).slice(0, 2000);
      if (!raw) return null;
      if (raw.startsWith("{") || raw.startsWith("[")) {
        try {
          return normalizeFocusSourceLocator(JSON.parse(raw));
        } catch {
          return raw;
        }
      }
      return raw;
    }
    if (typeof value !== "object") return null;
    try {
      const raw = JSON.stringify(value);
      return raw && raw.length <= 2000 ? JSON.parse(raw) : null;
    } catch {
      return null;
    }
  }

  function normalizeFocusCitation(value, fallback = {}) {
    if (typeof value === "string")
      return {
        source_id: text(value).slice(0, 160),
        role: text(fallback.role, "support"),
      };
    if (!value || typeof value !== "object") return null;
    const sourceID = text(
      value.source_id || value.sourceID || value.source || fallback.source_id
    );
    const entryID = text(
      value.entry_id ||
        value.entryID ||
        value.article_id ||
        value.source_entry_id ||
        fallback.entry_id
    );
    const blockID = text(value.block_id || value.blockID || value.anchor_id);
    const quote = text(value.quote || value.excerpt || value.text || fallback.quote);
    const title = text(value.title || fallback.title);
    const sourceLocator = normalizeFocusSourceLocator(
      value.source_locator ||
        value.sourceLocator ||
        value.source_locator_json ||
        value.sourceLocatorJSON ||
        fallback.source_locator
    );
    const locatorObject = sourceLocator && typeof sourceLocator === "object"
      ? sourceLocator
      : {};
    const sourceKind = text(
      value.source_kind || value.sourceKind || locatorObject.source_kind || fallback.source_kind
    ).toLowerCase();
    const assetID = text(
      value.asset_id ||
        value.assetID ||
        locatorObject.asset_id ||
        locatorObject.material_id ||
        fallback.asset_id
    );
    const mime = text(value.mime || value.media_mime || locatorObject.mime || fallback.mime).toLowerCase();
    if (!sourceID && !entryID && !assetID && !blockID && !quote) return null;
    return {
      source_kind: sourceKind.slice(0, 32),
      source_id: sourceID.slice(0, 160),
      entry_id: entryID.slice(0, 160),
      asset_id: assetID.slice(0, 160),
      mime: mime.slice(0, 120),
      block_id: blockID.slice(0, 128),
      content_hash: text(value.content_hash || value.contentHash || fallback.content_hash).slice(0, 160),
      text_hash: text(value.text_hash || value.textHash || fallback.text_hash).slice(0, 160),
      quote: quote.slice(0, 900),
      title: title.slice(0, 240),
      locator: text(
        value.locator ||
          (typeof sourceLocator === "string" ? sourceLocator : locatorObject.url || locatorObject.path) ||
          fallback.locator
      ).slice(0, 1000),
      source_locator: sourceLocator,
      role: text(value.role || value.evidence_role || fallback.role, "support").toLowerCase(),
    };
  }

  function focusResultCitations(item) {
    const data = focusResultData(item);
    const sourceIDs = focusResultSourceIDs(item);
    const raw = [
      ...(Array.isArray(item?.citations) ? item.citations : []),
      ...(Array.isArray(item?.evidence) ? item.evidence : []),
      ...(Array.isArray(data.citations) ? data.citations : []),
      ...(Array.isArray(data.evidence) ? data.evidence : []),
      ...(Array.isArray(data.source_citations) ? data.source_citations : []),
      ...(Array.isArray(data.supporting_citations) ? data.supporting_citations : []).map((value) =>
        typeof value === "object" ? { ...value, role: "support" } : { source_id: value, role: "support" }
      ),
      ...(Array.isArray(data.challenging_citations) ? data.challenging_citations : []).map((value) =>
        typeof value === "object" ? { ...value, role: "challenge" } : { source_id: value, role: "challenge" }
      ),
    ];
    const evidenceByID = new Map();
    for (const evidence of focusAnalysisEvidenceRows()) {
      const keys = [evidence.id, evidence.evidence_id, evidence.source_id]
        .map((value) => text(value))
        .filter(Boolean);
      for (const key of keys) evidenceByID.set(key, evidence);
    }
    for (const sourceID of sourceIDs) {
      const evidence = evidenceByID.get(sourceID);
      raw.push(
        evidence
          ? {
              ...evidence,
              source_id: evidence.source_id || sourceID,
              entry_id: evidence.entry_id || evidence.article_id,
              quote: evidence.quote || evidence.excerpt,
              role: "support",
            }
          : sourceID
      );
    }
    const seen = new Set();
    return raw
      .map((value) => enrichFocusCitation(value))
      .filter(Boolean)
      .filter((citation) => {
        // Newer result payloads carry a rich citation and the legacy
        // source_ids mirror for the same Reader entry.  The two shapes differ
        // in quote/source_kind, so de-duplicating the serialized objects leaks
        // a second raw `entry-*` chip into the action/decision drawer.  Treat
        // the resolved Reader entry as the identity and keep block_id only
        // when two independently addressable paragraphs are intentional.
        const key = [
          citation.asset_id ? "asset" : "entry",
          citation.asset_id || citation.entry_id || citation.source_id || citation.quote,
          citation.block_id,
        ].join("\u0000");
        if (!key.replace(/\u0000/g, "" ) || seen.has(key)) return false;
        seen.add(key);
        return true;
      })
      .slice(0, 8);
  }

  function enrichFocusCitation(value) {
    const citation = normalizeFocusCitation(value);
    if (!citation) return null;
    const entry = citationEntryFor(citation);
    if (!entry) return citation;
    const entryID = text(entry.item?.id || entry.id);
    const sourceKind = entry.kind === "recording"
      ? "meeting"
      : entry.kind === "evidence" && text(entry.item?.source_type).toLowerCase() === "obsidian"
        ? "obsidian"
        : "entry";
    const locator = text(
      citation.locator || entry.item?.url || entry.item?.locator || entry.item?.path
    );
    return normalizeFocusCitation({
      ...citation,
      source_kind: citation.source_kind || sourceKind,
      source_id: citation.source_id || entryID,
      entry_id: citation.entry_id || entryID,
      content_hash: citation.content_hash || entry.item?.content_hash,
      quote: citation.quote || entry.summary,
      title: citation.title || entry.title,
      locator,
      source_locator: citation.source_locator || {
        source_kind: sourceKind,
        entry_id: entryID,
        url: locator,
      },
    });
  }

  function focusCitationRoleLabel(role) {
    const value = text(role).toLowerCase();
    if (["challenge", "challenges", "counter", "counterevidence"].includes(value))
      return readerFocusCopy("挑战依据", "Challenges");
    if (["qualifies", "qualification", "gap"].includes(value))
      return readerFocusCopy("限定条件", "Qualifies");
    return readerFocusCopy("支持依据", "Supports");
  }

  function focusCitationDataAttributes(citation) {
    return [
      ["data-focus-citation", "1"],
      ["data-citation-source-id", citation.source_id],
      ["data-citation-source-kind", citation.source_kind],
      ["data-citation-entry-id", citation.entry_id],
      ["data-citation-asset-id", citation.asset_id],
      ["data-citation-mime", citation.mime],
      ["data-citation-block-id", citation.block_id],
      ["data-citation-content-hash", citation.content_hash],
      ["data-citation-text-hash", citation.text_hash],
      ["data-citation-quote", citation.quote],
      ["data-citation-title", citation.title],
      ["data-citation-locator", citation.locator],
      [
        "data-citation-source-locator",
        citation.source_locator && typeof citation.source_locator === "object"
          ? JSON.stringify(citation.source_locator)
          : citation.source_locator,
      ],
      ["data-citation-role", citation.role],
    ]
      .filter(([, value]) => text(value))
      .map(([name, value]) => `${name}="${esc(value)}"`)
      .join(" ");
  }

  function focusCitationButtonMarkup(citation, index = 0) {
    const label = text(
      citation.title || citation.quote || citation.source_id,
      readerFocusCopy(`依据 ${index + 1}`, `Evidence ${index + 1}`)
    ).slice(0, 120);
    const attrs = focusCitationDataAttributes(citation);
    return `<button class="focus-evidence-chip" type="button" ${attrs} title="${esc(
      citation.quote || label
    )}"><span>${esc(focusCitationRoleLabel(citation.role))}</span><strong>${esc(
      label
    )}</strong>${citation.block_id ? `<small>${esc(citation.block_id)}</small>` : icon("arrow")}</button>`;
  }

  function focusMaterialCitation(material) {
    if (!material || typeof material !== "object") return null;
    const sourceKind = text(material.source_kind).toLowerCase();
    if (sourceKind === "image") {
      return normalizeFocusCitation({
        source_kind: "image",
        source_id: material.source_id,
        asset_id: material.asset_id || material.id,
        mime: material.mime,
        content_hash: material.content_hash,
        locator: material.source_url,
        source_locator: {
          material_id: material.id,
          asset_id: material.asset_id || material.id,
          mime: material.mime,
        },
      });
    }
    if (sourceKind !== "entry") return null;
    return normalizeFocusCitation({
      source_kind: "entry",
      source_id: material.source_id,
      entry_id: material.entry_id || material.source_id,
      content_hash: material.content_hash,
      quote: material.summary_text,
      title: material.title,
      locator: material.source_url,
      source_locator: {
        material_id: material.id,
        entry_id: material.entry_id || material.source_id,
        url: material.source_url,
      },
    });
  }

  function focusMaterialsMarkup(focus) {
    const materials = (Array.isArray(focus?.materials) ? focus.materials : [])
      .filter((material) => material && material.enabled !== false)
      .map((material) => ({ material, citation: focusMaterialCitation(material) }))
      .filter((item) => item.citation);
    if (!materials.length) return "";
    const cards = materials.map(({ material, citation }) => {
      const attrs = focusCitationDataAttributes(citation);
      if (citation.source_kind === "image") {
        const source = focusImageSourceURL(citation);
        if (!source) return "";
        return `<button class="focus-material-card is-image" type="button" ${attrs} aria-label="${esc(readerFocusCopy("查看图片资料", "View image material"))}"><span class="focus-material-image-preview">${icon("picture")}<img src="${esc(source)}" alt="" loading="lazy" decoding="async" /></span></button>`;
      }
      const title = text(material.title, readerFocusCopy("文章", "Article"));
      const source = text(material.source);
      const summary = text(material.summary_text);
      return `<button class="focus-material-card is-entry" type="button" ${attrs}><span class="focus-material-icon">${icon("note")}</span><span class="focus-material-copy"><strong>${esc(title)}</strong>${source ? `<small>${esc(source)}</small>` : ""}${summary ? `<span>${esc(summary)}</span>` : ""}</span>${icon("arrow")}</button>`;
    }).filter(Boolean).join("");
    if (!cards) return "";
    return `<div class="focus-space-section focus-space-materials"><div class="overview-subsection-label"><span>${esc(readerFocusCopy("资料", "Materials"))}</span><small>${materials.length}</small></div><div class="focus-material-list">${cards}</div></div>`;
  }

  function focusSourceEntry(source) {
    const sourceID = text(source?.source_id);
    const sourceKind = text(source?.source_kind).toLowerCase();
    if (!sourceID) return null;
    return makeEntries(state.dashboard || {}).find((entry) => {
      if (text(entry.id) !== sourceID) return false;
      if (sourceKind === "meeting") return entry.kind === "recording";
      if (sourceKind === "obsidian")
        return entry.kind === "evidence" && text(entry.item?.source_type).toLowerCase() === "obsidian";
      return false;
    }) || null;
  }

  function focusLinkedSourceCitation(source, entry = focusSourceEntry(source)) {
    if (!source || !entry) return null;
    const kind = text(source.source_kind).toLowerCase();
    if (!["meeting", "obsidian"].includes(kind)) return null;
    const locator = text(entry.item?.locator || entry.item?.path || source.locator);
    return normalizeFocusCitation({
      source_kind: kind,
      source_id: entry.id,
      entry_id: entry.id,
      quote: entry.summary,
      title: entry.title,
      locator,
      source_locator: {
        source_kind: kind,
        entry_id: entry.id,
        locator,
      },
    });
  }

  function focusLinkedSourcesMarkup(focus) {
    const sources = (Array.isArray(focus?.sources) ? focus.sources : [])
      .filter((source) => source && source.enabled !== false && ["meeting", "obsidian"].includes(text(source.source_kind).toLowerCase()));
    if (!sources.length) return "";
    const cards = sources.map((source) => {
      const entry = focusSourceEntry(source);
      const citation = focusLinkedSourceCitation(source, entry);
      const kind = text(source.source_kind).toLowerCase();
      const fallback = kind === "meeting"
        ? readerFocusCopy("会议纪要", "Meeting notes")
        : readerFocusCopy("Obsidian 笔记", "Obsidian note");
      const title = text(entry?.title, fallback);
      const summary = text(entry?.summary);
      const kindLabel = kind === "meeting"
        ? readerFocusCopy("会议与录音", "Meetings and recordings")
        : readerFocusCopy("Obsidian", "Obsidian");
      const inner = `<span class="focus-material-icon">${icon(kind === "meeting" ? "audio" : "note")}</span><span class="focus-material-copy"><strong>${esc(title)}</strong><small>${esc(kindLabel)}</small>${summary ? `<span>${esc(summary.slice(0, 240))}</span>` : ""}</span>${entry ? icon("arrow") : ""}`;
      return citation
        ? `<button class="focus-material-card is-entry" type="button" ${focusCitationDataAttributes(citation)}>${inner}</button>`
        : `<div class="focus-material-card is-entry">${inner}</div>`;
    }).join("");
    return `<div class="focus-space-section focus-space-materials"><div class="overview-subsection-label"><span>${esc(readerFocusCopy("会议与笔记", "Meetings and notes"))}</span><small>${sources.length}</small></div><div class="focus-material-list">${cards}</div></div>`;
  }

  function decisionHistoryCacheKey(focusID, decisionID) {
    return `${text(focusID)}\u0000${text(decisionID)}`;
  }

  function decisionHistoryTriggerMarkup(item, focusID, fallbackEvidenceCount = 0) {
    const decisionID = text(item?.id);
    if (!focusID || !decisionID) return "";
    const history = parity.decisionHistories.get(decisionHistoryCacheKey(focusID, decisionID));
    const stats = history?.stats || {};
    const counts = history
      ? [
          `${readerFocusNumber(stats.evidence_count)} ${readerFocusCopy("条资料", "sources")}`,
          `${readerFocusNumber(stats.conflict_count)} ${readerFocusCopy("条冲突", "conflicts")}`,
          `${readerFocusNumber(stats.action_count)} ${readerFocusCopy("个行动", "actions")}`,
        ].join(" · ")
      : fallbackEvidenceCount > 0
        ? `${fallbackEvidenceCount} ${readerFocusCopy("条依据", "sources")}`
        : "";
    return `<button class="text-btn decision-history-trigger" type="button" data-decision-history="${esc(decisionID)}" data-focus-id="${esc(focusID)}">${icon("decision")}<span>${esc(readerFocusCopy("查看决策依据", "View decision rationale"))}</span>${counts ? `<small>${esc(counts)}</small>` : icon("arrow")}</button>`;
  }

  function focusResultCardMarkup(item) {
    const kind = text(item?.kind, "signal").toLowerCase();
    if (!["signal", "action", "decision"].includes(kind) || !text(item?.id)) return "";
    const data = focusResultData(item);
    const status = text(item.feedback_status, "draft").toLowerCase();
    const confirmedDecision = kind === "decision" && status === "accepted";
    const title = kind === "action"
      ? text(data.task, readerFocusCopy("未命名行动", "Untitled action"))
      : kind === "decision"
        ? text(data.question, readerFocusCopy("未命名决策", "Untitled decision"))
        : text(data.title, readerFocusCopy("未命名信号", "Untitled signal"));
    const detail = kind === "action"
      ? [text(data.owner), text(data.due_at)].filter(Boolean).join(" · ")
      : kind === "decision"
        ? text(data.recommendation)
        : text(data.detail);
    const sourceIDs = focusResultSourceIDs(item);
    const citations = focusResultCitations(item);
    const evidenceCount = citations.length || sourceIDs.length;
    const evidence = confirmedDecision
      ? ""
      : citations.length
        ? `<div class="focus-result-evidence"><span>${esc(readerFocusCopy("依据", "Evidence"))}</span><div class="focus-evidence-chips">${citations
            .map((citation, index) => focusCitationButtonMarkup(citation, index))
            .join("")}</div></div>`
        : sourceIDs.length
          ? `<div class="focus-result-evidence"><span>${esc(readerFocusCopy("依据", "Evidence"))}</span><strong class="focus-result-evidence-count">${sourceIDs.length} ${esc(readerFocusCopy("条", "sources"))}</strong></div>`
          : "";
    const focusID = text(item?.focus_id || activeFocusSpaceProjection(state.dashboard || {})?.id || parity.selectedFocusID);
    const rationaleAction = kind === "decision" && status === "accepted"
      ? decisionHistoryTriggerMarkup(item, focusID, evidenceCount)
      : citations.length || sourceIDs.length
        ? `<button class="text-btn focus-rationale-trigger" type="button" data-focus-rationale="${esc(item.id)}" data-focus-rationale-kind="${esc(kind)}">${icon("sparkles")}${esc(readerFocusCopy("查看依据", "View rationale"))}</button>`
        : "";
    const revision = Number(item.revision) || 1;
    const editorFields = kind === "action"
      ? `<label><span>${esc(readerFocusCopy("行动", "Action"))}</span><textarea data-focus-edit-field="task" maxlength="1000">${esc(text(data.task))}</textarea></label><label><span>${esc(readerFocusCopy("负责人", "Owner"))}</span><input data-focus-edit-field="owner" maxlength="120" value="${esc(text(data.owner))}" /></label><label><span>${esc(readerFocusCopy("截止时间", "Due"))}</span><input data-focus-edit-field="due_at" maxlength="80" value="${esc(text(data.due_at))}" /></label><label><span>${esc(readerFocusCopy("优先级", "Priority"))}</span><select data-focus-edit-field="priority"><option value="low" ${text(data.priority) === "low" ? "selected" : ""}>${esc(readerFocusCopy("低", "Low"))}</option><option value="medium" ${text(data.priority, "medium") === "medium" ? "selected" : ""}>${esc(readerFocusCopy("中", "Medium"))}</option><option value="high" ${text(data.priority) === "high" ? "selected" : ""}>${esc(readerFocusCopy("高", "High"))}</option></select></label>`
      : kind === "decision"
        ? `<label><span>${esc(readerFocusCopy("关键问题", "Question"))}</span><textarea data-focus-edit-field="question" maxlength="500">${esc(text(data.question))}</textarea></label>`
        : `<label><span>${esc(readerFocusCopy("信号详情", "Signal detail"))}</span><textarea data-focus-edit-field="detail" maxlength="2000">${esc(text(data.detail))}</textarea></label>`;
    const decisionNeedsReview = kind === "decision" && ["draft", "edited", "snoozed"].includes(status);
    const resolvedResult = ["accepted", "rejected", "snoozed", "edited"].includes(status) && !decisionNeedsReview;
    const resultActions = confirmedDecision
      ? `${rationaleAction}<button class="text-btn focus-result-undo" type="button" data-focus-feedback="draft">${icon("refresh")}${esc(readerFocusCopy("撤销接受", "Undo accept"))}</button>`
      : resolvedResult
        ? rationaleAction
        : `${rationaleAction}<button class="text-btn" type="button" data-focus-feedback="accepted">${icon("check")}${esc(readerFocusCopy("接受", "Accept"))}</button><button class="text-btn" type="button" data-focus-feedback="rejected">${icon("close")}${esc(readerFocusCopy("拒绝", "Reject"))}</button><button class="text-btn" type="button" data-focus-feedback="snoozed">${icon("inbox")}${esc(readerFocusCopy("稍后处理", "Snooze"))}</button><button class="text-btn" type="button" data-focus-edit-toggle>${icon("edit")}${esc(readerFocusCopy("编辑", "Edit"))}</button>`;
    const editor = resolvedResult || confirmedDecision
      ? ""
      : `<form class="focus-result-editor" data-focus-result-editor hidden><div class="focus-result-editor-fields">${editorFields}</div><div class="focus-result-editor-actions"><button class="primary-btn" type="submit">${icon("check")}${esc(readerFocusCopy("保存修改", "Save changes"))}</button><button class="text-btn" type="button" data-focus-edit-cancel>${esc(readerFocusCopy("取消", "Cancel"))}</button></div></form>`;
    return `<article class="focus-result-card${confirmedDecision ? " is-confirmed-decision" : ""}" data-focus-result-card data-focus-result-kind="${esc(kind)}" data-focus-result-id="${esc(item.id)}" data-focus-result-revision="${revision}"><div class="focus-result-head"><span class="focus-result-kind">${icon(kind === "action" ? "action" : kind === "decision" ? "decision" : "signal")}${esc(confirmedDecision ? readerFocusCopy("当前决定", "Current decision") : focusResultKindLabel(kind))}</span><span class="focus-result-status ${esc(status)}" role="status">${esc(focusResultStatusLabel(status))}</span></div><h4>${esc(title.slice(0, 320))}</h4>${detail ? `<p class="focus-result-detail">${esc(detail.slice(0, 800))}</p>` : ""}${evidence}<div class="focus-result-actions${confirmedDecision ? " is-confirmed-decision" : ""}" role="group" aria-label="${esc(readerFocusCopy("焦点结果操作", "Focus result actions"))}">${resultActions}</div>${editor}</article>`;
  }

  function focusWorkProductTypeLabel(value) {
    return ({
      decision_memo: readerFocusCopy("决策备忘录", "Decision memo"),
      plan: readerFocusCopy("计划", "Plan"),
      travel_guide: readerFocusCopy("旅行攻略", "Travel guide"),
      writing_brief: readerFocusCopy("写作 Brief", "Writing brief"),
      outline: readerFocusCopy("文章大纲", "Outline"),
      comparison: readerFocusCopy("比较报告", "Comparison"),
      custom_document: readerFocusCopy("文档", "Document"),
    })[text(value)] || readerFocusCopy("产出物", "Work product");
  }

  function focusWorkProductSaveHint(item) {
    const data = focusResultData(item);
    const title = text(data.title, readerFocusCopy("未命名产出物", "Untitled work product"));
    const folder = ["writing_brief", "outline"].includes(text(data.type))
      ? readerFocusCopy("写作/", "Writing/")
      : text(data.type) === "travel_guide"
        ? readerFocusCopy("旅行/", "Travel/")
        : readerFocusCopy("焦点产出/", "Focus outputs/");
    return `${folder}${title.replace(/[\\/:*?"<>|]/g, "-").slice(0, 80)}.md`;
  }

  function focusWorkProductCardMarkup(item) {
    if (!item || text(item.kind, "work_product") !== "work_product" || !text(item.id)) return "";
    const data = focusResultData(item);
    const status = text(item.feedback_status || item.status, "draft").toLowerCase();
    if (status === "rejected") return "";
    const title = text(data.title, readerFocusCopy("未命名产出物", "Untitled work product"));
    const summary = text(data.summary);
    const markdown = text(data.content_markdown);
    const type = text(data.type);
    const revision = Number(item.revision) || 1;
    const pending = !["accepted", "edited"].includes(status);
    const validationItems = Array.isArray(data.validation_items)
      ? data.validation_items.map((value) => text(value)).filter(Boolean).slice(0, 10)
      : Array.isArray(item.validation_items)
        ? item.validation_items.map((value) => text(value)).filter(Boolean).slice(0, 10)
        : [];
    const citations = focusResultCitations(item);
    const feedback = pending
      ? `<button class="text-btn" type="button" data-work-product-feedback="accepted">${icon("check")}${esc(readerFocusCopy("接受", "Accept"))}</button><button class="text-btn" type="button" data-work-product-feedback="rejected">${icon("close")}${esc(readerFocusCopy("拒绝", "Reject"))}</button><button class="text-btn" type="button" data-work-product-feedback="snoozed">${icon("inbox")}${esc(readerFocusCopy("稍后处理", "Snooze"))}</button>`
      : `<span class="focus-result-status ${esc(status)}" role="status">${esc(focusResultStatusLabel(status))}</span>`;
    return `<article class="focus-work-product-card" data-focus-work-product-card data-focus-id="${esc(item.focus_id || parity.selectedFocusID)}" data-work-product-id="${esc(item.id)}" data-work-product-revision="${revision}"><div class="focus-work-product-head"><span>${icon(type === "travel_guide" ? "focus" : type === "outline" ? "note" : "edit")}${esc(focusWorkProductTypeLabel(type))}</span><small>v${revision}</small></div><h3>${esc(title)}</h3>${summary ? `<p class="focus-work-product-summary">${esc(summary)}</p>` : ""}<details class="focus-work-product-body" open><summary>${esc(readerFocusCopy("阅读正文", "Read content"))}</summary><div class="fulltext-rich fulltext-markdown">${renderMarkdownContent(markdown, `focus-work-product-${text(item.id)}`)}</div></details>${validationItems.length ? `<section class="focus-work-product-validation"><strong>${esc(readerFocusCopy("交付前检查", "Validation checklist"))}</strong><ul>${validationItems.map((value) => `<li>${esc(value)}</li>`).join("")}</ul></section>` : ""}${citations.length ? `<div class="focus-result-evidence"><span>${esc(readerFocusCopy("引用资料", "Cited sources"))}</span><div class="focus-evidence-chips">${citations.map((citation, index) => focusCitationButtonMarkup(citation, index)).join("")}</div></div>` : ""}<div class="focus-work-product-handoff"><span>${esc(readerFocusCopy("建议保存到", "Suggested location"))}</span><code>${esc(focusWorkProductSaveHint(item))}</code><small>${esc(readerFocusCopy("当前仅支持复制或下载 Markdown，不会直接写入 Obsidian。", "Copy or download Markdown; this version does not write directly to Obsidian."))}</small></div><div class="focus-work-product-actions" role="group" aria-label="${esc(readerFocusCopy("产出物操作", "Work product actions"))}"><button class="text-btn" type="button" data-work-product-copy>${icon("note")}${esc(readerFocusCopy("复制 Markdown", "Copy Markdown"))}</button><button class="text-btn" type="button" data-work-product-download>${icon("external")}${esc(readerFocusCopy("下载 Markdown", "Download Markdown"))}</button>${feedback}</div><p class="focus-work-product-message" role="status"></p></article>`;
  }

  function focusWorkProductFromCard(card) {
    const focusID = text(card?.dataset.focusId || parity.selectedFocusID);
    const workProductID = text(card?.dataset.workProductId);
    if (!focusID || !workProductID) return null;
    const analyses = [focusAnalysisForPanel(focusID), focusAnalysisFor(focusID)];
    for (const analysis of analyses) {
      const item = focusWorkProducts(analysis).find((candidate) => text(candidate.id) === workProductID);
      if (item) return item;
    }
    return null;
  }

  function focusWorkProductFeedbackKey(item, decision) {
    return focusIdempotencyKey([
      "focus-work-product-feedback",
      text(item?.focus_id || parity.selectedFocusID),
      text(item?.id),
      Number(item?.revision) || 1,
      text(decision),
    ].join(":"));
  }

  async function submitFocusWorkProductFeedback(item, decision, card = null) {
    if (!item || !["accepted", "rejected", "snoozed"].includes(text(decision))) return;
    const focusID = text(item.focus_id || parity.selectedFocusID);
    const workProductID = text(item.id);
    const revision = Number(item.revision) || 1;
    const busyKey = `focus:work-product:${workProductID}`;
    if (!focusID || !workProductID || parity.feedbackBusy.has(busyKey)) return;
    parity.feedbackBusy.add(busyKey);
    const message = card?.querySelector(".focus-work-product-message");
    card?.setAttribute("aria-busy", "true");
    card?.querySelectorAll("button").forEach((button) => button.disabled = true);
    if (message) message.textContent = readerFocusCopy("正在保存…", "Saving…");
    try {
      await callFocusOperation("focus_work_product.feedback", {
        body: {
          schema_version: 1,
          focus_id: focusID,
          work_product_id: workProductID,
          expected_revision: revision,
          decision,
        },
        key: focusWorkProductFeedbackKey(item, decision),
      });
      parity.focusAnalysisRunSelections.delete(focusID);
      await reloadFocusProjection(focusID, { silent: true });
      showToast(decision === "accepted"
        ? readerFocusCopy("产出物已接受", "Work product accepted")
        : decision === "rejected"
          ? readerFocusCopy("产出物已拒绝", "Work product rejected")
          : readerFocusCopy("产出物已移到稍后处理", "Work product snoozed"));
    } catch (error) {
      if (message) message.textContent = error?.message || readerFocusCopy("产出物没有保存", "Work product was not saved");
      showError(error?.message || readerFocusCopy("产出物没有保存", "Work product was not saved"));
    } finally {
      card?.removeAttribute("aria-busy");
      card?.querySelectorAll("button").forEach((button) => button.disabled = false);
      parity.feedbackBusy.delete(busyKey);
    }
  }

  async function copyFocusWorkProduct(item, card = null) {
    const markdown = text(focusResultData(item).content_markdown);
    if (!markdown) return;
    try {
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(markdown);
      else {
        const input = document.createElement("textarea");
        input.value = markdown;
        input.setAttribute("readonly", "");
        input.style.position = "fixed";
        input.style.opacity = "0";
        document.body.appendChild(input);
        input.select();
        document.execCommand("copy");
        input.remove();
      }
      const message = card?.querySelector(".focus-work-product-message");
      if (message) message.textContent = readerFocusCopy("Markdown 已复制", "Markdown copied");
    } catch (error) {
      showError(error?.message || readerFocusCopy("复制失败", "Copy failed"));
    }
  }

  function downloadFocusWorkProduct(item) {
    const data = focusResultData(item);
    const markdown = text(data.content_markdown);
    if (!markdown) return;
    const filename = `${text(data.title, "focus-result").replace(/[\\/:*?"<>|]/g, "-").slice(0, 80) || "focus-result"}.md`;
    const url = URL.createObjectURL(new Blob([markdown], { type: "text/markdown;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  function citationEntryMatches(entry, citation) {
    if (!entry || !citation) return false;
    const entryID = text(citation.entry_id).toLowerCase();
    const sourceID = text(citation.source_id).toLowerCase();
    const wanted = [entryID, sourceID]
      .map((value) => text(value).toLowerCase())
      .filter(Boolean);
    if (!wanted.length) return false;
    const values = [
      entry.key,
      entry.id,
      entry.item?.id,
      entry.item?.external_id,
    ]
      .map((value) => text(value).toLowerCase())
      .filter(Boolean);
    // A bare source_id can be a feed/source identifier shared by many
    // articles. Only compare it with an article's own IDs; never select an
    // arbitrary article merely because it belongs to that source.
    return wanted.some((needle) =>
      values.some(
        (value) =>
          value === needle ||
          value === needle.replace(/^news:/, "") ||
          value.endsWith(`:${needle}`) ||
          needle.endsWith(`:${value}`)
      )
    );
  }

  function citationEntryFor(citation) {
    const entries = makeEntries(state.dashboard || {});
    return entries.find((entry) => citationEntryMatches(entry, citation)) || null;
  }

  function citationEntryFallback(citation) {
    // Older Focus payloads used `rss:<entry_id>` as source_id. Keep the
    // display/source identifier intact, but strip the transport prefix for
    // the article endpoint, which addresses the plain news row ID.
    const rawID = text(citation.entry_id || citation.source_id);
    if (!text(citation.entry_id) && !/^(?:rss|news):/i.test(rawID)) return null;
    const id = rawID.replace(/^(?:rss|news):/i, "");
    if (!id) return null;
    return {
      key: `news:${id}`,
      kind: "news",
      group: "news",
      type: "rss",
      id,
      title: text(citation.title, readerFocusCopy("引用原文", "Referenced article")),
      summary: text(citation.quote),
      item: {
        id,
        title: text(citation.title, readerFocusCopy("引用原文", "Referenced article")),
        summary_text: text(citation.quote),
        content_hash: text(citation.content_hash),
        source_id: text(citation.source_id),
        url: "",
        content_format: "text",
      },
    };
  }

  function citationDrawerElements() {
    return Array.from(document.querySelectorAll("[data-focus-citation-drawer]"));
  }

  function refreshCitationDrawerStack() {
    const drawers = citationDrawerElements();
    drawers.forEach((drawer, index) => {
      drawer.dataset.layerIndex = String(index);
      drawer.style.setProperty("--citation-stack-index", String(index));
      const mask = drawer.__focusCitationMask;
      if (mask) mask.style.zIndex = String(80 + index * 2);
      drawer.style.zIndex = String(81 + index * 2);
    });
    document.body.classList.toggle("focus-citation-open", drawers.length > 0);
    refreshFocusV3LayerState();
  }

  function presentCitationDrawer(drawer, kind = "source") {
    citationDrawerElements()
      .filter((existing) => text(existing.dataset.citationLayerKind, "source") === kind)
      .forEach((existing) => closeCitationDrawer(existing));
    drawer.dataset.citationLayerKind = kind;
    const mask = document.createElement("button");
    mask.className = "focus-citation-mask";
    mask.type = "button";
    mask.setAttribute("aria-label", readerFocusCopy("关闭资料来源", "Close source detail"));
    mask.addEventListener("click", () => closeCitationDrawer(drawer));
    drawer.__focusCitationMask = mask;
    document.body.append(mask, drawer);
    refreshCitationDrawerStack();
    return drawer;
  }

  function closeCitationDrawer(drawer) {
    if (!drawer) return;
    drawer.__decisionGraphResizeObserver?.disconnect?.();
    if (drawer.__decisionGraphResizeFrame)
      window.cancelAnimationFrame(drawer.__decisionGraphResizeFrame);
    // 抽屉整个移除时把 cytoscape 实例一并销毁（渲染方不再重建它），避免实例
    // 连同 rAF 渲染循环跟随已摘下的 DOM 泄漏。
    drawer.__decisionGraphCy?.destroy?.();
    drawer.__decisionGraphCy = null;
    drawer.__decisionGraphSignature = "";
    drawer.__focusCitationMask?.remove?.();
    drawer.__focusCitationMask = null;
    drawer.remove();
    refreshCitationDrawerStack();
  }

  function closeTopCitationDrawer() {
    const drawers = citationDrawerElements();
    if (drawers.length) closeCitationDrawer(drawers[drawers.length - 1]);
  }

  function focusCitationIsImage(citation) {
    const sourceKind = text(citation?.source_kind).toLowerCase();
    const mime = text(citation?.mime).toLowerCase();
    return sourceKind === "image" || mime.startsWith("image/");
  }

  function focusImageSourceURL(citation) {
    const sourceLocator = citation?.source_locator && typeof citation.source_locator === "object"
      ? citation.source_locator
      : {};
    const assetID = text(
      citation?.asset_id || sourceLocator.asset_id || sourceLocator.material_id
    );
    if (assetID) return `./api/focus-assets?id=${encodeURIComponent(assetID)}`;
    const locator = text(citation?.locator || sourceLocator.url);
    if (!locator) return "";
    try {
      const resolved = new URL(locator, window.location.href);
      if (
        resolved.origin === window.location.origin &&
        /\/api\/focus-assets$/.test(resolved.pathname)
      )
        return resolved.href;
    } catch {
      return "";
    }
    return "";
  }

  function openFocusImageDrawer(rawCitation, options = {}) {
    const citation = normalizeFocusCitation(rawCitation) || {};
    const source = focusImageSourceURL(citation);
    if (!source) {
      showError(readerFocusCopy("图片原件暂时不可用", "The original image is unavailable"));
      return null;
    }
    const drawer = document.createElement("aside");
    drawer.className = "focus-citation-drawer focus-image-drawer";
    drawer.dataset.focusCitationDrawer = "1";
    drawer.setAttribute("role", "dialog");
    drawer.setAttribute("aria-label", readerFocusCopy("查看图片资料", "View image material"));
    drawer.innerHTML = `<div class="focus-image-body"><img src="${esc(source)}" alt="" /></div><button class="icon-btn focus-image-close" type="button" data-close-focus-citation aria-label="${esc(readerFocusCopy("关闭图片", "Close image"))}" title="${esc(readerFocusCopy("关闭", "Close"))}">${icon("close")}</button>`;
    presentCitationDrawer(drawer, "source");
    const close = drawer.querySelector("[data-close-focus-citation]");
    close?.addEventListener("click", () => closeCitationDrawer(drawer));
    drawer.querySelector("img")?.addEventListener("error", () => {
      closeCitationDrawer(drawer);
      showError(readerFocusCopy("图片原件暂时不可用", "The original image is unavailable"));
    });
    if (options.focusAfterOpen !== false) close?.focus?.();
    return drawer;
  }

  function citationHashMismatch(citation, content, entry) {
    const expected = text(citation?.content_hash);
    const actual = text(content?.content_hash || entry?.item?.content_hash);
    return Boolean(expected && actual && expected !== actual);
  }

  function cachedCitationContentUsable(citation, content) {
    if (!content) return false;
    const blockID = text(citation?.block_id);
    if (!blockID) return true;
    const blocks = Array.isArray(content.blocks)
      ? content.blocks
      : content.article_blocks;
    return Array.isArray(blocks) && blocks.some((block) => {
      return text(block?.block_id || block?.id) === blockID;
    });
  }

  function citationTargetIn(drawer, citation, hashMismatch) {
    if (!drawer) return null;
    const root = drawer.querySelector(".focus-citation-body");
    if (!root) return null;
    const blockID = text(citation?.block_id);
    if (blockID && !hashMismatch) {
      const direct = root.querySelector(`[data-block-id="${CSS.escape(blockID)}"]`);
      if (direct) return direct;
    }
    const quote = normalizeArticleBlockText(citation?.quote);
    if (!quote) return null;
    const candidates = Array.from(
      root.querySelectorAll("[data-block-id],p,h1,h2,h3,h4,li,blockquote,pre,figcaption")
    ).filter((node) => text(node.textContent));
    const matches = candidates.filter((node) => {
      const value = normalizeArticleBlockText(node.textContent);
      return value === quote || value.includes(quote) || quote.includes(value);
    });
    if (matches.length === 1) return matches[0];
    if (matches.length > 1) {
      // Legacy evidence can contain a multi-paragraph excerpt without a
      // block_id. In that case every paragraph inside the excerpt matches.
      // Anchor to the first sufficiently specific block in excerpt order;
      // ties prefer the longer block so short labels cannot steal focus.
      const ranked = matches
        .map((node, domIndex) => {
          const value = normalizeArticleBlockText(node.textContent);
          return { node, domIndex, value, quoteIndex: quote.indexOf(value) };
        })
        .filter((item) => item.value.length >= 12 && item.quoteIndex >= 0)
        .sort((left, right) =>
          left.quoteIndex - right.quoteIndex ||
          right.value.length - left.value.length ||
          left.domIndex - right.domIndex
        );
      if (ranked.length) return ranked[0].node;
    }
    return null;
  }

  function highlightCitationTarget(drawer, citation, content, entry) {
    const mismatch = citationHashMismatch(citation, content, entry);
    const target = citationTargetIn(drawer, citation, mismatch);
    const notice = drawer.querySelector("[data-citation-notice]");
    if (notice) {
      if (mismatch) {
        notice.hidden = false;
        notice.textContent = readerFocusCopy(
          "文章版本已更新，已尝试用引用内容定位。请核对原文。",
          "This article changed; the quote was used as a best-effort anchor. Verify the original."
        );
      } else if (!target && (citation.block_id || citation.quote)) {
        notice.hidden = false;
        notice.textContent = readerFocusCopy(
          "没有找到安全的段落定位，已显示文章原文。",
          "A safe paragraph anchor was not found; the original is shown without forced scrolling."
        );
      } else notice.hidden = true;
    }
    if (!target) return;
    target.classList.remove("focus-citation-highlight");
    void target.offsetWidth;
    target.classList.add("focus-citation-highlight");
    target.scrollIntoView({ block: "center", behavior: "smooth" });
    window.setTimeout(() => target.classList.remove("focus-citation-highlight"), 2600);
  }

  async function citationContentFor(entry, citation) {
    if (!entry) return null;
    if (entry.kind === "news") {
      const cached = cachedFullText(entry.item?.id, citation.content_hash || entry.item?.content_hash);
      if (cached && cachedCitationContentUsable(citation, cached)) return cached;
      if (entry.item?.safe_html) {
        const inline = inlineArticleContent(entry);
        if (cachedCitationContentUsable(citation, inline)) return inline;
      }
      if (entry.item?.id) {
        try {
          return await request(
            `./api/entries/content?id=${encodeURIComponent(entry.item.id)}`,
            { timeoutMs: 30000 }
          );
        } catch {
          return null;
        }
      }
    }
    if (entry.kind === "evidence") {
      const item = entry.item || {};
      return {
        content_text: text(item.content || item.quote || citation.quote),
        content_html: text(item.content_format) === "html" ? text(item.content) : "",
        content_format: text(item.content_format, "text"),
        content_hash: text(item.content_hash || citation.content_hash),
        blocks: Array.isArray(item.blocks) ? item.blocks : [],
        source_url: text(item.url),
      };
    }
    return {
      content_text: text(citation.quote),
      content_format: "text",
      content_hash: text(citation.content_hash),
      blocks: [],
    };
  }

  function citationFocusPickerMarkup(entry) {
    const descriptor = focusLinkDescriptor(entry?.key);
    const linkedCount = descriptor
      ? parity.focusSpaces.filter((focus) => focusHasLinkedEntry(focus, descriptor)).length
      : 0;
    const manageLabel = readerFocusCopy(
      `管理焦点，已添加 ${linkedCount} 个`,
      `Manage focuses; ${linkedCount} linked`
    );
    const addLabel = readerFocusCopy("添加到焦点", "Add to focus");
    const title = linkedCount
      ? readerFocusCopy(`已添加 ${linkedCount} 个焦点`, `${linkedCount} focuses linked`)
      : addLabel;
    return `<button class="article-v3-focus-picker${linkedCount ? " is-linked" : ""}" type="button" data-add-entry-focus="${esc(
      entry?.key
    )}" aria-label="${esc(
      linkedCount ? manageLabel : addLabel
    )}" title="${esc(
      title
    )}">${icon("focus")}<span>${esc(readerFocusCopy("焦点", "Focus"))}${linkedCount ? ` ${linkedCount}` : ""}</span>${icon(
      "chevron"
    )}</button>`;
  }

  function refreshCitationPreferenceButton(drawer, entry, field) {
    const button = drawer?.querySelector(
      field === "starred" ? "[data-star-entry]" : "[data-later-entry]"
    );
    if (!button) return;
    const active = Boolean(pref(entry)[field]);
    const label = field === "starred"
      ? active ? readerFocusCopy("取消收藏", "Remove from favorites") : readerFocusCopy("收藏", "Add to favorites")
      : active ? readerFocusCopy("移出稍后读", "Remove from read later") : readerFocusCopy("稍后读", "Read later");
    button.classList.toggle(field === "starred" ? "starred" : "active", active);
    button.setAttribute("aria-label", label);
    button.title = label;
    button.innerHTML = field === "starred"
      ? active ? icon("starFill") : icon("star")
      : icon("inbox");
  }

  function bindCitationDetailControls(drawer, entry) {
    drawer
      ?.querySelectorAll("[data-close-focus-citation]")
      .forEach((button) =>
        button.addEventListener("click", () => closeCitationDrawer(drawer))
      );
    drawer?.querySelector("[data-star-entry]")?.addEventListener("click", async (event) => {
      event.preventDefault();
      event.stopPropagation();
      await itemMutation(entry.key, "starred", !pref(entry).starred);
      if (drawer.isConnected) refreshCitationPreferenceButton(drawer, entry, "starred");
    });
    drawer?.querySelector("[data-later-entry]")?.addEventListener("click", async (event) => {
      event.preventDefault();
      event.stopPropagation();
      await toggleLater(entry.key);
      if (drawer.isConnected) refreshCitationPreferenceButton(drawer, entry, "read_later");
    });
    drawer
      ?.querySelectorAll(
        "[data-generate-article-overview], [data-retry-article-ai-overview]"
      )
      .forEach((button) =>
        button.addEventListener("click", async (event) => {
          event.preventDefault();
          event.stopPropagation();
          await requestArticleAIOverview(
            entry,
            Boolean(button.dataset.retryArticleAiOverview)
          );
          const current = drawer.querySelector("[data-article-ai-overview]");
          if (current) current.outerHTML = articleAIOverviewMarkup(entry.item);
        })
      );
  }

  function renderCitationArticleDetail(drawer, entry, citation, loaded) {
    const preference = pref(entry);
    const source = sourceLabel(entry);
    const date = formatDate(entryDate(entry));
    const author = text(entry.item?.author, source || readerFocusCopy("订阅来源", "Feed source"));
    const scope = text(entry.item?.permission_scope) === "public_read"
      ? readerFocusCopy("公开可读", "Public")
      : text(entry.item?.permission_scope, readerFocusCopy("来源可读", "Source access"));
    const external = entry.item?.url && publicURL(entry.item.url)
      ? `<a class="icon-btn article-v3-external" href="${esc(
          entry.item.url
        )}" target="_blank" rel="noopener noreferrer" aria-label="${esc(readerFocusCopy("打开原文", "Open original"))}" title="${esc(readerFocusCopy("打开原文", "Open original"))}">${icon(
          "external"
        )}</a>`
      : "";
    drawer.className = "focus-citation-drawer focus-source-detail-drawer detail-panel article-v3-detail";
    drawer.setAttribute("aria-label", readerFocusCopy("文章详情", "Article detail"));
    drawer.innerHTML = `<div class="article-v3-topbar"><span class="article-v3-column-label">${esc(
      readerFocusCopy("文章详情", "Article detail")
    )}</span><div class="article-v3-actions"><button class="icon-btn ${
      preference.starred ? "starred" : ""
    }" type="button" data-star-entry="${esc(entry.key)}" aria-label="${esc(
      preference.starred ? readerFocusCopy("取消收藏", "Remove from favorites") : readerFocusCopy("收藏", "Add to favorites")
    )}" title="${esc(preference.starred ? readerFocusCopy("取消收藏", "Remove from favorites") : readerFocusCopy("收藏", "Add to favorites"))}">${
      preference.starred ? icon("starFill") : icon("star")
    }</button><button class="icon-btn ${preference.read_later ? "active" : ""}" type="button" data-later-entry="${esc(
      entry.key
    )}" aria-label="${esc(preference.read_later ? readerFocusCopy("移出稍后读", "Remove from read later") : readerFocusCopy("稍后读", "Read later"))}" title="${esc(
      preference.read_later ? readerFocusCopy("移出稍后读", "Remove from read later") : readerFocusCopy("稍后读", "Read later")
    )}">${icon("inbox")}</button>${external}${citationFocusPickerMarkup(
      entry
    )}<button class="icon-btn" type="button" data-close-focus-citation aria-label="${esc(readerFocusCopy("关闭文章详情", "Close article detail"))}" title="${esc(readerFocusCopy("关闭文章详情", "Close article detail"))}">${icon(
      "close"
    )}</button></div></div><div class="focus-source-scroll"><div class="article-v3-heading"><div class="detail-top-copy"><div class="article-v3-kicker">${kindIcon(
      entry
    )}<span>${esc(source)}</span>${date ? `<span>${esc(date)}</span>` : ""}</div><h2 class="article-v3-title">${esc(
      entry.title
    )}</h2></div></div>${articleAIOverviewMarkup(
      entry.item
    )}<p class="article-v3-byline">${esc(author)} · ${esc(
      scope
    )}</p><div class="focus-citation-notice" data-citation-notice hidden role="status"></div><article class="detail-body focus-citation-body" data-citation-body tabindex="-1"></article></div>`;
    const body = drawer.querySelector("[data-citation-body]");
    if (loaded) renderArticleInto(body, entry, loaded);
    else if (body)
      body.innerHTML = `<div class="article-fallback"><strong>${esc(
        readerFocusCopy("原文暂时不可用", "Source unavailable")
      )}</strong><p>${esc(
        citation.quote || readerFocusCopy("当前只保留了引用摘录。", "Only the quoted excerpt is available.")
      )}</p></div>`;
    bindCitationDetailControls(drawer, entry);
    return body;
  }

  async function hydrateCitationEntry(entry) {
    if (entry?.kind === "recording" && !state.recordingDetails.has(text(entry.id))) {
      try {
        const detail = entry.item?.live_device
          ? await loadLiveMeetingDetail(entry.id)
          : (await request(`./api/recordings/detail?id=${encodeURIComponent(entry.id)}`, {
              timeoutMs: 7000,
            }))?.recording;
        if (detail) state.recordingDetails.set(text(entry.id), detail);
      } catch {
        // The list projection remains a useful degraded real detail when the
        // device read is transiently unavailable.
      }
    }
    if (
      entry?.kind === "evidence" &&
      text(entry.item?.source_type).toLowerCase() === "obsidian" &&
      entry.item?.live_device &&
      !text(entry.item?.content)
    ) {
      try {
        await loadLiveObsidianDetail(entry.item.locator || entry.item.path);
      } catch {
        // Keep the real metadata surface visible with its unavailable state.
      }
    }
  }

  async function openCitationDrawer(rawCitation, options = {}) {
    const citation = normalizeFocusCitation(rawCitation) || {};
    if (focusCitationIsImage(citation))
      return openFocusImageDrawer(citation, options);
    const entry = citationEntryFor(citation) || citationEntryFallback(citation);
    const drawer = document.createElement("aside");
    drawer.className = "focus-citation-drawer focus-source-detail-drawer";
    drawer.dataset.focusCitationDrawer = "1";
    drawer.setAttribute("role", "dialog");
    drawer.setAttribute("aria-label", readerFocusCopy("查看原文依据", "View source evidence"));
    drawer.innerHTML = `<div class="focus-source-loading" role="status"><span class="detail-loading-spinner" aria-hidden="true"></span><span>${esc(
      readerFocusCopy("正在读取原文…", "Loading source…")
    )}</span></div>`;
    presentCitationDrawer(drawer, "source");

    await hydrateCitationEntry(entry);
    if (entry?.kind === "recording") {
      renderMeetingDetail(entry, drawer, { citation: true });
      bindCitationDetailControls(drawer, entry);
      if (options.focusAfterOpen !== false)
        drawer.querySelector("[data-close-focus-citation]")?.focus?.();
      return drawer;
    }
    if (
      entry?.kind === "evidence" &&
      text(entry.item?.source_type).toLowerCase() === "obsidian"
    ) {
      renderObsidianDetail(entry, drawer, { citation: true });
      bindCitationDetailControls(drawer, entry);
      if (options.focusAfterOpen !== false)
        drawer.querySelector("[data-close-focus-citation]")?.focus?.();
      return drawer;
    }

    const loaded = await citationContentFor(entry, citation);
    const cacheable = text(loaded?.status, "ok") !== "summary";
    if (entry?.kind === "news" && entry.item?.id && loaded && cacheable) {
      storeFullText(entry.item.id, loaded);
      void writeBrowserFullText(entry, loaded);
    }
    if (entry?.kind === "news") {
      const body = renderCitationArticleDetail(drawer, entry, citation, loaded);
      highlightCitationTarget(drawer, citation, loaded, entry);
      if (options.focusAfterOpen !== false) body?.focus?.();
      return drawer;
    }

    const title = text(entry?.title || citation.title, readerFocusCopy("引用原文", "Referenced article"));
    drawer.className = "focus-citation-drawer";
    drawer.innerHTML = `<div class="focus-citation-head"><div><span class="focus-citation-kicker">${esc(
      focusCitationRoleLabel(citation.role)
    )}</span><h2>${esc(title)}</h2></div><button class="icon-btn" type="button" data-close-focus-citation aria-label="${esc(
      readerFocusCopy("关闭依据", "Close evidence")
    )}" title="${esc(readerFocusCopy("关闭", "Close"))}">${icon(
      "close"
    )}</button></div><div class="focus-citation-notice" data-citation-notice hidden role="status"></div><div class="focus-citation-body" data-citation-body><div class="article-fallback"><strong>${esc(
      readerFocusCopy("原文暂时不可用", "Source unavailable")
    )}</strong><p>${esc(
      citation.quote || readerFocusCopy("当前只保留了引用摘录。", "Only the quoted excerpt is available.")
    )}</p></div></div>`;
    bindCitationDetailControls(drawer, entry || { key: "" });
    highlightCitationTarget(drawer, citation, loaded, entry);
    return drawer;
  }

  function openRationaleDrawer(item) {
    const citations = focusResultCitations(item);
    const kind = text(item?.kind, "signal");
    const data = focusResultData(item);
    const title = kind === "action"
      ? text(data.task, readerFocusCopy("行动依据", "Action rationale"))
      : kind === "decision"
        ? text(data.question, readerFocusCopy("决策依据", "Decision rationale"))
        : text(data.title, readerFocusCopy("信号依据", "Signal rationale"));
    const drawer = document.createElement("aside");
    drawer.className = "focus-citation-drawer focus-rationale-drawer";
    drawer.dataset.focusCitationDrawer = "1";
    drawer.setAttribute("role", "dialog");
    drawer.setAttribute("aria-label", readerFocusCopy("查看决策依据", "View rationale"));
    drawer.innerHTML = `<div class="focus-citation-head"><div><span class="focus-citation-kicker">${esc(readerFocusCopy("形成依据", "Rationale"))}</span><h2>${esc(title.slice(0, 320))}</h2></div><button class="icon-btn" type="button" data-close-focus-citation aria-label="${esc(readerFocusCopy("关闭依据", "Close rationale"))}" title="${esc(readerFocusCopy("关闭", "Close"))}">${icon("close")}</button></div><div class="focus-rationale-summary"><strong>${esc(readerFocusCopy("为什么形成这个结果", "Why this result was formed"))}</strong><p>${esc(text(data.recommendation || data.detail || data.task || readerFocusCopy("以下是本次分析使用的来源。", "These are the sources used for this analysis.")))}</p></div><section class="focus-rationale-evidence"><div class="focus-citation-section-title"><span>${esc(readerFocusCopy("依据来源", "Evidence sources"))}</span><span>${citations.length}</span></div>${citations.length ? `<div class="focus-rationale-list">${citations.map((citation, index) => focusCitationButtonMarkup(citation, index)).join("")}</div>` : `<p class="focus-citation-empty">${esc(readerFocusCopy("当前结果没有可定位的段落引用。", "No paragraph-level citations are available for this result."))}</p>`}</section>`;
    presentCitationDrawer(drawer, "workspace");
    drawer.querySelector("[data-close-focus-citation]")?.addEventListener("click", () => closeCitationDrawer(drawer));
    return drawer;
  }

  function normalizeDecisionHistoryResponse(value, focusID = "", decisionID = "") {
    if (!value || typeof value !== "object") return null;
    const root = value.data && typeof value.data === "object" ? value.data : value;
    const revisions = (Array.isArray(root.revisions) ? root.revisions : []).filter(
      (item) => item && text(item.id)
    );
    const selectedID = text(root.selected_revision_id || revisions[0]?.id);
    const nodes = (Array.isArray(root.nodes) ? root.nodes : []).filter(
      (item) => item && text(item.id)
    );
    const nodeIDs = new Set(nodes.map((item) => text(item.id)));
    const edges = (Array.isArray(root.edges) ? root.edges : []).filter(
      (item) => item && nodeIDs.has(text(item.source)) && nodeIDs.has(text(item.target))
    );
    const stats = root.stats && typeof root.stats === "object" ? root.stats : {};
    return {
      schemaVersion: Number(root.schema_version) || 2,
      focusID: text(root.focus_id || focusID),
      decisionID: text(revisions.find((item) => text(item.id) === selectedID)?.decision_id || decisionID),
      selectedID,
      revisions,
      nodes,
      edges,
      events: (Array.isArray(root.events) ? root.events : []).filter(Boolean),
      reviews: (Array.isArray(root.reviews) ? root.reviews : []).filter(Boolean),
      stats: {
        evidence_count: readerFocusNumber(stats.evidence_count),
        conflict_count: readerFocusNumber(stats.conflict_count),
        gap_count: readerFocusNumber(stats.gap_count),
        action_count: readerFocusNumber(stats.action_count),
        alternative_count: readerFocusNumber(stats.alternative_count),
        node_count: readerFocusNumber(stats.node_count, nodes.length),
        edge_count: readerFocusNumber(stats.edge_count, edges.length),
        revision_count: readerFocusNumber(stats.revision_count, revisions.length),
        is_complex: Boolean(stats.is_complex),
      },
    };
  }

  function selectedDecisionRevision(history) {
    return history?.revisions?.find((item) => text(item.id) === history.selectedID) || history?.revisions?.[0] || null;
  }

  function decisionHistoryMutationKey(operation, payload) {
    const canonical = JSON.stringify({ operation: text(operation), payload });
    let hash = 2166136261;
    let secondaryHash = 2246822519;
    for (let index = 0; index < canonical.length; index += 1) {
      const code = canonical.charCodeAt(index);
      hash ^= code;
      hash = Math.imul(hash, 16777619);
      secondaryHash ^= code;
      secondaryHash = Math.imul(secondaryHash, 3266489917);
    }
    return `decision-history:${(hash >>> 0).toString(16).padStart(8, "0")}${(secondaryHash >>> 0).toString(16).padStart(8, "0")}`;
  }

  async function requestDecisionHistory(focusID, decisionID, revisionID = "") {
    const query = new URLSearchParams({ focus_id: focusID, decision_id: decisionID });
    if (revisionID) query.set("revision_id", revisionID);
    const result = await request(`./api/app-data/v1/operations/decision_revision.get?${query}`, {
      method: "POST",
      timeoutMs: 10000,
    });
    return normalizeDecisionHistoryResponse(result, focusID, decisionID);
  }

  async function mutateDecisionHistory(operation, payload) {
    return request(`./api/app-data/v1/operations/${operation}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": decisionHistoryMutationKey(operation, payload),
      },
      body: JSON.stringify(payload),
      timeoutMs: 15000,
    });
  }

  function decisionNodeTypeLabel(type) {
    return ({
      evidence: readerFocusCopy("资料", "Evidence"),
      user_answer: readerFocusCopy("你的回答", "Your answer"),
      ai_judgment: readerFocusCopy("AI 判断", "AI judgment"),
      gap: readerFocusCopy("信息缺口", "Information gap"),
      conflict: readerFocusCopy("冲突依据", "Conflicting evidence"),
      alternative: readerFocusCopy("落选方案", "Alternative"),
      decision: readerFocusCopy("已确认决策", "Confirmed decision"),
      action: readerFocusCopy("行动", "Action"),
      user_note: readerFocusCopy("补充说明", "User note"),
      correction: readerFocusCopy("纠错", "Correction"),
      replacement: readerFocusCopy("替代版本", "Replacement version"),
      review: readerFocusCopy("结果回顾", "Review"),
    })[text(type)] || readerFocusCopy("判断", "Judgment");
  }

  function decisionEventActorLabel(actor) {
    return ({ user: readerFocusCopy("你", "You"), ai: "AI", source: readerFocusCopy("资料", "Source"), system: readerFocusCopy("系统", "System") })[text(actor)] || readerFocusCopy("系统", "System");
  }

  function decisionNodeCitation(node) {
    if (!node) return null;
    const metadata = node.metadata && typeof node.metadata === "object" ? node.metadata : {};
    const sourceLocator = node.source_locator || metadata.source_locator;
    if (!text(node.entry_id || node.source_id || node.asset_id) && !sourceLocator) return null;
    return normalizeFocusCitation({
      source_kind: node.source_kind || metadata.source_kind,
      source_id: node.source_id,
      entry_id: node.entry_id,
      asset_id: node.asset_id || metadata.asset_id,
      mime: node.mime || metadata.mime,
      block_id: node.block_id,
      content_hash: node.content_hash,
      text_hash: node.text_hash || metadata.text_hash,
      quote: node.body,
      title: node.title,
      locator: node.locator || metadata.locator,
      source_locator: sourceLocator,
      role: node.evidence_role || metadata.evidence_role || metadata.role,
    });
  }

  function decisionHistoryStatsMarkup(history) {
    const stats = history?.stats || {};
    return `<div class="decision-history-stats" aria-label="${esc(readerFocusCopy("决策依据统计", "Decision rationale statistics"))}"><span><strong>${readerFocusNumber(stats.evidence_count)}</strong>${esc(readerFocusCopy("资料", "Sources"))}</span><span><strong>${readerFocusNumber(stats.conflict_count)}</strong>${esc(readerFocusCopy("冲突", "Conflicts"))}</span><span><strong>${readerFocusNumber(stats.gap_count)}</strong>${esc(readerFocusCopy("缺口", "Gaps"))}</span><span><strong>${readerFocusNumber(stats.action_count)}</strong>${esc(readerFocusCopy("行动", "Actions"))}</span></div>`;
  }

  function decisionComparisonStateLabel(value, relation = false) {
    const state = text(value);
    const labels = relation
      ? {
          added: readerFocusCopy("新增关系", "Added relationship"),
          changed: readerFocusCopy("变化关系", "Changed relationship"),
          removed: readerFocusCopy("删除关系", "Removed relationship"),
        }
      : {
          added: readerFocusCopy("新增", "Added"),
          changed: readerFocusCopy("变化", "Changed"),
          removed: readerFocusCopy("删除", "Removed"),
        };
    return labels[state] || "";
  }

  function decisionGraphProjection(workspace) {
    const history = workspace?.history || {};
    const comparison = workspace?.comparison;
    const changes = comparison?.changes && typeof comparison.changes === "object"
      ? comparison.changes
      : {};
    const comparisonGraph = comparison?.compare && typeof comparison.compare === "object"
      ? comparison.compare
      : null;
    const baseGraph = comparison?.base && typeof comparison.base === "object"
      ? comparison.base
      : null;
    const currentNodes = Array.isArray(comparisonGraph?.nodes)
      ? comparisonGraph.nodes
      : Array.isArray(history.nodes)
        ? history.nodes
        : [];
    const currentEdges = Array.isArray(comparisonGraph?.edges)
      ? comparisonGraph.edges
      : Array.isArray(history.edges)
        ? history.edges
        : [];
    const baseNodes = Array.isArray(baseGraph?.nodes) ? baseGraph.nodes : [];
    const baseEdges = Array.isArray(baseGraph?.edges) ? baseGraph.edges : [];
    const ids = (key) => new Set(
      (Array.isArray(changes[key]) ? changes[key] : [])
        .map((value) => text(value))
        .filter(Boolean)
    );
    const addedNodes = ids("added_node_ids");
    const changedNodes = ids("changed_node_ids");
    const removedNodes = ids("removed_node_ids");
    const addedEdges = ids("added_edge_ids");
    const changedEdges = ids("changed_edge_ids");
    const removedEdges = ids("removed_edge_ids");
    const nodeStates = new Map();
    const edgeStates = new Map();
    for (const id of addedNodes) nodeStates.set(id, "added");
    for (const id of changedNodes) nodeStates.set(id, "changed");
    for (const id of removedNodes) nodeStates.set(id, "removed");
    for (const id of addedEdges) edgeStates.set(id, "added");
    for (const id of changedEdges) edgeStates.set(id, "changed");
    for (const id of removedEdges) edgeStates.set(id, "removed");

    const nodes = currentNodes.filter((node) => node && text(node.id));
    const nodeIDs = new Set(nodes.map((node) => text(node.id)));
    for (const node of baseNodes) {
      const id = text(node?.id);
      if (!id || !removedNodes.has(id) || nodeIDs.has(id)) continue;
      nodes.push(node);
      nodeIDs.add(id);
    }
    const edges = currentEdges.filter(
      (edge) => edge && text(edge.source) && text(edge.target)
    );
    const edgeIDs = new Set(
      edges.map((edge) => text(edge.id)).filter(Boolean)
    );
    for (const edge of baseEdges) {
      const id = text(edge?.id);
      if (!id || !removedEdges.has(id) || edgeIDs.has(id)) continue;
      edges.push(edge);
      edgeIDs.add(id);
    }
    const highlightedNodeIDs = new Set(nodeStates.keys());
    for (const edge of edges) {
      if (!edgeStates.has(text(edge.id))) continue;
      if (text(edge.source)) highlightedNodeIDs.add(text(edge.source));
      if (text(edge.target)) highlightedNodeIDs.add(text(edge.target));
    }
    return {
      nodes,
      edges,
      nodeStates,
      edgeStates,
      highlightedNodeIDs,
      summary: {
        addedNodes: addedNodes.size,
        changedNodes: changedNodes.size,
        removedNodes: removedNodes.size,
        addedEdges: addedEdges.size,
        changedEdges: changedEdges.size,
        removedEdges: removedEdges.size,
      },
    };
  }

  function decisionNodeMarkup(node, selectedID = "", graph = false, comparisonStates = new Map()) {
    const type = text(node?.type, "judgment").replace(/[^a-z_]/g, "");
    const status = text(node?.status, "active").replace(/[^a-z_]/g, "");
    const nodeID = text(node?.id);
    const comparisonState = comparisonStates instanceof Map
      ? text(comparisonStates.get(nodeID))
      : comparisonStates?.has?.(nodeID)
        ? "changed"
        : "";
    const classes = ["decision-logic-node", `is-${type}`, `is-${status}`];
    if (nodeID === selectedID) classes.push("is-selected");
    if (comparisonState) classes.push(`is-compare-${comparisonState}`);
    if (comparisonState === "changed") classes.push("is-changed");
    const stateLabel = decisionComparisonStateLabel(comparisonState);
    const label = `${decisionNodeTypeLabel(type)}${node.author === "user" ? ` · ${readerFocusCopy("你", "You")}` : node.author === "ai" ? " · AI" : ""}`;
    const iconName = ({
      evidence: "note",
      conflict: "note",
      user_answer: "note",
      ai_judgment: "sparkles",
      decision: "decision",
      action: "action",
      gap: "signal",
      alternative: "close",
      replacement: "refresh",
      correction: "refresh",
      user_note: "note",
    })[type] || "note";
    const head = graph
      ? `<span class="decision-graph-node-head"><b>${icon(iconName)}</b><em>${esc(label)}</em>${stateLabel ? `<i class="decision-compare-state is-${esc(comparisonState)}">${esc(stateLabel)}</i>` : ""}</span>`
      : `<span>${esc(label)}${stateLabel ? `<small class="decision-compare-state is-${esc(comparisonState)}">${esc(stateLabel)}</small>` : ""}</span>`;
    return `<button class="${classes.join(" ")}" type="button" data-decision-node-id="${esc(node.id)}"${graph ? ` data-decision-graph-node="${esc(node.id)}"` : ""}${comparisonState ? ` data-decision-compare-state="${esc(comparisonState)}"` : ""}${comparisonState === "removed" ? " disabled" : ""}>${head}<strong>${esc(text(node.title, decisionNodeTypeLabel(type)))}</strong>${node.body ? `<p>${esc(text(node.body))}</p>` : ""}</button>`;
  }

  function decisionGraphNodeCategory(node) {
    const type = text(node?.type);
    if (["evidence", "conflict"].includes(type)) return "evidence";
    if (type === "gap") return "gap";
    if (["user_answer", "ai_judgment", "user_note", "correction"].includes(type)) return "answer";
    if (["action", "review"].includes(type)) return "action";
    return "decision";
  }

  function decisionGraphChainNodeMarkup(node, kind, workspace) {
    if (!node) return "";
    const nodeID = text(node.id);
    const revision = selectedDecisionRevision(workspace.history);
    const label = kind === "decision"
      ? readerFocusCopy("已确认决策", "Confirmed decision")
      : readerFocusCopy("行动", "Action");
    const title = kind === "decision"
      ? text(revision?.content, node.title)
      : text(node.title, node.body);
    const detail = kind === "decision"
      ? text(revision?.expected_outcome, node.body, readerFocusCopy("已确认的决策方案", "Confirmed decision"))
      : text(node.body, readerFocusCopy("由当前决策生成", "Generated by this decision"));
    const classes = ["decision-graph-main-node", `is-${kind}`];
    if (nodeID === workspace.selectedNodeID) classes.push("is-selected");
    return `<button class="${classes.join(" ")}" type="button" data-decision-node-id="${esc(nodeID)}" data-decision-graph-node="${esc(nodeID)}"><i class="decision-graph-handle is-left" aria-hidden="true"></i><span class="decision-graph-node-head"><b>${kind === "decision" ? icon("decision") : icon("action")}</b><em>${esc(label)}</em></span><strong>${esc(title)}</strong><small>${esc(detail)}</small><i class="decision-graph-handle is-right" aria-hidden="true"></i></button>`;
  }

  function decisionOverviewGroup(history, label, types) {
    const nodes = history.nodes.filter((node) => types.includes(text(node.type)));
    if (!nodes.length) return "";
    return `<section class="decision-evidence-section"><div class="decision-section-title"><span>${esc(label)}</span><strong>${nodes.length}</strong></div><div class="decision-evidence-list">${nodes.map((node) => decisionNodeMarkup(node)).join("")}</div></section>`;
  }

  function decisionReviewIsDue(revision, reviews) {
    const reviewAt = text(revision?.review_at);
    const timestamp = reviewAt ? Date.parse(reviewAt) : 0;
    return Boolean(
      timestamp &&
      timestamp <= Date.now() &&
      text(revision?.status) === "confirmed" &&
      !reviews.some((item) => text(item.decision_revision_id) === text(revision.id))
    );
  }

  function decisionReviewMarkup(history) {
    const revision = selectedDecisionRevision(history);
    if (!revision) return "";
    const review = history.reviews.find((item) => text(item.decision_revision_id) === text(revision.id));
    if (review)
      return `<section class="decision-review-result"><span>${esc(readerFocusCopy("复盘结果", "Review result"))}</span><strong>${esc(({ maintain: readerFocusCopy("继续执行", "Maintain"), adjust: readerFocusCopy("调整决策", "Adjust"), overturn: readerFocusCopy("推翻决策", "Overturn") })[text(review.result)] || text(review.result))}</strong><p>${esc(text(review.outcome))}</p></section>`;
    if (!decisionReviewIsDue(revision, history.reviews)) return "";
    return `<form class="decision-review-form" data-decision-review-form><div class="decision-section-title"><span>${esc(readerFocusCopy("到达回顾时间", "Review is due"))}</span></div><label><span>${esc(readerFocusCopy("实际结果", "Actual outcome"))}</span><textarea name="outcome" maxlength="3000" required placeholder="${esc(readerFocusCopy("记录发生了什么，以及与预期有什么差异", "Describe what happened and how it differed from expectations"))}"></textarea></label><div class="decision-review-options"><label><input type="radio" name="result" value="maintain" checked><span>${esc(readerFocusCopy("继续执行", "Maintain"))}</span></label><label><input type="radio" name="result" value="adjust"><span>${esc(readerFocusCopy("调整", "Adjust"))}</span></label><label><input type="radio" name="result" value="overturn"><span>${esc(readerFocusCopy("推翻", "Overturn"))}</span></label></div><label><span>${esc(readerFocusCopy("新决策（调整或推翻时必填）", "Replacement decision (required for adjust/overturn)"))}</span><textarea name="replacement_content" maxlength="4000"></textarea></label><div class="decision-form-message" role="status"></div><button class="primary-btn" type="submit">${esc(readerFocusCopy("保存复盘", "Save review"))}</button></form>`;
  }

  function decisionHistoryOverviewMarkup(history) {
    const revision = selectedDecisionRevision(history);
    if (!revision) return `<div class="decision-history-empty"><strong>${esc(readerFocusCopy("还没有已确认决策", "No confirmed decision yet"))}</strong><p>${esc(readerFocusCopy("确认决策后，这里会保留它的依据、冲突和行动。", "Confirm a decision to preserve its rationale, conflicts, and actions here."))}</p></div>`;
    const assumptionItems = (Array.isArray(revision.assumptions) ? revision.assumptions : []).map((value) => `<li>${esc(value)}</li>`).join("");
    const riskItems = (Array.isArray(revision.risks) ? revision.risks : []).map((value) => `<li>${esc(value)}</li>`).join("");
    return `<div class="decision-overview"><p class="decision-overview-intro">${esc(readerFocusCopy("这里说明结论依据、仍存在的不确定性，以及由此产生的待办，帮助你复核这项决定是否值得接受。", "This explains the evidence, remaining uncertainty, and resulting to-dos so you can review whether the decision is worth accepting."))}</p><section class="decision-conclusion"><span>${esc(readerFocusCopy("已确认结论", "Confirmed conclusion"))} · v${Number(revision.revision_no) || 1}</span><h3>${esc(text(revision.content))}</h3>${revision.expected_outcome ? `<p><strong>${esc(readerFocusCopy("预期结果", "Expected outcome"))}</strong>${esc(revision.expected_outcome)}</p>` : ""}<div class="decision-conclusion-meta"><span>${esc(readerFocusCopy("确认于", "Confirmed"))} ${esc(formatDateByLocale(revision.confirmed_at || revision.created_at, { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }))}</span>${revision.review_at ? `<span>${esc(readerFocusCopy("回顾", "Review"))} ${esc(formatDateByLocale(revision.review_at, { year: "numeric", month: "short", day: "numeric" }))}</span>` : ""}</div></section>${decisionHistoryStatsMarkup(history)}${decisionOverviewGroup(history, readerFocusCopy("支持依据", "Supporting evidence"), ["evidence"])}${decisionOverviewGroup(history, readerFocusCopy("冲突与信息缺口", "Conflicts and gaps"), ["conflict", "gap"])}${decisionOverviewGroup(history, readerFocusCopy("你的回答与判断", "Answers and judgments"), ["user_answer", "ai_judgment", "user_note", "correction", "replacement"])}${decisionOverviewGroup(history, readerFocusCopy("生成待办", "Generated to-dos"), ["action"])}${assumptionItems || riskItems ? `<section class="decision-assumption-grid">${assumptionItems ? `<div><strong>${esc(readerFocusCopy("关键假设", "Assumptions"))}</strong><ul>${assumptionItems}</ul></div>` : ""}${riskItems ? `<div><strong>${esc(readerFocusCopy("风险", "Risks"))}</strong><ul>${riskItems}</ul></div>` : ""}</section>` : ""}${decisionReviewMarkup(history)}</div>`;
  }

  function decisionHistoryGraphMarkup(workspace) {
    const history = workspace.history;
    const projection = decisionGraphProjection(workspace);
    const hiddenTypes = workspace.hiddenTypes instanceof Set ? workspace.hiddenTypes : new Set();
    const expanded = workspace.expandedGroups.has("all");
    const summary = projection.summary;
    const comparisonBanner = workspace.comparison
      ? `<div class="decision-compare-banner" role="status"><strong>${esc(readerFocusCopy("版本差异", "Version differences"))}</strong><span class="is-added">${esc(readerFocusCopy(`新增 ${summary.addedNodes} 个节点 · ${summary.addedEdges} 条关系`, `Added ${summary.addedNodes} nodes · ${summary.addedEdges} relationships`))}</span><span class="is-changed">${esc(readerFocusCopy(`变化 ${summary.changedNodes} 个节点 · ${summary.changedEdges} 条关系`, `Changed ${summary.changedNodes} nodes · ${summary.changedEdges} relationships`))}</span><span class="is-removed">${esc(readerFocusCopy(`删除 ${summary.removedNodes} 个节点 · ${summary.removedEdges} 条关系`, `Removed ${summary.removedNodes} nodes · ${summary.removedEdges} relationships`))}</span></div>`
      : "";
    const filters = [
      ["evidence", readerFocusCopy("资料", "Sources")],
      ["answer", readerFocusCopy("回答与判断", "Answers")],
      ["gap", readerFocusCopy("待验证", "Gaps")],
      ["action", readerFocusCopy("行动", "Actions")],
    ].map(([key, label]) => `<button type="button" data-decision-filter-type="${key}" class="${hiddenTypes.has(key) ? "" : "is-active"}"><span>${hiddenTypes.has(key) ? "" : icon("check")}</span>${esc(label)}</button>`).join("");
    const fullscreenLabel = workspace.fullscreen ? readerFocusCopy("退出全屏", "Exit fullscreen") : readerFocusCopy("展开全屏", "Open fullscreen");
    return `<div class="decision-graph-toolbar"><div class="decision-graph-reading"><strong>${esc(readerFocusCopy("从左往右", "Left to right"))}</strong><span>${icon("note")}${readerFocusNumber(history.stats.evidence_count)} ${esc(readerFocusCopy("条资料", "sources"))}</span>${icon("chevron")}<span>${icon("decision")}1 ${esc(readerFocusCopy("个结论", "decision"))}</span>${icon("chevron")}<span>${icon("action")}${readerFocusNumber(history.stats.action_count)} ${esc(readerFocusCopy("个行动", "actions"))}</span>${history.stats.gap_count ? `<em>${readerFocusNumber(history.stats.gap_count)} ${esc(readerFocusCopy("项待验证", "gaps"))}</em>` : ""}</div><div class="decision-graph-tools"><button class="decision-graph-detail-toggle" type="button" data-decision-expand-group="all">${icon(expanded ? "minimize" : "action")}<span>${esc(expanded ? readerFocusCopy("回到主线", "Main chain") : readerFocusCopy("展开推理链条", "Open reasoning chain"))}</span></button><button type="button" data-decision-toggle-filter class="${workspace.filtersOpen ? "is-active" : ""}" aria-label="${esc(readerFocusCopy("筛选", "Filter"))}" title="${esc(readerFocusCopy("筛选", "Filter"))}">${icon("filter")}</button><button type="button" data-decision-fullscreen aria-label="${esc(fullscreenLabel)}" title="${esc(fullscreenLabel)}">${icon(workspace.fullscreen ? "minimize" : "maximize")}</button></div>${workspace.filtersOpen ? `<div class="decision-graph-filter-menu">${filters}</div>` : ""}</div>${comparisonBanner}<div class="decision-graph-scroll" data-decision-graph-scroll><div class="decision-graph-cytoscape" data-decision-graph-cytoscape aria-label="${esc(readerFocusCopy("决策逻辑关系图", "Decision logic graph"))}"></div><div class="decision-graph-controls" aria-label="${esc(readerFocusCopy("画布缩放", "Canvas zoom"))}"><button type="button" data-decision-zoom="in" aria-label="${esc(readerFocusCopy("放大", "Zoom in"))}">+</button><button type="button" data-decision-zoom="out" aria-label="${esc(readerFocusCopy("缩小", "Zoom out"))}">−</button><button type="button" data-decision-fit-graph aria-label="${esc(readerFocusCopy("适应画布", "Fit canvas"))}">${icon("maximize")}</button></div></div>`;
  }

  function decisionHistoryTimelineMarkup(history) {
    const events = [...history.events].sort((left, right) => Number(right.event_seq) - Number(left.event_seq));
    if (!events.length) return `<div class="decision-history-empty"><strong>${esc(readerFocusCopy("形成过程尚未记录", "Formation history is not available"))}</strong></div>`;
    return `<div class="decision-timeline">${events.map((event) => `<article><i></i><div><span>${esc(decisionEventActorLabel(event.actor))} · ${esc(formatDateByLocale(event.created_at, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }))}</span><strong>${esc(text(event.label, event.type))}</strong>${event.payload?.content ? `<p>${esc(text(event.payload.content))}</p>` : ""}</div></article>`).join("")}</div>`;
  }

  function decisionNodeInspectorIcon(node) {
    return ({
      evidence: "note",
      conflict: "note",
      user_answer: "note",
      ai_judgment: "sparkles",
      decision: "decision",
      action: "action",
      gap: "signal",
      alternative: "close",
      replacement: "refresh",
      correction: "refresh",
      user_note: "note",
      review: "refresh",
    })[text(node?.type)] || "note";
  }

  function decisionNodeSourceLabel(sourceKind) {
    return ({
      meeting: readerFocusCopy("会议与录音", "Meeting and recording"),
      rss: readerFocusCopy("订阅文章", "Subscribed article"),
      entry: readerFocusCopy("文章", "Article"),
      obsidian: "Obsidian",
      chat_upload: readerFocusCopy("对话上传", "Chat upload"),
      web_search: readerFocusCopy("网页搜索", "Web search"),
      image: readerFocusCopy("图片", "Image"),
      video: readerFocusCopy("视频", "Video"),
    })[text(sourceKind).toLowerCase()] || text(sourceKind, readerFocusCopy("资料", "Source"));
  }

  function decisionInspectorDefinitionList(rows) {
    const items = rows
      .filter((item) => item && text(item[0]))
      .map(([label, value]) => `<div><dt>${esc(label)}</dt><dd>${esc(text(value) || readerFocusCopy("未记录", "Not recorded"))}</dd></div>`)
      .join("");
    return items ? `<dl class="decision-node-inspector-meta">${items}</dl>` : "";
  }

  function decisionNodeInspectorMarkup(workspace) {
    const node = workspace.history?.nodes.find((item) => text(item.id) === workspace.selectedNodeID);
    if (!node) return "";
    const history = workspace.history || {};
    const revision = selectedDecisionRevision(history);
    const metadata = node.metadata && typeof node.metadata === "object" ? node.metadata : {};
    const citation = decisionNodeCitation(node);
    const actionID = text(metadata.action_id || node.action_id);
    const action = actionID
      ? allFocusResults("action").find((item) => text(item.id) === actionID)
      : null;
    const actionData = action ? focusResultData(action) : {};
    const confirmedEvent = (Array.isArray(history.events) ? history.events : []).find((event) =>
      ["decision_confirmed", "decision_replaced"].includes(text(event?.type)) &&
      (!text(event?.object_id) || text(event.object_id) === text(revision?.id))
    );
    const author = decisionEventActorLabel(node.author);
    const detail = text(citation?.quote) || text(node.body);
    const sourceMeta = citation
      ? decisionInspectorDefinitionList([
          [readerFocusCopy("来源", "Source"), decisionNodeSourceLabel(citation.source_kind)],
          [readerFocusCopy("定位", "Location"), text(citation.locator) || text(citation.source_locator?.label) || text(node.locator)],
        ])
      : "";
    const actionMeta = text(node.type) === "action"
      ? decisionInspectorDefinitionList([
          [readerFocusCopy("期限", "Due"), text(actionData.due_at) || text(metadata.due_at) || readerFocusCopy("未设置", "Not set")],
          [readerFocusCopy("状态", "Status"), ["done", "completed"].includes(text(action?.status || metadata.status).toLowerCase()) || actionData.completed === true ? readerFocusCopy("已完成", "Completed") : readerFocusCopy("待完成", "To do")],
        ])
      : "";
    const decisionMeta = text(node.type) === "decision"
      ? decisionInspectorDefinitionList([
          [readerFocusCopy("确认人", "Confirmed by"), decisionEventActorLabel(confirmedEvent?.actor || node.author || "user")],
          [readerFocusCopy("确认时间", "Confirmed at"), confirmedEvent?.created_at || revision?.confirmed_at ? formatDateByLocale(confirmedEvent?.created_at || revision?.confirmed_at, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) : readerFocusCopy("未记录", "Not recorded")],
          [readerFocusCopy("思考模板", "Thinking model"), text(metadata.model_name) || text(metadata.model_id) || readerFocusCopy("未记录", "Not recorded")],
          [readerFocusCopy("预期结果", "Expected outcome"), text(revision?.expected_outcome) || readerFocusCopy("未记录", "Not recorded")],
          [readerFocusCopy("回顾时间", "Review at"), text(revision?.review_at) || readerFocusCopy("未设置", "Not set")],
        ])
      : "";
    const form = workspace.feedbackMode
      ? `<form class="decision-feedback-form decision-node-inline-editor" data-decision-feedback-form data-feedback-action="${esc(workspace.feedbackMode)}"><label><span>${esc(workspace.feedbackMode === "correction" ? readerFocusCopy("说明哪里有误", "Explain what is incorrect") : readerFocusCopy("补充你的判断", "Add your judgment"))}</span><textarea name="content" rows="4" maxlength="2000" required autofocus></textarea></label><div class="decision-form-message" role="status"></div><div><button class="text-btn" type="button" data-decision-feedback-cancel>${esc(readerFocusCopy("取消", "Cancel"))}</button><button class="primary-btn" type="submit">${esc(readerFocusCopy("保存记录", "Save record"))}</button></div></form>`
      : "";
    return `<aside class="decision-node-inspector" aria-label="${esc(readerFocusCopy("节点详情", "Node details"))}"><div class="decision-node-inspector-head"><span>${icon(decisionNodeInspectorIcon(node))}</span><div><small>${esc(decisionNodeTypeLabel(node.type))} · ${esc(author)}</small><strong>${esc(text(node.title))}</strong></div><button class="icon-btn" type="button" data-decision-node-close aria-label="${esc(readerFocusCopy("关闭节点详情", "Close node details"))}" title="${esc(readerFocusCopy("关闭", "Close"))}">${icon("close")}</button></div>${detail ? `<p class="decision-node-inspector-detail">${esc(detail)}</p>` : ""}${sourceMeta}${actionMeta}${decisionMeta}${citation ? `<button class="decision-node-open-source" type="button" data-decision-node-source="${esc(node.id)}">${esc(readerFocusCopy("查看原文", "View source"))}${icon("external")}</button>` : ""}<div class="decision-node-actions"><button type="button" class="${workspace.feedbackMode === "user_note" ? "is-active" : ""}" data-decision-feedback-mode="user_note">${icon("note")}${esc(readerFocusCopy("补充说明", "Add context"))}</button>${node.status === "active" && node.type !== "correction" ? `<button type="button" class="${workspace.feedbackMode === "correction" ? "is-danger" : ""}" data-decision-feedback-mode="correction">${icon("refresh")}${esc(readerFocusCopy("标记有误", "Mark incorrect"))}</button>` : ""}</div>${form}</aside>`;
  }

  function decisionGraphCyTheme(drawer) {
    const styles = window.getComputedStyle(drawer);
    const color = (name, fallback) => styles.getPropertyValue(name).trim() || fallback;
    return {
      canvas: color("--canvas", "#f7f8f9"),
      surface: color("--surface", "#ffffff"),
      ink: color("--ink", "#202327"),
      muted: color("--muted", "#616970"),
      faint: color("--faint", "#8b9298"),
      line: color("--line-strong", "#c7ccd1"),
      green: color("--green", "#2f8f55"),
      greenSoft: color("--green-soft", "#eef8f2"),
      blue: color("--blue", "#3e6fc5"),
      blueSoft: color("--blue-soft", "#edf2fe"),
      amber: color("--amber", "#9a681d"),
      amberSoft: color("--amber-soft", "#fbf4e7"),
      red: color("--red", "#bd4e50"),
      redSoft: color("--red-soft", "#fdf0f0"),
    };
  }

  function decisionGraphShortText(value, maxLength = 88) {
    const normalized = text(value).replace(/\s+/g, " ").trim();
    return normalized.length > maxLength ? `${normalized.slice(0, maxLength)}…` : normalized;
  }

  function decisionGraphTextUnits(value) {
    return Array.from(text(value)).reduce((total, character) => {
      if (/\s/u.test(character)) return total + 0.35;
      if (/[\u0000-\u024f]/u.test(character)) return total + 0.58;
      return total + 1;
    }, 0);
  }

  function decisionGraphWrapToken(token, maxUnits) {
    const lines = [];
    let current = "";
    let units = 0;
    Array.from(token).forEach((character) => {
      const nextUnits = decisionGraphTextUnits(character);
      if (current && units + nextUnits > maxUnits) {
        lines.push(current);
        current = character;
        units = nextUnits;
      } else {
        current += character;
        units += nextUnits;
      }
    });
    if (current) lines.push(current);
    return lines;
  }

  function decisionGraphWrapLine(value, maxUnits) {
    const tokens = text(value).trim().match(/[A-Za-z0-9]+(?:[._:/+\-][A-Za-z0-9]+)*|[\u3400-\u9fff]|[^\s]/gu) || [];
    const lines = [];
    let current = "";
    let currentUnits = 0;
    let previousWord = false;
    tokens.forEach((token) => {
      const word = /^[A-Za-z0-9]/u.test(token);
      const separator = current && previousWord && word ? " " : "";
      const tokenUnits = decisionGraphTextUnits(token);
      const separatorUnits = decisionGraphTextUnits(separator);
      if (tokenUnits > maxUnits) {
        if (current) lines.push(current);
        const chunks = decisionGraphWrapToken(token, maxUnits);
        lines.push(...chunks.slice(0, -1));
        current = chunks.at(-1) || "";
        currentUnits = decisionGraphTextUnits(current);
        previousWord = word;
        return;
      }
      if (current && currentUnits + separatorUnits + tokenUnits > maxUnits) {
        lines.push(current);
        current = token;
        currentUnits = tokenUnits;
      } else {
        current += separator + token;
        currentUnits += separatorUnits + tokenUnits;
      }
      previousWord = word;
    });
    if (current) lines.push(current);
    return lines.length ? lines : [""];
  }

  function decisionGraphWrapText(value, maxUnits = 28) {
    return text(value)
      .split("\n")
      .flatMap((line) => decisionGraphWrapLine(line, maxUnits))
      .join("\n");
  }

  // 决策图节点文字按真实像素测量折行：measureText 用的字体串与
  // decisionGraphCyStyle 的节点文字样式完全一致（600 + fontSize + Inter 栈），
  // 因此折出的行数与 cytoscape text-wrap: wrap 的实际渲染一致。旧实现按
  // "字符单位 × 8.6px" 估宽——中文按字号 11px 实渲染时普遍比估值宽，画布端
  // 二次折行折出更多行，节点高度却按较少行数算，文字就纵向溢出了方框。
  // 画布 2d context 只建一次、按需复用（有界，单个离屏 canvas）。
  let decisionGraphMeasureContext = null;
  function decisionGraphMeasureWidth(value, fontSize) {
    try {
      decisionGraphMeasureContext ||= document.createElement("canvas").getContext("2d");
      decisionGraphMeasureContext.font = `600 ${fontSize}px Inter, ui-sans-serif, system-ui, sans-serif`;
      return decisionGraphMeasureContext.measureText(value).width;
    } catch {
      return null;
    }
  }

  function decisionGraphFallbackWidth(value, fontSize) {
    return decisionGraphTextUnits(value) * fontSize;
  }

  function decisionGraphWrapTokenPx(token, maxPx, measure) {
    const lines = [];
    let current = "";
    let width = 0;
    Array.from(token).forEach((character) => {
      const characterWidth = measure(character);
      if (current && width + characterWidth > maxPx) {
        lines.push(current);
        current = character;
        width = characterWidth;
      } else {
        current += character;
        width += characterWidth;
      }
    });
    if (current) lines.push(current);
    return lines;
  }

  function decisionGraphWrapLinePx(value, maxPx, measure) {
    const tokens = text(value).trim().match(/[A-Za-z0-9]+(?:[._:/+\-][A-Za-z0-9]+)*|[\u3400-\u9fff]|[^\s]/gu) || [];
    const lines = [];
    let current = "";
    let currentPx = 0;
    let previousWord = false;
    tokens.forEach((token) => {
      const word = /^[A-Za-z0-9]/u.test(token);
      const separator = current && previousWord && word ? " " : "";
      const tokenPx = measure(token);
      if (tokenPx > maxPx) {
        if (current) lines.push(current);
        const chunks = decisionGraphWrapTokenPx(token, maxPx, measure);
        lines.push(...chunks.slice(0, -1));
        current = chunks.at(-1) || "";
        currentPx = measure(current);
        previousWord = word;
        return;
      }
      const separatorPx = measure(separator);
      if (current && currentPx + separatorPx + tokenPx > maxPx) {
        lines.push(current);
        current = token;
        currentPx = tokenPx;
      } else {
        current += separator + token;
        currentPx += separatorPx + tokenPx;
      }
      previousWord = word;
    });
    if (current) lines.push(current);
    return lines.length ? lines : [""];
  }

  function decisionGraphWrapTextPx(value, maxPx, measure) {
    return text(value)
      .split("\n")
      .flatMap((line) => decisionGraphWrapLinePx(line, maxPx, measure))
      .join("\n");
  }

  function decisionGraphRawLabel(node, workspace) {
    if (node?.synthetic === "basis") {
      const projection = decisionGraphProjection(workspace);
      const visible = projection.nodes.filter((item) => !workspace.hiddenTypes?.has?.(decisionGraphNodeCategory(item)));
      const basis = visible.filter((item) => ["evidence", "conflict", "user_answer", "ai_judgment", "user_note", "correction", "gap"].includes(text(item.type)));
      return `${readerFocusCopy("判断依据", "Rationale")}\n${readerFocusCopy(`${basis.length || workspace.history?.stats?.evidence_count || 0} 项依据`, `${basis.length || workspace.history?.stats?.evidence_count || 0} rationale items`)}`;
    }
    if (node?.synthetic === "decision") {
      const revision = selectedDecisionRevision(workspace.history);
      return `${readerFocusCopy("已确认决策", "Confirmed decision")}\n${decisionGraphShortText(text(revision?.content, readerFocusCopy("已确认的决策方案", "Confirmed decision")), 96)}`;
    }
    if (node?.synthetic === "action") {
      const action = workspace.history?.nodes?.find((item) => ["action", "review"].includes(text(item.type)));
      return `${readerFocusCopy("待办", "To-do")}\n${decisionGraphShortText(text(action?.title, readerFocusCopy("由当前决策生成", "Generated by this decision")), 88)}`;
    }
    const type = text(node?.type, "judgment");
    const normalizedTitle = text(node?.title, decisionNodeTypeLabel(type)).replace(/\s+/g, " ").trim();
    const displayedTitle = workspace.expandedGroups.has("all")
      ? normalizedTitle
      : decisionGraphShortText(normalizedTitle, 88);
    return `${decisionNodeTypeLabel(type)}${node?.author === "user" ? ` · ${readerFocusCopy("你", "You")}` : node?.author === "ai" ? " · AI" : ""}\n${displayedTitle}`;
  }

  function decisionGraphNodePresentation(node, workspace) {
    const rawLabel = decisionGraphRawLabel(node, workspace);
    const expanded = workspace.expandedGroups.has("all");
    const fullscreen = expanded && Boolean(workspace.fullscreen);
    const fontSize = fullscreen ? 13 : 11;
    const measure = (value) => decisionGraphMeasureWidth(value, fontSize) ?? decisionGraphFallbackWidth(value, fontSize);
    // 按真实像素折行：wrapPx 始终 ≤ maxWidth - 34，行宽加内边距不会顶到
    // maxWidth 上限，textMaxWidth 又比最宽行多 6px，画布端不会折出更多行。
    const wrapPx = fullscreen ? 396 : expanded ? 336 : 256;
    const label = decisionGraphWrapTextPx(rawLabel, wrapPx, measure);
    const lines = label.split("\n");
    const widestLine = Math.max(...lines.map((line) => measure(line)), 1);
    const type = text(node?.type || node?.synthetic);
    const emphasized = ["decision", "action"].includes(type);
    const minWidth = fullscreen ? (emphasized ? 310 : 280) : emphasized ? 238 : 220;
    const maxWidth = fullscreen ? 460 : expanded ? 380 : 310;
    const width = Math.max(minWidth, Math.min(maxWidth, Math.ceil(widestLine) + 34));
    const minHeight = fullscreen
      ? type === "decision" ? 122 : emphasized ? 108 : 96
      : type === "decision" ? 104 : emphasized ? 92 : 84;
    const lineHeight = fullscreen ? 20 : 17;
    const naturalHeight = 42 + lines.length * lineHeight;
    // 高度跟随实测行数；收起态不再压 148 上限——压上限等于按旧行数截高，
    // 文字照旧从底部溢出。
    const height = expanded
      ? Math.max(minHeight, Math.min(520, naturalHeight))
      : Math.max(minHeight, naturalHeight);
    return {
      label,
      width,
      height,
      // ≥ 最宽实测行 + 6px：cytoscape 的 text-wrap 重排时每行都放得下，
      // 行数与预估一致，文字不再纵向溢出方框。
      textMaxWidth: Math.max(168, width - 28),
      fontSize,
    };
  }

  function decisionGraphLabel(node, workspace) {
    return decisionGraphNodePresentation(node, workspace).label;
  }

  function decisionGraphElements(workspace) {
    const projection = decisionGraphProjection(workspace);
    const hiddenTypes = workspace.hiddenTypes instanceof Set ? workspace.hiddenTypes : new Set();
    const visibleNodes = projection.nodes.filter((node) => !hiddenTypes.has(decisionGraphNodeCategory(node)));
    const expanded = workspace.expandedGroups.has("all");
    if (!expanded) {
      const decision = visibleNodes.find((node) => text(node.type) === "decision") || visibleNodes.find((node) => ["decision", "alternative", "replacement"].includes(text(node.type)));
      const action = visibleNodes.find((node) => text(node.type) === "action") || visibleNodes.find((node) => ["action", "review"].includes(text(node.type)));
      const basis = { id: "__decision_basis__", synthetic: "basis" };
      const decisionNode = { id: "__decision_decision__", synthetic: "decision", sourceNode: decision };
      const actionNode = { id: "__decision_action__", synthetic: "action", sourceNode: action };
      const nodes = [basis, decisionNode, ...(action ? [actionNode] : [])];
      const edges = [{ id: "__decision_basis_edge__", source: basis.id, target: decisionNode.id, relation: "supports", label: readerFocusCopy("支持", "supports") }];
      if (action) edges.push({ id: "__decision_action_edge__", source: decisionNode.id, target: actionNode.id, relation: "leads_to", label: readerFocusCopy("生成", "leads to") });
      return { projection, nodes, edges, expanded: false };
    }
    const nodeIDs = new Set(visibleNodes.map((node) => text(node.id)));
    const edges = projection.edges.filter((edge) => nodeIDs.has(text(edge.source)) && nodeIDs.has(text(edge.target)));
    return { projection, nodes: visibleNodes, edges, expanded: true };
  }

  function decisionGraphColumnPositions(nodes, x, presentations, positions, gap = 38) {
    const heights = nodes.map((node) => presentations.get(text(node.id))?.height || 104);
    const totalHeight = heights.reduce((total, height) => total + height, 0) + Math.max(0, nodes.length - 1) * gap;
    let cursor = -totalHeight / 2;
    nodes.forEach((node, index) => {
      const height = heights[index];
      positions.set(text(node.id), { x, y: cursor + height / 2 });
      cursor += height + gap;
    });
  }

  function decisionGraphChunkColumn(nodes, maxRows = 6) {
    if (!nodes.length) return [];
    const columns = [];
    for (let index = 0; index < nodes.length; index += maxRows)
      columns.push(nodes.slice(index, index + maxRows));
    return columns;
  }

  // 语义分层：依据 → 待验证/备选 → AI 判断 → 决策 → 待办。旧实现把
  // ai_judgment 和 gap/alternative 混进同一"分析"列再按行数切块，6 个节点
  // 被切成 5+1 两列，AI 判断和单个备选被挤进同一列，边的跨度全乱了。
  // 未知类型与 gap/alternative 同层（沿用旧实现的归类倾向）。
  function decisionGraphLayerCategory(node) {
    const type = text(node.type);
    if (["evidence", "conflict", "user_answer", "user_note", "correction"].includes(type)) return 0;
    if (["gap", "alternative"].includes(type)) return 1;
    if (type === "ai_judgment") return 2;
    if (["decision", "replacement"].includes(type)) return 3;
    if (["action", "review"].includes(type)) return 4;
    return 1;
  }

  // 列内重心排序（Sugiyama 式 barycenter，两轮扫掠）：节点按"邻列邻居的
  // 平均行号"重排，压低左右穿线的交叉数。同分按 id 排序保证确定性。
  function decisionGraphBarycenterOrder(layers, edges) {
    const rowOf = new Map();
    const refreshRows = () => layers.forEach((layer) => layer.forEach((node, row) => rowOf.set(text(node.id), row)));
    refreshRows();
    const neighborRows = new Map();
    edges.forEach((edge) => {
      const source = text(edge.source);
      const target = text(edge.target);
      if (!rowOf.has(source) || !rowOf.has(target)) return;
      if (!neighborRows.has(source)) neighborRows.set(source, []);
      if (!neighborRows.has(target)) neighborRows.set(target, []);
      neighborRows.get(source).push(target);
      neighborRows.get(target).push(source);
    });
    const barycenter = (id) => {
      const rows = (neighborRows.get(id) || []).map((other) => rowOf.get(other)).filter((row) => Number.isFinite(row));
      return rows.length ? rows.reduce((total, row) => total + row, 0) / rows.length : rowOf.get(id);
    };
    for (let sweep = 0; sweep < 2; sweep += 1) {
      for (const layer of layers) {
        layer.sort((left, right) => (barycenter(text(left.id)) - barycenter(text(right.id))) || text(left.id).localeCompare(text(right.id)));
        layer.forEach((node, row) => rowOf.set(text(node.id), row));
      }
    }
  }

  // 列序优化:语义分层只负责分组,不锁死先后。固定分组序会把大量边变
  // 成跨列长边(本图:依据→AI判断 5 条全跨过待验证/备选列)。在保持
  // 有直接边的层相对次序不变(绝不产生反向边)的前提下做相邻交换,
  // 压低 Σ 边数×跨列数——大扇入落回相邻列走 taxi,绕行弧只剩零星长边。
  function decisionGraphOptimizeColumnOrder(layers, edges) {
    const present = layers.map((layer, index) => (layer.length ? index : -1)).filter((index) => index >= 0);
    if (present.length < 3) return present;
    const layerOf = new Map();
    layers.forEach((layer, index) => layer.forEach((node) => layerOf.set(text(node.id), index)));
    const weights = new Map();
    edges.forEach((edge) => {
      const from = layerOf.get(text(edge.source));
      const to = layerOf.get(text(edge.target));
      if (!Number.isFinite(from) || !Number.isFinite(to) || from === to) return;
      const key = `${Math.min(from, to)}>${Math.max(from, to)}`;
      weights.set(key, (weights.get(key) || 0) + 1);
    });
    if (!weights.size) return present;
    const connected = (a, b) => weights.has(`${Math.min(a, b)}>${Math.max(a, b)}`);
    const cost = (order) => {
      const at = new Map();
      order.forEach((layer, index) => at.set(layer, index));
      let total = 0;
      weights.forEach((count, key) => {
        const [from, to] = key.split(">").map(Number);
        total += count * Math.abs(at.get(to) - at.get(from));
      });
      return total;
    };
    let best = cost(present);
    for (let pass = 0; pass < present.length * present.length; pass += 1) {
      let swapped = false;
      for (let index = 0; index + 1 < present.length; index += 1) {
        const a = present[index];
        const b = present[index + 1];
        if (connected(a, b)) continue;
        present[index] = b;
        present[index + 1] = a;
        const next = cost(present);
        if (next < best) {
          best = next;
          swapped = true;
        } else {
          present[index] = a;
          present[index + 1] = b;
        }
      }
      if (!swapped) break;
    }
    return present;
  }

  function decisionGraphLayeredLayout(graph, presentations, workspace) {
    const positions = new Map();
    if (!graph.expanded) {
      const widths = graph.nodes.map((node) => presentations.get(text(node.id))?.width || 224);
      const totalWidth = widths.reduce((total, width) => total + width, 0) + Math.max(0, widths.length - 1) * 150;
      let cursor = -totalWidth / 2;
      graph.nodes.forEach((node, index) => {
        const width = widths[index];
        positions.set(text(node.id), { x: cursor + width / 2, y: 0 });
        cursor += width + 150;
      });
      return { positions, columnIndex: new Map(), columns: [] };
    }
    const layers = [[], [], [], [], []];
    graph.nodes.forEach((node) => layers[decisionGraphLayerCategory(node)].push(node));
    // 先整层重心排序、再按行数切块：相关节点在序上相邻，不会被切块拆散。
    decisionGraphBarycenterOrder(layers, graph.edges);
    const fullscreen = Boolean(workspace?.fullscreen);
    const maxRows = fullscreen ? 9 : 6;
    const order = decisionGraphOptimizeColumnOrder(layers, graph.edges);
    const columns = order.flatMap((layerIndex) => decisionGraphChunkColumn(layers[layerIndex], maxRows)).filter((column) => column.length);
    const widths = columns.map((column) => Math.max(...column.map((node) => presentations.get(text(node.id))?.width || 224)));
    const columnGap = fullscreen ? 135 : 138;
    const totalWidth = widths.reduce((total, width) => total + width, 0) + Math.max(0, widths.length - 1) * columnGap;
    let cursor = -totalWidth / 2;
    const columnRects = [];
    columns.forEach((column, index) => {
      const width = widths[index];
      const centerX = cursor + width / 2;
      decisionGraphColumnPositions(column, centerX, presentations, positions, fullscreen ? 30 : 38);
      let top = Infinity;
      let bottom = -Infinity;
      column.forEach((node) => {
        const position = positions.get(text(node.id));
        const presentation = presentations.get(text(node.id));
        top = Math.min(top, position.y - (presentation?.height || 104) / 2);
        bottom = Math.max(bottom, position.y + (presentation?.height || 104) / 2);
      });
      columnRects.push({ left: cursor, right: cursor + width, top, bottom });
      cursor += width + columnGap;
    });
    const columnIndex = new Map();
    columns.forEach((column, index) => column.forEach((node) => columnIndex.set(text(node.id), index)));
    return { positions, columnIndex, columns: columnRects };
  }

  // 边路由：相邻列之间的边走 taxi 直角线，转弯竖段按边序在列间通道里
  // 错开；跨列（越过中间列）的边改走 unbundled-bezier，从中间列上/下方
  // 整体绕过，不再横穿节点方框。旧实现所有边都固定在源右侧 46px 转弯，
  // 长边一条条贴着穿过整列节点——这是"线条太乱"的主要来源。
  // bezier 控制点距离的正方向为连线法向（右向连线即屏幕向下）。cytoscape
  // 把 2 个控制点画成两段二次曲线、精确经过两控制点的中点：把每个控制
  // 点直接压到峰值高度（距离不缩放），曲线就单调爬到峰值再落下，不会
  // 像立方近似那样数百像素过冲。
  function decisionGraphEdgeRoutes(graph, layout) {
    const routes = new Map();
    if (!layout.columns.length) return routes;
    const { positions, columnIndex, columns } = layout;
    const adjacent = new Map();
    const bypass = new Map();
    graph.edges.forEach((edge) => {
      const source = positions.get(text(edge.source));
      const target = positions.get(text(edge.target));
      const from = columnIndex.get(text(edge.source));
      const to = columnIndex.get(text(edge.target));
      if (!source || !target || !Number.isFinite(from) || !Number.isFinite(to)) return;
      if (to <= from) return; // 反向/同列边保持默认渲染，不硬套左右流路由
      const pair = `${from}>${to}`;
      if (to - from === 1) {
        if (!adjacent.has(pair)) adjacent.set(pair, []);
        adjacent.get(pair).push(edge);
      } else {
        if (!bypass.has(pair)) bypass.set(pair, []);
        bypass.get(pair).push(edge);
      }
    });
    adjacent.forEach((edges, pair) => {
      const [from, to] = pair.split(">").map(Number);
      const gutterStart = columns[from].right + 14;
      const gutterWidth = Math.max(28, columns[to].left - columns[from].right - 28);
      edges
        .sort((left, right) => (positions.get(text(left.source)).y - positions.get(text(right.source)).y) || text(left.id).localeCompare(text(right.id)))
        .forEach((edge, index) => {
          const source = positions.get(text(edge.source));
          const turn = Math.round(gutterStart + gutterWidth * ((index + 0.5) / edges.length) - source.x);
          routes.set(text(edge.id), { "curve-style": "taxi", "taxi-turn": Math.max(18, turn) });
        });
    });
    Array.from(bypass.keys())
      .sort((left, right) => {
        const [fromA, toA] = left.split(">").map(Number);
        const [fromB, toB] = right.split(">").map(Number);
        return fromA - fromB || toA - toB;
      })
      .forEach((pair, pairIndex) => {
      const edges = bypass.get(pair);
      const [from, to] = pair.split(">").map(Number);
      // 多组跨列边按组序上下交替分侧：若都按源/目标均值选边（本图两组均
      // 判为上），两组弧会挤进同一片顶部区域互相穿插。奇偶交替保证最多
      // 只有一半绕行组共享同一侧。
      const upward = pairIndex % 2 === 0;
      let boundary = upward ? Infinity : -Infinity;
      for (let column = from + 1; column < to; column += 1) {
        boundary = upward ? Math.min(boundary, columns[column].top) : Math.max(boundary, columns[column].bottom);
      }
      if (!Number.isFinite(boundary)) return;
      edges
        .sort((left, right) => (positions.get(text(left.source)).y - positions.get(text(right.source)).y) || text(left.id).localeCompare(text(right.id)))
        .forEach((edge, index) => {
          // 弧顶嵌套：靠边一侧的源节点占最外侧弧，其余依次内收，弧与弧不相交。
          const stagger = upward ? edges.length - 1 - index : index;
          const peak = upward ? boundary - 36 - stagger * 26 : boundary + 36 + stagger * 26;
          const source = positions.get(text(edge.source));
          const target = positions.get(text(edge.target));
          const span = target.x - source.x || 1;
          const clamp = (value, low, high) => Math.min(high, Math.max(low, value));
          const weights = [
            clamp((columns[from + 1].left - source.x) / span - 0.06, 0.15, 0.8),
            clamp((columns[to - 1].right - source.x) / span + 0.06, 0.25, 0.9),
          ];
          if (weights[1] - weights[0] < 0.12) weights[1] = Math.min(0.9, weights[0] + 0.12);
          const distances = weights.map((weight) => {
            const lineY = source.y + (target.y - source.y) * weight;
            return Math.round(peak - lineY);
          });
          routes.set(text(edge.id), {
            "curve-style": "unbundled-bezier",
            "control-point-weights": weights.map((weight) => Math.round(weight * 100) / 100).join(" "),
            "control-point-distances": distances.join(" "),
          });
        });
    });
    return routes;
  }

  function applyDecisionGraphEdgeRoutes(cy, routes) {
    if (!routes?.size) return;
    cy.edges().forEach((edge) => {
      const route = routes.get(text(edge.id()));
      if (route) edge.style(route);
    });
  }

  function decisionGraphCyStyle(drawer) {
    const theme = decisionGraphCyTheme(drawer);
    return [
      { selector: "node", style: { "background-color": theme.surface, "border-color": theme.line, "border-width": 1, "shape": "roundrectangle", "width": "data(width)", "height": "data(height)", "label": "data(label)", "color": theme.ink, "font-family": "Inter, ui-sans-serif, system-ui, sans-serif", "font-size": "data(fontSize)", "font-weight": 600, "line-height": 1.35, "text-wrap": "wrap", "text-max-width": "data(textMaxWidth)", "text-valign": "center", "text-halign": "center", "text-justification": "center", "padding": 12, "overlay-opacity": 0, "text-outline-width": 0 } },
      { selector: "node.synthetic-basis", style: { "background-color": theme.surface, "border-style": "dashed", "border-color": theme.line, "color": theme.muted } },
      { selector: "node.synthetic-decision", style: { "background-color": theme.greenSoft, "border-color": theme.green, "border-width": 2, "color": theme.ink } },
      { selector: "node.synthetic-action", style: { "background-color": theme.blueSoft, "border-color": theme.blue, "color": theme.ink } },
      { selector: "node.is-evidence, node.is-conflict", style: { "background-color": theme.surface, "border-color": theme.line } },
      { selector: "node.is-answer", style: { "background-color": theme.blueSoft, "border-color": theme.blue } },
      { selector: "node.is-gap", style: { "background-color": theme.amberSoft, "border-color": theme.amber, "border-style": "dashed" } },
      { selector: "node.is-decision", style: { "background-color": theme.greenSoft, "border-color": theme.green, "border-width": 2 } },
      { selector: "node.is-action", style: { "background-color": theme.blueSoft, "border-color": theme.blue } },
      { selector: "node.is-alternative, node.is-replacement", style: { "background-color": theme.redSoft, "border-color": theme.red } },
      { selector: "node.is-selected", style: { "border-color": theme.blue, "border-width": 3, "underlay-color": theme.blue, "underlay-opacity": 0.14, "underlay-padding": 5 } },
      { selector: "node.is-compare-added", style: { "border-color": theme.green, "border-width": 3 } },
      { selector: "node.is-compare-changed", style: { "border-color": theme.amber, "border-width": 3 } },
      { selector: "node.is-compare-removed", style: { "border-color": theme.red, "border-style": "dashed", "opacity": 0.55 } },
      { selector: "edge", style: { "curve-style": "taxi", "taxi-direction": "rightward", "taxi-turn": 46, "taxi-turn-min-distance": 18, "line-color": theme.line, "target-arrow-color": theme.line, "target-arrow-shape": "triangle", "width": 1.8, "label": "data(label)", "color": theme.faint, "font-size": 8, "font-weight": 600, "text-background-color": theme.canvas, "text-background-opacity": 0.94, "text-background-padding": 3, "text-rotation": "none", "text-margin-y": -6, "overlay-opacity": 0 } },
      { selector: "edge.is-supports", style: { "line-color": theme.green, "target-arrow-color": theme.green, "width": 2.2, "color": theme.green } },
      { selector: "edge.is-challenges, edge.is-conflicts", style: { "line-color": theme.red, "target-arrow-color": theme.red, "line-style": "dashed", "color": theme.red } },
      { selector: "edge.is-leads_to", style: { "line-color": theme.blue, "target-arrow-color": theme.blue, "color": theme.blue } },
      { selector: "edge.is-compare-added", style: { "line-color": theme.green, "target-arrow-color": theme.green, "width": 2.5 } },
      { selector: "edge.is-compare-changed", style: { "line-color": theme.amber, "target-arrow-color": theme.amber, "width": 2.5 } },
      { selector: "edge.is-compare-removed", style: { "line-color": theme.red, "target-arrow-color": theme.red, "line-style": "dashed", "opacity": 0.6 } },
    ];
  }

  function syncDecisionGraphElements(cy, elements, positions) {
    // 存活实例上整批换元素：单次 batch 内先清后加再按 layout 档位定位，不销毁
    // renderer、不重建画布——展开推理链/换版本/改筛选时图内容一帧内完成切换。
    // 契约：positions 必须覆盖全部节点 id，缺失的落 (0,0)。
    cy.batch(() => {
      cy.elements().remove();
      cy.add(elements);
      elements.forEach((element) => {
        if (element.group !== "nodes") return;
        const node = cy.getElementById(element.data.id);
        if (node.nonempty()) node.position(positions.get(element.data.id) || { x: 0, y: 0 });
      });
    });
  }

  function initializeDecisionGraph(drawer, workspace) {
    const container = drawer?.querySelector("[data-decision-graph-cytoscape]");
    if (!container || typeof window.cytoscape !== "function") return null;
    const graph = decisionGraphElements(workspace);
    const presentations = new Map(
      graph.nodes.map((node) => [
        text(node.id),
        decisionGraphNodePresentation(node.synthetic ? node : node.sourceNode || node, workspace),
      ])
    );
    const layout = decisionGraphLayeredLayout(graph, presentations, workspace);
    const positions = layout.positions;
    const nodeStates = graph.projection.nodeStates;
    const edgeStates = graph.projection.edgeStates;
    const elements = [
      ...graph.nodes.map((node) => {
        const sourceNode = node.sourceNode || node;
        const type = text(sourceNode.type, node.synthetic || "judgment").replace(/[^a-z_]/g, "");
        const classes = [node.synthetic ? `synthetic-${node.synthetic}` : `is-${decisionGraphNodeCategory(sourceNode)}`, `is-${type}`];
        const comparisonState = node.synthetic ? "" : text(nodeStates.get(text(node.id)));
        if (text(node.id) === text(workspace.selectedNodeID)) classes.push("is-selected");
        if (comparisonState) classes.push(`is-compare-${comparisonState}`);
        const presentation = presentations.get(text(node.id)) || decisionGraphNodePresentation(node.synthetic ? node : sourceNode, workspace);
        return { group: "nodes", data: { id: text(node.id), label: presentation.label, width: presentation.width, height: presentation.height, textMaxWidth: presentation.textMaxWidth, fontSize: presentation.fontSize, type, sourceNodeID: text(sourceNode.id), synthetic: node.synthetic || "" }, classes: classes.join(" ") };
      }),
      ...graph.edges.map((edge) => {
        const relation = text(edge.relation, "supports").replace(/[^a-z_]/g, "");
        const comparisonState = graph.expanded ? text(edgeStates.get(text(edge.id))) : "";
        const classes = [`is-${relation}`];
        if (comparisonState) classes.push(`is-compare-${comparisonState}`);
        return { group: "edges", data: { id: text(edge.id, `${text(edge.source)}__${text(edge.target)}`), source: text(edge.source), target: text(edge.target), label: [text(edge.label), decisionComparisonStateLabel(comparisonState, true)].filter(Boolean).join(" · ") }, classes: classes.join(" ") };
      }),
    ];
    // 形状指纹（节点/边/尺寸/布局档位，刻意不含选中态）：一致说明这次渲染
    // 只变了选中、busy/error 横幅等外围状态。此时把存活实例挂回（可能被
    // innerHTML 重建过的）容器并同步选中类即可，避免整图销毁重建的闪屏。
    const shapeSignature = JSON.stringify([
      graph.expanded ? 1 : 0,
      workspace.fullscreen ? 1 : 0,
      positions.size,
      ...elements.map((element) => [
        element.data.id,
        element.classes.replace(/(^| )is-selected( |$)/g, " ").trim(),
        element.data.label,
        element.data.width || 0,
        element.data.height || 0,
        element.data.source || "",
        element.data.target || "",
      ]),
    ]);
    // 容器未换（渲染方保留了画布子树）时不要 mount：捆绑版 cytoscape 的
    // mount() 无条件销毁 renderer、重建空白画布并整体重排样式表——"复用"
    // 路径自己就是一次闪屏源。只有容器真的换了（loading/版本切换重建整棵
    // 树）才把存活实例挂回。
    const existing = drawer.__decisionGraphCy;
    const sameContainer = Boolean(existing && existing.container() === container);
    if (existing && !sameContainer) existing.mount(container);
    const cy = existing || window.cytoscape({
      container,
      elements,
      style: decisionGraphCyStyle(drawer),
      layout: { name: "null" },
      autolock: false,
      autoungrabify: true,
      autounselectify: false,
      boxSelectionEnabled: false,
      userPanningEnabled: true,
      userZoomingEnabled: false,
      minZoom: 0.25,
      maxZoom: 2.5,
      pixelRatio: "auto",
    });
    if (!existing) {
      cy.on("tap", "node", (event) => {
        const node = event.target;
        if (node.data("synthetic") === "basis") {
          workspace.expandedGroups.add("all");
          workspace.graphFitPending = true;
          return renderDecisionHistoryDrawer(drawer, workspace);
        }
        const sourceNodeID = text(node.data("sourceNodeID"));
        if (!sourceNodeID) return;
        workspace.selectedNodeID = sourceNodeID;
        workspace.feedbackMode = "";
        workspace.graphFitPending = false;
        renderDecisionHistoryDrawer(drawer, workspace);
      });
      cy.layout({ name: "preset", fit: false, positions: (node) => positions.get(node.id()) || { x: 0, y: 0 } }).run();
      // 用户拖拽/缩放实时回写 workspace：后续任何兜底还原（!viewportPreserved
      // 分支）拿到的都是新鲜视口，不会把画面拽回陈旧位置。
      cy.on("pan zoom", () => {
        workspace.zoom = cy.zoom();
        workspace.graphPan = cy.pan();
      });
      applyDecisionGraphEdgeRoutes(cy, decisionGraphEdgeRoutes(graph, layout));
    } else {
      if (!sameContainer) cy.resize();
      if (drawer.__decisionGraphSignature !== shapeSignature) {
        syncDecisionGraphElements(cy, elements, positions);
        applyDecisionGraphEdgeRoutes(cy, decisionGraphEdgeRoutes(graph, layout));
      } else {
        // 指纹一致：这次只是选中态/横幅等外围状态变化，同步选中类即可。
        const selectedID = text(workspace.selectedNodeID);
        cy.batch(() => {
          cy.nodes().removeClass("is-selected");
          if (selectedID) {
            cy.nodes().filter((node) => text(node.data("sourceNodeID")) === selectedID || text(node.id()) === selectedID).addClass("is-selected");
          }
        });
      }
    }
    drawer.__decisionGraphCy = cy;
    drawer.__decisionGraphSignature = shapeSignature;
    return cy;
  }

  function applyDecisionGraphZoom(drawer, workspace, nextZoom, clientX = null, clientY = null) {
    const cy = drawer?.__decisionGraphCy;
    const container = drawer?.querySelector("[data-decision-graph-cytoscape]");
    if (!cy || !container) return;
    const currentZoom = cy.zoom();
    const zoom = Math.max(0.25, Math.min(2.5, Number(nextZoom) || currentZoom));
    if (zoom === currentZoom) return;
    const rect = container.getBoundingClientRect();
    const renderedPosition = {
      x: clientX === null ? rect.width / 2 : clientX - rect.left,
      y: clientY === null ? rect.height / 2 : clientY - rect.top,
    };
    cy.zoom({ level: zoom, renderedPosition });
    workspace.zoom = zoom;
    workspace.graphPan = cy.pan();
  }

  function observeDecisionGraphSize(drawer, workspace) {
    if (!window.ResizeObserver || drawer.__decisionGraphResizeObserver) return;
    drawer.__decisionGraphResizeObserver = new ResizeObserver(() => {
      const cy = drawer.__decisionGraphCy;
      if (!cy || !drawer.isConnected || workspace.view !== "graph") return;
      cy.resize();
    });
    drawer.__decisionGraphResizeObserver.observe(drawer);
  }

  function fitDecisionGraph(drawer, workspace) {
    const cy = drawer?.__decisionGraphCy;
    if (!cy) return;
    cy.resize();
    // 只按节点包围盒 fit：绕行弧的控制点会把边包围盒上下撑大几百像素，
    // 把 fit 缩放压到最小可读缩放以下，钳制后左右两端的列反而被裁出画布。
    cy.fit(cy.nodes(), workspace.fullscreen ? 64 : 40);
    // Preserve a readable card size after the reasoning chain is expanded.
    // The graph can extend beyond the viewport and remains reachable by
    // panning; the fit control is still available when an overview is needed.
    const expanded = workspace.expandedGroups.has("all");
    const minimumReadableZoom = expanded ? (workspace.fullscreen ? 0.6 : 0.56) : 0.48;
    if (cy.zoom() < minimumReadableZoom) {
      const container = drawer.querySelector("[data-decision-graph-cytoscape]");
      const rect = container?.getBoundingClientRect();
      cy.zoom({ level: minimumReadableZoom, renderedPosition: { x: (rect?.width || 0) / 2, y: (rect?.height || 0) / 2 } });
    }
    workspace.zoom = cy.zoom();
    workspace.graphPan = cy.pan();
    workspace.graphFitPending = false;
  }

  function renderDecisionHistoryDrawer(drawer, workspace) {
    if (!drawer?.isConnected) return;
    // 不再开头就销毁 cytoscape 实例：选中节点等只改状态的渲染若重建画布，
    // 整图会先清空再重画（大图上就是用户看到的整屏闪）。是否重建由
    // initializeDecisionGraph 的形状指纹决定；loading/empty 分支替换
    // innerHTML 后实例仍可挂回新容器。
    drawer.classList.toggle("is-fullscreen", workspace.fullscreen);
    if (workspace.loading) {
      drawer.innerHTML = `<div class="focus-citation-head"><div><span class="focus-citation-kicker">${esc(readerFocusCopy("决策依据", "Decision rationale"))}</span><h2>${esc(workspace.title)}</h2></div><button class="icon-btn" type="button" data-close-decision-history>${icon("close")}</button></div><div class="decision-history-loading" role="status" aria-busy="true"><span class="timeline-loading-spinner"></span>${esc(readerFocusCopy("正在读取已保存的决策关系…", "Loading saved decision relationships…"))}</div>`;
      return;
    }
    if (!workspace.history) {
      drawer.innerHTML = `<div class="focus-citation-head"><div><span class="focus-citation-kicker">${esc(readerFocusCopy("决策依据", "Decision rationale"))}</span><h2>${esc(workspace.title)}</h2></div><button class="icon-btn" type="button" data-close-decision-history>${icon("close")}</button></div><div class="decision-history-empty"><strong>${esc(readerFocusCopy("决策依据暂时不可用", "Decision rationale is unavailable"))}</strong><p>${esc(workspace.error || readerFocusCopy("现有决策内容仍然保留。", "The existing decision remains available."))}</p><div class="decision-history-empty-actions"><button class="text-btn" type="button" data-decision-history-fallback>${icon("decision")}${esc(readerFocusCopy("查看基础依据", "View basic rationale"))}</button><button class="text-btn" type="button" data-decision-history-retry>${icon("refresh")}${esc(readerFocusCopy("重新读取", "Retry"))}</button></div></div>`;
      return;
    }
    const history = workspace.history;
    const revision = selectedDecisionRevision(history);
    const currentView = workspace.view || (history.stats.is_complex ? "graph" : "overview");
    workspace.view = currentView;
    parity.decisionHistoryViews.set(decisionHistoryCacheKey(workspace.focusID, workspace.decisionID), currentView);
    const body = currentView === "graph"
      ? decisionHistoryGraphMarkup(workspace)
      : currentView === "timeline"
        ? decisionHistoryTimelineMarkup(history)
        : decisionHistoryOverviewMarkup(history);
    const versions = history.revisions.map((item) => {
      const status = item.status === "confirmed"
        ? readerFocusCopy("已确认", "Confirmed")
        : item.status === "superseded"
          ? readerFocusCopy("已替代", "Superseded")
          : text(item.status);
      return `<option value="${esc(item.id)}"${text(item.id) === history.selectedID ? " selected" : ""}>v${Number(item.revision_no) || 1} · ${esc(status)}</option>`;
    }).join("");
    const tabMeta = `<div class="decision-workspace-tab-meta"><span>${readerFocusNumber(history.stats.evidence_count)} ${esc(readerFocusCopy("条资料", "sources"))}</span>${history.stats.conflict_count ? `<span>${readerFocusNumber(history.stats.conflict_count)} ${esc(readerFocusCopy("条冲突", "conflicts"))}</span>` : ""}${history.stats.gap_count ? `<span>${readerFocusNumber(history.stats.gap_count)} ${esc(readerFocusCopy("项待验证", "gaps"))}</span>` : ""}<span>${readerFocusNumber(history.stats.action_count)} ${esc(readerFocusCopy("个行动", "actions"))}</span></div>`;
    const graphLegend = `<footer class="decision-workspace-foot decision-graph-legend"><span><i class="is-support"></i>${esc(readerFocusCopy("支持结论", "Supports"))}</span>${history.stats.conflict_count ? `<span><i class="is-challenge"></i>${esc(readerFocusCopy("挑战结论", "Challenges"))}</span>` : ""}<span><i class="is-answer"></i>${esc(readerFocusCopy("你的回答", "Your answer"))}</span><span><i class="is-decision"></i>${esc(readerFocusCopy("已确认结论", "Confirmed decision"))}</span><small>${esc(readerFocusCopy("点击节点查看原文和定位", "Select a node to view its source and location"))}</small></footer>`;
    // 图视图下先取走存活的 [data-decision-graph-scroll] 子树（cytoscape 画布
    // DOM 挂在里面），innerHTML 直写后再把新树里的占位换回它：画布全程不离
    // 文档，状态类渲染（选中/横幅/筛选）不再空帧。工具栏/横幅在 scroll 之外，
    // 仍随整树更新。
    const preservedGraphScroll = currentView === "graph" ? drawer.querySelector("[data-decision-graph-scroll]") : null;
    drawer.innerHTML = `<div class="decision-workspace-head"><div class="decision-workspace-identity"><span class="decision-workspace-brand">${icon("decision")}</span><div><span class="decision-workspace-kicker">${esc(readerFocusCopy("为什么这样决定", "Why this decision"))}<i>·</i><strong>${esc(workspace.focusTitle)}</strong></span><h2>${esc(text(revision?.content, workspace.title))}</h2></div></div><div class="decision-workspace-head-actions"><label class="decision-version-select"><span>${esc(readerFocusCopy("版本", "Version"))}</span><select data-decision-revision-select aria-label="${esc(readerFocusCopy("选择决策版本", "Select decision version"))}">${versions}</select></label><button class="icon-btn" type="button" data-close-decision-history aria-label="${esc(readerFocusCopy("关闭", "Close"))}">${icon("close")}</button></div></div><div class="decision-workspace-tabbar"><div class="decision-workspace-tabs" role="tablist"><button type="button" role="tab" data-decision-view="overview" aria-selected="${currentView === "overview"}">${esc(readerFocusCopy("为什么这样决定", "Why this decision"))}</button><button type="button" role="tab" data-decision-view="graph" aria-selected="${currentView === "graph"}">${icon("decision")}${esc(readerFocusCopy("关系视图", "Relationships"))}</button><button type="button" role="tab" data-decision-view="timeline" aria-selected="${currentView === "timeline"}">${esc(readerFocusCopy("决策历程", "Decision history"))}</button></div>${tabMeta}</div><div class="decision-workspace-layout${currentView === "graph" ? " is-graph" : ""}"><main class="decision-workspace-body${currentView === "graph" ? " is-graph" : ""}">${workspace.busy ? `<div class="decision-workspace-busy" aria-live="polite">${esc(readerFocusCopy("正在保存…", "Saving…"))}</div>` : ""}${workspace.error ? `<div class="decision-workspace-error" role="alert">${esc(workspace.error)}</div>` : ""}${body}</main>${decisionNodeInspectorMarkup(workspace)}</div>${currentView === "graph" ? graphLegend : `<footer class="decision-workspace-foot"><span>${esc(`${history.stats.evidence_count} ${readerFocusCopy("条资料", "sources")} · ${history.stats.action_count} ${readerFocusCopy("个行动", "actions")}`)}</span><span>${esc(readerFocusCopy("修改会形成新记录", "Changes create a new record"))}</span></footer>`}`;
    const freshScroll = drawer.querySelector("[data-decision-graph-scroll]");
    if (preservedGraphScroll && freshScroll) freshScroll.replaceWith(preservedGraphScroll);
    if (currentView === "graph") {
      observeDecisionGraphSize(drawer, workspace);
      const shouldFit = workspace.graphFitPending !== false;
      workspace.graphFitPending = false;
      // 画布子树已被保留时，实例与视口都还是活的：不还原陈旧 zoom/pan（用户
      // 拖移后的下一次状态渲染会把视口拽回去），只在重建/换容器时恢复视口。
      const viewportPreserved = Boolean(preservedGraphScroll);
      // 赋值给 closeCitationDrawer 已在取消的句柄 + isConnected 守卫：渲染后
      // 同一帧内关抽屉时，pending 回调不得在游离 drawer 上重建 cytoscape 实例
      // （孤儿 rAF 渲染循环 + 游离 DOM 子树永不释放）。
      drawer.__decisionGraphResizeFrame = window.requestAnimationFrame(() => {
        if (!drawer.isConnected) return;
        const cy = initializeDecisionGraph(drawer, workspace);
        if (!cy) return;
        if (shouldFit) fitDecisionGraph(drawer, workspace);
        else if (!viewportPreserved) {
          cy.resize();
          if (workspace.graphPan) cy.pan(workspace.graphPan);
          if (Number.isFinite(Number(workspace.zoom))) cy.zoom(Number(workspace.zoom));
        }
      });
    }
  }

  async function loadDecisionHistoryDrawer(drawer, workspace, revisionID = "") {
    workspace.loading = true;
    workspace.error = "";
    renderDecisionHistoryDrawer(drawer, workspace);
    try {
      const history = await requestDecisionHistory(workspace.focusID, workspace.decisionID, revisionID);
      if (!history?.revisions?.length)
        throw new Error(readerFocusCopy("这个旧决策还没有关系记录，可先查看基础依据。", "This legacy decision has no relationship history; basic rationale is still available."));
      workspace.history = history;
      workspace.selectedNodeID = "";
      workspace.comparison = null;
      parity.decisionHistories.set(decisionHistoryCacheKey(workspace.focusID, workspace.decisionID), history);
      if (!workspace.view)
        workspace.view = parity.decisionHistoryViews.get(decisionHistoryCacheKey(workspace.focusID, workspace.decisionID)) || (history.stats.is_complex ? "graph" : "overview");
    } catch (error) {
      workspace.history = null;
      workspace.error = error?.message || readerFocusCopy("读取失败", "Could not load");
    } finally {
      workspace.loading = false;
      workspace.graphFitPending = true;
      renderDecisionHistoryDrawer(drawer, workspace);
    }
  }

  function openDecisionHistoryDrawer(item) {
    const focusID = text(item?.focus_id || activeFocusSpaceProjection(state.dashboard || {})?.id);
    const decisionID = text(item?.id);
    if (!focusID || !decisionID) return null;
    const data = focusResultData(item);
    const activeFocus = activeFocusSpaceProjection(state.dashboard || {});
    const drawer = document.createElement("aside");
    drawer.className = "focus-citation-drawer decision-workspace-drawer";
    drawer.dataset.focusCitationDrawer = "1";
    drawer.setAttribute("role", "dialog");
    drawer.setAttribute("aria-label", readerFocusCopy("查看决策依据", "View decision rationale"));
    const workspace = {
      focusID,
      decisionID,
      title: text(data.recommendation || data.question, readerFocusCopy("决策依据", "Decision rationale")),
      focusTitle: text(item?.focus_title, activeFocus?.title, readerFocusCopy("当前焦点", "Current focus")),
      item,
      history: parity.decisionHistories.get(decisionHistoryCacheKey(focusID, decisionID)) || null,
      view: parity.decisionHistoryViews.get(decisionHistoryCacheKey(focusID, decisionID)) || "",
      loading: true,
      busy: false,
      fullscreen: false,
      selectedNodeID: "",
      feedbackMode: "",
      expandedGroups: new Set(),
      hiddenTypes: new Set(),
      filtersOpen: false,
      zoom: 1,
      graphFitPending: true,
      comparison: null,
      error: "",
    };
    drawer.__decisionWorkspace = workspace;
    presentCitationDrawer(drawer, "workspace");
    drawer.addEventListener("click", async (event) => {
      const filterSurface = event.target?.closest?.("[data-decision-toggle-filter], .decision-graph-filter-menu");
      if (workspace.filtersOpen && !filterSurface) {
        workspace.filtersOpen = false;
        renderDecisionHistoryDrawer(drawer, workspace);
        return;
      }
      const target = event.target?.closest?.("button");
      if (!target) return;
      if (target.matches("[data-close-decision-history]")) return closeCitationDrawer(drawer);
      if (target.matches("[data-decision-history-retry]")) return void loadDecisionHistoryDrawer(drawer, workspace);
      if (target.matches("[data-decision-history-fallback]")) return openRationaleDrawer(workspace.item);
      if (target.matches("[data-decision-fullscreen]")) {
        workspace.fullscreen = !workspace.fullscreen;
        workspace.graphFitPending = true;
        return renderDecisionHistoryDrawer(drawer, workspace);
      }
      if (target.dataset.decisionView) {
        workspace.view = target.dataset.decisionView;
        workspace.graphFitPending = workspace.view === "graph";
        return renderDecisionHistoryDrawer(drawer, workspace);
      }
      if (target.dataset.decisionRevisionId) return void loadDecisionHistoryDrawer(drawer, workspace, target.dataset.decisionRevisionId);
      if (target.dataset.decisionExpandGroup) {
        const key = target.dataset.decisionExpandGroup;
        if (workspace.expandedGroups.has(key)) workspace.expandedGroups.delete(key);
        else workspace.expandedGroups.add(key);
        workspace.zoom = 1;
        workspace.graphFitPending = true;
        return renderDecisionHistoryDrawer(drawer, workspace);
      }
      if (target.matches("[data-decision-toggle-filter]")) {
        workspace.filtersOpen = !workspace.filtersOpen;
        return renderDecisionHistoryDrawer(drawer, workspace);
      }
      if (target.dataset.decisionFilterType) {
        const key = target.dataset.decisionFilterType;
        if (workspace.hiddenTypes.has(key)) workspace.hiddenTypes.delete(key);
        else workspace.hiddenTypes.add(key);
        workspace.filtersOpen = true;
        workspace.graphFitPending = true;
        return renderDecisionHistoryDrawer(drawer, workspace);
      }
      if (target.dataset.decisionZoom) {
        const step = target.dataset.decisionZoom === "in" ? 0.1 : -0.1;
        return applyDecisionGraphZoom(drawer, workspace, (Number(workspace.zoom) || 1) + step);
      }
      if (target.matches("[data-decision-fit-graph]")) {
        workspace.zoom = 1;
        workspace.graphFitPending = true;
        return renderDecisionHistoryDrawer(drawer, workspace);
      }
      if (target.matches("[data-decision-compare]")) {
        if (workspace.comparison) {
          workspace.comparison = null;
          return renderDecisionHistoryDrawer(drawer, workspace);
        }
        const revisions = workspace.history?.revisions || [];
        const currentIndex = revisions.findIndex((entry) => text(entry.id) === workspace.history.selectedID);
        const base = revisions[currentIndex + 1] || revisions[currentIndex - 1];
        if (!base) return;
        workspace.error = "";
        try {
          const query = new URLSearchParams({ focus_id: focusID, base_revision_id: base.id, compare_revision_id: workspace.history.selectedID });
          const comparison = await request(`./api/app-data/v1/operations/decision_revision.compare?${query}`, { method: "POST", timeoutMs: 10000 });
          workspace.comparison = comparison?.data && typeof comparison.data === "object" ? comparison.data : comparison;
        } catch (error) {
          workspace.error = error?.message || readerFocusCopy("版本比较失败", "Version comparison failed");
        }
        return renderDecisionHistoryDrawer(drawer, workspace);
      }
      if (target.dataset.decisionNodeId) {
        workspace.selectedNodeID = target.dataset.decisionNodeId;
        workspace.feedbackMode = "";
        return renderDecisionHistoryDrawer(drawer, workspace);
      }
      if (target.dataset.decisionNodeSource) {
        const node = workspace.history.nodes.find((entry) => text(entry.id) === target.dataset.decisionNodeSource);
        const citation = decisionNodeCitation(node);
        if (citation) void openCitationDrawer(citation);
        return;
      }
      if (target.matches("[data-decision-node-close]")) {
        workspace.selectedNodeID = "";
        workspace.feedbackMode = "";
        return renderDecisionHistoryDrawer(drawer, workspace);
      }
      if (target.dataset.decisionFeedbackMode) {
        workspace.feedbackMode = target.dataset.decisionFeedbackMode;
        return renderDecisionHistoryDrawer(drawer, workspace);
      }
      if (target.matches("[data-decision-feedback-cancel]")) {
        workspace.feedbackMode = "";
        return renderDecisionHistoryDrawer(drawer, workspace);
      }
    });
    drawer.addEventListener("wheel", (event) => {
      if (!event.ctrlKey) return;
      const scroll = event.target?.closest?.("[data-decision-graph-scroll]");
      if (!scroll || workspace.view !== "graph") return;
      event.preventDefault();
      const delta = Number(event.deltaY) || Number(event.deltaX) || 0;
      const factor = Math.exp(-Math.max(-240, Math.min(240, delta)) * 0.0025);
      applyDecisionGraphZoom(drawer, workspace, (Number(workspace.zoom) || 1) * factor, event.clientX, event.clientY);
    }, { passive: false });
    drawer.addEventListener("change", (event) => {
      const select = event.target?.closest?.("[data-decision-revision-select]");
      if (!select) return;
      void loadDecisionHistoryDrawer(drawer, workspace, select.value);
    });
    drawer.addEventListener("submit", async (event) => {
      const form = event.target;
      if (!(form instanceof HTMLFormElement)) return;
      event.preventDefault();
      const revision = selectedDecisionRevision(workspace.history);
      if (!revision || workspace.busy) return;
      let operation = "";
      let payload = null;
      if (form.matches("[data-decision-feedback-form]")) {
        operation = "decision_feedback.apply";
        payload = { schema_version: 2, focus_id: focusID, decision_revision_id: revision.id, action: form.dataset.feedbackAction, target_node_id: workspace.selectedNodeID, content: text(new FormData(form).get("content")).slice(0, 2000) };
      } else if (form.matches("[data-decision-review-form]")) {
        const data = new FormData(form);
        const result = text(data.get("result"));
        const replacement = text(data.get("replacement_content")).slice(0, 4000);
        const message = form.querySelector(".decision-form-message");
        if (result !== "maintain" && !replacement) {
          if (message) message.textContent = readerFocusCopy("调整或推翻时需要填写新决策。", "Enter the replacement decision for adjust or overturn.");
          return;
        }
        operation = "decision_review.apply";
        payload = { schema_version: 2, focus_id: focusID, decision_revision_id: revision.id, outcome: text(data.get("outcome")).slice(0, 3000), result };
        if (replacement) payload.replacement_content = replacement;
      }
      if (!operation || !payload) return;
      workspace.busy = true;
      workspace.error = "";
      renderDecisionHistoryDrawer(drawer, workspace);
      try {
        const result = await mutateDecisionHistory(operation, payload);
        const history = normalizeDecisionHistoryResponse(result, focusID, decisionID);
        if (history) {
          workspace.history = history;
          parity.decisionHistories.set(decisionHistoryCacheKey(focusID, decisionID), history);
        }
        workspace.feedbackMode = "";
      } catch (error) {
        workspace.error = error?.message || readerFocusCopy("保存失败", "Could not save");
      } finally {
        workspace.busy = false;
        renderDecisionHistoryDrawer(drawer, workspace);
      }
    });
    void loadDecisionHistoryDrawer(drawer, workspace);
    return drawer;
  }

  function focusResultFromCard(card) {
    if (!card) return null;
    const kind = text(card.dataset.focusResultKind);
    const id = text(card.dataset.focusResultId);
    return [...focusResultItems(state.dashboard?.focus_analysis), ...[...parity.focusAnalyses.values()].flatMap((analysis) => focusResultItems(analysis))].find(
      (item) => text(item?.kind) === kind && text(item?.id) === id
    ) || null;
  }

  function focusResultFeedbackKey(focusID, kind, resultID, revision, decision, patch = null) {
    // Feedback retries must address the same mutation. A bounded synchronous
    // FNV-1a digest keeps this compatible with older WebViews without placing
    // the editable payload in the transport header.
    const canonical = JSON.stringify({
      focus_id: text(focusID),
      result_kind: text(kind, "result"),
      result_id: text(resultID),
      expected_revision: Number(revision) || 1,
      decision: text(decision),
      patch: patch || null,
    });
    let hash = 2166136261;
    let secondaryHash = 2246822519;
    for (let index = 0; index < canonical.length; index += 1) {
      const code = canonical.charCodeAt(index);
      hash ^= code;
      hash = Math.imul(hash, 16777619);
      secondaryHash ^= code;
      secondaryHash = Math.imul(secondaryHash, 3266489917);
    }
    return `focus-feedback:${(hash >>> 0).toString(16).padStart(8, "0")}${(secondaryHash >>> 0).toString(16).padStart(8, "0")}`;
  }

  async function confirmDecisionRevision(item, focusID, revision) {
    // Confirmation is a CAS mutation. Always read the three authoritative
    // revisions immediately before applying it instead of trusting the card
    // snapshot, which can be older than an analysis/run update.
    const [focusResult, analysisResult, run] = await Promise.all([
      callFocusOperation("focus_space.get", { query: { focus_id: focusID } }),
      callFocusOperation("focus_analysis.get", {
        query: { focus_id: focusID, limit: 20 },
      }),
      loadDecisionRunState(focusID, { force: true, silent: true }),
    ]);
    const freshFocus = (Array.isArray(focusResult?.focuses)
      ? focusResult.focuses
      : []
    )
      .map(normalizeFocusV3)
      .find((candidate) => text(candidate?.id) === text(focusID));
    const freshItem = focusResultItems(analysisResult).find(
      (candidate) =>
        text(candidate?.kind).toLowerCase() === "decision" &&
        text(candidate?.id) === text(item?.id)
    );
    if (!freshFocus || !freshItem)
      throw new Error(
        readerFocusCopy(
          "决策已经变化，请刷新后重试",
          "The decision changed. Refresh and try again"
        )
      );
    parity.focusSpaces = (Array.isArray(focusResult?.focuses)
      ? focusResult.focuses
      : []
    ).map(normalizeFocusV3);
    parity.focusPolicy = focusResult?.policy || parity.focusPolicy;
    parity.focusAnalyses.set(focusID, analysisResult || {});
    const data = focusResultData(freshItem);
    const analysisRunID = text(
      freshItem?.analysis_run_id || analysisResult?.run_id
    );
    const payload = {
      schema_version: 2,
      focus_id: focusID,
      decision_id: text(freshItem?.id),
      expected_focus_revision: Number(freshFocus?.revision) || 0,
      expected_result_revision: Number(freshItem?.revision) || Number(revision) || 1,
    };
    if (data.expected_outcome) payload.expected_outcome = text(data.expected_outcome).slice(0, 2000);
    if (data.review_at) payload.review_at = text(data.review_at).slice(0, 80);
    const compatibleRun =
      run?.runID &&
      run.revision > 0 &&
      run.status === "completed" &&
      run.analysisRunID === analysisRunID &&
      run.decisionID === payload.decision_id;
    // decision_revision.apply is the versioned decision-history path and its
    // contract requires a concrete decision run. Older analyses can still
    // contain valid decisions without such a run; those must fall back to the
    // backwards-compatible focus_result.feedback mutation below.
    if (!compatibleRun) return false;
    payload.run_id = run.runID;
    payload.expected_run_revision = run.revision;
    const result = await mutateDecisionHistory("decision_revision.apply", payload);
    const history = normalizeDecisionHistoryResponse(result, focusID, payload.decision_id);
    if (history)
      parity.decisionHistories.set(decisionHistoryCacheKey(focusID, payload.decision_id), history);
    return true;
  }

  async function submitFocusResultFeedback(item, decision, patch = null, card = null) {
    if (!item || !["accepted", "rejected", "snoozed", "edited", "draft"].includes(decision)) return;
    const focusID = text(item.focus_id || state.dashboard?.focus_space?.id || state.dashboard?.focusSpace?.id);
    const kind = text(item.kind).toLowerCase();
    const resultID = text(item.id);
    const revision = Number(item.revision) || 1;
    if (!focusID || !resultID || !["signal", "action", "decision"].includes(kind) || (decision === "draft" && kind !== "decision")) return;
    const busyKey = `focus:${kind}:${resultID}`;
    if (parity.feedbackBusy.has(busyKey)) return;
    const payload = {
      schema_version: 1,
      focus_id: focusID,
      result_kind: kind,
      result_id: resultID,
      expected_revision: revision,
      decision,
    };
    if (patch) payload.patch = patch;
    parity.feedbackBusy.add(busyKey);
    if (card) {
      card.classList.add("is-feedback-pending");
      card.setAttribute("aria-busy", "true");
      const actions = card.querySelector(".focus-result-actions");
      if (actions) actions.innerHTML = `<span class="focus-result-saving">${esc(readerFocusCopy("正在保存…", "Saving…"))}</span>`;
    }
    card?.querySelectorAll("button, input, textarea, select").forEach((node) => {
      node.disabled = true;
    });
    try {
      const shouldConfirmDecisionRevision =
        kind === "decision" &&
        decision === "accepted" &&
        !patch &&
        Number(activeFocusSpaceProjection(state.dashboard || {})?.revision) > 0;
      const confirmedRevision = shouldConfirmDecisionRevision
        ? await confirmDecisionRevision(item, focusID, revision)
        : false;
      if (!confirmedRevision) {
        await request("./api/app-data/v1/operations/focus_result.feedback", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": focusResultFeedbackKey(
              focusID,
              kind,
              resultID,
              revision,
              decision,
              patch
            ),
          },
          body: JSON.stringify(payload),
        });
      }
      await reloadFocusProjection(focusID, { silent: true });
      showToast(
        decision === "accepted"
          ? readerFocusCopy("已接受焦点结果", "Focus result accepted")
          : decision === "rejected"
            ? readerFocusCopy("已拒绝焦点结果", "Focus result rejected")
            : decision === "snoozed"
              ? readerFocusCopy("已移到稍后处理", "Focus result snoozed")
              : decision === "draft"
                ? readerFocusCopy("已撤销接受，可重新确认", "Acceptance undone; the decision can be reviewed again")
                : readerFocusCopy("焦点结果已修改", "Focus result edited")
      );
    } catch (error) {
      await reloadFocusProjection(focusID, { silent: true });
      showError(error.message || readerFocusCopy("焦点结果没有保存", "Focus result was not saved"));
    } finally {
      card?.classList.remove("is-feedback-pending");
      card?.removeAttribute("aria-busy");
      parity.feedbackBusy.delete(busyKey);
    }
  }

  function focusResultEditPatch(card, item) {
    const kind = text(item?.kind).toLowerCase();
    const value = (field, fallback = "") =>
      text(card?.querySelector(`[data-focus-edit-field="${field}"]`)?.value, fallback);
    if (kind === "action") {
      return {
        task: value("task", focusResultData(item).task).slice(0, 1000),
        owner: value("owner", focusResultData(item).owner).slice(0, 120),
        due_at: value("due_at", focusResultData(item).due_at).slice(0, 80),
        priority: value("priority", focusResultData(item).priority || "medium"),
      };
    }
    if (kind === "decision") return { question: value("question", focusResultData(item).question).slice(0, 500) };
    return { detail: value("detail", focusResultData(item).detail).slice(0, 2000) };
  }

  function decisionRunSignature(run) {
    if (!run) return "";
    return JSON.stringify([
      run.runID,
      run.status,
      run.phase,
      run.message,
      run.completed,
      run.total,
      run.percent,
      run.question?.id,
      run.question?.revision,
      run.question?.answerRevision,
      run.revision,
      run.errorCode,
      run.errorMessage,
      run.creditMessage,
      run.updatedAt,
    ]);
  }

  function refreshDecisionRunPanel(focusID) {
	const drawer = $("focus-v3-drawer-layer");
	if (
	  text(parity.selectedFocusID) === text(focusID) &&
	  drawer &&
	  !drawer.hidden
	) {
	  refreshOpenFocusV3Drawer(focusID, parity.focusPanel);
	  return;
	}
    if (text(state.dashboard?.focus_space?.id || state.dashboard?.focusSpace?.id) !== text(focusID))
      return;
    if (state.selected || !document.querySelector('[data-overview-module="focus_space"]'))
      return;
    renderOverview();
  }

  function clearDecisionRunPoll(focusID) {
    const timer = parity.decisionRunPollTimers.get(focusID);
    if (timer) window.clearTimeout(timer);
    parity.decisionRunPollTimers.delete(focusID);
  }

  function decisionRunNeedsPolling(run) {
    return Boolean(
      run &&
        (decisionRunActiveStatuses.has(run.status) ||
          (run.status === "waiting_clarification" && !run.question))
    );
  }

  function scheduleDecisionRunPoll(focusID, run) {
    clearDecisionRunPoll(focusID);
    if (!decisionRunNeedsPolling(run)) return;
    const delay = document.hidden ? 8000 : 2500;
    const timer = window.setTimeout(() => {
      parity.decisionRunPollTimers.delete(focusID);
      void loadDecisionRunState(focusID, { force: true, silent: true });
    }, delay);
    parity.decisionRunPollTimers.set(focusID, timer);
  }

  async function loadDecisionRunState(focusID, options = {}) {
    const id = text(focusID);
    if (!id || parity.decisionRunLoading.has(id)) return parity.decisionRuns.get(id) || null;
    if (!options.force && parity.decisionRunLoaded.has(id)) return parity.decisionRuns.get(id) || null;
    parity.decisionRunLoading.add(id);
    const previous = parity.decisionRuns.get(id) || null;
    try {
      const result = await request(
        `./api/app-data/v1/operations/decision_run.get?focus_id=${encodeURIComponent(id)}`,
        { method: "POST", timeoutMs: 7000 }
      );
      const next = normalizeDecisionRunResponse(result, id);
      if (next) parity.decisionRuns.set(id, next);
      else parity.decisionRuns.delete(id);
      if (next && ["completed", "failed_retryable", "failed_terminal", "superseded"].includes(next.status))
        parity.decisionRunMessages.delete(id);
      parity.decisionRunErrors.delete(id);
      parity.decisionRunLoaded.add(id);
      if (decisionRunSignature(previous) !== decisionRunSignature(next))
        refreshDecisionRunPanel(id);
      scheduleDecisionRunPoll(id, next);
      // 终态只在"转入"的那一次刷新投影。这里的 loadDecisionRunState 会被
      // workspace 面板刷新反复强制触发；若每个终态响应都 reloadFocusProjection，
      // 刷新→读取→再刷新会在网络往返速度上自持成环（实测 ~10Hz，思考与行动
      // 链图每圈销毁重建一次，即用户看到的决策图持续闪屏）。
      const decisionRunTerminalNow = next && ["completed", "failed_retryable", "failed_terminal", "superseded"].includes(next.status);
      const decisionRunTerminalBefore = previous && ["completed", "failed_retryable", "failed_terminal", "superseded"].includes(previous.status);
      if (decisionRunTerminalNow && !decisionRunTerminalBefore)
        void reloadFocusProjection(id, { silent: true });
      if (decisionRunNeedsAgentTurn(next))
        void triggerDecisionRunAgentTurn(id, next, "reconcile");
      return next;
    } catch (error) {
      // The endpoint is additive. Older installed dashboards keep the existing
      // Focus overview without an error card; an already visible run keeps its
      // last persisted state and reports only that refresh was interrupted.
      parity.decisionRunLoaded.add(id);
      parity.decisionRunErrors.set(
        id,
        error?.message || readerFocusCopy("决策状态暂时不可用", "Decision status is unavailable")
      );
      if (previous || (text(parity.selectedFocusID) === id && parity.focusPanel === "workspace")) {
        refreshDecisionRunPanel(id);
      }
      if (previous) {
        scheduleDecisionRunPoll(id, previous);
      } else clearDecisionRunPoll(id);
      if (!options.silent && previous) showError(parity.decisionRunErrors.get(id));
      return previous;
    } finally {
      parity.decisionRunLoading.delete(id);
    }
  }

  function decisionRunMutationKey(operation, focusID, payload, subject = "") {
    const canonical = JSON.stringify({
      operation: text(operation),
      focus_id: text(focusID),
      subject: text(subject),
      payload: payload && typeof payload === "object" ? payload : null,
    });
    let hash = 2166136261;
    let secondaryHash = 2246822519;
    for (let index = 0; index < canonical.length; index += 1) {
      const code = canonical.charCodeAt(index);
      hash ^= code;
      hash = Math.imul(hash, 16777619);
      secondaryHash ^= code;
      secondaryHash = Math.imul(secondaryHash, 3266489917);
    }
    return `decision-run:${text(operation).replace(/[^A-Za-z0-9._:-]/g, "_").slice(0, 32)}:${(hash >>> 0).toString(16).padStart(8, "0")}${(secondaryHash >>> 0).toString(16).padStart(8, "0")}`;
  }

  function decisionRunNeedsAgentTurn(run) {
    return Boolean(
      run?.runID &&
        run.revision > 0 &&
        ["queued", "collecting_context"].includes(run.status)
    );
  }

  async function triggerDecisionRunAgentTurn(focusID, run, intent = "reconcile") {
    const id = text(focusID);
    if (!id || !decisionRunNeedsAgentTurn(run)) return null;
    const turnKey = `${id}\u0000${run.runID}\u0000${run.revision}`;
    if (parity.decisionRunTurnBusy.has(turnKey)) return null;
    parity.decisionRunTurnBusy.add(turnKey);
    try {
      const payload = {
        focus_id: id,
        run_id: run.runID,
        expected_revision: run.revision,
      };
      const receipt = await request("./api/focus-decision/turn", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": decisionRunMutationKey(
            "turn",
            id,
            payload,
            intent
          ),
        },
        body: JSON.stringify(payload),
        timeoutMs: 15000,
      });
      const state = text(receipt?.state);
      if (["accepted", "running"].includes(state)) {
        parity.decisionRunMessages.set(
          id,
          intent === "answer"
            ? readerFocusCopy("回答已保存，AI 正在继续分析。", "Answer saved; AI analysis is continuing.")
            : intent === "resume"
              ? readerFocusCopy("已从保存的进度继续分析。", "Analysis resumed from saved progress.")
              : readerFocusCopy("AI 已开始分析。", "AI analysis has started.")
        );
      }
      scheduleDecisionRunPoll(id, run);
      return receipt;
    } catch (_error) {
      // The run mutation is already durable. Keep polling and retry this
      // idempotent host trigger instead of claiming that AI work has started.
      parity.decisionRunMessages.set(
        id,
        readerFocusCopy(
          "进度已保存，AI 暂未启动，将自动重试。",
          "Progress is saved. AI has not started yet and will retry automatically."
        )
      );
      scheduleDecisionRunPoll(id, run);
      return null;
    } finally {
      parity.decisionRunTurnBusy.delete(turnKey);
      refreshDecisionRunPanel(id);
    }
  }

  async function mutateDecisionRun(operation, focusID, payload, subject = "") {
    const id = text(focusID);
    const runID = text(payload?.run_id);
    if (!id || parity.decisionRunBusy.has(id)) return null;
    parity.decisionRunBusy.add(id);
    parity.decisionRunMessages.delete(id);
    refreshDecisionRunPanel(id);
    try {
      const result = await request(
        `./api/app-data/v1/operations/decision_run.${operation}`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": decisionRunMutationKey(operation, id, payload, subject),
          },
          body: JSON.stringify(payload),
          timeoutMs: 15000,
        }
      );
      let next = normalizeDecisionRunResponse(result, id);
      if (next) {
        parity.decisionRuns.set(id, next);
        parity.decisionRunErrors.delete(id);
        parity.decisionRunLoaded.add(id);
      }
      if (operation === "answer" && subject)
        parity.decisionRunDrafts.delete(decisionRunDraftKey(id, subject));
      if (!next) next = await loadDecisionRunState(id, { force: true, silent: true });
      if (operation === "answer")
        await reloadFocusProjection(id, { silent: true, rerender: false });
      if (decisionRunNeedsAgentTurn(next)) {
        await triggerDecisionRunAgentTurn(id, next, operation);
      } else {
        parity.decisionRunMessages.set(
          id,
          operation === "answer" && next?.status === "waiting_clarification"
            ? readerFocusCopy("回答已保存，下一项需要你的确认。", "Answer saved; the next item needs your input.")
            : operation === "start" && next?.status === "waiting_clarification"
              ? readerFocusCopy("分析已准备好，需要你先确认一个问题。", "Analysis is ready and needs one answer first.")
              : operation === "answer"
                ? readerFocusCopy("回答已保存。", "Answer saved.")
                : operation === "cancel"
                  ? readerFocusCopy("本轮分析已停止。", "This analysis run was stopped.")
                : operation === "start"
                  ? readerFocusCopy("分析请求已保存。", "Analysis request saved.")
                  : readerFocusCopy("恢复请求已保存。", "Resume request saved.")
        );
        scheduleDecisionRunPoll(id, next);
      }
      return next;
    } catch (error) {
      parity.decisionRunMessages.set(
        id,
        error?.message || readerFocusCopy("操作没有完成，请重试。", "The operation did not complete. Try again.")
      );
      return null;
    } finally {
      parity.decisionRunBusy.delete(id);
      refreshDecisionRunPanel(id);
    }
  }

  function closeDecisionRunStartDialog() {
    $("decision-run-start-layer")?.remove();
  }

  function openDecisionRunStartDialog(button) {
    if (!button || button.disabled) return;
    closeDecisionRunStartDialog();
    const selectedMode = text(button.dataset.decisionMode, "guided");
    const layer = document.createElement("div");
    layer.id = "decision-run-start-layer";
    layer.className = "decision-run-start-layer";
    layer.innerHTML = `<button class="decision-run-start-mask" type="button" data-decision-run-dialog-close aria-label="${esc(readerFocusCopy("关闭重新分析", "Close analysis dialog"))}"></button><form class="decision-run-start-dialog" data-decision-run-dialog role="dialog" aria-modal="true" aria-labelledby="decision-run-start-title"><header><div><span>${esc(readerFocusCopy("新一轮分析", "New analysis"))}</span><h2 id="decision-run-start-title">${esc(readerFocusCopy("选择思维模型", "Choose a reasoning model"))}</h2></div><button class="icon-btn" type="button" data-decision-run-dialog-close aria-label="${esc(readerFocusCopy("关闭", "Close"))}">${icon("close")}</button></header><label><span>${esc(readerFocusCopy("思维模型", "Reasoning model"))}</span><select data-decision-mode-select><option value="guided"${selectedMode === "guided" ? " selected" : ""}>${esc(readerFocusCopy("引导式", "Guided"))}</option><option value="rational"${selectedMode === "rational" ? " selected" : ""}>${esc(readerFocusCopy("理性分析", "Rational"))}</option><option value="bounded_rationality"${selectedMode === "bounded_rationality" ? " selected" : ""}>${esc(readerFocusCopy("有限理性", "Bounded rationality"))}</option><option value="first_principles"${selectedMode === "first_principles" ? " selected" : ""}>${esc(readerFocusCopy("第一性原理", "First principles"))}</option></select></label><p>${esc(readerFocusCopy("上一轮结果会保留，新结果将作为独立版本生成。", "The previous result is preserved and the new result is saved as a separate version."))}</p><footer><button class="text-btn" type="button" data-decision-run-dialog-close>${esc(readerFocusCopy("取消", "Cancel"))}</button><button class="primary-btn" type="submit">${icon("sparkles")}${esc(readerFocusCopy("开始分析", "Start analysis"))}</button></footer></form>`;
    document.body.appendChild(layer);
    layer.querySelectorAll("[data-decision-run-dialog-close]").forEach((node) =>
      node.addEventListener("click", closeDecisionRunStartDialog)
    );
    layer.addEventListener("keydown", (event) => {
      if (event.key === "Escape") closeDecisionRunStartDialog();
    });
    layer.querySelector("[data-decision-run-dialog]")?.addEventListener("submit", (event) => {
      event.preventDefault();
      const mode = text(layer.querySelector("[data-decision-mode-select]")?.value, "guided");
      closeDecisionRunStartDialog();
      startOrResumeDecisionRun(button, mode);
    });
    requestAnimationFrame(() => layer.querySelector("[data-decision-mode-select]")?.focus());
  }

  function startOrResumeDecisionRun(button, decisionMode = "") {
    const focusID = text(button?.dataset.focusId);
    const action = text(button?.dataset.decisionRunAction, button?.hasAttribute("data-decision-run-configure") ? "start" : "");
    if (!focusID || !["start", "resume", "cancel"].includes(action)) return;
    const payload = { schema_version: 2 };
    const focusRevision = Number(button.dataset.focusRevision);
    const runRevision = Number(button.dataset.runRevision);
    if (action === "start") {
      payload.focus_id = focusID;
      payload.expected_focus_revision = focusRevision;
      payload.decision_mode = text(decisionMode || button.dataset.decisionMode, "guided");
      const analysisRunID = text(button.dataset.analysisRunId);
      const decisionID = text(button.dataset.decisionId);
      if (analysisRunID) payload.analysis_run_id = analysisRunID;
      if (decisionID) payload.decision_id = decisionID;
    }
    if (action === "resume") {
      payload.run_id = text(button.dataset.runId);
      if (Number.isInteger(runRevision) && runRevision > 0)
        payload.expected_revision = runRevision;
    }
    if (action === "cancel") {
      payload.run_id = text(button.dataset.runId);
      payload.expected_revision = runRevision;
      payload.reason = readerFocusCopy("用户停止了本轮分析", "The user stopped this analysis run");
    }
    const previousRunID = text(button.dataset.previousRunId);
    const previousRunRevision = Number(button.dataset.previousRunRevision) || 0;
    const mutationSubject = action === "start"
      ? [focusRevision || 0, previousRunID, previousRunRevision].join(":")
      : payload.run_id || String(focusRevision || "");
    void mutateDecisionRun(action, focusID, payload, mutationSubject);
  }

  function captureDecisionRunDraft(form) {
    const focusID = text(form?.dataset.focusId);
    const questionID = text(form?.dataset.questionId);
    if (!focusID || !questionID) return null;
    const draft = {
      optionIDs: Array.from(form.querySelectorAll('input[type="radio"]:checked,input[type="checkbox"]:checked'))
        .map((node) => text(node.value))
        .filter(Boolean)
        .slice(0, 12),
      answerText: text(form.querySelector("[data-decision-answer-text]")?.value).slice(0, 2000),
    };
    parity.decisionRunDrafts.set(decisionRunDraftKey(focusID, questionID), draft);
    return draft;
  }

  function submitDecisionRunAnswer(form) {
    const focusID = text(form?.dataset.focusId);
    const runID = text(form?.dataset.runId);
    const questionID = text(form?.dataset.questionId);
    if (!focusID || !runID || !questionID) return;
    const draft = captureDecisionRunDraft(form) || {};
    const optionIDs = Array.isArray(draft.optionIDs) ? draft.optionIDs : [];
    const answerText = text(draft.answerText).slice(0, 2000);
    const questionType = text(form.dataset.questionType);
    const required = form.dataset.questionRequired === "1" || ["choice", "multi", "scale"].includes(questionType);
    const message = form.querySelector("[data-decision-run-inline-message]");
    if (required && !optionIDs.length && !answerText) {
      if (message)
        message.textContent = readerFocusCopy("请选择一个选项或填写回答。", "Choose an option or enter an answer.");
      return;
    }
    const maxSelect = Number(form.dataset.questionMaxSelect) || 0;
    if (questionType === "multi" && maxSelect > 0 && optionIDs.length > maxSelect) {
      if (message)
        message.textContent = readerFocusCopy(`最多选择 ${maxSelect} 项。`, `Choose up to ${maxSelect} options.`);
      return;
    }
    if (message) message.textContent = "";
    const answerValue = questionType === "multi"
      ? optionIDs
      : questionType === "choice"
        ? optionIDs[0] || ""
        : questionType === "scale"
          ? Number(optionIDs[0])
          : answerText;
    const payload = {
      schema_version: 2,
      run_id: runID,
      question_id: questionID,
      expected_run_revision: Number(form.dataset.runRevision) || 0,
      expected_answer_revision: Number(form.dataset.answerRevision) || 0,
      answer: {
        type: questionType,
        value: answerValue,
      },
    };
    void mutateDecisionRun("answer", focusID, payload, questionID);
  }

  const focusV3Views = new Set(["today", "decision", "action", "signal"]);
  const focusWorkflowColumns = [
    ["accumulating", "积累", "Accumulating", "持续收集真实资料", "Collecting verified materials"],
    ["deciding", "决策", "Deciding", "形成并确认判断", "Forming and confirming a decision"],
    ["executing", "执行", "Executing", "推进已确认行动", "Advancing confirmed actions"],
    ["done", "完成", "Done", "保留结果与复盘", "Keeping results and review history"],
    ["archived", "归档", "Archived", "已结束的焦点", "Archived focus"],
  ];

  function focusOperationURL(operation, query = {}) {
    const params = new URLSearchParams();
    Object.entries(query).forEach(([key, value]) => {
      if (value !== undefined && value !== null && String(value) !== "") params.set(key, String(value));
    });
    const suffix = params.toString();
    return `./api/app-data/v1/operations/${operation}${suffix ? `?${suffix}` : ""}`;
  }

  function focusIdempotencyKey(rawValue) {
    const value = text(rawValue);
    if (/^[A-Za-z0-9._:-]{1,128}$/.test(value)) return value;
    const bytes = typeof TextEncoder === "function"
      ? new TextEncoder().encode(value)
      : Array.from(unescape(encodeURIComponent(value)), (character) => character.charCodeAt(0));
    let first = 0x811c9dc5;
    let second = 0x9e3779b9;
    bytes.forEach((byte) => {
      first = Math.imul(first ^ byte, 0x01000193) >>> 0;
      second = Math.imul(second ^ byte, 0x85ebca6b) >>> 0;
    });
    return `focus-v3:${bytes.length}:${first.toString(16).padStart(8, "0")}${second
      .toString(16)
      .padStart(8, "0")}`;
  }

  async function callFocusOperation(operation, { query = {}, body, key = "" } = {}) {
    const options = { method: "POST", headers: {} };
    if (body !== undefined) {
      options.headers["Content-Type"] = "application/json";
      options.headers["Idempotency-Key"] = focusIdempotencyKey(
        key || `focus-v3:${Date.now()}:${Math.random().toString(16).slice(2)}`
      );
      options.body = JSON.stringify(body);
    }
    return request(focusOperationURL(operation, query), options);
  }

  function normalizeFocusV3(raw) {
    const focus = raw && typeof raw === "object" ? { ...raw } : {};
    focus.decision_mode = ["sprint", "slow_burn"].includes(text(focus.decision_mode))
      ? text(focus.decision_mode)
      : text(focus.horizon_end) ? "sprint" : "slow_burn";
    focus.workflow_state = focus.status === "archived"
      ? "archived"
      : ["accumulating", "deciding", "executing", "done"].includes(text(focus.workflow_state))
        ? text(focus.workflow_state)
        : "accumulating";
    focus.intent_kind = ["decide", "plan", "create", "monitor"].includes(text(focus.intent_kind))
      ? text(focus.intent_kind)
      : text(focus.kind) === "decision" ? "decide" : "monitor";
    focus.blueprint_id = text(focus.blueprint_id, `${focus.intent_kind}.v1`);
    focus.thinking_template_id = ["guided", "rational", "bounded_rationality", "first_principles"].includes(text(focus.thinking_template_id))
      ? text(focus.thinking_template_id)
      : ({ decide: "rational", plan: "bounded_rationality", create: "first_principles", monitor: "guided" })[focus.intent_kind];
    return focus;
  }

  async function loadFocusAnalysis(focusID, silent = true) {
    if (!focusID) return null;
    try {
      const result = await callFocusOperation("focus_analysis.get", { query: { focus_id: focusID, limit: 20 } });
      parity.focusAnalyses.set(focusID, result || {});
      return result;
    } catch (error) {
      if (!silent) showError(error.message || readerFocusCopy("焦点结果载入失败", "Could not load Focus results"));
      return null;
    }
  }

  async function loadFocusSpaces(silent = true) {
    if (parity.focusLoading) return;
    parity.focusLoading = true;
    try {
      const result = await callFocusOperation("focus_space.get");
      parity.focusSpaces = (Array.isArray(result?.focuses) ? result.focuses : []).map(normalizeFocusV3);
      parity.focusPolicy = result?.policy || null;
      parity.focusLoaded = true;
      await Promise.all(parity.focusSpaces.map((focus) => loadFocusAnalysis(text(focus.id), true)));
    } catch (error) {
      if (!silent) showError(error.message || readerFocusCopy("焦点空间载入失败", "Could not load Focus Space"));
    } finally {
      parity.focusLoading = false;
    }
  }

  async function reloadFocusProjection(focusID, options = {}) {
    const id = text(focusID);
    if (!id) return null;
    // 同一焦点并发的投影 reload 只保留一个在飞：轮询终态、用户回答、面板
    // 刷新可能同时触发；重叠的响应会各自 refreshOpenFocusV3Drawer 一次，
    // 放大面板重写次数（工作流图跟着销毁重建）。
    parity.focusProjectionReloads ||= new Map();
    if (parity.focusProjectionReloads.has(id)) return parity.focusProjectionReloads.get(id);
    const started = (async () => {
    const silent = options.silent !== false;
    try {
      const [focusResult, analysisResult] = await Promise.all([
        callFocusOperation("focus_space.get", { query: { focus_id: id } }),
        callFocusOperation("focus_analysis.get", { query: { focus_id: id, limit: 20 } }),
      ]);
      const freshFocus = (Array.isArray(focusResult?.focuses) ? focusResult.focuses : [])
        .map(normalizeFocusV3)
        .find((item) => text(item?.id) === id);
      if (freshFocus) {
        const index = parity.focusSpaces.findIndex((item) => text(item?.id) === id);
        if (index >= 0) parity.focusSpaces.splice(index, 1, freshFocus);
        else parity.focusSpaces.push(freshFocus);
      }
      parity.focusPolicy = focusResult?.policy || parity.focusPolicy;
      parity.focusAnalyses.set(id, analysisResult || {});
      parity.focusLoaded = true;
      if (options.rerender !== false && focusV3Views.has(state.view)) renderFocusV3Workspace();
      const layer = $("focus-v3-drawer-layer");
      if (text(parity.selectedFocusID) === id && layer && !layer.hidden)
        refreshOpenFocusV3Drawer(id, parity.focusPanel);
      refreshDecisionRunPanel(id);
      return { focus: freshFocus || null, analysis: analysisResult || {} };
    } catch (error) {
      if (!silent) showError(error?.message || readerFocusCopy("焦点结果载入失败", "Could not load Focus results"));
      return null;
    }
    })();
    parity.focusProjectionReloads.set(id, started);
    try {
      return await started;
    } finally {
      parity.focusProjectionReloads.delete(id);
    }
  }

  function focusAnalysisFor(focusID) {
    return parity.focusAnalyses.get(text(focusID)) || {};
  }

  function allFocusResults(kind) {
    const output = [];
    parity.focusSpaces.forEach((focus) => {
      const analysis = focusAnalysisFor(focus.id);
      const items = Array.isArray(analysis?.[`${kind}s`]) ? analysis[`${kind}s`] : [];
      items.forEach((item) => output.push({ ...item, focus_id: item.focus_id || focus.id }));
    });
    return output;
  }

  const productV3QuickViews = [
    ["today", "焦点空间", "Focus space", "focus"],
    ["decision", "待决策", "Decisions", "decision"],
    ["action", "行动", "Actions", "action"],
    ["signal", "风险与信号", "Risks & signals", "signal"],
    ["meeting", "会议与录音", "Meetings & recordings", "meeting"],
    ["obsidian", "Obsidian", "Obsidian", "obsidian"],
  ];
  const productV3LibraryViews = [
    ["starred", "收藏", "Favorites", "star"],
    ["later", "稍后读", "Read later", "timer"],
  ];

  function productV3Icon(name) {
    const known = icon(name);
    if (known) return known;
    return {
      focus: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="8"/><circle cx="12" cy="12" r="3"/><path d="M12 2v3M22 12h-3M12 22v-3M2 12h3"/></svg>',
      timer: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="13" r="8"/><path d="M9 2h6M12 5v8l4 2"/></svg>',
      sun: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M19 5l-1.5 1.5M6.5 17.5 5 19"/></svg>',
      moon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M20 15.5A8 8 0 0 1 8.5 4 8.2 8.2 0 1 0 20 15.5Z"/></svg>',
      // 与 #view-nav 的"全部内容"按钮同一图形，聚合视图入口在两套侧栏里
      // 保持一致。
      all: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 5h16v14H4z"/><path d="M8 9h8M8 13h5"/></svg>',
    }[name] || icon("note");
  }

  function focusNavigationCounts() {
    return {
      today: parity.focusSpaces.filter(focus => !["done", "archived"].includes(focus.workflow_state)).length,
      decision: allFocusResults("decision").filter(item => !["accepted", "edited", "rejected"].includes(text(item.feedback_status))).length,
      action: allFocusResults("action").filter(item => text(item.feedback_status) !== "rejected" && text(item.status) !== "done").length,
      signal: allFocusResults("signal").filter(item => text(item.feedback_status) !== "rejected").length,
    };
  }
  window.__plaudFocusCounts = focusNavigationCounts;

  function productV3ViewCount(view) {
    if (["today", "decision", "action", "signal"].includes(view)) return focusNavigationCounts()[view];
    if (["meeting", "obsidian"].includes(view)) return knownCount(parity.deviceCounts[view]) ?? "—";
    const original = document.querySelector(
      `#sidebar > :not(#focus-v3-navigation) [data-count="${CSS.escape(view)}"]`
    );
    const parsed = Number(text(original?.textContent));
    if (Number.isFinite(parsed) && parsed > 0) return parsed;
    if (view === "today")
      return parity.focusSpaces.filter(
        (focus) => !["done", "archived"].includes(focus.workflow_state)
      ).length;
    if (["decision", "action", "signal"].includes(view))
      return allFocusResults(view).filter(
        (item) => text(item.feedback_status) !== "rejected"
      ).length;
    return 0;
  }

  function productV3SourceGroupsMarkup() {
    // 分组以 folder_id 为主键（置顶是伪分组），名称只做展示；这样拖拽放置
    // 目标能直接携带 folder_id，与下拉选择共用同一 PATCH 契约。
    const groups = new Map();
    uniqueCatalogSources(state.sources || [])
      .filter((source) => source.subscribed && !source.hidden)
      .forEach((source) => {
        const key = source.pinned
          ? "__pinned__"
          : `folder:${text(source.folder_id)}`;
        if (!groups.has(key))
          groups.set(key, {
            title: source.pinned
              ? readerFocusCopy("置顶", "Pinned")
              : text(source.folder_name, readerFocusCopy("订阅", "Subscriptions")),
            folderID: source.pinned ? null : text(source.folder_id),
            sources: [],
          });
        groups.get(key).sources.push(source);
      });
    if (!groups.size)
      return `<div class="focus-v3-nav-source-empty">${state.sourcesResolved ? readerFocusCopy("暂无来源记录", "No sources yet") : "正在读取来源…"}</div>`;
    return [...groups.entries()]
      .map(([key, group]) => {
        const sources = group.sources;
        const collapsed = sourceGroupCollapsed(group.title);
        const unread = sources.some(source => sourceUnreadTotal(source) === null) ? null : sources.reduce((sum, source) => sum + sourceUnreadTotal(source), 0);
        const rows = sources
          .map((source) => {
            const count = sourceUnreadTotal(source);
            return `<button class="focus-v3-nav-source-item" type="button" draggable="true" data-drag-source-id="${esc(
              source.id
            )}" data-product-view="source:${esc(source.id)}">${sourceAvatar(
              source,
              "focus-v3-nav-avatar"
            )}<span class="focus-v3-nav-label">${esc(
              source.title_override || source.name
            )}</span><span class="focus-v3-nav-count" title="未读">${count ?? "—"}</span></button>`;
          })
          .join("");
        return `<section class="focus-v3-nav-source-group ${
          collapsed ? "collapsed" : ""
        }"><button class="focus-v3-nav-group-trigger" type="button" ${
          group.folderID !== null
            ? `data-folder-drop="${esc(group.folderID)}"`
            : ""
        } data-product-source-group="${esc(
          group.title
        )}" aria-expanded="${String(!collapsed)}"><span>${icon(
          "chevron"
        )}<b>${esc(group.title)}</b></span><span>${
          `<i>${unread ?? "—"}</i>`
        }<small>${sources.length}</small></span></button><div class="focus-v3-nav-group-content">${rows}</div></section>`;
      })
      .join("");
  }

  const productV3AppearanceKey = "zettlab-appearance";
  const productV3AppearanceSyncKey = "zettlab-appearance-host-sync-v1";
  const productV3AppearanceMedia = window.matchMedia(
    "(prefers-color-scheme: light)"
  );

  function resolveProductV3Appearance(appearance) {
    return appearance === "system"
      ? productV3AppearanceMedia.matches
        ? "light"
        : "dark"
      : appearance;
  }

  function applyProductV3Appearance(mode, persist = true) {
    const appearance = ["light", "dark", "system"].includes(mode)
      ? mode
      : "system";
    const resolved = resolveProductV3Appearance(appearance);
    document.documentElement.dataset.appearance = appearance;
    document.documentElement.dataset.theme = resolved;
    document.documentElement.style.colorScheme = resolved;
    document
      .querySelectorAll("[data-product-appearance]")
      .forEach((button) => {
        const active = button.dataset.productAppearance === appearance;
        button.classList.toggle("active", active);
        button.setAttribute("aria-pressed", String(active));
      });
    if (!persist) return;
    try {
      localStorage.setItem(productV3AppearanceKey, appearance);
    } catch (_) {}
  }

  productV3AppearanceMedia.addEventListener("change", () => {
    if (document.documentElement.dataset.appearance !== "system") return;
    applyProductV3Appearance("system", false);
  });

  window.addEventListener("storage", (event) => {
    if (event.key !== productV3AppearanceKey) return;
    applyProductV3Appearance(event.newValue || "system", false);
    renderProductV3Navigation();
  });

  function syncProductV3NavigationState(navigation = $("focus-v3-navigation")) {
    if (!navigation) return;
    navigation.querySelectorAll("[data-product-view]").forEach((button) => {
      const active = text(button.dataset.productView) === state.view;
      button.classList.toggle("active", active);
      if (active) button.setAttribute("aria-current", "page");
      else button.removeAttribute("aria-current");
    });
    const appearance = text(
      document.documentElement.dataset.appearance,
      "system"
    );
    navigation
      .querySelectorAll("[data-product-appearance]")
      .forEach((button) => {
        const active = button.dataset.productAppearance === appearance;
        button.classList.toggle("active", active);
        button.setAttribute("aria-pressed", String(active));
      });
  }

  function switchProductV3View(view) {
    const next = text(view);
    if (!next || next === state.view) {
      syncProductV3NavigationState();
      return;
    }
    rememberObsidianDirectoryScroll();
    if (
      ["all", "rss", "starred", "later", "context"].includes(next) ||
      next.startsWith("source:")
    )
      parity.autoSelectEntry = true;
    if (next.startsWith("source:")) loadSourceOnDemand(next.slice(7));
    if (liveDeviceDataView(next))
      window.setTimeout(
        () => scheduleLiveDeviceDataRefresh(false, false, next),
        0
      );
    state.view = next;
    // Product-v3 navigation is also used by programmatic links (for example
    // overview cards).  Invalidate a request owned by the previous view before
    // the new projection is rendered; renderTimeline covers legacy nav paths.
    cancelStaleReaderEntryLoad(next);
    parity.entryPageError = "";
    try {
      localStorage.setItem("plaud.reader.view", next);
    } catch (_) {}
    state.selected = null;
    document.body.classList.remove("detail-open");
    syncProductV3NavigationState();
    renderReaderProjectionOnly();
  }
  // 缺省页快捷入口（renderOverview）与侧栏共用同一条视图切换路径，
  // 避免面板内链接绕过 live device 数据刷新与请求失效逻辑。
  window.__plaudSwitchView = switchProductV3View;

  function ensureProductV3Navigation() {
    const sidebar = $("sidebar");
    if (!sidebar) return null;
    let navigation = $("focus-v3-navigation");
    if (!navigation) {
      navigation = document.createElement("div");
      navigation.id = "focus-v3-navigation";
      navigation.className = "focus-v3-navigation";
      sidebar.append(navigation);
      navigation.addEventListener("click", (event) => {
        const viewButton = event.target.closest("[data-product-view]");
        if (viewButton) {
          switchProductV3View(viewButton.dataset.productView);
          return;
        }
        const groupButton = event.target.closest("[data-product-source-group]");
        if (groupButton) {
          const group = text(groupButton.dataset.productSourceGroup);
          toggleSourceGroup(group);
          renderProductV3Navigation();
          return;
        }
        if (event.target.closest("[data-product-add-source]")) {
          $("sidebar-add")?.click();
          return;
        }
        if (event.target.closest("[data-product-refresh]")) {
          liveDeviceData.loadedAt = 0;
          $("refresh")?.click();
          return;
        }
        const appearanceButton = event.target.closest("[data-product-appearance]");
        if (appearanceButton) {
          applyProductV3Appearance(appearanceButton.dataset.productAppearance);
          renderProductV3Navigation();
        }
      });
      let stored = "system";
      try {
        // Earlier board builds silently defaulted this app to dark. Migrate once
        // to the host-following mode; explicit choices made afterwards remain.
        if (localStorage.getItem(productV3AppearanceSyncKey) === "1") {
          stored = localStorage.getItem(productV3AppearanceKey) || "system";
        } else {
          localStorage.setItem(productV3AppearanceSyncKey, "1");
          localStorage.setItem(productV3AppearanceKey, "system");
        }
      } catch (_) {}
      applyProductV3Appearance(stored);
      document.body.classList.add("product-v3-shell");
      $("reader-shell")?.classList.add("product-v3-shell");
    }
    return navigation;
  }

  function renderProductV3Navigation() {
    const navigation = ensureProductV3Navigation();
    if (!navigation) return;
    const quick = productV3QuickViews
      .map(([view, label, english, iconName]) => {
        const count = productV3ViewCount(view);
        const copy = readerFocusCopy(label, english);
        return `<button class="focus-v3-nav-quick" type="button" data-product-view="${view}" aria-label="${esc(
          copy
        )}" title="${esc(copy)}"><span>${productV3Icon(iconName)}</span><b>${esc(
          copy
        )}</b><small title="${parity.deviceCounts.stale && ["meeting", "obsidian"].includes(view) ? "计数暂未更新" : ""}">${count > 99 ? "99+" : count}</small></button>`;
      })
      .join("");
    const library = productV3LibraryViews
      .map(([view, label, english, iconName]) => {
        const count = productV3ViewCount(view);
        const copy = readerFocusCopy(label, english);
        return `<button class="focus-v3-nav-item" type="button" data-product-view="${view}">${productV3Icon(
          iconName
        )}<span>${esc(copy)}</span>${
          count > 0 ? `<small>${count}</small>` : ""
        }</button>`;
      })
      .join("");
    const sourceTotal = (state.sources || []).filter(
      (source) => source.subscribed && !source.hidden
    ).length;
    // 信息库分类视图（与 #view-nav 的阅读视图一一对应：全部内容/订阅文章/
    // 视频/图片）。product-v3-shell 挂载后 #view-nav 被 CSS 永久隐藏，这些
    // 入口是切换聚合阅读视图的唯一路径——缺了它们视图就再也回不去。
    const category = [
      ["all", "全部内容", "All", "all"],
      ["rss", "订阅文章", "Articles", "rss"],
      ["video", "视频", "Videos", "video"],
      ["picture", "图片", "Pictures", "picture"],
    ]
      .map(([view, label, english, iconName]) => {
        const count = productV3ViewCount(view);
        const copy = readerFocusCopy(label, english);
        return `<button class="focus-v3-nav-item focus-v3-nav-category" type="button" data-product-view="${view}">${productV3Icon(
          iconName
        )}<span>${esc(copy)}</span>${
          count > 0 ? `<small>${count}</small>` : ""
        }</button>`;
      })
      .join("");
    const markup = `<div class="focus-v3-nav-top"><div class="focus-v3-nav-brand"><button class="focus-v3-nav-brand-home" type="button" data-product-view="today" aria-label="${readerFocusCopy("进入焦点空间", "Open Focus Space")}" title="${readerFocusCopy("焦点空间", "Focus Space")}"><img src="./static/zettlab-focus-logo.svg" alt=""></button><button type="button" data-product-add-source aria-label="${readerFocusCopy("添加订阅", "Add subscription")}" title="${readerFocusCopy("添加订阅", "Add subscription")}">${icon(
      "plus"
    )}</button></div><div class="focus-v3-nav-quick-list">${quick}</div></div><div class="focus-v3-nav-middle"><nav class="focus-v3-nav-library"><div class="focus-v3-nav-heading">${readerFocusCopy("信息库", "Library")}</div>${library}</nav><nav class="focus-v3-nav-categories">${category}</nav><section class="focus-v3-nav-sources"><div class="focus-v3-nav-heading"><span>${readerFocusCopy("来源", "Sources")}</span><span><button type="button" data-product-refresh aria-label="${readerFocusCopy("刷新内容", "Refresh content")}" title="${readerFocusCopy("刷新内容", "Refresh content")}">${icon(
      "refresh"
    )}</button><small>${sourceTotal}</small></span></div>${parity.countsStale || parity.deviceCounts.stale ? '<div class="focus-v3-nav-source-empty" role="status">数字暂未更新，可刷新重试</div>' : ""}${productV3SourceGroupsMarkup()}</section></div><div class="focus-v3-nav-footer"><div class="focus-v3-appearance" role="group" aria-label="${readerFocusCopy("外观模式", "Appearance")}">${[
      ["light", "浅色", "Light", "sun"],
      ["dark", "深色", "Dark", "moon"],
      ["system", "系统", "System", "layout"],
    ]
      .map(
        ([mode, label, english, iconName]) => {
          const copy = readerFocusCopy(label, english);
          return `<button type="button" data-product-appearance="${mode}" aria-label="${copy}" aria-pressed="false" title="${copy}">${productV3Icon(
            iconName
          )}</button>`;
        }
      )
      .join("")}</div></div>`;
    const mountedNavigationScroller = navigation.querySelector(
      ".focus-v3-nav-middle"
    );
    if (mountedNavigationScroller)
      parity.productV3NavigationScrollTop = mountedNavigationScroller.scrollTop;
    if (parity.productV3NavigationMarkup !== markup) {
      navigation.innerHTML = markup;
      parity.productV3NavigationMarkup = markup;
      const nextNavigationScroller = navigation.querySelector(
        ".focus-v3-nav-middle"
      );
      if (nextNavigationScroller)
        nextNavigationScroller.scrollTop = parity.productV3NavigationScrollTop;
    }
    syncProductV3NavigationState(navigation);
  }

  function ensureFocusV3Surface() {
    renderProductV3Navigation();
    let workspace = $("focus-v3-workspace");
    if (!workspace) {
      const splitter = document.createElement("div");
      splitter.className = "focus-v3-product-splitter";
      splitter.setAttribute("aria-hidden", "true");
      $("reader-shell")?.append(splitter);
      workspace = document.createElement("main");
      workspace.id = "focus-v3-workspace";
      workspace.className = "focus-v3-workspace";
      workspace.setAttribute("aria-live", "polite");
      $("reader-shell")?.append(workspace);
    }
    let actionDrawer = $("focus-v3-action-drawer-layer");
    if (!actionDrawer) {
      actionDrawer = document.createElement("div");
      actionDrawer.id = "focus-v3-action-drawer-layer";
      actionDrawer.className = "focus-v3-action-drawer-layer";
      actionDrawer.hidden = true;
      document.body.append(actionDrawer);
    }
    let focusDrawer = $("focus-v3-drawer-layer");
    if (!focusDrawer) {
      focusDrawer = document.createElement("div");
      focusDrawer.id = "focus-v3-drawer-layer";
      focusDrawer.className = "focus-v3-drawer-layer";
      focusDrawer.hidden = true;
      document.body.append(focusDrawer);
    }
    let modal = $("focus-v3-create-layer");
    if (!modal) {
      modal = document.createElement("div");
      modal.id = "focus-v3-create-layer";
      modal.className = "focus-v3-create-layer";
      modal.hidden = true;
      document.body.append(modal);
    }
    let picker = $("focus-v3-picker-layer");
    if (!picker) {
      picker = document.createElement("div");
      picker.id = "focus-v3-picker-layer";
      picker.className = "focus-v3-picker-layer";
      picker.hidden = true;
      document.body.append(picker);
    }
    return workspace;
  }

  function refreshFocusV3LayerState() {
    const actionLayer = $("focus-v3-action-drawer-layer");
    const focusLayer = $("focus-v3-drawer-layer");
    const actionOpen = Boolean(actionLayer && !actionLayer.hidden);
    const focusOpen = Boolean(focusLayer && !focusLayer.hidden);
    const citationDrawers = citationDrawerElements();
    const topCitation = citationDrawers[citationDrawers.length - 1];
    focusLayer?.classList.toggle("is-above-action", actionOpen);
    actionLayer?.querySelector('[role="dialog"]')?.setAttribute(
      "aria-modal",
      actionOpen && !focusOpen && !citationDrawers.length ? "true" : "false"
    );
    focusLayer?.querySelector('[role="dialog"]')?.setAttribute(
      "aria-modal",
      focusOpen && !citationDrawers.length ? "true" : "false"
    );
    citationDrawers.forEach((drawer) =>
      drawer.setAttribute("aria-modal", drawer === topCitation ? "true" : "false")
    );
    document.body.classList.toggle(
      "focus-v3-drawer-open",
      actionOpen || focusOpen || citationDrawers.length > 0
    );
  }

  function focusTitle(focusID) {
    return text(
      parity.focusSpaces.find((item) => text(item.id) === text(focusID))?.title,
      readerFocusCopy("未命名焦点", "Untitled focus")
    );
  }

  function focusModeBadge(focus) {
    return focus.decision_mode === "sprint"
      ? `<span class="focus-v3-mode sprint">${icon("sparkles")}${readerFocusCopy("冲刺", "Sprint")}</span>`
      : `<span class="focus-v3-mode slow">${icon("focus") || icon("note")}${readerFocusCopy("酝酿", "Slow burn")}</span>`;
  }

  function focusDeadlineBadge(focus) {
    const horizon = text(focus?.horizon_end);
    if (!horizon && focus?.decision_mode !== "sprint") return "";
    const label = horizon
      ? formatDateByLocale(horizon, { year: "numeric", month: "short", day: "numeric" })
      : "";
    return `<span>${label ? `${readerFocusCopy("截止", "Due")} ${esc(label)}` : readerFocusCopy("无截止时间", "No deadline")}</span>`;
  }

  function focusDaysLeft(focus) {
    const raw = text(focus.horizon_end);
    if (!raw) return null;
    const target = new Date(raw);
    if (Number.isNaN(target.getTime())) return null;
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    target.setHours(0, 0, 0, 0);
    return Math.ceil((target.getTime() - today.getTime()) / 86400000);
  }

  function focusNextStepV3(focus) {
    const analysis = focusAnalysisFor(focus.id);
    const decisions = Array.isArray(analysis.decisions) ? analysis.decisions : [];
    const actions = Array.isArray(analysis.actions) ? analysis.actions : [];
    const proposed = decisions.find(
      (item) => !["accepted", "rejected"].includes(text(item.feedback_status))
    );
    if (proposed)
      return {
        text: `${readerFocusCopy("确认决策：", "Confirm decision: ")}${text(focusResultData(proposed).question, readerFocusCopy("待确认判断", "Pending judgment"))}`,
        attention: true,
      };
    const drafts = actions.filter(
      (item) => !["accepted", "edited", "rejected"].includes(text(item.feedback_status))
    );
    if (drafts.length)
      return { text: readerFocusCopy(`确认 ${drafts.length} 条行动草案`, `Confirm ${drafts.length} action drafts`), attention: true };
    const open = actions.find(
      (item) =>
        ["accepted", "edited"].includes(text(item.feedback_status)) &&
        text(item.status) !== "done" &&
        focusResultData(item).completed !== true
    );
    if (open)
      return {
        text: `${readerFocusCopy("完成「", "Complete “")}${text(focusResultData(open).task, readerFocusCopy("下一步行动", "next action"))}${readerFocusCopy("」", "”")}`,
        attention: false,
      };
    if (focus.workflow_state === "done")
      return { text: readerFocusCopy("在回顾日记录实际结果", "Record the actual outcome on the review date"), attention: false };
    if (focus.workflow_state === "archived")
      return { text: readerFocusCopy("无需继续推进", "No further action needed"), attention: false };
    return { text: readerFocusCopy("等待订阅来源产生新变化", "Waiting for new source changes"), attention: false };
  }

  function focusProgressV3(focus) {
    const labels = {
      accumulating: readerFocusCopy("积累", "Accumulating"),
      deciding: readerFocusCopy("决策", "Deciding"),
      executing: readerFocusCopy("执行", "Executing"),
      done: readerFocusCopy("完成", "Done"),
      archived: readerFocusCopy("归档", "Archived"),
    };
    const analysis = focusAnalysisFor(focus.id);
    const evidenceCount =
      (focus.materials?.length || 0) +
      (focus.sources || []).filter((item) => item?.enabled !== false).length +
      (analysis.evidence?.length || 0);
    const actions = Array.isArray(analysis.actions)
      ? analysis.actions.filter((item) => text(item.feedback_status) !== "rejected")
      : [];
    if (focus.workflow_state === "executing" && actions.length) {
      const done = actions.filter(
        (item) => text(item.status) === "done" || focusResultData(item).completed === true
      ).length;
      return `${labels[focus.workflow_state]} · ${readerFocusCopy(`${done}/${actions.length} 项行动完成`, `${done}/${actions.length} actions completed`)}`;
    }
    return `${labels[focus.workflow_state] || focus.workflow_state} · ${
      evidenceCount ? readerFocusCopy(`已纳入 ${evidenceCount} 条资料`, `${evidenceCount} sources included`) : readerFocusCopy("尚未纳入资料", "No sources included")
    }`;
  }

  function focusBoardCardMarkup(focus) {
    const analysis = focusAnalysisFor(focus.id);
    const linkedSourceCount = (focus.sources || []).filter((item) => item?.enabled !== false).length;
    const evidenceCount = (focus.materials?.length || 0) + linkedSourceCount + (analysis.evidence?.length || 0);
    const pendingDecisions = Array.isArray(analysis.decisions)
      ? analysis.decisions.filter((item) => !["accepted", "rejected"].includes(text(item.feedback_status))).length
      : 0;
    const pendingActions = Array.isArray(analysis.actions)
      ? analysis.actions.filter((item) => text(item.feedback_status) !== "rejected" && text(item.status) !== "done").length
      : 0;
    const next = focusNextStepV3(focus);
    const days = focusDaysLeft(focus);
    const sprint = focus.decision_mode === "sprint";
    return `<article class="focus-v3-board-card ${sprint ? "is-sprint" : "is-slow"}" draggable="true" data-focus-v3-card="${esc(focus.id)}" data-open-focus="${esc(focus.id)}"><strong>${esc(text(focus.title, readerFocusCopy("未命名焦点", "Untitled focus")))}</strong><span class="focus-v3-board-next ${next.attention ? "attention" : ""}">${esc(next.text)}</span><span class="focus-v3-board-meta"><i class="focus-v3-board-chip ${sprint ? "sprint" : "slow"}">${sprint ? `${days === null ? readerFocusCopy("未定期限", "No due date") : Math.abs(days)}${productV3Icon("sparkles")}` : `${readerFocusCopy("酝酿", "Slow burn")}${productV3Icon("focus")}`}</i>${evidenceCount ? `<i>${productV3Icon("note")}${evidenceCount}</i>` : ""}${pendingDecisions ? `<i class="attention">${productV3Icon("decision")}${pendingDecisions}</i>` : ""}${pendingActions ? `<i>${productV3Icon("action")}${pendingActions}</i>` : ""}</span></article>`;
  }

  function focusFlowRowMarkup(focus, dimmed = false) {
    const sprint = focus.decision_mode === "sprint";
    const days = focusDaysLeft(focus);
    const due = days === null ? "" : days < 0 ? readerFocusCopy(`已逾期 ${-days} 天`, `${-days} days overdue`) : days === 0 ? readerFocusCopy("今天截止", "Due today") : readerFocusCopy(`还剩 ${days} 天`, `${days} days left`);
    const next = focusNextStepV3(focus);
    return `<button class="focus-v3-flow-row ${dimmed ? "dimmed" : ""}" type="button" data-open-focus="${esc(focus.id)}"><span class="focus-v3-flow-top"><i class="focus-v3-flow-type ${sprint ? "sprint" : "slow"}">${productV3Icon(sprint ? "sparkles" : "focus")}${readerFocusCopy(sprint ? "冲刺" : "酝酿", sprint ? "Sprint" : "Slow burn")}</i><strong>${esc(text(focus.title, readerFocusCopy("未命名焦点", "Untitled focus")))}</strong>${due ? `<i class="focus-v3-flow-due ${days <= 3 ? "urgent" : ""}">${due}</i>` : ""}${productV3Icon("arrow")}</span><span class="focus-v3-flow-fields"><span><i>${readerFocusCopy("目标", "Goal")}</i><b title="${esc(text(focus.goal))}">${esc(text(focus.goal, readerFocusCopy("尚未填写目标", "No goal set")))}</b></span><span><i>${readerFocusCopy("当前进展", "Progress")}</i><b>${esc(focusProgressV3(focus))}</b></span><span class="${next.attention ? "attention" : ""}"><i>${readerFocusCopy("下一步", "Next step")}</i><b>${esc(next.text)}</b></span></span></button>`;
  }

  function focusBoardMarkup() {
    if (!parity.focusSpaces.length) return `<div class="focus-v3-empty"><span class="focus-v3-empty-mascot" aria-hidden="true"><svg class="focus-v3-mascot-svg" viewBox="0 0 160 160" role="img" aria-label="${readerFocusCopy("皮卡丘", "Pikachu")}"><path d="M38 58 20 18l37 19c9-5 20-8 32-8s23 3 32 8l37-19-18 40c9 10 14 23 14 37 0 32-29 52-65 52S24 127 24 95c0-14 5-27 14-37Z" fill="#f6c945"/><path d="m20 18 18 40M140 18l-18 40" stroke="#c78a19" stroke-width="5" stroke-linecap="round"/><circle cx="61" cy="87" r="7" fill="#2b2520"/><circle cx="99" cy="87" r="7" fill="#2b2520"/><circle cx="48" cy="108" r="10" fill="#e95b45"/><circle cx="112" cy="108" r="10" fill="#e95b45"/><path d="M73 111q7 8 14 0" fill="none" stroke="#2b2520" stroke-width="4" stroke-linecap="round"/><path d="m127 65 13-8-9 15 14 3-18 5" fill="none" stroke="#f0a51c" stroke-width="5" stroke-linecap="round" stroke-linejoin="round"/></svg></span><h3>${readerFocusCopy("皮卡丘正在等你", "Pikachu is waiting for you")}</h3><p>${readerFocusCopy("这里暂时没有需要展示的焦点，先建立一个焦点吧。", "Nothing needs your attention yet. Create a focus to get started.")}</p><button class="primary-btn" type="button" data-create-focus>${readerFocusCopy("新建焦点", "Add focus")}</button></div>`;
    if (parity.focusViewMode === "list") {
      const active = parity.focusSpaces.filter((focus) => !["done", "archived"].includes(focus.workflow_state));
      const closed = parity.focusSpaces.filter((focus) => ["done", "archived"].includes(focus.workflow_state));
      return `<div class="focus-v3-flow">${active.map((focus) => focusFlowRowMarkup(focus)).join("")}${closed.length ? `<details class="focus-v3-flow-closed"><summary>${productV3Icon("arrow")}${readerFocusCopy("已完成", "Done")} ${closed.filter((item) => item.workflow_state === "done").length} · ${readerFocusCopy("已归档", "Archived")} ${closed.filter((item) => item.workflow_state === "archived").length}</summary>${closed.map((focus) => focusFlowRowMarkup(focus, true)).join("")}</details>` : ""}</div>`;
    }
    const columns = focusWorkflowColumns.filter(([id]) => id !== "archived");
    const lanes = [["sprint", readerFocusCopy("冲刺", "Sprint"), "sparkles"], ["slow_burn", readerFocusCopy("酝酿", "Slow burn"), "focus"]];
    const archived = parity.focusSpaces.filter((focus) => focus.workflow_state === "archived").length;
    return `<div class="focus-v3-board-wrap"><div class="focus-v3-board"><div class="focus-v3-board-columns">${columns.map(([id, label, english]) => `<div>${productV3Icon(id === "accumulating" ? "inbox" : id === "deciding" ? "decision" : id === "executing" ? "video" : "check")}<b>${readerFocusCopy(label, english)}</b><span>${parity.focusSpaces.filter((focus) => focus.workflow_state === id).length}</span></div>`).join("")}</div>${lanes.map(([mode, label, iconName]) => `<section class="focus-v3-board-lane"><header class="${mode}">${productV3Icon(iconName)}<b>${label}</b><span>${parity.focusSpaces.filter((focus) => focus.decision_mode === mode).length}</span></header><div class="focus-v3-board-cells">${columns.map(([id]) => { const items = parity.focusSpaces.filter((focus) => focus.decision_mode === mode && focus.workflow_state === id); return `<div class="focus-v3-board-cell ${mode} ${items.length ? "" : "empty"}" data-focus-lane="${id}" data-focus-mode="${mode}">${items.map(focusBoardCardMarkup).join("")}</div>`; }).join("")}</div></section>`).join("")}</div><button class="focus-v3-archive-strip" type="button" data-product-archive>${productV3Icon("inbox")}<span>${archived}</span></button></div>`;
  }

  function queueResultMarkup(item, kind) {
    const data = focusResultData(item);
    const title = kind === "action" ? text(data.task) : kind === "decision" ? text(data.question) : text(data.title);
    const detail = kind === "action" ? [text(data.owner), text(data.due_at)].filter(Boolean).join(" · ") : kind === "decision" ? text(data.recommendation) : text(data.detail);
    const feedbackStatus = text(item.feedback_status, "draft");
    const actionDone = kind === "action" && (text(item.status) === "done" || data.completed === true);
    const resolvedStatus = actionDone
      ? `<span>${esc(readerFocusCopy("已完成", "Completed"))}</span>`
      : ["accepted", "rejected", "edited"].includes(feedbackStatus)
        ? `<span>${esc(focusResultStatusLabel(feedbackStatus))}</span>`
        : `<button class="text-btn" type="button" data-focus-feedback="accepted">${icon("check")}${readerFocusCopy("接受", "Accept")}</button><button class="text-btn" type="button" data-focus-feedback="rejected">${icon("close")}${readerFocusCopy("拒绝", "Reject")}</button><button class="text-btn" type="button" data-focus-feedback="snoozed">${readerFocusCopy("稍后", "Later")}</button>`;
    return `<article class="focus-v3-queue-row" data-focus-result-card data-focus-result-kind="${kind}" data-focus-result-id="${esc(item.id)}" data-focus-result-revision="${Number(item.revision) || 1}"><button type="button" data-open-focus="${esc(item.focus_id)}"><span class="focus-v3-queue-icon">${icon(kind)}</span><span><strong>${esc(title || readerFocusCopy("未命名项目", "Untitled item"))}</strong><small>${esc(focusTitle(item.focus_id))}${detail ? ` · ${esc(detail)}` : ""}</small></span>${icon("arrow")}</button><div class="focus-v3-queue-actions">${resolvedStatus}</div></article>`;
  }

  function focusActionConfirmation(item) {
    const status = text(item?.feedback_status, "draft").toLowerCase();
    return status === "edited" ? "accepted" : status;
  }

  function focusActionDone(item) {
    const data = focusResultData(item);
    return text(item?.status).toLowerCase() === "done" || data.completed === true;
  }

  function focusActionStatusLabel(item) {
    if (focusActionDone(item)) return readerFocusCopy("已完成", "Completed");
    return {
      draft: readerFocusCopy("待确认", "Pending confirmation"),
      accepted: readerFocusCopy("已确认", "Confirmed"),
      snoozed: readerFocusCopy("稍后处理", "Snoozed"),
      rejected: readerFocusCopy("已拒绝", "Rejected"),
    }[focusActionConfirmation(item)] || readerFocusCopy("待确认", "Pending confirmation");
  }

  function focusActionCardMarkup(item, layout = "board") {
    const data = focusResultData(item);
    const confirmation = focusActionConfirmation(item);
    const done = focusActionDone(item);
    const title = text(data.task, readerFocusCopy("未命名行动", "Untitled action"));
    const detail = [focusTitle(item.focus_id), text(data.due_at) ? `${readerFocusCopy("截止", "Due")} ${text(data.due_at)}` : ""]
      .filter(Boolean)
      .join(" · ");
    const stateMark = confirmation === "accepted"
      ? `<button class="focus-v3-action-state is-checkbox ${done ? "is-checked" : ""}" type="button" role="checkbox" aria-checked="${done}" data-action-toggle="${esc(item.id)}" data-action-completed="${done}" aria-label="${esc(done ? readerFocusCopy("恢复未完成", "Mark as not done") : readerFocusCopy("标记完成", "Mark complete"))}">${done ? icon("check") : ""}</button>`
      : `<span class="focus-v3-action-state is-${esc(confirmation)}"></span>`;
    const quickActions = confirmation === "draft"
      ? `<div class="focus-v3-action-quick"><button class="is-primary" type="button" data-action-feedback="accepted">${icon("check")}${readerFocusCopy("接受", "Accept")}</button><button class="is-danger" type="button" data-action-feedback="rejected">${icon("close")}${readerFocusCopy("拒绝", "Reject")}</button></div>`
      : confirmation === "snoozed"
        ? `<div class="focus-v3-action-quick"><button type="button" data-action-feedback="accepted">${icon("refresh")}${readerFocusCopy("恢复确认", "Restore confirmation")}</button></div>`
        : confirmation === "rejected"
          ? `<div class="focus-v3-action-quick"><button type="button" data-action-feedback="accepted">${icon("refresh")}${readerFocusCopy("恢复为草案", "Restore draft")}</button></div>`
          : "";
    return `<article class="focus-v3-action-card ${done ? "is-done" : ""} is-${esc(layout)}" data-focus-result-card data-focus-result-kind="action" data-focus-result-id="${esc(item.id)}" data-focus-result-revision="${Number(item.revision) || 1}" data-action-confirmation="${esc(confirmation)}"><div class="focus-v3-action-summary">${stateMark}<button class="focus-v3-action-open" type="button" data-open-action="${esc(item.id)}"><span><strong>${esc(title)}</strong><small>${esc(detail)}</small></span><em>${esc(focusActionStatusLabel(item))}</em></button></div>${quickActions}</article>`;
  }

  function focusActionBoardMarkup(items) {
    const filtered = parity.actionFocusFilter === "all"
      ? items
      : items.filter((item) => text(item.focus_id) === text(parity.actionFocusFilter));
    const visible = filtered.filter((item) => focusActionConfirmation(item) !== "rejected");
    const rejected = filtered.filter((item) => focusActionConfirmation(item) === "rejected");
    const lanes = [
      ["pending", readerFocusCopy("待确认", "Pending"), (item) => focusActionConfirmation(item) === "draft"],
      ["progress", readerFocusCopy("进行中", "In progress"), (item) => focusActionConfirmation(item) === "accepted" && !focusActionDone(item)],
      ["done", readerFocusCopy("已完成", "Completed"), (item) => focusActionConfirmation(item) === "accepted" && focusActionDone(item)],
      ["deferred", readerFocusCopy("已推迟", "Snoozed"), (item) => focusActionConfirmation(item) === "snoozed"],
    ];
    const counts = lanes.map(([, , match]) => visible.filter(match).length);
    const focusOptions = parity.focusSpaces
      .map((focus) => `<option value="${esc(focus.id)}" ${text(parity.actionFocusFilter) === text(focus.id) ? "selected" : ""}>${esc(focus.title)}</option>`)
      .join("");
    const toolbar = `<div class="focus-v3-action-toolbar"><div class="focus-v3-action-toolbar-left"><div class="focus-v3-action-switch" role="group" aria-label="${readerFocusCopy("行动项视图", "Action view")}"><button class="${parity.actionViewMode === "board" ? "active" : ""}" type="button" data-action-view-mode="board">${productV3Icon("layout")}${readerFocusCopy("看板", "Board")}</button><button class="${parity.actionViewMode === "list" ? "active" : ""}" type="button" data-action-view-mode="list">${productV3Icon("action")}${readerFocusCopy("列表", "List")}</button></div><label class="focus-v3-action-filter">${productV3Icon("focus")}<span>${readerFocusCopy("焦点", "Focus")}</span><select data-action-focus-filter aria-label="${readerFocusCopy("按焦点筛选行动项", "Filter actions by focus")}"><option value="all">${readerFocusCopy("全部焦点", "All focuses")}</option>${focusOptions}</select></label></div><div class="focus-v3-action-metrics"><span><b>${counts[0]}</b> ${lanes[0][1]}</span><span><b>${counts[1]}</b> ${lanes[1][1]}</span><span><b>${counts[2]}</b> ${lanes[2][1]}</span><span><b>${counts[3]}</b> ${lanes[3][1]}</span></div></div>`;
    const board = parity.actionViewMode === "list"
      ? `<div class="focus-v3-action-list">${visible.map((item) => focusActionCardMarkup(item, "list")).join("") || `<p class="focus-v3-action-empty">${readerFocusCopy("暂无行动", "No actions yet")}</p>`}</div>`
      : `<div class="focus-v3-action-board">${lanes.map(([id, label, match]) => { const laneItems = visible.filter(match); return `<section data-action-lane="${id}"><header><strong>${label}</strong><span>${laneItems.length}</span></header><div>${laneItems.map((item) => focusActionCardMarkup(item)).join("") || `<p>${readerFocusCopy("暂无行动", "No actions yet")}</p>`}</div></section>`; }).join("")}</div>`;
    const history = rejected.length
      ? `<details class="focus-v3-action-rejected"><summary>${productV3Icon("inbox")}<span>${readerFocusCopy("拒绝记录", "Rejected")}</span><b>${rejected.length}</b><em>${readerFocusCopy("保留原因，可恢复或重新拟定", "Reasons are kept; restore or redraft when needed")}</em>${productV3Icon("chevron")}</summary><div>${rejected.map((item) => focusActionCardMarkup(item, "rejected")).join("")}</div></details>`
      : "";
    return `<section class="focus-v3-action-space">${toolbar}${board}${history}</section>`;
  }

  function focusQueueMarkup(view) {
    const kind = view === "decision" ? "decision" : view === "action" ? "action" : "signal";
    const labels = { decision: [readerFocusCopy("待决策", "Decisions"), readerFocusCopy("集中确认 AI 提出的判断与依据", "Review and confirm the reasoning and evidence")], action: [readerFocusCopy("待办", "To-dos"), readerFocusCopy("确认、推进并完成真实待办", "Confirm, progress and complete real to-dos")], signal: [readerFocusCopy("风险与信号", "Risks and signals"), readerFocusCopy("查看新变化、风险和反向证据", "Review changes, risks and counter-evidence")] };
    const items = allFocusResults(kind).filter((item) => text(item.feedback_status) !== "rejected" && (kind !== "decision" || !["accepted", "edited"].includes(text(item.feedback_status))));
    if (kind === "action") {
      return focusActionBoardMarkup(allFocusResults(kind));
    }
    return `<section class="focus-v3-queue"><header><span class="focus-v3-eyebrow">${readerFocusCopy("焦点空间", "Focus Space")}</span><h2>${labels[kind][0]}</h2><p>${labels[kind][1]}</p><strong>${items.length} ${readerFocusCopy("项", "items")}</strong></header><div class="focus-v3-queue-list">${items.map((item) => queueResultMarkup(item, kind)).join("") || `<div class="focus-v3-empty compact"><h3>${readerFocusCopy(`当前没有${labels[kind][0]}项`, `No ${labels[kind][0].toLowerCase()} yet`)}</h3><p>${readerFocusCopy("这里只显示板子真实分析结果，不使用占位数据。", "Only real analysis results are shown here; no placeholders.")}</p></div>`}</div></section>`;
  }

  function actionDecisionFor(item) {
    const data = focusResultData(item);
    const decisionID = text(item?.decision_id || data.decision_id);
    const decisions = allFocusResults("decision").filter(
      (decision) => text(decision.focus_id) === text(item.focus_id)
    );
    return decisions.find((decision) => text(decision.id) === decisionID) ||
      decisions.find((decision) => ["accepted", "edited"].includes(text(decision.feedback_status))) ||
      null;
  }

  function closeFocusV3ActionDrawer() {
    const layer = $("focus-v3-action-drawer-layer");
    if (layer) {
      layer.hidden = true;
      layer.innerHTML = "";
    }
    parity.selectedActionID = "";
    refreshFocusV3LayerState();
  }

  function openFocusV3ActionDrawer(actionID) {
    const item = allFocusResults("action").find((action) => text(action.id) === text(actionID));
    if (!item) return;
    const data = focusResultData(item);
    const focus = parity.focusSpaces.find((entry) => text(entry.id) === text(item.focus_id));
    const decision = actionDecisionFor(item);
    const decisionData = focusResultData(decision);
    const citations = focusResultCitations(item);
    const workflow = focusWorkflowColumns.find(([id]) => id === focus?.workflow_state);
    const workflowLabel = workflow
      ? readerFocusCopy(workflow[1], workflow[2])
      : text(focus?.workflow_state, readerFocusCopy("推进中", "In progress"));
    const accepted = focusActionConfirmation(item) === "accepted";
    const done = focusActionDone(item);
    const origin = text(item.origin || data.origin).toLowerCase() === "extracted" ? readerFocusCopy("从内容提取", "Extracted from content") : readerFocusCopy("由决策生成", "Generated by decision");
    const rationaleTitle = text(decisionData.recommendation || decisionData.question || data.rationale, accepted ? readerFocusCopy("由焦点中的已确认结论拆解", "Derived from the confirmed focus decision") : readerFocusCopy("候选行动尚未形成已确认决策", "Candidate action has no confirmed decision yet"));
    const rationaleDetail = text(decisionData.expected_outcome || decisionData.detail || data.expected_outcome, accepted ? readerFocusCopy("预期结果由当前焦点决策与资料共同确定。", "The expected outcome is grounded in the focus decision and sources.") : readerFocusCopy("接受后会保留行动与判断依据的关系。", "Accepting it will preserve the link between the action and its rationale."));
    const citationMarkup = citations.length
      ? `<div class="focus-v3-action-evidence">${citations.map((citation, index) => `<button type="button" ${focusCitationDataAttributes(citation)}><span>${icon("note")}</span><b>${esc(text(citation.title || citation.source_id, readerFocusCopy(`来源 ${index + 1}`, `Source ${index + 1}`)))}</b>${icon("arrow")}</button>`).join("")}</div>`
      : `<small>${readerFocusCopy("该行动暂无直接引用，依据来自焦点结论。", "This action has no direct citation; its rationale comes from the focus conclusion.")}</small>`;
    parity.selectedActionID = item.id;
    const layer = ensureFocusV3Surface() && $("focus-v3-action-drawer-layer");
    layer.hidden = false;
    layer.innerHTML = `<button class="focus-v3-drawer-mask" type="button" data-close-action aria-label="${readerFocusCopy("关闭行动项详情", "Close action details")}"></button><aside class="focus-v3-drawer focus-v3-action-drawer" role="dialog" aria-modal="true" aria-label="${readerFocusCopy("行动项详情", "Action details")}" tabindex="-1"><header><div class="focus-v3-drawer-head-copy"><span class="focus-v3-action-eyebrow">${readerFocusCopy("行动项", "Action")}</span><h2>${esc(text(data.task, readerFocusCopy("未命名行动", "Untitled action")))}</h2></div><button class="icon-btn" type="button" data-close-action aria-label="${readerFocusCopy("关闭行动项详情", "Close action details")}" title="${readerFocusCopy("关闭", "Close")}">${icon("close")}</button></header><div class="focus-v3-action-drawer-body"><div class="focus-v3-action-status-row"><span class="is-${esc(focusActionConfirmation(item))}">${esc(focusActionStatusLabel(item))}</span><em>${esc(origin)}</em>${accepted ? `<label><button class="focus-v3-action-state is-checkbox ${done ? "is-checked" : ""}" type="button" role="checkbox" aria-checked="${done}" data-action-toggle="${esc(item.id)}" data-action-completed="${done}">${done ? icon("check") : ""}</button>${readerFocusCopy("已完成", "Completed")}</label>` : ""}</div>${focusActionConfirmation(item) === "draft" ? `<div class="focus-v3-action-drawer-actions"><button class="is-primary" type="button" data-action-feedback="accepted">${icon("check")}${readerFocusCopy("接受", "Accept")}</button><button class="is-danger" type="button" data-action-feedback="rejected">${icon("close")}${readerFocusCopy("拒绝", "Reject")}</button></div>` : ""}<section><span class="focus-v3-action-context-label">${productV3Icon("focus")}${readerFocusCopy("关联焦点", "Related focus")}</span>${focus ? `<button class="focus-v3-action-context-link" type="button" data-open-action-focus="${esc(focus.id)}">${esc(focus.title)}${icon("arrow")}</button><p>${esc(text(focus.goal, readerFocusCopy("尚未填写焦点目标", "No focus goal yet")))}</p><small>${esc(workflowLabel)} · ${focus.horizon_end ? `${readerFocusCopy("目标日期", "Target date")} ${esc(text(focus.horizon_end).slice(0, 10))}` : readerFocusCopy("未设目标日期", "No target date")}</small>` : `<p>${readerFocusCopy("关联焦点已不可用", "Related focus is unavailable")}</p>`}</section><section><span class="focus-v3-action-context-label">${productV3Icon("decision")}${readerFocusCopy("行动项判断依据", "Action rationale")}</span><strong>${esc(rationaleTitle)}</strong><p>${esc(rationaleDetail)}</p>${citationMarkup}</section></div></aside>`;
    layer.querySelectorAll("[data-close-action]").forEach((node) => node.addEventListener("click", closeFocusV3ActionDrawer));
    layer.querySelector("[data-open-action-focus]")?.addEventListener("click", (event) => {
      const focusID = event.currentTarget.dataset.openActionFocus;
      openFocusV3Drawer(focusID, "overview");
    });
    layer.querySelectorAll("[data-action-feedback]").forEach((node) => node.addEventListener("click", async () => {
      await submitFocusResultFeedback(item, node.dataset.actionFeedback, null, layer.querySelector(".focus-v3-action-drawer"));
      closeFocusV3ActionDrawer();
      renderFocusV3Workspace();
    }));
    layer.querySelector("[data-action-toggle]")?.addEventListener("click", async (event) => {
      await submitFocusResultFeedback(item, "edited", { completed: event.currentTarget.dataset.actionCompleted !== "true" }, layer.querySelector(".focus-v3-action-drawer"));
      closeFocusV3ActionDrawer();
      renderFocusV3Workspace();
    });
    bindFocusSpaceOverviewEvents(layer);
    refreshFocusV3LayerState();
    requestAnimationFrame(() => layer.querySelector(".focus-v3-action-drawer")?.focus({ preventScroll: true }));
  }

  function focusWorkflowGraphState(focusID) {
    const id = text(focusID);
    if (!parity.focusWorkflowStates.has(id)) {
      parity.focusWorkflowStates.set(id, {
        expanded: false,
        fullscreen: false,
        selectedNodeID: "",
        zoom: 0,
        pan: null,
        fitPending: true,
      });
    }
    return parity.focusWorkflowStates.get(id);
  }

  function focusWorkflowBuilder(intentKind) {
    const nodes = [];
    const edges = [];
    const nodeIDs = new Set();
    const edgeIDs = new Set();
    const addNode = (node) => {
      const id = text(node?.id);
      if (!id || nodeIDs.has(id)) return null;
      nodeIDs.add(id);
      const value = { layer: 0, order: nodes.length, ...node, id };
      nodes.push(value);
      return value;
    };
    const addEdge = (source, target, label = "", relation = "leads_to") => {
      const from = text(source);
      const to = text(target);
      if (!from || !to || from === to || !nodeIDs.has(from) || !nodeIDs.has(to)) return;
      const base = `${from}__${to}__${relation}`;
      let id = base;
      let suffix = 2;
      while (edgeIDs.has(id)) id = `${base}__${suffix++}`;
      edgeIDs.add(id);
      edges.push({ id, source: from, target: to, label, relation });
    };
    return { intentKind, nodes, edges, addNode, addEdge };
  }

  function focusWorkflowEvidenceNode(item, index, layer = 0) {
    const citation = normalizeFocusCitation({
      ...item,
      source_id: item?.source_id || item?.id,
      quote: item?.quote || item?.excerpt,
      role: item?.evidence_role || "support",
    });
    return {
      id: `evidence:${text(item?.id || item?.source_id, String(index + 1))}`,
      kind: "evidence",
      label: text(item?.title, readerFocusCopy(`资料 ${index + 1}`, `Source ${index + 1}`)),
      detail: text(item?.quote || item?.excerpt),
      layer,
      citation,
    };
  }

  function focusWorkflowRootNode(focus, layer = 0) {
    return {
      id: "focus:goal",
      kind: "goal",
      label: text(focus?.goal, focus?.title, readerFocusCopy("当前目标", "Current goal")),
      detail: text(focus?.decision_question),
      layer,
    };
  }

  function focusWorkflowProductNode(item, layer, index = 0) {
    const data = focusResultData(item);
    return {
      id: `work_product:${text(item?.id, String(index + 1))}`,
      kind: "work_product",
      label: text(data.title, focusWorkProductTypeLabel(data.type)),
      detail: text(data.summary),
      layer,
      item,
    };
  }

  function focusWorkflowDecisionNode(item, layer, index = 0) {
    const data = focusResultData(item);
    return {
      id: `decision:${text(item?.id, String(index + 1))}`,
      kind: "decision",
      label: text(data.recommendation, data.question, readerFocusCopy("判断", "Decision")),
      detail: text(data.question),
      layer,
      item,
    };
  }

  function focusWorkflowActionNode(item, layer, index = 0) {
    const data = focusResultData(item);
    return {
      id: `action:${text(item?.id, String(index + 1))}`,
      kind: "action",
      label: text(data.task, readerFocusCopy("行动", "Action")),
      detail: [text(data.owner), text(data.due_at)].filter(Boolean).join(" · "),
      layer,
      item,
    };
  }

  function focusWorkflowSignalNode(item, layer, index = 0) {
    const data = focusResultData(item);
    return {
      id: `signal:${text(item?.id, String(index + 1))}`,
      kind: "signal",
      label: text(data.title, readerFocusCopy("信号", "Signal")),
      detail: text(data.detail),
      layer,
      item,
    };
  }

  function projectDecisionWorkflow(focus, analysis, expanded = false) {
    const graph = focusWorkflowBuilder("decide");
    const evidence = (analysis?.evidence || []).slice(0, 24);
    const decisions = (analysis?.decisions || []).filter((item) => text(item.feedback_status) !== "rejected").slice(0, 4);
    const actions = (analysis?.actions || []).filter((item) => text(item.feedback_status) !== "rejected").slice(0, 10);
    const evidenceNodes = expanded ? evidence.map((item, index) => graph.addNode(focusWorkflowEvidenceNode(item, index, 0))).filter(Boolean) : [];
    const root = graph.addNode(focusWorkflowRootNode(focus, expanded ? 1 : 0));
    const optionNodes = [];
    decisions.forEach((item, decisionIndex) => {
      const data = focusResultData(item);
      const options = Array.isArray(data.options) && data.options.length ? data.options.slice(0, 8) : [data.question];
      options.forEach((option, optionIndex) => {
        const node = graph.addNode({
          id: `option:${text(item.id, String(decisionIndex + 1))}:${optionIndex + 1}`,
          kind: "option",
          label: text(option, readerFocusCopy("判断标准", "Criterion")),
          detail: text(data.question),
          layer: expanded ? 2 : 1,
          item,
        });
        if (node) optionNodes.push(node);
      });
    });
    const decisionNodes = decisions.map((item, index) => graph.addNode(focusWorkflowDecisionNode(item, expanded ? 3 : 2, index))).filter(Boolean);
    const actionNodes = actions.map((item, index) => graph.addNode(focusWorkflowActionNode(item, expanded ? 4 : 3, index))).filter(Boolean);
    if (!optionNodes.length) optionNodes.push(graph.addNode({ id: "option:criterion", kind: "option", label: readerFocusCopy("判断标准与方案", "Criteria and options"), detail: text(focus?.decision_question), layer: expanded ? 2 : 1 }));
    optionNodes.filter(Boolean).forEach((node) => {
      if (expanded && evidenceNodes.length) evidenceNodes.forEach((source) => graph.addEdge(source.id, node.id, readerFocusCopy("支持", "supports"), "supports"));
      else graph.addEdge(root.id, node.id, readerFocusCopy("形成方案", "frames"), "leads_to");
    });
    decisionNodes.forEach((decision) => {
      optionNodes.filter(Boolean).forEach((option) => graph.addEdge(option.id, decision.id, readerFocusCopy("比较", "compares"), "supports"));
      actionNodes.forEach((action) => graph.addEdge(decision.id, action.id, readerFocusCopy("生成", "leads to"), "leads_to"));
    });
    if (!decisionNodes.length) optionNodes.filter(Boolean).forEach((node) => actionNodes.forEach((action) => graph.addEdge(node.id, action.id, readerFocusCopy("转为", "becomes"), "leads_to")));
    return graph;
  }

  function projectPlanWorkflow(focus, analysis, expanded = false) {
    const graph = focusWorkflowBuilder("plan");
    const evidenceNodes = expanded ? (analysis?.evidence || []).slice(0, 20).map((item, index) => graph.addNode(focusWorkflowEvidenceNode(item, index, 0))).filter(Boolean) : [];
    const root = graph.addNode(focusWorkflowRootNode(focus, expanded ? 1 : 0));
    const products = focusWorkProducts(analysis).filter((item) => text(item.feedback_status || item.status) !== "rejected").slice(0, 4);
    const decisions = (analysis?.decisions || []).filter((item) => text(item.feedback_status) !== "rejected").slice(0, 4);
    const milestones = [
      ...products.map((item, index) => graph.addNode(focusWorkflowProductNode(item, expanded ? 2 : 1, index))),
      ...decisions.map((item, index) => graph.addNode({ ...focusWorkflowDecisionNode(item, expanded ? 2 : 1, index), kind: "milestone" })),
    ].filter(Boolean);
    if (!milestones.length) milestones.push(graph.addNode({ id: "milestone:plan", kind: "milestone", label: readerFocusCopy("计划与里程碑", "Plan and milestones"), detail: text(focus?.decision_question), layer: expanded ? 2 : 1 }));
    const actions = (analysis?.actions || []).filter((item) => text(item.feedback_status) !== "rejected").slice(0, 16).map((item, index) => graph.addNode(focusWorkflowActionNode(item, expanded ? 3 : 2, index))).filter(Boolean);
    milestones.filter(Boolean).forEach((milestone) => {
      graph.addEdge(root.id, milestone.id, readerFocusCopy("拆解", "breaks down"), "leads_to");
      if (expanded) evidenceNodes.forEach((source) => graph.addEdge(source.id, milestone.id, readerFocusCopy("约束", "informs"), "supports"));
      actions.forEach((action) => graph.addEdge(milestone.id, action.id, readerFocusCopy("推进", "drives"), "leads_to"));
    });
    return graph;
  }

  function focusMarkdownHeadings(markdown) {
    return text(markdown).split("\n").map((line) => {
      const match = line.match(/^#{1,4}\s+(.+)$/);
      return match ? text(match[1]).slice(0, 160) : "";
    }).filter(Boolean).slice(0, 20);
  }

  function projectCreateWorkflow(focus, analysis, expanded = false) {
    const graph = focusWorkflowBuilder("create");
    const root = graph.addNode(focusWorkflowRootNode(focus, 0));
    const products = focusWorkProducts(analysis).filter((item) => text(item.feedback_status || item.status) !== "rejected").slice(0, 4);
    const briefs = products.filter((item) => text(focusResultData(item).type) === "writing_brief");
    const outlines = products.filter((item) => text(focusResultData(item).type) === "outline");
    const others = products.filter((item) => !["writing_brief", "outline"].includes(text(focusResultData(item).type)));
    const briefNodes = (briefs.length ? briefs : others.slice(0, 1)).map((item, index) => graph.addNode(focusWorkflowProductNode(item, 1, index))).filter(Boolean);
    if (!briefNodes.length) briefNodes.push(graph.addNode({ id: "work_product:brief", kind: "work_product", label: readerFocusCopy("主题与 Brief", "Topic and brief"), detail: text(focus?.decision_question), layer: 1 }));
    const outlineItems = outlines.length ? outlines : others.slice(briefs.length ? 0 : 1);
    const outlineNodes = outlineItems.map((item, index) => graph.addNode(focusWorkflowProductNode(item, 2, index))).filter(Boolean);
    if (!outlineNodes.length) outlineNodes.push(graph.addNode({ id: "work_product:outline", kind: "work_product", label: readerFocusCopy("内容大纲", "Content outline"), detail: readerFocusCopy("等待分析生成大纲", "Waiting for an outline"), layer: 2 }));
    const sectionNodes = [];
    outlineItems.forEach((item, productIndex) => {
      focusMarkdownHeadings(focusResultData(item).content_markdown).forEach((heading, headingIndex) => {
        const node = graph.addNode({ id: `section:${productIndex + 1}:${headingIndex + 1}`, kind: "section", label: heading, detail: text(focusResultData(item).title), layer: 3, item });
        if (node) sectionNodes.push(node);
      });
    });
    graph.addEdge(root.id, briefNodes[0].id, readerFocusCopy("定义", "defines"), "leads_to");
    briefNodes.forEach((brief) => outlineNodes.filter(Boolean).forEach((outline) => graph.addEdge(brief.id, outline.id, readerFocusCopy("展开", "develops"), "leads_to")));
    outlineNodes.filter(Boolean).forEach((outline) => sectionNodes.forEach((section) => graph.addEdge(outline.id, section.id, readerFocusCopy("包含", "contains"), "leads_to")));
    if (expanded) {
      const evidenceNodes = (analysis?.evidence || []).slice(0, 20).map((item, index) => graph.addNode(focusWorkflowEvidenceNode(item, index, 4))).filter(Boolean);
      const targets = sectionNodes.length ? sectionNodes : outlineNodes.filter(Boolean);
      evidenceNodes.forEach((source, index) => {
        const target = targets[index % Math.max(1, targets.length)];
        if (target) graph.addEdge(target.id, source.id, readerFocusCopy("引用", "references"), "references");
      });
    }
    return graph;
  }

  function projectMonitorWorkflow(focus, analysis, expanded = false) {
    const graph = focusWorkflowBuilder("monitor");
    const evidenceNodes = expanded ? (analysis?.evidence || []).slice(0, 24).map((item, index) => graph.addNode(focusWorkflowEvidenceNode(item, index, 0))).filter(Boolean) : [];
    const root = graph.addNode(focusWorkflowRootNode(focus, expanded ? 1 : 0));
    const signals = (analysis?.signals || []).filter((item) => text(item.feedback_status) !== "rejected").slice(0, 16).map((item, index) => graph.addNode(focusWorkflowSignalNode(item, expanded ? 2 : 1, index))).filter(Boolean);
    const decisions = (analysis?.decisions || []).filter((item) => text(item.feedback_status) !== "rejected").slice(0, 6).map((item, index) => graph.addNode(focusWorkflowDecisionNode(item, expanded ? 3 : 2, index))).filter(Boolean);
    const actions = (analysis?.actions || []).filter((item) => text(item.feedback_status) !== "rejected").slice(0, 10).map((item, index) => graph.addNode(focusWorkflowActionNode(item, expanded ? 4 : 3, index))).filter(Boolean);
    signals.forEach((signal) => {
      if (expanded && evidenceNodes.length) evidenceNodes.forEach((source) => graph.addEdge(source.id, signal.id, readerFocusCopy("产生", "produces"), "supports"));
      else graph.addEdge(root.id, signal.id, readerFocusCopy("关注", "tracks"), "leads_to");
      decisions.forEach((decision) => graph.addEdge(signal.id, decision.id, readerFocusCopy("触发复核", "triggers review"), "leads_to"));
    });
    decisions.forEach((decision) => actions.forEach((action) => graph.addEdge(decision.id, action.id, readerFocusCopy("建议", "suggests"), "leads_to")));
    if (!decisions.length) signals.forEach((signal) => actions.forEach((action) => graph.addEdge(signal.id, action.id, readerFocusCopy("建议", "suggests"), "leads_to")));
    return graph;
  }

  function boundFocusWorkflowProjection(graph) {
    const originalNodeCount = graph.nodes.length;
    const originalEdgeCount = graph.edges.length;
    if (originalNodeCount <= 80 && originalEdgeCount <= 120) return graph;
    const nodes = [...graph.nodes].sort((left, right) => Number(left.layer) - Number(right.layer) || Number(left.order) - Number(right.order)).slice(0, 79);
    const kept = new Set(nodes.map((node) => node.id));
    const collapsed = {
      id: "workflow:collapsed",
      kind: "collapsed",
      label: readerFocusCopy(`还有 ${Math.max(0, originalNodeCount - nodes.length)} 个节点`, `${Math.max(0, originalNodeCount - nodes.length)} more nodes`),
      detail: readerFocusCopy("为保证画布清晰，已按层级折叠。", "Collapsed by level to keep the canvas readable."),
      layer: Math.max(0, ...nodes.map((node) => Number(node.layer) || 0)) + 1,
      order: nodes.length,
    };
    nodes.push(collapsed);
    kept.add(collapsed.id);
    const edges = graph.edges.filter((edge) => kept.has(edge.source) && kept.has(edge.target)).slice(0, 119);
    const anchor = nodes.find((node) => node.kind === "goal") || nodes[0];
    if (anchor && edges.length < 120) edges.push({ id: "workflow:collapsed-edge", source: anchor.id, target: collapsed.id, label: readerFocusCopy("折叠", "collapsed"), relation: "leads_to" });
    return { ...graph, nodes, edges, truncated: true, originalNodeCount, originalEdgeCount };
  }

  function focusWorkflowProjection(focus, analysis, expanded = false) {
    const intent = ["decide", "plan", "create", "monitor"].includes(text(focus?.intent_kind)) ? text(focus.intent_kind) : "decide";
    const graph = intent === "plan"
      ? projectPlanWorkflow(focus, analysis, expanded)
      : intent === "create"
        ? projectCreateWorkflow(focus, analysis, expanded)
        : intent === "monitor"
          ? projectMonitorWorkflow(focus, analysis, expanded)
          : projectDecisionWorkflow(focus, analysis, expanded);
    return boundFocusWorkflowProjection(graph);
  }

  function focusWorkflowNodePresentation(node, fullscreen = false) {
    const raw = [text(node?.label), text(node?.detail)].filter(Boolean).join("\n");
    const fontSize = fullscreen ? 13 : 11;
    const maxWidth = fullscreen ? 420 : 340;
    // 与决策图同一套 px 实测换行:单位估宽对中文偏窄,cytoscape 按真实字宽
    // 重排后行数变多,算出的方框高度不够,文字溢出方框。
    const measure = (value) => decisionGraphMeasureWidth(value, fontSize) ?? decisionGraphFallbackWidth(value, fontSize);
    const label = decisionGraphWrapTextPx(decisionGraphShortText(raw, fullscreen ? 260 : 180), maxWidth - 28, measure);
    const lines = label.split("\n");
    const widest = Math.max(1, ...lines.map((line) => measure(line)));
    const width = Math.max(fullscreen ? 260 : 210, Math.min(maxWidth, Math.ceil(widest) + 40));
    const height = Math.max(fullscreen ? 94 : 82, Math.min(260, 38 + lines.length * (fullscreen ? 19 : 17)));
    return { label, width, height, textMaxWidth: Math.max(168, width - 28), fontSize };
  }

  function focusWorkflowLayout(graph, presentations, fullscreen = false) {
    const positions = new Map();
    const layers = new Map();
    graph.nodes.forEach((node) => {
      const layer = Number(node.layer) || 0;
      if (!layers.has(layer)) layers.set(layer, []);
      layers.get(layer).push(node);
    });
    const orderedLayers = [...layers.keys()].sort((left, right) => left - right).map((key) => layers.get(key));
    decisionGraphBarycenterOrder(orderedLayers, graph.edges);
    const maxRows = fullscreen ? 8 : 6;
    // 复用决策图的列序优化:展开后"依据→备选"会跨过孤立的目标列,固定
    // taxi 42px 转弯就是一条横穿目标方框的直线;先按边权交换列序,让跨列
    // 边尽量落回相邻列。
    const order = decisionGraphOptimizeColumnOrder(orderedLayers, graph.edges);
    const columns = order
      .flatMap((layerIndex) => {
        const values = orderedLayers[layerIndex] || [];
        const chunks = [];
        for (let index = 0; index < values.length; index += maxRows) chunks.push(values.slice(index, index + maxRows));
        return chunks;
      })
      .filter((column) => column.length);
    const widths = columns.map((column) => Math.max(...column.map((node) => presentations.get(node.id)?.width || 220)));
    const gap = fullscreen ? 150 : 125;
    const total = widths.reduce((sum, width) => sum + width, 0) + Math.max(0, columns.length - 1) * gap;
    let cursor = -total / 2;
    const columnRects = [];
    columns.forEach((column, index) => {
      const width = widths[index];
      decisionGraphColumnPositions(column, cursor + width / 2, presentations, positions, fullscreen ? 34 : 28);
      let top = Infinity;
      let bottom = -Infinity;
      column.forEach((node) => {
        const position = positions.get(node.id);
        const presentation = presentations.get(node.id);
        top = Math.min(top, position.y - (presentation?.height || 104) / 2);
        bottom = Math.max(bottom, position.y + (presentation?.height || 104) / 2);
      });
      columnRects.push({ left: cursor, right: cursor + width, top, bottom });
      cursor += width + gap;
    });
    const columnIndex = new Map();
    columns.forEach((column, index) => column.forEach((node) => columnIndex.set(node.id, index)));
    return { positions, columnIndex, columns: columnRects };
  }

  function focusWorkflowGraphMarkup(focus, analysis) {
    const graphState = focusWorkflowGraphState(focus.id);
    const projection = focusWorkflowProjection(focus, analysis, graphState.expanded);
    const selected = projection.nodes.find((node) => node.id === graphState.selectedNodeID);
    const fallback = projection.nodes.map((node) => `<li><span>${esc(focusWorkflowNodeTypeLabel(node.kind))}</span><strong>${esc(node.label)}</strong>${node.detail ? `<small>${esc(node.detail)}</small>` : ""}</li>`).join("");
    const inspector = selected
      ? `<aside class="focus-workflow-inspector"><span>${esc(focusWorkflowNodeTypeLabel(selected.kind))}</span><strong>${esc(selected.label)}</strong>${selected.detail ? `<p>${esc(selected.detail)}</p>` : ""}<button class="icon-btn" type="button" data-focus-workflow-node-close aria-label="${esc(readerFocusCopy("关闭详情", "Close details"))}">${icon("close")}</button></aside>`
      : "";
    return `<section class="focus-workflow-card${graphState.fullscreen ? " is-fullscreen" : ""}" data-focus-workflow-root data-focus-id="${esc(focus.id)}"><header><div><span>${esc(focusIntentLabel(focus.intent_kind))}</span><h3>${esc(readerFocusCopy("思考与行动链", "Reasoning and action chain"))}</h3></div><div class="focus-workflow-tools"><button class="text-btn" type="button" data-focus-workflow-expand>${icon(graphState.expanded ? "minimize" : "action")}${esc(graphState.expanded ? readerFocusCopy("只看主链", "Main chain") : readerFocusCopy("展开依据", "Expand evidence"))}</button><button class="icon-btn" type="button" data-focus-workflow-fit title="${esc(readerFocusCopy("适应画布", "Fit canvas"))}" aria-label="${esc(readerFocusCopy("适应画布", "Fit canvas"))}">${icon("layout")}</button><button class="icon-btn" type="button" data-focus-workflow-fullscreen title="${esc(graphState.fullscreen ? readerFocusCopy("退出全屏", "Exit fullscreen") : readerFocusCopy("展开全屏", "Open fullscreen"))}" aria-label="${esc(graphState.fullscreen ? readerFocusCopy("退出全屏", "Exit fullscreen") : readerFocusCopy("展开全屏", "Open fullscreen"))}">${icon(graphState.fullscreen ? "minimize" : "maximize")}</button></div></header><div class="focus-workflow-canvas" data-focus-workflow-graph role="img" aria-label="${esc(readerFocusCopy("焦点工作流关系图", "Focus workflow graph"))}"></div><ol class="focus-workflow-fallback" data-focus-workflow-fallback>${fallback}</ol>${projection.truncated ? `<p class="focus-workflow-limit">${esc(readerFocusCopy(`原图包含 ${projection.originalNodeCount} 个节点、${projection.originalEdgeCount} 条关系，已按层级折叠到 80/120 上限内。`, `The original graph had ${projection.originalNodeCount} nodes and ${projection.originalEdgeCount} relationships; it was collapsed to the 80/120 limits.`))}</p>` : ""}${inspector}<footer><span>${esc(readerFocusCopy("左键拖动画布，点击节点查看详情", "Drag the canvas with the left mouse button; select a node for details"))}</span><span>${esc(readerFocusCopy("Ctrl/⌘ + 滚轮缩放", "Ctrl/⌘ + wheel to zoom"))}</span></footer></section>`;
  }

  function focusWorkflowNodeTypeLabel(kind) {
    return ({
      goal: readerFocusCopy("目标", "Goal"),
      evidence: readerFocusCopy("资料", "Source"),
      option: readerFocusCopy("标准/选项", "Criterion/option"),
      decision: readerFocusCopy("决定", "Decision"),
      milestone: readerFocusCopy("里程碑", "Milestone"),
      work_product: readerFocusCopy("产出物", "Work product"),
      section: readerFocusCopy("章节", "Section"),
      signal: readerFocusCopy("信号", "Signal"),
      action: readerFocusCopy("行动", "Action"),
      collapsed: readerFocusCopy("已折叠", "Collapsed"),
    })[text(kind)] || readerFocusCopy("节点", "Node");
  }

  function initializeFocusWorkflowGraph(root, focus, analysis) {
    const graphRoot = root?.querySelector?.("[data-focus-workflow-root]");
    const container = graphRoot?.querySelector?.("[data-focus-workflow-graph]");
    const fallback = graphRoot?.querySelector?.("[data-focus-workflow-fallback]");
    if (!graphRoot || !container) return null;
    const graphState = focusWorkflowGraphState(focus.id);
    const projection = focusWorkflowProjection(focus, analysis, graphState.expanded);
    graphRoot.__focusWorkflowProjection = projection;
    if (typeof window.cytoscape !== "function") {
      container.hidden = true;
      if (fallback) fallback.hidden = false;
      graphRoot.classList.add("is-fallback");
      return null;
    }
    if (fallback) fallback.hidden = true;
    graphRoot.classList.remove("is-fallback");
    const presentations = new Map(projection.nodes.map((node) => [node.id, focusWorkflowNodePresentation(node, graphState.fullscreen)]));
    const layout = focusWorkflowLayout(projection, presentations, graphState.fullscreen);
    const positions = layout.positions;
    const theme = decisionGraphCyTheme(graphRoot);
    const elements = [
      ...projection.nodes.map((node) => {
        const presentation = presentations.get(node.id);
        return { group: "nodes", data: { id: node.id, label: presentation.label, width: presentation.width, height: presentation.height, textMaxWidth: presentation.textMaxWidth, fontSize: presentation.fontSize, kind: node.kind }, classes: [`is-${node.kind}`, node.id === graphState.selectedNodeID ? "is-selected" : ""].filter(Boolean).join(" ") };
      }),
      ...projection.edges.map((edge) => ({ group: "edges", data: { id: edge.id, source: edge.source, target: edge.target, label: edge.label }, classes: `is-${edge.relation}` })),
    ];
    // 形状指纹（节点/边/尺寸/展开与全屏档位，刻意不含选中态）：面板每次刷新
    // 都会整块重写 innerHTML（updateFocusV3DrawerPanel），若每次都销毁重建
    // cytoscape 实例，画布先清空再重画，大图上就是持续闪屏。指纹一致说明这
    // 次渲染只变了选中态等外围状态：把存活实例挂回（可能被重建过的）容器、
    // 同步选中类即可，不做整图销毁重建。
    const shapeSignature = JSON.stringify([
      graphState.expanded ? 1 : 0,
      graphState.fullscreen ? 1 : 0,
      positions.size,
      ...elements.map((element) => [
        element.data.id,
        element.classes.replace(/(^| )is-selected( |$)/g, " ").trim(),
        element.data.label,
        element.data.width || 0,
        element.data.height || 0,
        element.data.source || "",
        element.data.target || "",
      ]),
    ]);
    const bindWorkflowWheel = (targetCy) => {
      if (container.__wfWheelBound) return;
      container.__wfWheelBound = true;
      const onWheel = (event) => {
        if (!event.ctrlKey && !event.metaKey) return;
        event.preventDefault();
        const rect = container.getBoundingClientRect();
        const next = Math.max(0.25, Math.min(2.5, targetCy.zoom() * (event.deltaY < 0 ? 1.12 : 0.89)));
        targetCy.zoom({ level: next, renderedPosition: { x: event.clientX - rect.left, y: event.clientY - rect.top } });
        graphState.zoom = targetCy.zoom();
        graphState.pan = targetCy.pan();
      };
      container.addEventListener("wheel", onWheel, { passive: false });
    };
    const existing = graphState.workflowCy;
    if (existing && graphState.workflowSignature === shapeSignature) {
      existing.mount(container);
      bindWorkflowWheel(existing);
      graphRoot.__focusWorkflowCy = existing;
      existing.batch(() => {
        existing.nodes().removeClass("is-selected");
        if (graphState.selectedNodeID) {
          const selected = existing.nodes().filter((node) => node.id() === graphState.selectedNodeID);
          if (selected.nonempty()) selected.addClass("is-selected");
        }
      });
      existing.resize();
      if (graphState.fitPending || !graphState.pan) {
        existing.fit(existing.elements(), graphState.fullscreen ? 64 : 36);
        graphState.fitPending = false;
        graphState.zoom = existing.zoom();
        graphState.pan = existing.pan();
      } else {
        existing.pan(graphState.pan);
        if (Number(graphState.zoom) > 0) existing.zoom(Number(graphState.zoom));
      }
      return existing;
    }
    graphState.workflowCy?.destroy?.();
    graphState.workflowCy = null;
    graphState.workflowSignature = "";
    const cy = window.cytoscape({
      container,
      elements,
      layout: { name: "preset", fit: false, positions: (node) => positions.get(node.id()) || { x: 0, y: 0 } },
      style: [
        { selector: "node", style: { "background-color": theme.surface, "border-color": theme.line, "border-width": 1, "shape": "roundrectangle", "width": "data(width)", "height": "data(height)", "label": "data(label)", "color": theme.ink, "font-family": "Inter, ui-sans-serif, system-ui, sans-serif", "font-size": "data(fontSize)", "font-weight": 600, "line-height": 1.35, "text-wrap": "wrap", "text-max-width": "data(textMaxWidth)", "text-valign": "center", "text-halign": "center", "padding": 10, "overlay-opacity": 0 } },
        { selector: "node.is-goal", style: { "background-color": theme.blueSoft, "border-color": theme.blue, "border-width": 2 } },
        { selector: "node.is-decision", style: { "background-color": theme.greenSoft, "border-color": theme.green, "border-width": 2 } },
        { selector: "node.is-action", style: { "background-color": theme.blueSoft, "border-color": theme.blue } },
        { selector: "node.is-signal", style: { "background-color": theme.amberSoft, "border-color": theme.amber } },
        { selector: "node.is-work_product, node.is-milestone, node.is-section", style: { "background-color": "#f4efff", "border-color": "#8667c7" } },
        { selector: "node.is-evidence", style: { "border-style": "dashed", "color": theme.muted } },
        { selector: "node.is-collapsed", style: { "border-style": "dashed", "background-color": theme.canvas, "color": theme.faint } },
        { selector: "node.is-selected", style: { "border-color": theme.blue, "border-width": 3, "underlay-color": theme.blue, "underlay-opacity": 0.13, "underlay-padding": 5 } },
        { selector: "edge", style: { "curve-style": "taxi", "taxi-direction": "rightward", "taxi-turn": 42, "line-color": theme.line, "target-arrow-color": theme.line, "target-arrow-shape": "triangle", "width": 1.8, "label": "data(label)", "color": theme.faint, "font-size": 8, "text-background-color": theme.canvas, "text-background-opacity": 0.92, "text-background-padding": 3, "overlay-opacity": 0 } },
        { selector: "edge.is-supports", style: { "line-color": theme.green, "target-arrow-color": theme.green } },
        { selector: "edge.is-references", style: { "line-style": "dashed", "line-color": theme.blue, "target-arrow-color": theme.blue } },
      ],
      autoungrabify: true,
      autounselectify: false,
      boxSelectionEnabled: false,
      userPanningEnabled: true,
      userZoomingEnabled: false,
      minZoom: 0.25,
      maxZoom: 2.5,
      pixelRatio: "auto",
    });
    graphRoot.__focusWorkflowCy = cy;
    graphState.workflowCy = cy;
    graphState.workflowSignature = shapeSignature;
    cy.on("tap", "node", (event) => {
      graphState.pan = cy.pan();
      graphState.zoom = cy.zoom();
      graphState.selectedNodeID = event.target.id();
      if (!graphState.expanded && event.target.data("kind") !== "goal") graphState.expanded = true;
      graphState.fitPending = !graphState.pan;
      refreshOpenFocusV3Drawer(focus.id, "workspace");
    });
    bindWorkflowWheel(cy);
    // 边路由与决策图同一套:相邻列 taxi 在列间通道错开转弯,跨列边走弧线
    // 绕过中间列,替代全员固定 42px 转弯的横穿直线。
    applyDecisionGraphEdgeRoutes(cy, decisionGraphEdgeRoutes(projection, layout));
    cy.resize();
    if (graphState.fitPending || !graphState.pan) {
      cy.fit(cy.elements(), graphState.fullscreen ? 64 : 36);
      graphState.fitPending = false;
      graphState.zoom = cy.zoom();
      graphState.pan = cy.pan();
    } else {
      cy.pan(graphState.pan);
      if (Number(graphState.zoom) > 0) cy.zoom(Number(graphState.zoom));
    }
    return cy;
  }

  function renderFocusV3Workspace() {
    const workspace = ensureFocusV3Surface();
    const active = focusV3Views.has(state.view);
    const shell = $("reader-shell");
    const wasFocusActive = Boolean(
      document.body.classList.contains("focus-v3-active") ||
        shell?.classList.contains("focus-v3-active")
    );
    // Reader projections (video/X feeds) own a different grid.  They can
    // remain mounted while an article detail is open, so switching directly
    // to Focus must clear those classes before applying the two-column Focus
    // layout.  Otherwise the more-specific video-gallery rule wins and puts
    // the workspace in its 5px splitter track (one character per line).
    if (active) {
      if (!wasFocusActive) invalidateReaderProjectionRequests();
      document.body.classList.remove("x-feed", "video-gallery");
      shell?.classList.remove("x-feed", "video-gallery");
      $("timeline")?.classList.remove("video-grid");
    }
    // Keep the production shell invariant even when an async reader render
    // races with navigation and the navigation node was already mounted.
    document.body.classList.add("product-v3-shell");
    shell?.classList.add("product-v3-shell");
    document.body.classList.toggle("focus-v3-active", active);
    shell?.classList.toggle("focus-v3-active", active);
    workspace.classList.toggle("is-action-view", active && state.view === "action");
    workspace.classList.toggle(
      "is-focus-board-view",
      active && state.view === "today" && parity.focusViewMode === "board"
    );
    workspace.hidden = !active;
    if (!active) return false;
    if (!parity.focusLoaded && !parity.focusLoading) void loadFocusSpaces(false).then(() => render());
    if (!parity.focusLoaded) {
      workspace.innerHTML = `<div class="focus-v3-loading" role="status">${readerFocusCopy("正在读取真实焦点数据…", "Loading real focus data…")}</div>`;
      return true;
    }
    if (state.view === "today") {
      const activeCount = parity.focusSpaces.filter((focus) => !["done", "archived"].includes(focus.workflow_state)).length;
      workspace.innerHTML = `<section class="focus-v3-home-intro"><div class="focus-v3-home-actions"><div class="focus-v3-segment" role="group" aria-label="${readerFocusCopy("焦点视图", "Focus view")}"><button class="${parity.focusViewMode === "board" ? "active" : ""}" type="button" data-focus-view-mode="board">${readerFocusCopy("看板", "Board")}</button><button class="${parity.focusViewMode === "list" ? "active" : ""}" type="button" data-focus-view-mode="list">${readerFocusCopy("列表", "List")}</button></div><span>${activeCount} ${readerFocusCopy("进行中", "active")}</span><button class="focus-v3-create-button" type="button" data-create-focus>${icon("plus")}${readerFocusCopy("新建焦点", "Add focus")}</button></div></section>${focusBoardMarkup()}`;
    } else {
      workspace.innerHTML = focusQueueMarkup(state.view);
    }
    bindFocusV3WorkspaceEvents(workspace);
    return true;
  }

  function focusAnalysisRunStatusLabel(status) {
    return ({
      applied: readerFocusCopy("已完成", "Completed"),
      partial: readerFocusCopy("部分完成", "Partially completed"),
      claimed: readerFocusCopy("分析中", "Analyzing"),
      failed: readerFocusCopy("失败", "Failed"),
      superseded: readerFocusCopy("已被新版本替代", "Superseded"),
    })[text(status).toLowerCase()] || text(status, readerFocusCopy("未知状态", "Unknown"));
  }

  function focusAnalysisHistoryMarkup(focus, latestAnalysis) {
    const runs = Array.isArray(latestAnalysis?.runs) ? latestAnalysis.runs : [];
    if (!runs.length) return "";
    const selected = text(parity.focusAnalysisRunSelections.get(text(focus.id)));
    const options = runs.map((run, index) => {
      const runID = text(run?.run_id || run?.id);
      const created = text(run?.created_at)
        ? formatDateByLocale(run.created_at, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })
        : readerFocusCopy(`第 ${runs.length - index} 轮`, `Run ${runs.length - index}`);
      return `<option value="${esc(runID)}"${selected === runID ? " selected" : ""}>${esc(created)} · ${esc(focusAnalysisRunStatusLabel(run?.status))}</option>`;
    }).join("");
    return `<section class="focus-analysis-history${selected ? " is-history" : ""}"><label><span>${esc(readerFocusCopy("分析回合", "Analysis run"))}</span><select data-focus-analysis-run data-focus-id="${esc(focus.id)}"><option value=""${selected ? "" : " selected"}>${esc(readerFocusCopy("最新有效结果", "Latest effective result"))}</option>${options}</select></label>${selected ? `<p>${esc(readerFocusCopy("正在查看历史回合。当前有效结果和已接受决定没有被覆盖。", "Viewing a historical run. Current results and accepted decisions remain unchanged."))}</p>` : `<p>${esc(readerFocusCopy("重新分析会创建新回合，旧结果会保留在这里。", "Reanalysis creates a new run and keeps earlier results here."))}</p>`}</section>`;
  }

  async function selectFocusAnalysisRun(focusID, runID) {
    const id = text(focusID);
    const selectedRunID = text(runID);
    if (!id) return;
    if (!selectedRunID) {
      parity.focusAnalysisRunSelections.delete(id);
      refreshOpenFocusV3Drawer(id, "workspace");
      return;
    }
    const key = focusAnalysisSnapshotKey(id, selectedRunID);
    try {
      let snapshot = parity.focusAnalysisSnapshots.get(key);
      if (!snapshot) {
        snapshot = await callFocusOperation("focus_analysis.get", { query: { focus_id: id, run_id: selectedRunID, limit: 20 } });
        parity.focusAnalysisSnapshots.set(key, snapshot || {});
      }
      parity.focusAnalysisRunSelections.set(id, selectedRunID);
      const graphState = focusWorkflowGraphState(id);
      graphState.selectedNodeID = "";
      graphState.pan = null;
      graphState.zoom = 0;
      graphState.fitPending = true;
      refreshOpenFocusV3Drawer(id, "workspace");
    } catch (error) {
      showError(error?.message || readerFocusCopy("历史分析读取失败", "Could not load the historical analysis"));
    }
  }

  function focusAnalysisEvidenceMarkup(analysis) {
    const evidence = Array.isArray(analysis?.evidence) ? analysis.evidence : [];
    if (!evidence.length) return "";
    return `<section class="focus-analysis-evidence"><div class="overview-subsection-label"><span>${esc(readerFocusCopy("本轮实际上下文", "Evidence used in this run"))}</span><small>${evidence.length}</small></div><div class="focus-material-list">${evidence.map((item, index) => {
      const citation = normalizeFocusCitation({ ...item, source_id: item.source_id || item.id, quote: item.quote || item.excerpt, role: item.evidence_role || "support" });
      const title = text(item.title, readerFocusCopy(`资料 ${index + 1}`, `Source ${index + 1}`));
      const summary = text(item.quote || item.excerpt);
      return citation
        ? `<button class="focus-material-card is-entry" type="button" ${focusCitationDataAttributes(citation)}><span class="focus-material-icon">${icon(citation.source_kind === "meeting" ? "audio" : "note")}</span><span class="focus-material-copy"><strong>${esc(title)}</strong>${summary ? `<span>${esc(summary.slice(0, 320))}</span>` : ""}</span>${icon("arrow")}</button>`
        : "";
    }).join("")}</div></section>`;
  }

  function focusCurrentResultTitle(focus) {
    return ({
      decide: readerFocusCopy("当前决定", "Current decision"),
      plan: readerFocusCopy("当前计划", "Current plan"),
      create: readerFocusCopy("当前创作", "Current creation"),
      monitor: readerFocusCopy("最新变化", "Latest changes"),
    })[text(focus?.intent_kind)] || readerFocusCopy("当前结果", "Current result");
  }

  function focusDetailSections(focus) {
    const latestAnalysis = focusAnalysisFor(focus.id);
    const analysis = focusAnalysisForPanel(focus.id);
    const historySelected = Boolean(parity.focusAnalysisRunSelections.get(text(focus.id)));
    const linkedSourceCount = (focus.sources || []).filter((item) => item?.enabled !== false).length;
    const acceptedDecisions = (latestAnalysis.decisions || []).filter((item) => text(item.feedback_status) === "accepted");
    const pendingDecisions = (latestAnalysis.decisions || []).filter((item) => !["accepted", "rejected"].includes(text(item.feedback_status)));
    const activeActions = (latestAnalysis.actions || []).filter((item) => text(item.feedback_status) !== "rejected");
    const workProducts = focusWorkProducts(latestAnalysis).filter((item) => text(item.feedback_status || item.status) !== "rejected");
    const hasCurrentResult = acceptedDecisions.length > 0 || workProducts.length > 0;
    const hasReview = focusReviewAvailable(focus, latestAnalysis);
    const tabs = [
      ["overview", readerFocusCopy("概览", "Overview")],
      ["evidence", readerFocusCopy("资料", "Sources")],
      ["workspace", readerFocusCopy("工作台", "Workspace")],
      ...(hasCurrentResult ? [["result", readerFocusCopy("当前结果", "Current result")]] : []),
      ...(hasReview ? [["review", readerFocusCopy("复盘", "Review")]] : []),
    ];
    const allowedPanels = new Set(tabs.map(([id]) => id));
    if (!allowedPanels.has(parity.focusPanel)) parity.focusPanel = "overview";
    let body = "";
    if (parity.focusPanel === "overview") {
      const visibleSignals = (latestAnalysis.signals || []).filter((item) => text(item.feedback_status) !== "rejected").slice(0, 4);
      const evidenceCount = (focus.materials?.length || 0) + linkedSourceCount + (latestAnalysis.evidence?.length || 0);
      const signalMarkup = visibleSignals.length
        ? `<ul class="focus-v3-overview-insights">${visibleSignals.map((item) => {
            const data = focusResultData(item);
            return `<li><span class="focus-v3-overview-row-icon">${icon("sparkles")}</span><div class="focus-v3-overview-row-copy"><strong>${esc(text(data.title, readerFocusCopy("未命名信号", "Untitled signal")))}</strong><p>${esc(text(data.detail, readerFocusCopy("等待补充变化说明", "Waiting for more detail")))}</p></div></li>`;
          }).join("")}</ul>`
        : `<p class="focus-v3-muted">${readerFocusCopy("当前没有需要强调的新变化。", "There are no new changes to highlight.")}</p>`;
      const decisionMarkup = pendingDecisions[0]
        ? (() => {
            const data = focusResultData(pendingDecisions[0]);
            return `<button class="focus-v3-overview-decision" type="button" data-focus-panel="workspace"><span class="focus-v3-overview-decision-icon">${icon("decision")}</span><span class="focus-v3-overview-decision-copy"><small>${readerFocusCopy("等待确认", "Needs confirmation")}</small><strong>${esc(text(data.question, readerFocusCopy("打开工作台确认方向", "Open the workspace to confirm the direction")))}</strong><em>${esc(text(data.recommendation))}</em></span>${icon("arrow")}</button>`;
          })()
        : "";
      const actionMarkup = activeActions.length
        ? `<button class="focus-v3-overview-action" type="button" data-open-focus-actions><span class="focus-v3-overview-row-icon">${icon("action")}</span><span><strong>${esc(text(focusResultData(activeActions[0]).task, readerFocusCopy("未命名行动", "Untitled action")))}</strong><small>${esc(readerFocusCopy(`${activeActions.length} 条行动统一进入 TODO List`, `${activeActions.length} actions are in the To-do list`))}</small></span>${icon("arrow")}</button>`
        : `<p class="focus-v3-muted">${readerFocusCopy("当前没有待执行行动。", "There are no actions to execute.")}</p>`;
      body = `<div class="focus-v3-overview-properties"><button type="button" data-focus-panel="evidence">${icon("note")}<span>${readerFocusCopy("资料", "Sources")}</span>${evidenceCount ? `<strong>${evidenceCount}</strong>` : ""}</button><button type="button" data-focus-panel="workspace">${icon("decision")}<span>${readerFocusCopy("工作台", "Workspace")}</span>${pendingDecisions.length ? `<strong>${pendingDecisions.length}</strong>` : ""}</button><button type="button" data-open-focus-actions>${icon("action")}<span>${readerFocusCopy("TODO", "To-do")}</span>${activeActions.length ? `<strong>${activeActions.length}</strong>` : ""}</button></div><div class="focus-v3-overview-workspace"><main class="focus-v3-overview-main"><section class="focus-v3-overview-group"><div class="focus-v3-overview-group-head"><div><h3>${readerFocusCopy("重要变化", "Important changes")}</h3></div></div>${signalMarkup}</section>${decisionMarkup ? `<section class="focus-v3-overview-group"><div class="focus-v3-overview-group-head"><div><h3>${readerFocusCopy("需要确认", "Needs confirmation")}</h3></div><button class="focus-v3-overview-link" type="button" data-focus-panel="workspace">${readerFocusCopy("打开工作台", "Open workspace")} ${icon("arrow")}</button></div>${decisionMarkup}</section>` : ""}<section class="focus-v3-overview-group"><div class="focus-v3-overview-group-head"><div><h3>${readerFocusCopy("TODO", "To-do")}</h3></div><button class="focus-v3-overview-link" type="button" data-open-focus-actions>${readerFocusCopy("打开 TODO List", "Open To-do list")} ${icon("arrow")}</button></div>${actionMarkup}</section></main><aside class="focus-v3-overview-sidebar"><section class="focus-v3-overview-side-group"><div class="focus-v3-overview-side-head"><h3>${readerFocusCopy("焦点设置", "Focus settings")}</h3></div><dl class="focus-v3-overview-definition"><div><dt>${readerFocusCopy("任务模式", "Task mode")}</dt><dd>${esc(focusIntentLabel(focus.intent_kind))}</dd></div><div><dt>${readerFocusCopy("思考模板", "Thinking template")}</dt><dd>${esc(focusThinkingTemplateLabel(focus.thinking_template_id))}</dd></div><div><dt>${readerFocusCopy("目标", "Goal")}</dt><dd>${esc(text(focus.goal, readerFocusCopy("尚未填写", "Not set")))}</dd></div>${text(focus.decision_question) ? `<div><dt>${readerFocusCopy("关键问题", "Key question")}</dt><dd>${esc(focus.decision_question)}</dd></div>` : ""}<div><dt>${readerFocusCopy("时间范围", "Time horizon")}</dt><dd>${esc(text(focus.horizon_end, readerFocusCopy("持续滚动", "Ongoing")))}</dd></div></dl></section>${hasCurrentResult ? `<section class="focus-v3-overview-side-group"><div class="focus-v3-overview-side-head"><h3>${esc(focusCurrentResultTitle(focus))}</h3><button class="focus-v3-overview-link" type="button" data-focus-panel="result">${readerFocusCopy("查看", "View")}</button></div><p>${esc(readerFocusCopy(`${acceptedDecisions.length} 个决定 · ${workProducts.length} 个产出物`, `${acceptedDecisions.length} decisions · ${workProducts.length} work products`))}</p></section>` : ""}</aside></div>`;
    }
    if (parity.focusPanel === "evidence") {
      const evidenceMarkup = focusAnalysisEvidenceMarkup(analysis);
      const configured = `${focusMaterialsMarkup(focus)}${focusLinkedSourcesMarkup(focus)}`;
      body = evidenceMarkup || configured || `<p class="focus-v3-muted">${readerFocusCopy("还没有进入分析上下文的资料。", "No evidence has entered the analysis context yet.")}</p>`;
    }
    if (parity.focusPanel === "workspace") {
      const pendingMarkup = pendingDecisions.map(focusResultCardMarkup).filter(Boolean).join("");
      body = `${focusAnalysisHistoryMarkup(focus, latestAnalysis)}${decisionRunPanelMarkup(focus, latestAnalysis, state.dashboard || {})}<section class="focus-workspace-settings"><span>${icon(focusIntentIcon(focus.intent_kind))}${esc(focusIntentLabel(focus.intent_kind))}</span><strong>${esc(focusThinkingTemplateLabel(focus.thinking_template_id))}</strong><small>${esc(historySelected ? readerFocusCopy("图中展示所选历史回合", "The graph shows the selected historical run") : readerFocusCopy("任务模式决定产出，思考模板决定推理方式", "The task mode controls the output; the thinking template controls the reasoning"))}</small></section>${pendingMarkup ? `<section class="focus-v3-decision-section"><div class="overview-subsection-label"><span>${esc(readerFocusCopy("等待确认", "Needs confirmation"))}</span><small>${pendingDecisions.length}</small></div><div class="focus-result-list">${pendingMarkup}</div></section>` : ""}${focusWorkflowGraphMarkup(focus, analysis)}`;
    }
    if (parity.focusPanel === "result") {
      const decisions = acceptedDecisions.map(focusResultCardMarkup).filter(Boolean).join("");
      const products = workProducts.map(focusWorkProductCardMarkup).filter(Boolean).join("");
      body = `<section class="focus-current-result"><div class="focus-current-result-head"><span>${icon(focusIntentIcon(focus.intent_kind))}</span><div><small>${esc(focusIntentLabel(focus.intent_kind))}</small><h3>${esc(focusCurrentResultTitle(focus))}</h3></div></div>${decisions ? `<div class="focus-result-list">${decisions}</div>` : ""}${products ? `<div class="focus-work-product-list">${products}</div>` : ""}</section>`;
    }
    if (parity.focusPanel === "review") {
      const dueActions = activeActions.filter((item) => {
        const data = focusResultData(item);
        const due = Date.parse(text(data.due_at));
        return text(item.status) === "done" || data.completed === true || (Number.isFinite(due) && due <= Date.now());
      });
      body = `<section class="focus-v3-review"><div class="focus-v3-review-intro"><span>${readerFocusCopy("执行后的复盘", "Review after execution")}</span><h3>${readerFocusCopy("对照预期，记录实际结果", "Compare expectations with actual results")}</h3><p>${readerFocusCopy("复盘只记录执行效果，不覆盖分析产物、已接受决定或历史回合。", "Reviews record execution outcomes without overwriting analysis outputs, accepted decisions, or historical runs.")}</p></div>${acceptedDecisions.length ? `<div class="focus-result-list">${acceptedDecisions.map(focusResultCardMarkup).join("")}</div>` : ""}${dueActions.length ? `<div class="focus-v3-review-meta"><span>${readerFocusCopy("到期或已完成行动", "Due or completed actions")}</span><strong>${dueActions.length}</strong><button class="focus-v3-overview-link" type="button" data-open-focus-actions>${readerFocusCopy("前往 TODO List", "Open To-do list")} ${icon("arrow")}</button></div>` : ""}</section>`;
    }
    const focusDOMID = esc(text(focus.id, "focus"));
    const activeTabID = `focus-v3-tab-${focusDOMID}-${parity.focusPanel}`;
    const activePanelID = `focus-v3-panel-${focusDOMID}-${parity.focusPanel}`;
    return `<div class="focus-v3-drawer-body"><div class="focus-v3-goal"><p>${esc(text(focus.goal, readerFocusCopy("尚未填写焦点描述", "No focus description yet")))}</p></div><div class="focus-v3-badges"><span class="focus-v3-intent">${icon(focusIntentIcon(focus.intent_kind))}${esc(focusIntentLabel(focus.intent_kind))}</span>${focusModeBadge(focus)}${focusDeadlineBadge(focus)}<span>${readerFocusCopy("版本", "Version")} ${Number(focus.revision) || 1}</span></div><nav class="focus-v3-tabs" role="tablist" aria-label="${readerFocusCopy("焦点详情", "Focus details")}">${tabs.map(([id, label]) => { const selected = parity.focusPanel === id; return `<button id="focus-v3-tab-${focusDOMID}-${id}" class="${selected ? "active" : ""}" type="button" role="tab" aria-selected="${selected ? "true" : "false"}" aria-controls="focus-v3-panel-${focusDOMID}-${id}" tabindex="${selected ? "0" : "-1"}" data-focus-panel="${id}">${label}</button>`; }).join("")}</nav><div id="${activePanelID}" class="focus-v3-panel" role="tabpanel" aria-labelledby="${activeTabID}" data-focus-panel-content="${parity.focusPanel}" tabindex="0">${body}</div></div>`;
  }

  function updateFocusV3DrawerPanel(layer, focus, panel, options = {}) {
    if (!layer || !focus || !["overview", "evidence", "workspace", "result", "review"].includes(panel)) return;
    const drawerBody = layer.querySelector(".focus-v3-drawer-body");
    const currentPanel = layer.querySelector(".focus-v3-panel");
    if (!drawerBody || !currentPanel) return;
    const scrollTop = drawerBody.scrollTop;
    parity.focusPanel = panel;
    const template = document.createElement("template");
    template.innerHTML = focusDetailSections(focus);
    const nextPanel = template.content.querySelector(".focus-v3-panel");
    if (!nextPanel) return;
    currentPanel.innerHTML = nextPanel.innerHTML;
    [...nextPanel.attributes].forEach((attribute) => currentPanel.setAttribute(attribute.name, attribute.value));
    layer.querySelectorAll('.focus-v3-tabs [role="tab"]').forEach((tab) => {
      const selected = tab.dataset.focusPanel === panel;
      tab.classList.toggle("active", selected);
      tab.setAttribute("aria-selected", selected ? "true" : "false");
      tab.tabIndex = selected ? 0 : -1;
    });
    drawerBody.scrollTop = Math.min(scrollTop, Math.max(0, drawerBody.scrollHeight - drawerBody.clientHeight));
    bindFocusSpaceOverviewEvents(currentPanel);
    // 只有用户真正切换到 workspace tab 时才补读一次决策运行。数据刷新路径
    // （轮询/投影 reload）也会走到这里；若它们也强制读运行状态，读取完成又
    // 会刷新面板、刷新又触发读取，与终态 reload 叠加成自持刷新环。
    if (panel === "workspace" && options.tabSwitch)
      void loadDecisionRunState(focus.id, { force: true, silent: true });
  }

  function openFocusV3Drawer(focusID, panel = "overview") {
    const focus = parity.focusSpaces.find((item) => text(item.id) === text(focusID));
    if (!focus) return;
    parity.selectedFocusID = focus.id;
    parity.focusPanel = panel;
    const layer = ensureFocusV3Surface() && $("focus-v3-drawer-layer");
    layer.hidden = false;
    layer.innerHTML = `<button class="focus-v3-drawer-mask" type="button" data-close-focus aria-label="${readerFocusCopy("关闭焦点详情", "Close focus details")}"></button><aside class="focus-v3-drawer" role="dialog" aria-modal="true" aria-label="${readerFocusCopy("焦点详情", "Focus details")}" tabindex="-1"><header><div class="focus-v3-drawer-head-copy"><div class="focus-v3-inline-title"><h2>${esc(focus.title)}</h2><button class="focus-v3-inline-edit" type="button" data-edit-focus title="${readerFocusCopy("编辑焦点", "Edit focus")}" aria-label="${readerFocusCopy("编辑焦点", "Edit focus")}"><span>${readerFocusCopy("编辑", "Edit")}</span></button></div></div><div class="focus-v3-drawer-head-actions"><button class="icon-btn" type="button" data-close-focus title="${readerFocusCopy("关闭焦点详情", "Close focus details")}" aria-label="${readerFocusCopy("关闭焦点详情", "Close focus details")}">${icon("close")}</button></div></header>${focusDetailSections(focus)}</aside>`;
    bindFocusV3DrawerEvents(layer, focus);
    refreshFocusV3LayerState();
    requestAnimationFrame(() => layer.querySelector(".focus-v3-drawer")?.focus({ preventScroll: true }));
    void loadDecisionRunState(focus.id, { silent: true });
  }

  function refreshOpenFocusV3Drawer(focusID, panel = parity.focusPanel) {
    const focus = parity.focusSpaces.find((item) => text(item.id) === text(focusID));
    const layer = $("focus-v3-drawer-layer");
    const drawer = layer?.querySelector(".focus-v3-drawer");
    if (!focus || !layer || layer.hidden || !drawer || text(parity.selectedFocusID) !== text(focusID)) {
      openFocusV3Drawer(focusID, panel);
      return;
    }
    const title = drawer.querySelector(".focus-v3-inline-title h2");
    const goal = drawer.querySelector(".focus-v3-goal p");
    const badges = drawer.querySelector(".focus-v3-badges");
    if (title) title.textContent = focus.title;
    if (goal) goal.textContent = text(focus.goal, readerFocusCopy("尚未填写焦点描述", "No focus description yet"));
    if (badges) badges.innerHTML = `<span class="focus-v3-intent">${icon(focusIntentIcon(focus.intent_kind))}${esc(focusIntentLabel(focus.intent_kind))}</span>${focusModeBadge(focus)}${focusDeadlineBadge(focus)}<span>${readerFocusCopy("版本", "Version")} ${Number(focus.revision) || 1}</span>`;
    updateFocusV3DrawerPanel(layer, focus, panel);
  }

  function closeFocusV3Drawer() {
    const layer = $("focus-v3-drawer-layer");
    // 工作流图实例按焦点常驻 graphState 以便跨面板刷新复用；抽屉关闭即销毁，
    // 不留无容器的画布占内存（端侧内存预算）。
    const closingGraphState = focusWorkflowGraphState(parity.selectedFocusID);
    if (closingGraphState?.workflowCy) {
      closingGraphState.workflowCy.destroy?.();
    }
    // 状态整个注销而不只是销毁画布:全屏/展开/缩放留在这里的话,下次打开
    // 焦点详情会带着旧全屏档位直接放大。
    parity.focusWorkflowStates.delete(parity.selectedFocusID);
    if (layer) { layer.hidden = true; layer.innerHTML = ""; }
    parity.selectedFocusID = "";
    refreshFocusV3LayerState();
  }

  async function mutateFocusV3(focus, patch, action = "update") {
    if (!focus || parity.focusBusy.has(focus.id)) return;
    parity.focusBusy.add(focus.id);
    try {
      await callFocusOperation("focus_space.update", { body: { schema_version: 1, action, focus_id: focus.id, expected_revision: Number(focus.revision), ...patch }, key: `focus-v3:${focus.id}:${focus.revision}:${action}:${Date.now()}` });
      await loadFocusSpaces(false);
      render();
      if (parity.selectedFocusID) refreshOpenFocusV3Drawer(parity.selectedFocusID, parity.focusPanel);
      showToast(readerFocusCopy("焦点已更新", "Focus updated"));
    } catch (error) { showError(error.message || readerFocusCopy("焦点更新失败", "Focus update failed")); }
    finally { parity.focusBusy.delete(focus.id); }
  }

  function focusLinkDescriptor(entryKey) {
    const entry = makeEntries(state.dashboard || {}).find((item) => item.key === entryKey)
      || parity.detailCache.get(entryKey);
    if (!entry?.id) return null;
    if (entry.kind === "news")
      return { entry, sourceKind: "entry", sourceID: text(entry.id) };
    if (entry.kind === "recording")
      return { entry, sourceKind: "meeting", sourceID: text(entry.id) };
    if (entry.kind === "evidence" && text(entry.item?.source_type).toLowerCase() === "obsidian")
      return { entry, sourceKind: "obsidian", sourceID: text(entry.id) };
    return null;
  }

  function focusHasLinkedEntry(focus, descriptor) {
    if (descriptor.sourceKind === "entry")
      return (focus.materials || []).some((item) => item?.enabled !== false && text(item.source_kind) === "entry" && text(item.source_id) === descriptor.sourceID);
    return (focus.sources || []).some((item) => item?.enabled !== false && text(item.source_kind).toLowerCase() === descriptor.sourceKind && text(item.source_id) === descriptor.sourceID);
  }

  function positionFocusPicker(trigger, popover) {
    if (!trigger?.isConnected || !popover) return;
    const gap = 8;
    const edge = 12;
    const triggerRect = trigger.getBoundingClientRect();
    popover.style.maxHeight = `${Math.max(44, Math.max(triggerRect.top - gap - edge, window.innerHeight - triggerRect.bottom - gap - edge))}px`;
    popover.style.overflowY = "auto";
    const popoverWidth = popover.offsetWidth;
    const popoverHeight = popover.offsetHeight;
    const left = Math.max(
      edge,
      Math.min(window.innerWidth - popoverWidth - edge, triggerRect.right - popoverWidth)
    );
    const below = triggerRect.bottom + gap;
    const above = triggerRect.top - popoverHeight - gap;
    const top = below + popoverHeight <= window.innerHeight - edge || above < edge
      ? Math.min(below, window.innerHeight - popoverHeight - edge)
      : above;
    popover.style.left = `${Math.round(left)}px`;
    popover.style.top = `${Math.round(Math.max(edge, top))}px`;
  }

  function closeFocusPicker(returnFocus = false) {
    const layer = $("focus-v3-picker-layer");
    const trigger = parity.focusPickerTrigger;
    parity.focusPickerCleanup?.();
    parity.focusPickerCleanup = null;
    parity.focusPickerTrigger = null;
    trigger?.setAttribute("aria-expanded", "false");
    if (layer) { layer.hidden = true; layer.innerHTML = ""; }
    if (returnFocus && trigger?.isConnected) trigger.focus({ preventScroll: true });
  }

  function syncFocusPickerTrigger(trigger, descriptor) {
    if (!trigger?.isConnected || !descriptor) return;
    const linkedCount = parity.focusSpaces.filter((focus) =>
      focusHasLinkedEntry(focus, descriptor)
    ).length;
    trigger.classList.toggle("is-linked", linkedCount > 0);
    trigger.setAttribute(
      "aria-label",
      linkedCount ? readerFocusCopy(`管理焦点，已添加 ${linkedCount} 个`, `${linkedCount} focuses linked`) : readerFocusCopy("添加到焦点", "Add to focus")
    );
    trigger.setAttribute(
      "title",
      linkedCount ? readerFocusCopy(`已添加 ${linkedCount} 个焦点`, `${linkedCount} focuses linked`) : readerFocusCopy("添加到焦点", "Add to focus")
    );
    const label = trigger.querySelector("span");
    if (label) label.textContent = `${readerFocusCopy("焦点", "Focus")}${linkedCount ? ` ${linkedCount}` : ""}`;
  }

  async function openFocusPicker(entryKey, trigger = null) {
    const descriptor = focusLinkDescriptor(entryKey);
    if (!descriptor) { showError(readerFocusCopy("当前内容不支持加入焦点", "This content cannot be added to a focus")); return; }
    ensureFocusV3Surface();
    if (!parity.focusLoaded) await loadFocusSpaces(false);
    const layer = $("focus-v3-picker-layer");
    closeFocusPicker(false);
    trigger = trigger?.isConnected
      ? trigger
      : document.querySelector(`[data-add-entry-focus="${CSS.escape(entryKey)}"]`);
    parity.focusPickerTrigger = trigger;
    trigger?.setAttribute("aria-haspopup", "dialog");
    trigger?.setAttribute("aria-expanded", "true");
    trigger?.setAttribute("aria-controls", "focus-v3-picker-content");
    layer.hidden = false;
    const options = parity.focusSpaces.filter((focus) => focus.status !== "archived").map((focus) => {
      const linked = focusHasLinkedEntry(focus, descriptor);
      const search = `${text(focus.title)} ${text(focus.goal)}`.toLocaleLowerCase();
      return `<button type="button" role="option" aria-selected="${linked ? "true" : "false"}" class="focus-v3-picker-option ${linked ? "is-linked" : ""}" data-focus-picker-id="${esc(focus.id)}" data-focus-picker-search="${esc(search)}" ${linked ? "disabled" : ""}><span class="focus-v3-picker-option-copy"><strong>${esc(focus.title)}</strong><small>${esc(text(focus.goal))}</small></span><span class="${linked ? "focus-v3-picker-linked" : "focus-v3-picker-action"}">${linked ? `${icon("check")}${readerFocusCopy("已添加", "Added")}` : `${icon("plus")}${readerFocusCopy("添加", "Add")}`}</span></button>`;
    }).join("");
    layer.innerHTML = `<section id="focus-v3-picker-content" class="focus-v3-picker-popover" role="dialog" aria-modal="false" aria-label="${readerFocusCopy("选择焦点", "Select focus")}"><label class="focus-v3-picker-search">${icon("search")}<input type="search" autocomplete="off" placeholder="${readerFocusCopy("搜索焦点", "Search focuses")}" aria-label="${readerFocusCopy("搜索焦点", "Search focuses")}"></label><div class="focus-v3-picker-list" role="listbox" aria-label="${readerFocusCopy("选择焦点", "Select focus")}"><span class="focus-v3-picker-label">${readerFocusCopy("选择焦点", "Select focus")}</span>${options}<p class="focus-v3-picker-empty" ${options ? "hidden" : ""}>${options ? readerFocusCopy("没有匹配的焦点", "No matching focuses") : readerFocusCopy("还没有可用焦点", "No focuses available")}</p></div><p class="focus-v3-picker-message" role="status"></p></section>`;
    const popover = layer.querySelector(".focus-v3-picker-popover");
    const searchInput = layer.querySelector(".focus-v3-picker-search input");
    const empty = layer.querySelector(".focus-v3-picker-empty");
    const pickerOptions = [...layer.querySelectorAll("[data-focus-picker-id]")];
    const place = () => {
      if (layer.hidden || !trigger?.isConnected) return;
      positionFocusPicker(trigger, popover);
    };
    let placeFrame = 0;
    const schedulePlace = () => {
      if (placeFrame) return;
      placeFrame = requestAnimationFrame(() => {
        placeFrame = 0;
        place();
      });
    };
    const dismiss = (event) => {
      if (popover.contains(event.target) || trigger?.contains(event.target)) return;
      closeFocusPicker(false);
    };
    const filter = () => {
      const query = text(searchInput?.value).trim().toLocaleLowerCase();
      let visible = 0;
      pickerOptions.forEach((node) => {
        node.hidden = Boolean(query) && !text(node.dataset.focusPickerSearch).includes(query);
        if (!node.hidden) visible += 1;
      });
      if (empty) {
        empty.hidden = visible > 0;
        empty.textContent = parity.focusSpaces.some((focus) => focus.status !== "archived")
          ? readerFocusCopy("没有匹配的焦点", "No matching focuses")
          : readerFocusCopy("还没有可用焦点", "No focuses available");
      }
    };
    const navigate = (event) => {
      if (!["ArrowDown", "ArrowUp"].includes(event.key)) return;
      const candidates = pickerOptions.filter((node) => !node.hidden && !node.disabled);
      if (!candidates.length) return;
      event.preventDefault();
      const current = candidates.indexOf(document.activeElement);
      const delta = event.key === "ArrowDown" ? 1 : -1;
      candidates[(current + delta + candidates.length) % candidates.length].focus();
    };
    searchInput?.addEventListener("input", filter);
    popover.addEventListener("keydown", navigate);
    document.addEventListener("pointerdown", dismiss, true);
    document.addEventListener("scroll", schedulePlace, true);
    window.addEventListener("resize", schedulePlace);
    parity.focusPickerCleanup = () => {
      document.removeEventListener("pointerdown", dismiss, true);
      document.removeEventListener("scroll", schedulePlace, true);
      window.removeEventListener("resize", schedulePlace);
      if (placeFrame) cancelAnimationFrame(placeFrame);
    };
    layer.querySelectorAll("[data-focus-picker-id]").forEach((node) => node.addEventListener("click", async () => {
      const focus = parity.focusSpaces.find((item) => item.id === node.dataset.focusPickerId);
      if (!focus || node.disabled) return;
      const message = layer.querySelector(".focus-v3-picker-message");
      layer.querySelectorAll("button,input").forEach((control) => control.disabled = true);
      if (message) message.textContent = readerFocusCopy("正在加入焦点…", "Adding to focus…");
      try {
        await callFocusOperation("focus_space.update", { body: { schema_version: 1, action: "link_source", focus_id: focus.id, expected_revision: Number(focus.revision), source_kind: descriptor.sourceKind, source_id: descriptor.sourceID }, key: `focus-v3:link:${focus.id}:${focus.revision}:${descriptor.sourceKind}:${descriptor.sourceID}:on` });
        await loadFocusSpaces(false);
        syncFocusPickerTrigger(trigger, descriptor);
        render();
        closeFocusPicker();
        showToast(readerFocusCopy("已加入焦点", "Added to focus"));
      } catch (error) {
        if (message) message.textContent = error.message || readerFocusCopy("焦点更新失败", "Focus update failed");
        pickerOptions.forEach((control) => control.disabled = control.classList.contains("is-linked"));
        if (searchInput) searchInput.disabled = false;
      }
    }));
    requestAnimationFrame(() => {
      place();
      searchInput?.focus({ preventScroll: true });
    });
  }

  function openFocusCreateModal(existing = null) {
    ensureFocusV3Surface();
    const layer = $("focus-v3-create-layer");
    layer.hidden = false;
    const editing = Boolean(existing);
    const closeLabel = readerFocusCopy("关闭", "Close");
    const initialIntent = ["decide", "plan", "create", "monitor"].includes(text(existing?.intent_kind))
      ? text(existing.intent_kind)
      : recommendFocusIntent(`${text(existing?.title)} ${text(existing?.goal)} ${text(existing?.decision_question)}`);
    const initialThinking = text(existing?.thinking_template_id, focusDefaultThinkingTemplate(initialIntent));
    const intentOptions = [
      ["decide", readerFocusCopy("做决定", "Decide"), readerFocusCopy("比较方案并形成明确决定", "Compare options and reach a decision")],
      ["plan", readerFocusCopy("制订计划", "Plan"), readerFocusCopy("形成里程碑、安排和 TODO", "Create milestones, schedule, and to-dos")],
      ["create", readerFocusCopy("创作内容", "Create"), readerFocusCopy("形成 Brief、大纲和内容产出", "Create a brief, outline, and content")],
      ["monitor", readerFocusCopy("持续关注", "Monitor"), readerFocusCopy("持续收集信号，必要时升级判断", "Track signals and escalate judgments when needed")],
    ];
    const thinkingOptions = [
      ["guided", readerFocusCopy("引导式", "Guided")],
      ["rational", readerFocusCopy("理性分析", "Rational")],
      ["bounded_rationality", readerFocusCopy("有限理性", "Bounded rationality")],
      ["first_principles", readerFocusCopy("第一性原理", "First principles")],
    ];
    layer.innerHTML = `<button class="focus-v3-create-mask" type="button" data-close-focus-create aria-label="${esc(closeLabel)}"></button><form class="focus-v3-create-dialog" data-focus-create-form><header><div><span class="focus-v3-eyebrow">${esc(readerFocusCopy(editing ? "编辑焦点" : "新建焦点", editing ? "Edit focus" : "New focus"))}</span><h2>${esc(readerFocusCopy(editing ? "调整目标和工作方式" : "建立一个要推进的目标", editing ? "Adjust the goal and workflow" : "Set a goal to move forward"))}</h2></div><button class="icon-btn" type="button" data-close-focus-create aria-label="${esc(closeLabel)}">${icon("close")}</button></header><label><span>${esc(readerFocusCopy("焦点名称", "Focus name"))}</span><input name="title" maxlength="120" required value="${esc(text(existing?.title))}" /></label><label><span>${esc(readerFocusCopy("目标", "Goal"))}</span><textarea name="goal" maxlength="500" required>${esc(text(existing?.goal))}</textarea></label><label><span>${esc(readerFocusCopy("关键问题（可选）", "Key question (optional)"))}</span><textarea name="decision_question" maxlength="500">${esc(text(existing?.decision_question))}</textarea></label><div class="focus-v3-create-grid"><label><span>${esc(readerFocusCopy("任务模式", "Task mode"))}</span><select name="intent_kind">${intentOptions.map(([value, label]) => `<option value="${value}"${initialIntent === value ? " selected" : ""}>${esc(label)}</option>`).join("")}</select><small data-focus-intent-hint></small></label><label><span>${esc(readerFocusCopy("思考模板", "Thinking template"))}</span><select name="thinking_template_id">${thinkingOptions.map(([value, label]) => `<option value="${value}"${initialThinking === value ? " selected" : ""}>${esc(label)}</option>`).join("")}</select><small>${esc(readerFocusCopy("任务模式决定产出，思考模板决定推理方式。", "The task mode controls the output; the thinking template controls the reasoning."))}</small></label></div><label class="focus-v3-deadline-field"><span>${esc(readerFocusCopy("截止时间", "Deadline"))}</span><input name="deadline" type="date" value="${esc(text(existing?.horizon_end).slice(0, 10))}" /></label><div class="focus-v3-create-summary"><span data-focus-mode-hint></span><span data-focus-blueprint-hint></span></div><footer><button class="text-btn" type="button" data-close-focus-create>${esc(readerFocusCopy("取消", "Cancel"))}</button><button class="primary-btn" type="submit">${esc(readerFocusCopy(editing ? "保存" : "创建焦点", editing ? "Save" : "Create focus"))}</button></footer></form>`;
    const form = layer.querySelector("[data-focus-create-form]");
    const intentSelect = form.elements.intent_kind;
    const thinkingSelect = form.elements.thinking_template_id;
    const intentHint = form.querySelector("[data-focus-intent-hint]");
    const blueprintHint = form.querySelector("[data-focus-blueprint-hint]");
    const modeHint = form.querySelector("[data-focus-mode-hint]");
    let intentTouched = editing;
    let thinkingTouched = editing;
    const syncSummary = () => {
      const intent = text(intentSelect.value, "decide");
      const intentOption = intentOptions.find(([value]) => value === intent);
      if (intentHint) intentHint.textContent = intentOption?.[2] || "";
      if (blueprintHint) blueprintHint.textContent = `${readerFocusCopy("工作流", "Workflow")} · ${focusBlueprintID(intent)}`;
      const sprint = Boolean(form.elements.deadline.value);
      if (modeHint) modeHint.innerHTML = sprint
        ? `<span class="focus-v3-mode sprint">${icon("sparkles")}${readerFocusCopy("冲刺型焦点", "Sprint focus")}</span>`
        : `<span class="focus-v3-mode slow">${icon("note")}${readerFocusCopy("酝酿型焦点", "Slow-burn focus")}</span>`;
    };
    const recommendFromInput = () => {
      if (intentTouched) return;
      const recommended = recommendFocusIntent(`${form.elements.title.value} ${form.elements.goal.value} ${form.elements.decision_question.value}`);
      intentSelect.value = recommended;
      if (!thinkingTouched) thinkingSelect.value = focusDefaultThinkingTemplate(recommended);
      syncSummary();
    };
    [form.elements.title, form.elements.goal, form.elements.decision_question].forEach((input) => input.addEventListener("input", recommendFromInput));
    intentSelect.addEventListener("change", () => {
      intentTouched = true;
      if (!thinkingTouched) thinkingSelect.value = focusDefaultThinkingTemplate(intentSelect.value);
      syncSummary();
    });
    thinkingSelect.addEventListener("change", () => { thinkingTouched = true; });
    form.elements.deadline.addEventListener("input", syncSummary);
    syncSummary();
    layer.querySelectorAll("[data-close-focus-create]").forEach((node) => node.addEventListener("click", () => { layer.hidden = true; layer.innerHTML = ""; }));
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const data = new FormData(form);
      const deadline = text(data.get("deadline"));
      const intentKind = text(data.get("intent_kind"), "decide");
      const patch = {
        title: text(data.get("title")),
        goal: text(data.get("goal")),
        decision_question: text(data.get("decision_question")),
        decision_mode: deadline ? "sprint" : "slow_burn",
        horizon_mode: deadline ? "fixed" : "open",
        horizon_start: deadline ? new Date().toISOString() : "",
        horizon_end: deadline ? `${deadline}T23:59:59Z` : "",
        kind: intentKind === "decide" ? "decision" : "monitor",
        intent_kind: intentKind,
        blueprint_id: focusBlueprintID(intentKind),
        thinking_template_id: text(data.get("thinking_template_id"), focusDefaultThinkingTemplate(intentKind)),
      };
      form.querySelectorAll("button,input,textarea,select").forEach((node) => node.disabled = true);
      try {
        if (editing) await mutateFocusV3(existing, patch);
        else await callFocusOperation("focus_space.update", {
          body: { schema_version: 1, action: "create", expected_revision: 0, workflow_state: "accumulating", ...patch },
          key: `focus-v3:create:${Date.now()}`,
        });
        layer.hidden = true;
        layer.innerHTML = "";
        await loadFocusSpaces(false);
        render();
        showToast(readerFocusCopy(editing ? "焦点已保存" : "焦点已创建，确认资料后再开始分析", editing ? "Focus saved" : "Focus created; confirm sources before starting analysis"));
      } catch (error) {
        showError(error.message || readerFocusCopy("焦点保存失败", "Focus save failed"));
        form.querySelectorAll("button,input,textarea,select").forEach((node) => node.disabled = false);
      }
    });
    requestAnimationFrame(() => form.elements.title.focus());
  }


  function bindFocusV3WorkspaceEvents(workspace) {
    workspace.querySelectorAll("[data-focus-view-mode]").forEach((node) => node.addEventListener("click", () => { parity.focusViewMode = node.dataset.focusViewMode; renderFocusV3Workspace(); }));
    workspace.querySelectorAll("[data-action-view-mode]").forEach((node) => node.addEventListener("click", () => { parity.actionViewMode = node.dataset.actionViewMode; renderFocusV3Workspace(); }));
    workspace.querySelector("[data-action-focus-filter]")?.addEventListener("change", (event) => { parity.actionFocusFilter = event.currentTarget.value || "all"; renderFocusV3Workspace(); });
    workspace.querySelectorAll("[data-create-focus]").forEach((node) => node.addEventListener("click", () => openFocusCreateModal()));
    workspace.querySelectorAll("[data-open-focus]").forEach((node) => node.addEventListener("click", () => openFocusV3Drawer(node.dataset.openFocus)));
    workspace.querySelectorAll("[data-open-action]").forEach((node) => node.addEventListener("click", () => openFocusV3ActionDrawer(node.dataset.openAction)));
    workspace.querySelectorAll("[data-action-feedback]").forEach((node) => node.addEventListener("click", async () => {
      const card = node.closest("[data-focus-result-card]");
      const item = focusResultFromCard(card);
      if (!item) return;
      await submitFocusResultFeedback(item, node.dataset.actionFeedback, null, card);
      renderFocusV3Workspace();
    }));
    workspace.querySelectorAll("[data-focus-v3-card]").forEach((node) => node.addEventListener("dragstart", (event) => { event.dataTransfer.effectAllowed = "move"; event.dataTransfer.setData("text/plain", node.dataset.focusV3Card); }));
    workspace.querySelectorAll("[data-focus-lane]").forEach((lane) => { lane.addEventListener("dragover", (event) => { event.preventDefault(); lane.classList.add("drop-target"); }); lane.addEventListener("dragleave", () => lane.classList.remove("drop-target")); lane.addEventListener("drop", (event) => { event.preventDefault(); lane.classList.remove("drop-target"); const focus = parity.focusSpaces.find((item) => item.id === event.dataTransfer.getData("text/plain")); const mode = text(lane.dataset.focusMode); if (focus && (focus.workflow_state !== lane.dataset.focusLane || (mode && focus.decision_mode !== mode))) void mutateFocusV3(focus, { workflow_state: lane.dataset.focusLane, ...(mode ? { decision_mode: mode } : {}), status: lane.dataset.focusLane === "archived" ? "archived" : "active" }); }); });
    workspace.querySelectorAll("[data-action-toggle]").forEach((node) => node.addEventListener("click", async () => { const card = node.closest("[data-focus-result-card]"); const item = focusResultFromCard(card); if (!item) return; await submitFocusResultFeedback(item, "edited", { completed: node.dataset.actionCompleted !== "true" }, card); renderFocusV3Workspace(); }));
    bindFocusSpaceOverviewEvents(workspace);
  }

  function bindFocusV3DrawerEvents(layer, focus) {
    const currentFocus = () => parity.focusSpaces.find((item) => text(item.id) === text(focus.id)) || focus;
    layer.querySelectorAll("[data-close-focus]").forEach((node) => node.addEventListener("click", closeFocusV3Drawer));
    layer.querySelectorAll("[data-edit-focus]").forEach((node) => node.addEventListener("click", () => openFocusCreateModal(currentFocus())));
    // Bind the drawer tabs directly as well as through the delegated drawer
    // handler below. The drawer body is updated in place; a direct listener
    // keeps the primary tab action reliable even when another nested control
    // stops propagation during a focus detail refresh.
    layer.querySelectorAll('.focus-v3-tabs [role="tab"][data-focus-panel]').forEach((tab) =>
      tab.addEventListener("click", (event) => {
        event.preventDefault();
        event.stopPropagation();
        updateFocusV3DrawerPanel(layer, currentFocus(), text(tab.dataset.focusPanel), { tabSwitch: true });
      })
    );
    layer.addEventListener("click", (event) => {
      const panelTrigger = event.target?.closest?.("[data-focus-panel]");
      if (panelTrigger && layer.contains(panelTrigger)) {
        event.preventDefault();
        updateFocusV3DrawerPanel(layer, currentFocus(), text(panelTrigger.dataset.focusPanel), { tabSwitch: true });
        return;
      }
      const actionTrigger = event.target?.closest?.("[data-open-focus-actions]");
      if (actionTrigger && layer.contains(actionTrigger)) {
        closeFocusV3Drawer();
        switchProductV3View("action");
        return;
      }
      const entryTrigger = event.target?.closest?.("[data-open-linked-entry]");
      if (entryTrigger && layer.contains(entryTrigger)) {
        const key = text(entryTrigger.dataset.openLinkedEntry);
        const entry = makeEntries(state.dashboard || {}).find((item) => item.key === key);
        const sourceKind = entry?.kind === "recording"
          ? "meeting"
          : entry?.kind === "evidence" && text(entry.item?.source_type).toLowerCase() === "obsidian"
            ? "obsidian"
            : "";
        const citation = focusLinkedSourceCitation({ source_kind: sourceKind }, entry);
        if (citation) void openCitationDrawer(citation);
      }
    });
    const tablist = layer.querySelector('.focus-v3-tabs[role="tablist"]');
    tablist?.addEventListener("keydown", (event) => {
      if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
      const tabs = [...tablist.querySelectorAll('[role="tab"]')];
      if (!tabs.length) return;
      const current = Math.max(0, tabs.indexOf(document.activeElement));
      const next = event.key === "Home"
        ? 0
        : event.key === "End"
          ? tabs.length - 1
          : (current + (event.key === "ArrowRight" ? 1 : -1) + tabs.length) % tabs.length;
      event.preventDefault();
      tabs[next].focus({ preventScroll: true });
      updateFocusV3DrawerPanel(layer, currentFocus(), text(tabs[next].dataset.focusPanel));
    });
    bindFocusSpaceOverviewEvents(layer.querySelector(".focus-v3-panel"));
  }

  document.addEventListener("click", (event) => {
    const trigger = event.target?.closest?.("[data-add-entry-focus]");
    if (!trigger) return;
    event.preventDefault();
    const layer = $("focus-v3-picker-layer");
    if (layer && !layer.hidden && parity.focusPickerTrigger === trigger) {
      closeFocusPicker(true);
      return;
    }
    void openFocusPicker(text(trigger.dataset.addEntryFocus), trigger);
  });

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || event.repeat) return;
    const pickerLayer = $("focus-v3-picker-layer");
    if (pickerLayer && !pickerLayer.hidden) { closeFocusPicker(true); return; }
    const createLayer = $("focus-v3-create-layer");
    if (createLayer && !createLayer.hidden) { createLayer.hidden = true; createLayer.innerHTML = ""; return; }
    if (citationDrawerElements().length) return;
    if (!$("focus-v3-drawer-layer")?.hidden) {
      closeFocusV3Drawer();
      return;
    }
    if (!$("focus-v3-action-drawer-layer")?.hidden) closeFocusV3ActionDrawer();
  }, true);

  // Focus Space is an optional projection. The dashboard and Chat both read
  // the same persisted analysis run; older snapshots fall back to the first
  // enabled reader topic and disappear cleanly when neither projection exists.
  function activeFocusSpaceProjection(dashboard) {
    const raw = dashboard?.focus_space || dashboard?.focusSpace;
    const selectedV3 = parity.focusSpaces.find((item) => item.id === parity.selectedFocusID) || parity.focusSpaces.find((item) => item.status === "active");
    const explicitFocus = selectedV3 || (raw && typeof raw === "object"
      ? raw
      : Array.isArray(dashboard?.focus_spaces)
        ? dashboard.focus_spaces.find((item) => item && item.status === "active")
        : null);
    const legacyTopic = Array.isArray(dashboard?.reader_focus?.topics)
      ? dashboard.reader_focus.topics.find((item) => item && item.enabled !== false)
      : null;
    const focus = explicitFocus && (!explicitFocus.status || explicitFocus.status === "active")
      ? explicitFocus
      : legacyTopic
        ? {
            id: legacyTopic.id,
            title: legacyTopic.label,
            goal: legacyTopic.label,
            kind: "monitor",
            horizon_mode: "rolling",
            horizon_start: legacyTopic.watch_since,
            status: "active",
          }
        : null;
    return focus && typeof focus === "object" ? focus : null;
  }

  function renderFocusSpaceOverview(dashboard) {
    const hidden = new Set(dashboard?.hidden_modules || []);
    if (hidden.has("focus_space")) return "";
    const focus = activeFocusSpaceProjection(dashboard);
    if (!focus || typeof focus !== "object") return "";
    const projectedAnalysis = dashboard?.focus_analysis;
    const analysis = projectedAnalysis && typeof projectedAnalysis === "object" &&
      (!focus.id || !projectedAnalysis.focus_id || projectedAnalysis.focus_id === focus.id)
      ? projectedAnalysis
      : null;
    const runID = text(analysis?.run_id);
    const sameRun = (item) =>
      item && (!runID || text(item.analysis_run_id) === runID);
    const signals = Array.isArray(analysis?.signals)
      ? analysis.signals.filter(sameRun)
      : [];
    const actions = Array.isArray(analysis?.actions)
      ? analysis.actions.filter(sameRun)
      : [];
    const decisions = Array.isArray(analysis?.decisions)
      ? analysis.decisions.filter(sameRun)
      : [];
    const evidence = Array.isArray(analysis?.evidence)
      ? analysis.evidence.slice(0, 5)
      : [];
    const goal = text(focus.goal || focus.title).slice(0, 240);
    const question = text(focus.decision_question || focus.question).slice(0, 240);
    const horizonDates = [focus.horizon_start, focus.horizon_end]
      .map((value) => text(value))
      .filter(Boolean)
      .map((value) => formatDateByLocale(value, { year: "numeric", month: "short", day: "numeric" }));
    const horizonMode = text(focus.horizon_mode).toLowerCase();
    const horizon = horizonDates.length
      ? horizonDates.join(" – ")
      : horizonMode === "rolling"
        ? readerFocusCopy("持续滚动", "Rolling")
        : horizonMode === "fixed"
          ? readerFocusCopy("固定区间", "Fixed range")
          : readerFocusCopy("开放时间范围", "Open-ended");
    const kind = text(focus.kind).toLowerCase();
    const kindLabel = kind === "decision"
      ? readerFocusCopy("决策焦点", "Decision focus")
      : readerFocusCopy("监测焦点", "Monitoring focus");
    const listValue = (value) => {
      if (!Array.isArray(value)) return [];
      return value
        .map((item) => typeof item === "string" ? item : item?.text || item?.title || item?.summary || "")
        .map((item) => text(item).slice(0, 320))
        .filter(Boolean)
        .slice(0, 3);
    };
    const mergeValues = (...groups) => {
      const result = [];
      const seen = new Set();
      for (const group of groups) {
        for (const value of listValue(group)) {
          if (seen.has(value)) continue;
          seen.add(value);
          result.push(value);
          if (result.length === 3) return result;
        }
      }
      return result;
    };
    const resultData = (item) =>
      item?.data && typeof item.data === "object" ? item.data : {};
    const feedbackPending = (item) =>
      !["accepted", "rejected", "edited"].includes(
        text(item?.feedback_status, "draft").toLowerCase()
      );
    const signalChanges = signals
      .filter((item) => text(item.feedback_status).toLowerCase() !== "rejected")
      .map((item) => {
        const data = resultData(item);
        const title = text(data.title);
        const detail = text(data.detail);
        return title && detail ? `${title}：${detail}` : title || detail;
      });
    const evidenceLabels = evidence.map((item) => {
      const title = text(item?.title);
      const excerpt = text(item?.excerpt);
      return title && excerpt && title !== excerpt
        ? `${title}：${excerpt}`
        : title || excerpt;
    });
    const counterEvidence = signals.flatMap((item) => {
      const values = resultData(item).counterevidence;
      return Array.isArray(values) ? values : [];
    });
    const pendingConclusions = [
      ...decisions.filter(feedbackPending).map((item) => {
        const data = resultData(item);
        return text(data.recommendation || data.question);
      }),
      ...signals.filter(feedbackPending).map((item) => {
        const data = resultData(item);
        return text(data.title || data.detail);
      }),
    ];
    const actionSteps = actions
      .filter((item) => text(item.feedback_status).toLowerCase() !== "rejected")
      .map((item) => text(resultData(item).task));
    const changes = mergeValues(
      focus.important_changes || focus.changes,
      signalChanges
    );
    const supporting = mergeValues(
      focus.supporting_evidence || focus.supportingEvidence,
      evidenceLabels
    );
    const counter = mergeValues(
      focus.counter_evidence || focus.counterEvidence,
      counterEvidence
    );
    const conclusions = mergeValues(
      focus.pending_conclusions || focus.conclusions,
      pendingConclusions
    );
    const nextSteps = mergeValues(
      focus.next_steps || focus.nextSteps,
      actionSteps
    );
    const focusResults = focusResultItems(analysis)
      .map((item) => focusResultCardMarkup(item))
      .filter(Boolean)
      .join("");
    const focusResultsSection = focusResults
      ? `<div class="focus-space-section focus-space-results"><div class="overview-subsection-label">${esc(readerFocusCopy("焦点结果", "Focus results"))}</div><div class="focus-result-list">${focusResults}</div></div>`
      : "";
    const materialsSection = focusMaterialsMarkup(focus);
    const decisionRunPanel = decisionRunPanelMarkup(focus, analysis, dashboard);
    const field = (label, value) => value
      ? `<div class="focus-space-field"><dt>${esc(label)}</dt><dd>${esc(value)}</dd></div>`
      : "";
    const listSection = (label, values, className = "") => values.length
      ? `<div class="focus-space-section ${className}"><div class="overview-subsection-label">${esc(label)}</div><ul>${values.map((value) => `<li>${esc(value)}</li>`).join("")}</ul></div>`
      : "";
    const details = [
      field(readerFocusCopy("目标", "Goal"), goal),
      field(readerFocusCopy("关键问题", "Decision question"), question),
      field(readerFocusCopy("时间范围", "Time horizon"), horizon),
    ].join("");
    const sections = [
      listSection(readerFocusCopy("重要变化", "Important changes"), changes, "focus-space-changes"),
      listSection(readerFocusCopy("支持证据", "Supporting evidence"), supporting, "focus-space-supporting"),
      listSection(readerFocusCopy("反向证据", "Counter-evidence"), counter, "focus-space-counter"),
      listSection(readerFocusCopy("待确认结论", "Unconfirmed conclusions"), conclusions, "focus-space-conclusions"),
      listSection(readerFocusCopy("下一步", "Next step"), nextSteps, "focus-space-next"),
      materialsSection,
      focusResultsSection,
    ].join("");
    if (!details && !sections && !decisionRunPanel) return "";
    return `<section class="overview-module focus-space-module" data-overview-module="focus_space"><div class="module-title-row"><h3>${esc(readerFocusCopy("当前焦点", "Current focus"))}</h3>${badge(kindLabel, "blue")}${hideButton("focus_space")}</div>${details ? `<dl class="focus-space-fields">${details}</dl>` : ""}${decisionRunPanel}${sections}</section>`;
  }

  function renderReaderFocusOverview(dashboard) {
    // Focus Space is now the canonical place for source context, decisions,
    // actions, and results. The legacy reader-focus block duplicated that
    // information whenever an active Focus Space existed, so keep it only for
    // older dashboards that have no Focus Space projection yet.
    if (activeFocusSpaceProjection(dashboard)) return "";
    const focus = dashboard?.reader_focus;
    const recommendations = dashboard?.reader_recommendations;
    if (new Set(dashboard?.hidden_modules || []).has("reader_focus")) return "";
    if (
      (!focus || typeof focus !== "object") &&
      (!recommendations || typeof recommendations !== "object")
    )
      return "";

    const topics = Array.isArray(focus?.topics)
      ? focus.topics.filter((topic) => topic && topic.enabled !== false).slice(0, 8)
      : [];
    const policy = focus?.policy && typeof focus.policy === "object" ? focus.policy : {};
    const items = Array.isArray(recommendations?.items)
      ? recommendations.items.slice(0, 5)
      : [];
    const pending = readerFocusNumber(focus?.pending_count);
    const historical = readerFocusNumber(focus?.historical_unread_count);
	const candidateCount = readerFocusNumber(focus?.candidate_count);
	const candidateScanTruncated = focus?.candidate_scan_truncated === true;
    const estimatedLow = readerFocusNumber(focus?.estimated_tokens_low);
    const estimatedHigh = readerFocusNumber(focus?.estimated_tokens_high);
    const dailyUsed = readerFocusNumber(focus?.daily_used_tokens);
    const dailyRemaining = readerFocusNumber(focus?.daily_remaining_tokens);
    const dailyBudget = readerFocusNumber(policy.daily_token_budget);
    const statusRaw = text(focus?.bootstrap_status || focus?.status || policy.mode);
    const status = ["disabled", "off", "none"].includes(statusRaw.toLowerCase())
      ? readerFocusCopy("自动预分析未开启", "Daily analysis is off")
      : readerFocusLifecycleStatus(statusRaw);
	const hasContent = topics.length || items.length || candidateCount || pending || historical || status;
    if (!hasContent) return "";

    const topicMarkup = topics.length
      ? `<div class="reader-focus-topics" aria-label="${esc(
          readerFocusCopy("当前焦点", "Current focus")
        )}">${topics
          .map(
            (topic) =>
              `<span class="reader-focus-topic">${esc(
                text(topic.label || topic.name || topic.id).slice(0, 80)
              )}</span>`
          )
          .join("")}</div>`
      : `<p class="reader-focus-empty">${esc(
          readerFocusCopy("还没有设置当前焦点", "No current focus yet")
        )}</p>`;

    const budgetParts = [];
	if (candidateCount) {
	  budgetParts.push(
		`${readerFocusCopy("匹配候选", "Matching candidates")} ${candidateCount}${candidateScanTruncated ? "+" : ""}`
	  );
	}
	if (candidateScanTruncated) {
	  budgetParts.push(
		readerFocusCopy("统计仅覆盖最近 500 条未读内容", "Counts cover only the latest 500 unread items")
	  );
	}
    if (pending) budgetParts.push(`${readerFocusCopy("待分析", "Pending analysis")} ${pending}`);
    if (historical) budgetParts.push(`${readerFocusCopy("历史未读积压", "Historical unread backlog")} ${historical}`);
    if (estimatedLow || estimatedHigh) {
      budgetParts.push(
        `${readerFocusCopy("本轮估算", "Estimated this round")} ${estimatedLow.toLocaleString()}–${estimatedHigh.toLocaleString()} ${readerFocusCopy("tokens", "tokens")}`
      );
    }
    if (dailyBudget) {
      budgetParts.push(
        `${readerFocusCopy("今日预算", "Daily budget")} ${dailyUsed.toLocaleString()}/${dailyBudget.toLocaleString()} ${readerFocusCopy("tokens", "tokens")}`
      );
    } else if (dailyRemaining) {
      budgetParts.push(
        `${readerFocusCopy("今日剩余", "Remaining today")} ${dailyRemaining.toLocaleString()} ${readerFocusCopy("tokens", "tokens")}`
      );
    }
    const budgetMarkup = budgetParts.length
      ? `<div class="reader-focus-budget">${budgetParts
          .map((part) => `<span>${esc(part)}</span>`)
          .join("")}</div>`
      : "";
    const statusMarkup = status
      ? `<p class="reader-focus-status">${esc(status)}</p>`
      : "";

    const recommendationMarkup = items.length
      ? `<div class="reader-recommendation-list">${items
          .map((item) => {
            const entryKey = readerFocusEntryKey(item, dashboard);
            const url = readerFocusURL(item.source_url || item.url);
            const title = text(item.title, readerFocusCopy("未命名内容", "Untitled content")).slice(0, 240);
            const statusInfo = readerFocusStatus(item.status || item.overview?.status);
            const topicLabels = Array.isArray(item.matched_topics)
              ? item.matched_topics
                  .slice(0, 3)
                  .map((topic) => `<span class="reader-recommendation-topic">${esc(text(topic).slice(0, 60))}</span>`)
                  .join("")
              : "";
            const summary = readerFocusOverviewText(item);
            const reason = text(item.reason).slice(0, 240);
            const source = text(item.source, readerFocusCopy("未知来源", "Unknown source")).slice(0, 100);
            const date = item.published_at ? formatDateByLocale(item.published_at, { month: "short", day: "numeric" }) : "";
            const meta = [source, date].filter(Boolean).join(" · ");
            const body = `<span class="reader-recommendation-copy"><strong>${esc(title)}</strong><span class="reader-recommendation-meta">${esc(meta)}</span>${topicLabels ? `<span class="reader-recommendation-topics">${topicLabels}</span>` : ""}${summary ? `<span class="reader-recommendation-summary">${esc(summary)}</span>` : ""}${reason ? `<span class="reader-recommendation-reason">${esc(reason)}</span>` : ""}</span>`;
            const statusBadge = badge(statusInfo.label, statusInfo.kind);
            if (entryKey)
              return `<button class="reader-recommendation" type="button" data-reader-recommendation-entry="${esc(entryKey)}"><span class="reader-recommendation-marker" aria-hidden="true">${icon("sparkles")}</span>${body}<span class="reader-recommendation-status">${statusBadge}</span></button>`;
            if (url)
              return `<a class="reader-recommendation" href="${esc(url)}" target="_blank" rel="noopener noreferrer"><span class="reader-recommendation-marker" aria-hidden="true">${icon("external")}</span>${body}<span class="reader-recommendation-status">${statusBadge}</span></a>`;
            return `<div class="reader-recommendation"><span class="reader-recommendation-marker" aria-hidden="true">${icon("sparkles")}</span>${body}<span class="reader-recommendation-status">${statusBadge}</span></div>`;
          })
          .join("")}</div>`
      : `<p class="reader-focus-empty">${esc(
          readerFocusCopy("暂无匹配的未读内容", "No matching unread content")
        )}</p>`;

    const localCount = readerFocusNumber(recommendations?.local_count);
    const webCount = readerFocusNumber(recommendations?.web_supplement_count);
    const recommendationMeta = [];
    if (localCount) recommendationMeta.push(`${localCount} ${readerFocusCopy("条本地推荐", "local")}`);
    if (webCount) recommendationMeta.push(`${webCount} ${readerFocusCopy("条网络补充", "web supplements")}`);
    return `<section class="overview-module reader-focus-module" data-overview-module="reader_focus"><div class="module-title-row"><h3>${esc(readerFocusCopy("当前焦点", "Current focus"))}</h3><span class="reader-focus-module-meta">${esc(readerFocusCopy("有界预分析", "Bounded analysis"))}</span>${hideButton("reader_focus")}</div>${topicMarkup}${budgetMarkup}${statusMarkup}<div class="reader-recommendation-head"><span>${esc(readerFocusCopy("今日推荐", "Today's recommendations"))}</span>${recommendationMeta.length ? `<small>${esc(recommendationMeta.join(" · "))}</small>` : ""}</div>${recommendationMarkup}${recommendations?.generated_at ? `<div class="reader-focus-updated">${esc(readerFocusCopy("更新于", "Updated"))} ${esc(formatDateByLocale(recommendations.generated_at, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }))}</div>` : ""}</section>`;
  }

  function bindReaderFocusOverviewEvents() {
    const panel = $("detail-panel");
    panel?.querySelectorAll('[data-hide-module="reader_focus"]').forEach((node) =>
      node.addEventListener("click", () =>
        void mutate("./api/modules", "PATCH", {
          id: "reader_focus",
          hidden: true,
        })
      )
    );
    panel?.querySelectorAll("[data-reader-recommendation-entry]").forEach((node) => {
      node.addEventListener("click", () => {
        const key = text(node.dataset.readerRecommendationEntry);
        if (key) selectEntry(key);
      });
    });
  }

  function bindFocusSpaceOverviewEvents(root = null) {
    const panel = root || $("detail-panel");
    if (!panel) return;
    panel.querySelectorAll("[data-decision-run-action]").forEach((node) =>
      node.addEventListener("click", () => startOrResumeDecisionRun(node))
    );
    panel.querySelectorAll("[data-decision-run-configure]").forEach((node) =>
      node.addEventListener("click", () => openDecisionRunStartDialog(node))
    );
    panel.querySelectorAll("[data-decision-run-answer]").forEach((form) => {
      form.addEventListener("input", () => captureDecisionRunDraft(form));
      form.addEventListener("change", () => captureDecisionRunDraft(form));
      form.addEventListener("submit", (event) => {
        event.preventDefault();
        submitDecisionRunAnswer(form);
      });
    });
    panel.querySelectorAll("[data-focus-feedback]").forEach((node) =>
      node.addEventListener("click", (event) => {
        event.preventDefault();
        event.stopPropagation();
        const card = node.closest("[data-focus-result-card]");
        const item = focusResultFromCard(card);
        void submitFocusResultFeedback(item, text(node.dataset.focusFeedback), null, card);
      })
    );
    panel.querySelectorAll("[data-focus-edit-toggle]").forEach((node) =>
      node.addEventListener("click", () => {
        const card = node.closest("[data-focus-result-card]");
        const editor = card?.querySelector("[data-focus-result-editor]");
        if (!editor) return;
        editor.hidden = !editor.hidden;
        if (!editor.hidden) editor.querySelector("textarea, input, select")?.focus();
      })
    );
    panel.querySelectorAll("[data-focus-edit-cancel]").forEach((node) =>
      node.addEventListener("click", () => {
        const editor = node.closest("[data-focus-result-editor]");
        if (editor) editor.hidden = true;
      })
    );
    panel.querySelectorAll("[data-focus-result-editor]").forEach((form) =>
      form.addEventListener("submit", (event) => {
        event.preventDefault();
        const card = form.closest("[data-focus-result-card]");
        const item = focusResultFromCard(card);
        void submitFocusResultFeedback(item, "edited", focusResultEditPatch(card, item), card);
      })
    );
    panel.querySelectorAll("[data-focus-analysis-run]").forEach((select) =>
      select.addEventListener("change", () => void selectFocusAnalysisRun(select.dataset.focusId, select.value))
    );
    panel.querySelectorAll("[data-work-product-feedback]").forEach((button) =>
      button.addEventListener("click", () => {
        const card = button.closest("[data-focus-work-product-card]");
        void submitFocusWorkProductFeedback(focusWorkProductFromCard(card), button.dataset.workProductFeedback, card);
      })
    );
    panel.querySelectorAll("[data-work-product-copy]").forEach((button) =>
      button.addEventListener("click", () => {
        const card = button.closest("[data-focus-work-product-card]");
        void copyFocusWorkProduct(focusWorkProductFromCard(card), card);
      })
    );
    panel.querySelectorAll("[data-work-product-download]").forEach((button) =>
      button.addEventListener("click", () => downloadFocusWorkProduct(focusWorkProductFromCard(button.closest("[data-focus-work-product-card]"))))
    );
    panel.querySelectorAll("[data-focus-workflow-expand]").forEach((button) =>
      button.addEventListener("click", () => {
        const focusID = text(button.closest("[data-focus-workflow-root]")?.dataset.focusId || parity.selectedFocusID);
        const graphState = focusWorkflowGraphState(focusID);
        graphState.expanded = !graphState.expanded;
        graphState.selectedNodeID = "";
        graphState.pan = null;
        graphState.zoom = 0;
        graphState.fitPending = true;
        refreshOpenFocusV3Drawer(focusID, "workspace");
      })
    );
    panel.querySelectorAll("[data-focus-workflow-fit]").forEach((button) =>
      button.addEventListener("click", () => {
        const graphRoot = button.closest("[data-focus-workflow-root]");
        const cy = graphRoot?.__focusWorkflowCy;
        if (!cy) return;
        cy.resize();
        cy.fit(cy.elements(), graphRoot.classList.contains("is-fullscreen") ? 64 : 36);
        const graphState = focusWorkflowGraphState(graphRoot.dataset.focusId);
        graphState.zoom = cy.zoom();
        graphState.pan = cy.pan();
      })
    );
    panel.querySelectorAll("[data-focus-workflow-fullscreen]").forEach((button) =>
      button.addEventListener("click", () => {
        const focusID = text(button.closest("[data-focus-workflow-root]")?.dataset.focusId || parity.selectedFocusID);
        const graphState = focusWorkflowGraphState(focusID);
        graphState.fullscreen = !graphState.fullscreen;
        graphState.fitPending = true;
        graphState.pan = null;
        graphState.zoom = 0;
        refreshOpenFocusV3Drawer(focusID, "workspace");
      })
    );
    panel.querySelectorAll("[data-focus-workflow-node-close]").forEach((button) =>
      button.addEventListener("click", () => {
        const focusID = text(button.closest("[data-focus-workflow-root]")?.dataset.focusId || parity.selectedFocusID);
        focusWorkflowGraphState(focusID).selectedNodeID = "";
        refreshOpenFocusV3Drawer(focusID, "workspace");
      })
    );
    const focusID = text(panel.querySelector("[data-focus-workflow-root]")?.dataset.focusId || parity.selectedFocusID);
    const focus = parity.focusSpaces.find((item) => text(item.id) === focusID);
    if (focus && panel.querySelector("[data-focus-workflow-root]")) {
      const analysis = focusAnalysisForPanel(focusID);
      requestAnimationFrame(() => initializeFocusWorkflowGraph(panel, focus, analysis));
    }
  }

  renderOverview = function renderParityOverview() {
    const panel = $("detail-panel");
    if (panel) {
      panel.dataset.detailFingerprint = "";
      panel.classList.remove(
        "x-detail-panel",
        "youtube-detail-panel",
        "obsidian-detail-panel",
        "meeting-v3-detail-panel",
        "article-v3-detail"
      );
      panel.removeAttribute("aria-busy");
    }
    baseRenderOverview();
    const focusSpaceMarkup = "";
    const focusMarkup = "";
    if (!panel) return;
    const evidence = panel.querySelector('[data-overview-module="evidence"]');
    const markup = focusSpaceMarkup + focusMarkup;
    if (evidence) evidence.insertAdjacentHTML("beforebegin", markup);
    else panel.insertAdjacentHTML("beforeend", markup);
    panel.querySelector('[data-hide-module="focus_space"]')?.addEventListener("click", () =>
      void mutate("./api/modules", "PATCH", { id: "focus_space", hidden: true })
    );
    bindFocusSpaceOverviewEvents();
    bindReaderFocusOverviewEvents();
    const activeFocusID = text(activeFocusSpaceProjection(state.dashboard || {})?.id);
    if (activeFocusID && !parity.decisionRunLoaded.has(activeFocusID) && !parity.decisionRunLoading.has(activeFocusID))
      void loadDecisionRunState(activeFocusID, { silent: true });
  };

  function syncLoadedEntryMeta(entries = filteredEntries()) {
    if (liveDeviceDataView()) {
      const view = state.view === "meeting" || state.view === "recording" ? "meeting" : "obsidian";
      // Obsidian's title is a directory browser; the authoritative total is
      // useful for navigation badges but redundant beside the page title.
      $("timeline-count").textContent = view === "obsidian" ? "" : String(parity.deviceCounts[view] ?? "—");
      return;
    }
    if (!["rss", "video", "youtube", "picture", "all"].includes(state.view)) return;
    const configuredTotal = entryTotalForView(state.view);
    const loaded = loadedAggregateEntryCount(state.view, entries);
    const total = Math.max(loaded, configuredTotal === null ? 0 : configuredTotal);
    $("timeline-meta").textContent = readerFocusCopy(
      `已加载 ${loaded} / 共 ${total} 条`,
      `${loaded} loaded / ${total} total`
    );
  }

  function syncXFeedProfileHeader(source) {
    const head = document.querySelector(".timeline-head");
    head?.querySelector("[data-x-feed-profile]")?.remove();
    if (!head || !xSourceIsX(source)) return;
    const handle =
      xAccounts[text(source?.id)] || xProfileHandle(source?.site_url);
    if (!handle) return;
    const name = text(source?.title_override || source?.name, `@${handle}`);
    const profileURL = `https://x.com/${handle}`;
    head.insertAdjacentHTML(
      "afterbegin",
      `<section class="x-feed-profile" data-x-feed-profile aria-label="${esc(
        name
      )} 的 X 时间线"><div class="x-feed-profile-avatar">${sourceAvatar(
        source,
        "x-author-avatar"
      )}</div><div class="x-feed-profile-copy"><strong>${esc(
        name
      )}</strong><span>@${esc(
        handle
      )}</span></div><a href="${esc(
        profileURL
      )}" target="_blank" rel="noopener noreferrer" aria-label="在 X 查看 ${esc(
        name
      )}"><b aria-hidden="true">𝕏</b><span>在 X 查看</span>${icon(
        "external"
      )}</a></section>`
    );
  }

  renderNav = function renderParityNav() {
    baseRenderNav();
    document.body.classList.toggle("meeting-v3-view", state.view === "meeting");
    const focusCounts = focusNavigationCounts();
    Object.entries(focusCounts).forEach(([key, value]) => {
      const node = document.querySelector(`[data-count="${key}"]`);
      if (node) node.textContent = String(value);
    });
    const aggregateCounts = parity.counts || {};
    const totalEntries = Number(aggregateCounts.total_entries);
    const videoEntries = Number(aggregateCounts.video_entries);
    const derivedRSSEntries =
      Number.isFinite(totalEntries) && Number.isFinite(videoEntries)
        ? Math.max(0, totalEntries - videoEntries)
        : undefined;
    const navigationCounts = {
      all: aggregateCounts.total_entries,
      rss:
        aggregateCounts.rss_entries ??
        aggregateCounts.article_entries ??
        derivedRSSEntries,
      video: aggregateCounts.video_entries,
      youtube: aggregateCounts.youtube_entries,
      picture: aggregateCounts.picture_entries,
    };
    Object.entries(navigationCounts).forEach(([key, value]) => {
      if (!Number.isFinite(Number(value))) return;
      const node = document.querySelector(`[data-count="${key}"]`);
      if (node) node.textContent = String(Math.max(0, Number(value)));
    });
    const source = selectedCatalogSource();
    syncXFeedProfileHeader(source);
    if (source) {
      $("timeline-title").textContent = source.title_override || source.name;
      const stored = Number(source.entry_count || 0);
      $("timeline-meta").textContent =
        stored > 0
          ? source.status === "ok"
            ? t("sourceAvailableCount", { count: stored })
            : t("sourceAvailableCountDegraded", { count: stored })
          : gatewaySourcePreparing(source)
          ? t("gatewayPreparing")
          : t("subscriptionWaiting");
    }
    if (state.view === "later") {
      $("timeline-title").textContent = t("readLater");
      $("timeline-meta").textContent = t("savedContent");
    }
    if (state.view === "context") {
      $("timeline-title").textContent = t("knowledgeBase");
      $("timeline-meta").textContent = t("knowledgeOnly");
    }
    if (state.view === "obsidian" || state.view.startsWith("obsidian-folder:")) {
      const folder = obsidianFolderFromView(state.view);
      $("timeline-title").textContent = folder
        ? folder.split("/").pop() || "Obsidian"
        : "Obsidian";
      $("timeline-meta").textContent = folder
        ? readerFocusCopy(`Obsidian / ${folder}`, `Obsidian / ${folder}`)
        : readerFocusCopy("按 Obsidian 目录浏览笔记", "Browse notes by Obsidian folder");
    }
    if (state.view === "meeting") {
      $("timeline-title").textContent = "会议与录音";
      $("timeline-meta").textContent = "按时间回顾录音、AI 纪要与转录内容";
    }
    syncLoadedEntryMeta();
    const mode = state.reader.view_mode || "comfortable";
    document.body.classList.toggle("compact-mode", mode === "compact");
    $("toggle-view-mode")?.classList.toggle("active", mode === "compact");
  };

  renderSources = function renderParitySources() {
    const entries = makeEntries(state.dashboard || {});
    const knownSources = state.sources || [];
    const videoMode = ["video", "youtube"].includes(state.view);
    const dashboardVideoSourceIDs = new Set(
      entries
        .filter((entry) => entry.group === "video")
        .map((entry) => sourceIDForEntry(entry))
        .filter(Boolean)
    );
    const sourceIsVideo = (source) =>
      Boolean(videoProviderForSource(source)) ||
      dashboardVideoSourceIDs.has(text(source?.id));
    const subscribed = knownSources.filter(
      (source) =>
        source.subscribed &&
        !source.hidden &&
        (!videoMode || sourceIsVideo(source))
    );
    const groups = new Map();
    for (const source of subscribed) {
      const group = source.pinned ? "置顶" : source.folder_name || "订阅";
      if (!groups.has(group)) groups.set(group, []);
      groups.get(group).push(source);
    }
    const orderedGroups = [...groups.entries()].sort(([left], [right]) => {
      const leftVideo = left.trim() === "AI 视频";
      const rightVideo = right.trim() === "AI 视频";
      return leftVideo === rightVideo ? 0 : leftVideo ? 1 : -1;
    });
    let html = "";
    let groupIndex = 0;
    const renderGroup = (group, sources, sourceMarkup) => {
      const collapsed = sourceGroupCollapsed(group);
      const groupID = `reader-source-group-${groupIndex++}`;
      const groupLabel =
        {
          "置顶": t("pinnedSources"),
          "订阅": t("subscriptions"),
          "AI 视频": t("aiVideo"),
          "内置来源": t("builtInSources"),
        }[group] || group;
      return `<section class="source-folder ${collapsed ? "collapsed" : ""}" data-source-group-section="${esc(group)}">
        <button class="source-folder-toggle" type="button" data-toggle-source-group="${esc(group)}" aria-expanded="${String(!collapsed)}" aria-controls="${groupID}">
          <span class="source-folder-chevron" aria-hidden="true">${icon("chevron")}</span><span>${esc(groupLabel)}</span><span class="count">${sources.length}</span>
        </button><div class="source-folder-items" id="${groupID}">${sourceMarkup}</div>
      </section>`;
    };
    for (const [group, sources] of orderedGroups) {
      let sourceMarkup = "";
      for (const source of sources) {
        const preparing = gatewaySourcePreparing(source);
        const hydrated =
          !preparing &&
          Boolean(
            text(source.last_sync_at) ||
              Number(source.entry_count || 0) > 0 ||
              source.status === "ok" ||
              source.status === "partial"
          );
        const sourceCount = parity.counts?.by_source?.[source.id] || {};
        const sourceUnreadCount = Number.isFinite(Number(source.unread_count))
          ? Math.max(0, Number(source.unread_count))
          : 0;
        const unreadCount =
          sourceCount.unread_count ??
          sourceCount.unread_entries ??
          sourceUnreadCount;
        // Counts are independent metadata. Always render a numeric badge,
        // including 0 while a source is preparing; an empty badge causes the
        // sidebar to shift when the first sync completes.
        const count = Number.isFinite(Number(unreadCount))
          ? Math.max(0, Number(unreadCount))
          : 0;
        sourceMarkup += `<button class="source-item ${
          state.view === `source:${source.id}` ? "active" : ""
        }${preparing ? " gateway-preparing" : ""}" type="button" data-view="source:${esc(
          source.id
        )}"><i class="source-dot ${
          hydrated ? statusKind(source.status) : "idle"
        }"></i>${sourceAvatar(
          source,
          "sidebar-source-avatar"
        )}<span class="label">${esc(
          source.title_override || source.name
        )}</span><span class="count">${count}</span></button>`;
      }
      html += renderGroup(group, sources, sourceMarkup);
    }
    // Library navigation is rendered once by the fixed sidebar above. Do not
    // Library entries, recordings, meetings, actions, signals, and Obsidian
    // folders are all
    // represented by the fixed navigation or the middle-column Obsidian tree;
    // never recreate them in the sidebar source list.
    const sourceList = $("source-list");
    const sourceTotal = subscribed.length;
    const sourceMarkup = html || '<div class="source-empty">暂无来源记录</div>';
    const sourceFingerprint = `${state.view}\u0000${sourceTotal}\u0000${sourceMarkup}`;
    $("source-count").textContent = String(sourceTotal);
    if (sourceList.dataset.renderFingerprint === sourceFingerprint) return;
    sourceList.dataset.renderFingerprint = sourceFingerprint;
    sourceList.innerHTML = sourceMarkup;
  };

  updateSourceBar = function updateParitySourceBar() {
    const gateway = parity.gatewayStatus;
    if (
      gateway &&
      gateway.configured !== false &&
      (gateway.available === false || gateway.degraded === true) &&
      text(gateway.message)
    ) {
      // The cloud source path is degraded or down.  The visible content is the
      // last successful local snapshot; say so instead of implying emptiness.
      $("bar-status").innerHTML = `<i class="status-dot partial"></i><span>${esc(
        text(gateway.message)
      )}</span>`;
      return;
    }
    const subscriptions = (state.sources || []).filter(
      (source) => source.subscribed
    );
    const available = subscriptions.filter(
      (source) => source.status === "ok"
    ).length;
    if (!available) {
      baseUpdateSourceBar();
      return;
    }
    const partial =
      available < subscriptions.length ||
      state.dashboard?.source?.status !== "ok";
    $("bar-status").innerHTML = `<i class="status-dot ${
      partial ? "partial" : ""
    }"></i><span>${partial ? "部分来源可用" : "订阅来源正常"}</span>`;
  };

  function syncTimelineSelection(timeline, entries = []) {
    if (!timeline) return;
    const selectedKey = text(state.selected);
    const entriesByKey = new Map(
      entries.map((entry) => [entry.key, entry])
    );
    timeline.querySelectorAll("[data-entry-key]").forEach((node) => {
      const entry = entriesByKey.get(node.dataset.entryKey);
      if (entry) node.classList.toggle("read", Boolean(pref(entry).read));
    });
    timeline.querySelectorAll(".entry.selected").forEach((node) => {
      if (node.dataset.entryKey !== selectedKey) node.classList.remove("selected");
    });
    if (!selectedKey) return;
    for (const node of timeline.querySelectorAll("[data-entry-key]")) {
      if (node.dataset.entryKey !== selectedKey) continue;
      node.classList.add("selected");
      break;
    }
  }

  function timelineRenderFingerprint(entries, requestedScope, mode = "ready") {
    return [
      mode,
      state.view,
      state.query,
      requestedScope,
      parity.entryScope,
      text(parity.entryScopeError),
      parity.entryHasMore ? "more" : "done",
      parity.entryLoading ? "loading" : "idle",
      entries
        .map((entry) => {
          const item = entry.item || {};
          const preference = pref(entry);
          return [
            entry.key,
            text(item.content_hash),
            text(item.title),
            text(item.thumbnail_url),
            text(item.media_url),
            text(item.view_type),
            text(item.media_mime),
            text(item.duration_seconds),
            text(entry.videoProvider),
            preference.starred ? "starred" : "unstarred",
            preference.read_later ? "later" : "not-later",
          ].join("\u0000");
        })
        .join("\u0001"),
    ].join("\u0002");
  }

  function rememberObsidianDirectoryScroll(timeline = $("timeline")) {
    const browser = timeline?.querySelector(".obsidian-browser");
    if (!browser) return;
    const key = text(
      browser.closest("[data-obsidian-directory-view]")?.dataset
        .obsidianDirectoryView,
      "obsidian"
    );
    parity.obsidianDirectoryScrollTops.delete(key);
    parity.obsidianDirectoryScrollTops.set(key, browser.scrollTop);
    while (parity.obsidianDirectoryScrollTops.size > 32) {
      const oldest = parity.obsidianDirectoryScrollTops.keys().next().value;
      if (!oldest) break;
      parity.obsidianDirectoryScrollTops.delete(oldest);
    }
  }

  function restoreObsidianDirectoryScroll(timeline, view = state.view) {
    const browser = timeline?.querySelector(".obsidian-browser");
    if (!browser) return;
    const expectedView = text(view, "obsidian");
    const scrollTop =
      parity.obsidianDirectoryScrollTops.get(expectedView) || 0;
    browser.scrollTop = scrollTop;
    requestAnimationFrame(() => {
      const current = timeline?.querySelector(".obsidian-browser");
      const mountedView = text(
        current?.closest("[data-obsidian-directory-view]")?.dataset
          .obsidianDirectoryView
      );
      if (current && mountedView === expectedView) current.scrollTop = scrollTop;
    });
  }

  renderTimeline = function renderParityTimeline() {
    // Base/index navigation can assign state.view directly, bypassing
    // switchProductV3View.  Run the same stale-request guard before any Focus
    // early return so a previous reader request cannot leave a narrow/skeleton
    // projection mounted after the user changes tabs.
    cancelStaleReaderEntryLoad(state.view);
    // Runs before every early return (focus workspace / unresolved dashboard)
    // so the per-source selector never outlives its source scope.
    syncSourceDisplaySelector();
    if (renderFocusV3Workspace()) return;
    ensureFocusV3Surface();
    const requestedScope = entryScopeForView();
    // A restored `source:<id>` view can bypass the delegated click handler,
    // and the source catalog may arrive after the first paint. Reuse the same
    // rate-limited loader here so gateway sources hydrate on first render while
    // aggregate tabs remain fully lazy.
    if (requestedScope) loadSourceOnDemand(requestedScope);
    if (requestedScope !== parity.entryScope)
      void switchEntryScope(requestedScope);
    const timeline = $("timeline");
    if (!state.dashboardResolved) {
      timeline.innerHTML = state.dashboardError ? `<div class="empty-inline" role="alert">${esc(state.dashboardError)}<button class="text-btn" data-retry-reader>重试</button></div>` : timelineLoadingMarkup("initial");
      $("timeline-count").textContent = "—";
      $("timeline-meta").textContent = "正在读取内容…";
      return;
    }

    rememberObsidianDirectoryScroll(timeline);
    const topLevelVideo = ["video", "youtube"].includes(state.view);
    const xFeed = Boolean(xSourceIsX(selectedCatalogSource()));
    const scopedDisplayMode = viewModeForSourceID(sourceIDForCurrentView());
    // "gallery" is the picture wall: the aggregate 图片 view or a source whose
    // display override says gallery. Video sources keep the video grid through
    // the explicit "video" override and the auto provider heuristic.
    const pictureWall =
      state.view === "picture" || scopedDisplayMode === "gallery";
    const videoGallery =
      !pictureWall &&
      (scopedDisplayMode === "video" ||
        (scopedDisplayMode === "auto" &&
          (topLevelVideo ||
            Boolean(videoProviderForSource(selectedCatalogSource())))));
    const aggregateView = isAggregateEntryView(state.view);
    if (!topLevelVideo) parity.videoInitialRequestStarted = false;
    document.body.classList.toggle("x-feed", xFeed);
    $("reader-shell")?.classList.toggle("x-feed", xFeed);
    document.body.classList.toggle("video-gallery", videoGallery);
    $("reader-shell")?.classList.toggle("video-gallery", videoGallery);
    timeline?.classList.toggle("video-grid", videoGallery);
    timeline?.classList.toggle("picture-grid", pictureWall);
    if (aggregateView) {
      const viewKey = text(state.view);
      if (
        !parity.entryViewLoaded.has(viewKey) &&
        parity.entryViewLoading !== viewKey &&
        !parity.entryViewErrors.has(viewKey)
      )
        void loadAggregateEntriesOnDemand(state.view);
      if (parity.entryViewLoading === viewKey) {
        const loadingFingerprint = timelineRenderFingerprint([], "", "aggregate-loading");
        if (timeline?.dataset.renderFingerprint === loadingFingerprint) return;
        if (timeline) timeline.dataset.renderFingerprint = loadingFingerprint;
        $("timeline-count").textContent = "";
        timeline?.setAttribute("aria-busy", "true");
        if (timeline) timeline.innerHTML = timelineLoadingMarkup("scope");
        return;
      }
      if (
        parity.entryViewErrors.has(viewKey) &&
        !parity.entryViewLoaded.has(viewKey)
      ) {
        const errorFingerprint = timelineRenderFingerprint(
          [],
          "",
          "aggregate-error"
        );
        if (timeline?.dataset.renderFingerprint === errorFingerprint) return;
        if (timeline) {
          timeline.dataset.renderFingerprint = errorFingerprint;
          timeline.setAttribute("aria-busy", "false");
          timeline.innerHTML = aggregateEntryErrorMarkup(
            viewKey,
            parity.entryViewErrors.get(viewKey)
          );
        }
        return;
      }
    }
    if (
      topLevelVideo &&
      !parity.entryViewLoaded.has(text(state.view)) &&
      !parity.entryViewErrors.has(text(state.view)) &&
      !parity.videoInitialRequestStarted &&
      !parity.entryLoading &&
      !parity.entryScopeLoading
    ) {
      parity.entryCursor = "0";
      parity.entryPagesRead = 0;
      parity.entryHasMore = true;
      parity.videoInitialRequestStarted = true;
      void loadMoreEntries();
    }
    if (requestedScope && parity.entryScopeLoading) {
      const loadingFingerprint = timelineRenderFingerprint(
        [],
        requestedScope,
        "scope-loading"
      );
      if (timeline?.dataset.renderFingerprint === loadingFingerprint) return;
      if (timeline) timeline.dataset.renderFingerprint = loadingFingerprint;
      $("timeline-count").textContent = "";
      timeline.setAttribute("aria-busy", "true");
      timeline.innerHTML = timelineLoadingMarkup("scope");
      return;
    }
    if (liveDeviceDataView() && liveDeviceData.inflight && !liveDeviceData.loadedAt) {
      timeline.innerHTML = timelineLoadingMarkup("device");
      return;
    }
    if (liveDeviceDataView() && parity.deviceListError && !liveDeviceData.loadedAt) {
      timeline.innerHTML = `<div class="empty-inline" role="alert">${esc(parity.deviceListError)}<button class="text-btn" data-retry-device-list>重新读取</button></div>`;
      return;
    }
    const entries = filteredEntries();
    // Pagination updates the timeline after the navigation header rendered.
    // Reconcile the visible count with the same bounded entry projection.
    syncLoadedEntryMeta(entries);
    const obsidianDirectory =
      state.view === "obsidian" || state.view.startsWith("obsidian-folder:");
    timeline?.classList.toggle("obsidian-directory", obsidianDirectory);
    if (obsidianDirectory) {
      const treeMarkup = renderObsidianTree(entries);
      const fingerprint = `${timelineRenderFingerprint(
        entries,
        requestedScope,
        "obsidian-directory"
      )}\u0000${treeMarkup}`;
      if (timeline?.dataset.renderFingerprint === fingerprint) return;
      if (timeline) {
        timeline.dataset.renderFingerprint = fingerprint;
        timeline.setAttribute("aria-busy", "false");
        timeline.innerHTML = `<div class="obsidian-directory-panel" data-obsidian-directory-view="${esc(
          state.view
        )}">${treeMarkup}</div>`;
        restoreObsidianDirectoryScroll(timeline, state.view);
      }
      return;
    }
    if (xFeed) parity.autoSelectEntry = false;
    const autoSelectViews = [
      "all",
      "rss",
      "youtube",
      "starred",
      "later",
      "context",
    ];
    // Folo opens the first item when entering a feed. Do this only after a
    // source-scoped request has settled; selecting from the previous scope
    // while it is loading would show the wrong detail and then jump again.
    if (
      parity.autoSelectEntry &&
      !videoGallery &&
      !xFeed &&
      !state.selected &&
      (autoSelectViews.includes(state.view) || state.view.startsWith("source:")) &&
      requestedScope === parity.entryScope &&
      !parity.entryScopeLoading &&
      entries.length
    ) {
      const first =
        entries.find((entry) => entry.kind === "news") || entries[0];
      if (first?.key) {
        state.selected = first.key;
        cacheDetailEntry(first);
        document.body.classList.add("detail-open");
        parity.autoSelectEntry = false;
      }
    }
    const savedNewsComplete = savedNewsProjectionComplete();
    const fingerprint = `${timelineRenderFingerprint(
      entries,
      requestedScope
    )}\u0000${parity.entryPageError ? "entry-page-error" : ""}\u0000${savedNewsComplete}`;
    if (timeline?.dataset.renderFingerprint === fingerprint) {
      syncTimelineSelection(timeline, entries);
      return;
    }
    if (timeline) timeline.dataset.renderFingerprint = fingerprint;
    baseRenderTimeline();
    // 来源范围视图不再渲染订阅页头：视频网格的 4 列 grid 会把它压进单格
    // （名称逐字换行、大片空白），来源级操作全部保留在订阅管理器行内。
    syncTimelineSelection(timeline, entries);
    observeXFeedFullText(xFeed ? entries : []);
    timeline?.setAttribute("aria-busy", "false");
    if (["meeting", "recording", "source:my-recordings", "source:device-meetings"].includes(state.view) && liveDeviceData.meetingHasMore) {
      timeline?.insertAdjacentHTML("beforeend", `<div class="entry-page-more"><p>${esc(parity.deviceListError || "")}</p><button class="text-btn" data-load-more-meetings ${liveDeviceData.meetingLoading ? "disabled" : ""}>${liveDeviceData.meetingLoading ? "正在读取会议…" : "加载更多会议"}</button></div>`);
      return;
    }
    const source = selectedCatalogSource();
    const sourceHistoryMore = sourceCanLoadHistory(source);
    const visibleNews =
      ["all", "rss", "video", "youtube", "picture", "starred", "later", "context"].includes(
        state.view
      ) || state.view.startsWith("source:");
    if (!visibleNews || savedNewsComplete) return;
    if (parity.entryPageError) {
      timeline?.insertAdjacentHTML(
        "beforeend",
        entryPageErrorMarkup(parity.entryPageError)
      );
      return;
    }
    if (parity.entryScopeError && source && requestedScope === parity.entryScope) {
      if (timeline) {
        timeline.innerHTML = entries.length
          ? `${timeline.innerHTML}${sourceEntryErrorMarkup(source, parity.entryScopeError)}`
          : sourceEntryErrorMarkup(source, parity.entryScopeError);
      }
      return;
    }
    if (timeline && !entries.length && source && !parity.entryHasMore && !sourceHistoryMore) {
      timeline.innerHTML = sourceEmptyMarkup(source);
      return;
    }
    if (!timeline || (!parity.entryHasMore && !sourceHistoryMore)) return;
    timeline.insertAdjacentHTML(
      "beforeend",
      parity.entryLoading || parity.sourceHistoryLoading.has(source?.id)
        ? timelineLoadingMarkup("more")
        : parity.entryHasMore
        ? `<div class="entry-page-more" data-entry-page-sentinel><button class="text-btn" type="button" data-load-more-entries>加载更多内容</button></div>`
        : `<div class="entry-page-more" data-source-history-sentinel><button class="text-btn" type="button" data-load-more-source="${esc(source.id)}">${source.id.startsWith("bilibili-") ? "读取更早视频" : "读取更早文章"}</button></div>`
    );
    observeEntryPagination();
  };
  // Every timeline repaint (including early-return paths that leave the
  // previous cards in place) funnels through here afterwards, so the
  // scroll-read observer always mirrors the current card set exactly once.
  const baseRenderParityTimeline = renderTimeline;
  renderTimeline = function renderParityTimelineScrollRead() {
    baseRenderParityTimeline();
    observeTimelineForScrollRead();
  };

  function renderMeetingEntry(entry) {
    const item = entry.item || {};
    const detail = state.recordingDetails?.get(text(entry.id)) || item;
    const report = meetingReportObject(detail.report);
    const preference = pref(entry);
    const status = meetingStatusKind(item.status);
    const statusMarkup = status === "processing"
      ? `${icon("refresh")}<span>${esc(text(item.meeting_stage, "处理中"))}</span>`
      : status === "failed"
      ? `${icon("close")}<span>处理失败</span>`
      : icon("arrow");
    const speakerCount = Math.max(0, Number(item.speaker_count) || 0);
    const meta = [
      formatDate(entryDate(entry)),
      secondsLabel(item.duration_seconds),
      speakerCount ? `${speakerCount} 人` : "",
    ].filter(Boolean).join(" · ");
    const summary = text(
      entry.summary || detail.summary || item.summary || report.overview || report.headline,
      recordingStatusLabel(item.status)
    );
    return `<article class="meeting-v3-row is-${status} ${
      state.selected === entry.key ? "selected" : ""
    } ${preference.read ? "read" : ""}" data-entry-key="${esc(entry.key)}"><button class="meeting-v3-row-main" type="button" data-select-entry="${esc(entry.key)}"><span class="meeting-v3-glyph">${icon("audio")}</span><span class="meeting-v3-row-copy"><strong>${esc(entry.title)}</strong><small>${esc(meta)}</small><span>${esc(summary)}</span></span><span class="meeting-v3-row-status">${statusMarkup}</span></button></article>`;
  }

  renderEntry = function renderParityEntry(entry) {
    const preference = pref(entry);
    const xEntry = entry.kind === "news" && xAccountForItem(entry.item);
    const renderable = entry.item
      ? {
          ...entry,
          item: {
            ...entry.item,
            thumbnail_url:
              trustedXImageURL(entry.item.thumbnail_url, entry.item) ||
              localAppMediaURL(
                entry.item.thumbnail_url,
                entry.item.id,
                "thumbnail"
              ),
            media_url: xEntry
              ? entry.item.media_url
              : localAppMediaURL(
                  entry.item.media_url,
                  entry.item.id,
                  "media"
                ),
          },
        }
      : entry;
    const videoEntry = entry.kind === "news" && videoForItem(renderable.item);
    // The picture wall owns the aggregate picture view and the per-source
    // "gallery" display mode; everywhere else keeps the standard cards.
    const pictureWallActive =
      entry.kind === "news" &&
      (state.view === "picture" ||
        viewModeForSourceID(sourceIDForCurrentView()) === "gallery") &&
      pictureImageForItem(renderable.item);
    let html = entry.kind === "recording"
      ? renderMeetingEntry(renderable)
      : xEntry
      ? renderXEntry(renderable)
      : videoEntry
      ? renderVideoEntry(renderable)
      : pictureWallActive
      ? renderPictureEntry(renderable)
      : baseRenderEntry(renderable);
    if (contextIsActive(entry)) {
      html = html.replace(
        '<span class="entry-meta">',
        '<span class="entry-meta">' + badge("上下文", "green")
      );
    }
    if (entry.kind !== "recording") {
      const later = `<button class="icon-btn ${
        preference.read_later ? "active" : ""
      }" type="button" data-later-entry="${esc(entry.key)}" aria-label="${
        preference.read_later ? "移出稍后读" : "稍后读"
      }" title="${preference.read_later ? "移出稍后读" : "稍后读"}">${icon(
        "inbox"
      )}</button>`;
      const progress =
        preference.reading_progress > 0
          ? `<span class="entry-progress"><i style="width:${Math.round(
              preference.reading_progress * 100
            )}%"></i></span>`
          : "";
      html = html.replace(
        "</span></article>",
        `${later}</span>${progress}</article>`
      );
    }
    return html;
  };

  function installContextControl(entry) {
    const tools = $("detail-panel").querySelector(".detail-tools");
    if (!tools || tools.querySelector("[data-context-entry]")) return;
    const active = contextIsActive(entry);
    const label = active ? t("removeFromKnowledge") : t("addToKnowledge");
    tools.insertAdjacentHTML(
      "afterbegin",
      `<button class="text-btn context-tool ${
        active ? "active" : ""
      }" type="button" data-context-entry="${esc(
        entry.key
      )}" aria-label="${label}" title="${label}">${icon(
        active ? "check" : "plus"
      )}<span>${label}</span></button>`
    );
  }

  function installSignalCounterevidence(entry) {
    if (!entry || entry.kind !== "signal") return;
    const items = Array.isArray(entry.item?.counterevidence)
      ? entry.item.counterevidence
          .map((item) => text(item))
          .filter(Boolean)
          .slice(0, 10)
      : [];
    if (!items.length) return;
    const body = $("detail-panel").querySelector(".detail-body");
    if (!body || body.querySelector("[data-signal-counterevidence]")) return;
    body.insertAdjacentHTML(
      "beforeend",
      `<section data-signal-counterevidence>
      <h3>反向证据与缺口</h3>
      <ul>${items.map((item) => `<li>${esc(item)}</li>`).join("")}</ul>
    </section>`
    );
  }

  function safeWorkflowID(value, fallback) {
    const normalized = text(value, fallback)
      .replace(/[^A-Za-z0-9._:-]/g, "-")
      .slice(0, 128);
    return normalized || fallback;
  }

  function scheduleCandidateForAction(action, pending = {}) {
    const display = { task: text(action.task, "待确认行动") };
    if (text(action.owner)) display.owner = text(action.owner).slice(0, 120);
    if (text(action.due_at)) display.due_at = text(action.due_at).slice(0, 80);
    if (["low", "medium", "high"].includes(text(action.priority)))
      display.priority = text(action.priority);
    return {
      type: bridgeCandidateType,
      request_id: safeWorkflowID(
        pending.request_id || action.schedule_request_id,
        `request-${safeWorkflowID(action.id, "action")}`
      ),
      candidate_id: action.id,
      revision: Number(pending.revision || action.revision) || 1,
      display,
    };
  }

  function actionFieldsFromPanel(panel, action) {
    const value = (name, fallback) => {
      const node = panel.querySelector(`[data-action-field="${name}"]`);
      return text(node?.value, fallback).slice(
        0,
        name === "task" ? 800 : name === "owner" ? 120 : 80
      );
    };
    return {
      task: value("task", action.task),
      owner: value("owner", action.owner),
      due_at: value("due_at", action.due_at),
      priority: value("priority", action.priority || "medium"),
    };
  }

  async function sendActionFeedback(action, panel, confirmationStatus) {
    if (!action?.id || parity.feedbackBusy.has(action.id)) return;
    const scheduleState = text(action.schedule_state, "none") || "none";
    if (scheduleState === "pending" || scheduleState === "scheduled") {
      if (confirmationStatus !== "accepted") {
        showError(readerFocusCopy("已进入定时流程的行动不能撤回确认", "A scheduled action cannot be unconfirmed"));
        return;
      }
    }
    const fields = actionFieldsFromPanel(panel, action);
    if (!fields.task) {
      showError(readerFocusCopy("行动内容不能为空", "Action content cannot be empty"));
      return;
    }
    const payload = {
      action_id: action.id,
      expected_revision: Number(action.revision) || 1,
      confirmation_status: confirmationStatus,
      task: fields.task,
      owner: fields.owner,
      due_at: fields.due_at,
      priority: fields.priority,
    };
    const key = `action-feedback-${action.id}-${
      payload.expected_revision
    }-${confirmationStatus}-${uuidV4()}`;
    parity.feedbackBusy.add(action.id);
    panel.querySelectorAll("[data-action-feedback]").forEach((node) => {
      node.disabled = true;
    });
    try {
      await actionJSONRequest("./api/actions/feedback", payload, key);
      await refresh();
      showToast(
        confirmationStatus === "accepted"
          ? readerFocusCopy("行动已确认并加入稳定上下文", "Action confirmed and added to stable context")
          : confirmationStatus === "rejected"
          ? readerFocusCopy("已拒绝这条行动草案", "Action draft rejected")
          : readerFocusCopy("行动草案已保存", "Action draft saved")
      );
    } catch (error) {
      await refresh();
      showError(error.message || readerFocusCopy("行动状态没有保存", "Action status was not saved"));
    } finally {
      parity.feedbackBusy.delete(action.id);
    }
  }

  async function beginActionSchedule(action) {
    if (
      !action?.id ||
      !scheduleCapabilityAvailable() ||
      parity.scheduleBusy.has(action.id)
    )
      return;
    if (text(action.confirmation_status, "draft") !== "accepted") {
      showError(readerFocusCopy("请先确认行动，再创建定时任务", "Confirm the action before scheduling it"));
      return;
    }
    const existing = parity.pendingSchedules.get(action.id);
    const pending = {
      revision: Number(existing?.revision || action.revision) || 1,
      request_id: safeWorkflowID(
        existing?.request_id || action.schedule_request_id,
        `request-${uuidV4()}`
      ),
      candidate_id: action.id,
      server_pending: existing?.server_pending === true,
    };
    rememberPendingSchedule(action, pending);
    const candidate = scheduleCandidateForAction(action, pending);
    parity.scheduleBusy.add(action.id);
    const button = document.querySelector(
      `[data-action-schedule="${CSS.escape(action.id)}"]`
    );
    if (button) {
      button.disabled = true;
      button.textContent = readerFocusCopy("正在等待宿主确认…", "Waiting for host confirmation…");
    }
    try {
      if (!bridgePost(candidate)) throw new Error(readerFocusCopy("可信宿主暂时不可用", "Trusted host is temporarily unavailable"));
      showToast(readerFocusCopy("已发送给宿主，等待确认", "Sent to host; waiting for confirmation"));
    } catch (error) {
      // Keep the same display-only candidate for replay after the trusted host
      // reconnects. The host owns client_request_id and every state mutation.
      showError(error.message || readerFocusCopy("宿主暂时不可用，已保留待确认候选", "Host is temporarily unavailable; the pending candidate was kept"));
    } finally {
      parity.scheduleBusy.delete(action.id);
      renderDetail();
    }
  }

  function installActionWorkflowControl(entry) {
    if (!entry || entry.kind !== "action") return;
    const panel = $("detail-panel");
    if (!panel || panel.querySelector("[data-action-workflow]")) return;
    const action = entry.item || {};
    const confirmation = ["draft", "accepted", "rejected"].includes(
      text(action.confirmation_status)
    )
      ? text(action.confirmation_status)
      : "draft";
    const scheduleState = ["none", "pending", "scheduled", "failed"].includes(
      text(action.schedule_state)
    )
      ? text(action.schedule_state)
      : "none";
    const candidatePending =
      parity.pendingSchedules.get(action.id)?.server_pending === false;
    const canSchedule =
      scheduleCapabilityAvailable() && confirmation === "accepted";
    const scheduleButton = canSchedule
      ? `<button class="primary-btn" type="button" data-action-schedule="${esc(
          action.id
        )}" ${
          scheduleState === "pending" || scheduleState === "scheduled"
            ? "disabled"
            : ""
        }>${
          scheduleState === "pending"
            ? "等待定时任务回执"
            : scheduleState === "scheduled"
            ? "已创建定时任务"
            : candidatePending
            ? "再次打开确认"
            : scheduleState === "failed"
            ? "重试定时任务"
            : "创建定时任务"
        }</button>`
      : "";
    const statusText =
      scheduleState === "scheduled"
        ? `已创建 ${text(action.job_id)}`
        : scheduleState === "failed"
        ? `创建失败：${text(action.schedule_error_code, "未知错误")}`
        : scheduleState === "pending"
        ? "正在等待宿主回执"
        : candidatePending
        ? "候选已发送，可再次打开确认"
        : "尚未创建定时任务";
    const detailBody = panel.querySelector(".detail-body");
    if (!detailBody) return;
    detailBody.insertAdjacentHTML(
      "afterend",
      `<section class="action-workflow-panel" data-action-workflow>
      <div class="module-title-row"><h3>行动确认</h3>${badge(
        confirmation === "accepted"
          ? "已确认"
          : confirmation === "rejected"
          ? "已拒绝"
          : "草案",
        confirmation === "accepted"
          ? "green"
          : confirmation === "rejected"
          ? "red"
          : "amber"
      )}</div>
      <label class="workflow-field"><span>行动内容</span><textarea data-action-field="task" maxlength="800" rows="3">${esc(
        action.task || ""
      )}</textarea></label>
      <label class="workflow-field"><span>负责人</span><input data-action-field="owner" maxlength="120" value="${esc(
        action.owner || ""
      )}"></label>
      <label class="workflow-field"><span>完成期限</span><input data-action-field="due_at" maxlength="80" value="${esc(
        action.due_at || ""
      )}"></label>
      <label class="workflow-field"><span>优先级</span><select data-action-field="priority"><option value="low" ${
        action.priority === "low" ? "selected" : ""
      }>低</option><option value="medium" ${
        !action.priority || action.priority === "medium" ? "selected" : ""
      }>中</option><option value="high" ${
        action.priority === "high" ? "selected" : ""
      }>高</option></select></label>
      <div class="workflow-actions"><button class="text-btn" type="button" data-action-feedback="draft">保存草案修改</button><button class="primary-btn" type="button" data-action-feedback="accepted">确认行动</button><button class="text-btn" type="button" data-action-feedback="rejected">拒绝</button>${scheduleButton}</div>
      <p class="workflow-schedule-status">${esc(statusText)}</p>
    </section>`
    );
    panel.querySelectorAll("[data-action-feedback]").forEach((node) => {
      node.addEventListener(
        "click",
        () =>
          void sendActionFeedback(action, panel, node.dataset.actionFeedback)
      );
    });
    panel
      .querySelector(`[data-action-schedule="${CSS.escape(action.id)}"]`)
      ?.addEventListener("click", () => void beginActionSchedule(action));
  }

  function normalizeLeadImageIndex(value) {
    // Older extensions passed a boolean; preserve that call shape while making
    // the persisted article index authoritative for new content.
    if (value === true) return 0;
    if (value === false || value === null || value === undefined || value === "")
      return -1;
    const index = Number(value);
    return Number.isInteger(index) && index >= 0 && index < 24 ? index : -1;
  }

  function sanitizeRichEntryHTML(raw, entryID, leadImageIndex = -1) {
    const omitIndex = normalizeLeadImageIndex(leadImageIndex);
    const doc = new DOMParser().parseFromString(String(raw || ""), "text/html");
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
      "H4",
      "FIGURE",
      "FIGCAPTION",
      "IMG",
      "HR",
      "TABLE",
      "THEAD",
      "TBODY",
      "TR",
      "TH",
      "TD",
    ]);
    doc.body.querySelectorAll("*").forEach((node) => {
      if (!allowed.has(node.tagName)) {
        node.replaceWith(...node.childNodes);
        return;
      }
      const imageIndex =
        node.tagName === "IMG"
          ? Number(node.getAttribute("data-entry-image"))
          : -1;
      const href = node.tagName === "A" ? text(node.getAttribute("href")) : "";
      const alt = node.tagName === "IMG" ? text(node.getAttribute("alt")) : "";
      const imageSrc = node.tagName === "IMG" ? text(node.getAttribute("src")) : "";
      const imageURL = node.tagName === "IMG" ? text(node.getAttribute("data-entry-image-url")) : "";
      [...node.attributes].forEach((attribute) =>
        node.removeAttribute(attribute.name)
      );
      if (node.tagName === "A" && publicURL(href)) {
        node.setAttribute("href", href);
        node.setAttribute("target", "_blank");
        node.setAttribute("rel", "noopener noreferrer");
      }
      if (node.tagName === "IMG") {
        if (
          imageSrc.startsWith(
            "./_zettlab/device-data/v1/obsidian/asset?path="
          )
        ) {
          node.setAttribute("src", imageSrc);
          node.setAttribute("alt", alt);
          node.setAttribute("loading", "lazy");
          node.setAttribute("decoding", "async");
          return;
        }
        if (omitIndex >= 0 && imageIndex === omitIndex) {
          const figure =
            node.parentElement?.tagName === "FIGURE"
              ? node.parentElement
              : null;
          node.remove();
          if (
            figure &&
            !figure.querySelector("img") &&
            !text(figure.textContent)
          )
            figure.remove();
          return;
        }
        if (
          !Number.isInteger(imageIndex) ||
          imageIndex < 0 ||
          imageIndex >= 24
        ) {
          node.remove();
          return;
        }
        // WeChat body images are commonly served through a bridge or a
        // mmbiz.qpic.cn URL. Route them through the bounded AC image proxy so
        // CSP, hotlink checks, and expiring bridge URLs do not turn rich text
        // into a page of broken-image placeholders.
        // Every article-body image goes through the indexed device proxy.
        // Direct public URLs are intentionally absent from the page CSP and
        // commonly require a source-specific Referer, so emitting them here
        // turns valid article content into broken placeholders.
        node.setAttribute(
          "src",
          `./api/entries/content/image?id=${encodeURIComponent(
            entryID
          )}&index=${imageIndex}`
        );
        node.setAttribute("alt", alt);
        node.setAttribute("loading", "lazy");
        node.setAttribute("decoding", "async");
        node.dataset.articleImage = "1";
      }
    });
    // WeChat HTML commonly contains empty nested wrappers and long runs of
    // layout-only breaks. Remove those after sanitization so a malformed feed
    // cannot turn a short article into a page of blank vertical gaps.
    Array.from(doc.body.querySelectorAll("div,section,article,main,p,figure"))
      .reverse()
      .forEach((node) => {
        if (!node.querySelector("img,br") && !text(node.textContent)) node.remove();
      });
    doc.body.querySelectorAll("br").forEach((node) => {
      const previous = node.previousElementSibling;
      const beforePrevious = previous?.previousElementSibling;
      if (previous?.tagName === "BR" && beforePrevious?.tagName === "BR") node.remove();
    });
    doc.body.querySelectorAll("br").forEach((node) => {
      if (node.nextSibling?.nodeType === Node.TEXT_NODE && !text(node.nextSibling.textContent))
        node.nextSibling.remove();
    });
    return doc.body.innerHTML;
  }

  // Compatibility name kept for the base-template contract and older local
  // extensions. New code should use the renderer factory above.
  function safeEntryHTML(raw, entryID, omitLeadImage = false) {
    return sanitizeRichEntryHTML(raw, entryID, omitLeadImage);
  }

  // Keep source selection and DOM sanitization in one small factory. The
  // backend already stores bounded HTML; this layer only chooses the display
  // adapter and binds app-owned image URLs.
  function renderPlainTextContent(raw) {
    const paragraphs = text(raw)
      .replace(/\r\n?/g, "\n")
      .split(/\n\s*\n+/)
      .map((value) => value.trim())
      .filter(Boolean);
    const body = paragraphs.length
      ? paragraphs
          .map((value) => `<p class="fulltext-content">${esc(value)}</p>`)
          .join("")
      : '<p class="fulltext-content"></p>';
    return `<div class="fulltext-rich fulltext-plain">${body}</div>`;
  }

  const articleRenderers = Object.freeze({
    text: {
      render: (content) => renderPlainTextContent(content.content_text || ""),
    },
    markdown: {
      render: (content, entry, omitLeadImage) =>
        `<div class="fulltext-rich fulltext-markdown">${renderMarkdownContent(
          content.content_text,
          entry.item.id,
          omitLeadImage
        )}</div>`,
    },
    html: {
      render: (content, entry, omitLeadImage) =>
        `<div class="fulltext-rich">${sanitizeRichEntryHTML(
          articleHTML(content),
          entry.item.id,
          omitLeadImage
        )}</div>`,
    },
    wechat: {
      render: (content, entry, omitLeadImage) =>
        `<div class="fulltext-rich fulltext-wechat">${sanitizeRichEntryHTML(
          articleHTML(content),
          entry.item.id,
          omitLeadImage
        )}</div>`,
    },
  });

  // Full-text responses from older device packages called the sanitized
  // projection `safe_html`, while the current content endpoint returns
  // `content_html`. Keep the renderer tolerant so a valid WeChat article does
  // not fall back to a narrow/plain-text column just because the response was
  // produced by an older gateway or browser cache.
  function articleHTML(content) {
    const format = text(content?.content_format).toLowerCase();
    return [
      content?.content_html,
      content?.safe_html,
      format === "html" ? content?.content : "",
    ]
      .map((value) => text(value).trim())
      .find(Boolean) || "";
  }

  function articleSourceHost(raw) {
    try {
      return new URL(text(raw), location.href).hostname.toLowerCase();
    } catch {
      return "";
    }
  }

  function obsidianImageURL(notePath, imagePath) {
    const note = text(notePath).replaceAll("\\", "/");
    const image = text(imagePath).trim().replaceAll("\\", "/");
    if (!note || !image || image.startsWith("/") || image.startsWith("//") || image.includes("\u0000")) return "";
    const noteDir = note.includes("/") ? note.slice(0, note.lastIndexOf("/")) : "";
    const parts = [...(noteDir ? noteDir.split("/") : []), ...image.split("/")];
    const normalized = [];
    for (const part of parts) {
      if (!part || part === ".") continue;
      if (part === "..") {
        if (!normalized.length) return "";
        normalized.pop();
      } else normalized.push(part);
    }
    const path = normalized.join("/");
    if (!path || path.length > 1024 || !/\.(?:png|jpe?g|gif|webp)$/i.test(path)) return "";
    return deviceDataPath("obsidian/asset", { path });
  }

  function renderMarkdownInline(raw, imageNotePath = "") {
    const value = text(raw);
    const tokenRE = /(!?\[[^\]\n]*\]\([^\)\n]+\)|`[^`\n]+`|\*\*[^*\n]+\*\*|__[^_\n]+__|\*[^*\n]+\*|_[^_\n]+_)/g;
    let html = "";
    let cursor = 0;
    for (const match of value.matchAll(tokenRE)) {
      const token = match[0];
      const index = match.index ?? cursor;
      html += esc(value.slice(cursor, index));
      if (token.startsWith("![")) {
        const image = token.match(/^!\[([^\]\n]*)\]\(([^)\n]+)\)$/);
        const imageURL = image ? obsidianImageURL(imageNotePath, image[2].trim().replace(/^<|>$/g, "")) : "";
        html += imageURL
          ? `<img class="obsidian-note-image" src="${esc(imageURL)}" alt="${esc(image[1])}" loading="lazy" decoding="async">`
          : esc(token);
      } else if (token.startsWith("[") && token.endsWith(")")) {
        const link = token.match(/^\[([^\]\n]+)\]\(([^\)\n]+)\)$/);
        if (link && publicURL(link[2])) {
          html += `<a href="${esc(
            link[2]
          )}" target="_blank" rel="noopener noreferrer">${esc(
            link[1]
          )}</a>`;
        } else html += esc(token);
      } else if (token.startsWith("`") && token.endsWith("`")) {
        html += `<code>${esc(token.slice(1, -1))}</code>`;
      } else if (token.startsWith("**") || token.startsWith("__")) {
        html += `<strong>${esc(token.slice(2, -2))}</strong>`;
      } else {
        html += `<em>${esc(token.slice(1, -1))}</em>`;
      }
      cursor = index + token.length;
    }
    return html + esc(value.slice(cursor));
  }

  function renderMarkdownContent(raw, entryID, omitLeadImage = false, imageNotePath = "") {
    const lines = text(raw).replace(/\r\n?/g, "\n").slice(0, 32000).split("\n");
    let html = "";
    let paragraph = [];
    let listType = "";
    let inFence = false;
    let fenceLines = [];
    const flushParagraph = () => {
      if (!paragraph.length) return;
      html += `<p>${renderMarkdownInline(paragraph.join("\n"), imageNotePath)}</p>`;
      paragraph = [];
    };
    const closeList = () => {
      if (!listType) return;
      html += `</${listType}>`;
      listType = "";
    };
    const closeFence = () => {
      if (!inFence) return;
      html += `<pre><code>${esc(fenceLines.join("\n"))}</code></pre>`;
      fenceLines = [];
      inFence = false;
    };
    for (const line of lines) {
      if (/^\s*```/.test(line)) {
        flushParagraph();
        closeList();
        if (inFence) closeFence();
        else inFence = true;
        continue;
      }
      if (inFence) {
        fenceLines.push(line);
        continue;
      }
      if (!line.trim()) {
        flushParagraph();
        closeList();
        continue;
      }
      const heading = line.match(/^\s*(#{1,4})\s+(.+?)\s*#*\s*$/);
      if (heading) {
        flushParagraph();
        closeList();
        const level = heading[1].length;
        html += `<h${level}>${renderMarkdownInline(heading[2], imageNotePath)}</h${level}>`;
        continue;
      }
      if (/^\s*(?:(?:-\s*){3,}|(?:_\s*){3,}|(?:\*\s*){3,})$/.test(line)) {
        flushParagraph();
        closeList();
        html += "<hr>";
        continue;
      }
      const quote = line.match(/^\s*>\s?(.*)$/);
      if (quote) {
        flushParagraph();
        closeList();
        html += `<blockquote>${renderMarkdownInline(quote[1], imageNotePath)}</blockquote>`;
        continue;
      }
      const unordered = line.match(/^\s*[-+*]\s+(.+)$/);
      const ordered = line.match(/^\s*\d+[.)]\s+(.+)$/);
      if (unordered || ordered) {
        flushParagraph();
        const nextType = unordered ? "ul" : "ol";
        if (listType !== nextType) {
          closeList();
          listType = nextType;
          html += `<${listType}>`;
        }
        html += `<li>${renderMarkdownInline((unordered || ordered)[1], imageNotePath)}</li>`;
        continue;
      }
      closeList();
      paragraph.push(line.trim());
    }
    flushParagraph();
    closeList();
    closeFence();
    return sanitizeRichEntryHTML(html, entryID, omitLeadImage);
  }

  function renderObsidianMarkdown(raw, notePath) {
    const lines = text(raw).replace(/\r\n?/g, "\n").slice(0, 32000).split("\n");
    const markdown = lines.map((line) => line.replace(/!\[\[([^\]|]+)(?:\|[^\]]+)?\]\]/g, "![]($1)")).join("\n");
    const rendered = renderMarkdownContent(markdown, "obsidian", false, notePath);
    const doc = new DOMParser().parseFromString(rendered, "text/html");
    doc.body.querySelectorAll("img").forEach((node) => {
      const src = text(node.getAttribute("src"));
      if (
        !src.startsWith("./_zettlab/device-data/v1/obsidian/asset?path=")
      )
        node.remove();
    });
    return doc.body.innerHTML;
  }
  window.__renderObsidianMarkdown = renderObsidianMarkdown;

  function obsidianDisplayName(name) {
    return text(name).replace(/\.(md|markdown|txt|pdf|png|jpe?g|gif|webp)$/i, "");
  }

  function bindObsidianAssetClose(panel, options = {}) {
    const close = panel?.querySelector("[data-close-obsidian-asset]");
    if (!close) return;
    close.removeAttribute("data-close-obsidian-asset");
    close.setAttribute(
      options.citation ? "data-close-focus-citation" : "data-close-detail",
      ""
    );
    if (!options.citation && typeof bindDetailEvents === "function")
      bindDetailEvents();
  }

  function renderObsidianPDF(entry, panel, path, options = {}) {
    const item = entry.item || {};
    const name = text(entry.title, path.split("/").pop() || readerFocusCopy("未命名 PDF", "Untitled PDF"));
    const assetURL = text(
      item.asset_url,
      deviceDataPath("obsidian/asset", { path })
    );
    panel.classList.add("obsidian-detail-panel", "obsidian-asset-panel");
    if (options.citation)
      panel.classList.add("focus-source-detail-drawer", "detail-panel");
    const pdfPreview = readerFocusCopy("PDF 预览", "PDF preview");
    const previousPage = readerFocusCopy("上一页", "Previous page");
    const nextPage = readerFocusCopy("下一页", "Next page");
    const zoomOut = readerFocusCopy("缩小 PDF", "Zoom out PDF");
    const zoomIn = readerFocusCopy("放大 PDF", "Zoom in PDF");
    const closePDF = readerFocusCopy("关闭 PDF", "Close PDF");
    const unavailable = readerFocusCopy("PDF 暂时无法显示", "PDF is temporarily unavailable");
    const retryHint = readerFocusCopy("原文件仍然保留，可以稍后重新打开。", "The original file is still available; try opening it again later.");
    panel.innerHTML = `<div class="obsidian-pdf-detail" data-obsidian-note="${esc(
      path
    )}"><section class="obsidian-pdf-viewer" aria-label="${esc(
      `${name} ${pdfPreview}`
    )}"><div class="obsidian-pdf-toolbar"><div><button type="button" data-obsidian-pdf-prev aria-label="${previousPage}" title="${previousPage}" disabled>${icon(
      "chevron"
    )}</button><strong data-obsidian-pdf-page>1 / --</strong><button type="button" data-obsidian-pdf-next aria-label="${nextPage}" title="${nextPage}" class="next">${icon(
      "chevron"
    )}</button><i></i><button type="button" data-obsidian-pdf-minus aria-label="${zoomOut}" title="${readerFocusCopy("缩小", "Zoom out")}">−</button><strong data-obsidian-pdf-scale>100%</strong><button type="button" data-obsidian-pdf-plus aria-label="${zoomIn}" title="${readerFocusCopy("放大", "Zoom in")}">＋</button></div><button class="obsidian-pdf-close" type="button" data-close-obsidian-asset aria-label="${closePDF}" title="${closePDF}">${icon(
      "close"
    )}</button></div><div class="obsidian-pdf-frame"><iframe src="${esc(
      `${assetURL}#page=1&zoom=100&toolbar=0&navpanes=0`
    )}" title="${esc(name)}" loading="eager"></iframe><div class="obsidian-pdf-state" hidden><strong>${unavailable}</strong><p>${retryHint}</p></div></div></section></div>`;
    const frame = panel.querySelector(".obsidian-pdf-frame iframe");
    let page = 1;
    let scale = 1;
    const update = () => {
      const pageLabel = panel.querySelector("[data-obsidian-pdf-page]");
      const scaleLabel = panel.querySelector("[data-obsidian-pdf-scale]");
      const previous = panel.querySelector("[data-obsidian-pdf-prev]");
      const minus = panel.querySelector("[data-obsidian-pdf-minus]");
      const plus = panel.querySelector("[data-obsidian-pdf-plus]");
      if (pageLabel) pageLabel.textContent = `${page} / --`;
      if (scaleLabel) scaleLabel.textContent = `${Math.round(scale * 100)}%`;
      if (previous) previous.disabled = page <= 1;
      if (minus) minus.disabled = scale <= 0.75;
      if (plus) plus.disabled = scale >= 1.45;
      if (frame)
        frame.src = `${assetURL}#page=${page}&zoom=${Math.round(
          scale * 100
        )}&toolbar=0&navpanes=0`;
    };
    panel
      .querySelector("[data-obsidian-pdf-prev]")
      ?.addEventListener("click", () => {
        page = Math.max(1, page - 1);
        update();
      });
    panel
      .querySelector("[data-obsidian-pdf-next]")
      ?.addEventListener("click", () => {
        page += 1;
        update();
      });
    panel
      .querySelector("[data-obsidian-pdf-minus]")
      ?.addEventListener("click", () => {
        scale = Math.max(0.75, scale - 0.15);
        update();
      });
    panel
      .querySelector("[data-obsidian-pdf-plus]")
      ?.addEventListener("click", () => {
        scale = Math.min(1.45, scale + 0.15);
        update();
      });
    bindObsidianAssetClose(panel, options);
    return true;
  }

  function renderObsidianImage(entry, panel, path, options = {}) {
    const item = entry.item || {};
    const name = text(entry.title, path.split("/").pop() || readerFocusCopy("未命名图片", "Untitled image"));
    const assetURL = text(
      item.asset_url,
      deviceDataPath("obsidian/asset", { path })
    );
    panel.classList.add("obsidian-detail-panel", "obsidian-asset-panel");
    if (options.citation)
      panel.classList.add("focus-source-detail-drawer", "detail-panel");
    const imagePreview = readerFocusCopy("图片预览", "Image preview");
    const unavailable = readerFocusCopy("图片暂时无法显示", "Image is temporarily unavailable");
    const retryHint = readerFocusCopy("原文件仍然保留，可以稍后重新打开。", "The original file is still available; try opening it again later.");
    const closeImage = readerFocusCopy("关闭图片", "Close image");
    panel.innerHTML = `<div class="obsidian-image-detail" data-obsidian-note="${esc(
      path
    )}" role="region" aria-label="${esc(`${name} ${imagePreview}`)}"><img class="obsidian-image-asset" src="${esc(
      assetURL
    )}" alt="${esc(obsidianDisplayName(name))}" loading="eager" decoding="async"><div class="obsidian-image-state" hidden><strong>${unavailable}</strong><p>${retryHint}</p></div><button class="obsidian-image-close" type="button" data-close-obsidian-asset aria-label="${closeImage}" title="${closeImage}">${icon(
      "close"
    )}</button></div>`;
    panel.querySelector(".obsidian-image-asset")?.addEventListener("error", (event) => {
      event.currentTarget.hidden = true;
      const state = panel.querySelector(".obsidian-image-state");
      if (state) state.hidden = false;
    });
    bindObsidianAssetClose(panel, options);
    return true;
  }

  function renderObsidianDetail(entry, panelOverride = null, options = {}) {
    if (!entry || entry.kind !== "evidence") return false;
    const item = entry.item || {};
    if (text(item.source_type).toLowerCase() !== "obsidian") return false;
    const panel = panelOverride || $("detail-panel");
    if (!panel) return false;
    const path = normalizeObsidianPath(item.locator || item.path);
    const parts = path.split("/").filter(Boolean);
    const name = text(entry.title, parts.at(-1) || readerFocusCopy("未命名笔记", "Untitled note"));
    const format = text(item.content_format, "markdown").toLowerCase();
    if (format === "pdf") return renderObsidianPDF(entry, panel, path, options);
    if (format === "image")
      return renderObsidianImage(entry, panel, path, options);
    const previewLabel = format === "pdf"
      ? readerFocusCopy("PDF 预览", "PDF preview")
      : format === "image"
        ? readerFocusCopy("图片预览", "Image preview")
        : format === "text"
          ? readerFocusCopy("文本预览", "Text preview")
          : readerFocusCopy("笔记预览", "Note preview");
    const content = text(item.content);
    const body = content
      ? format === "markdown"
        ? `<div class="fulltext-rich fulltext-markdown obsidian-note-content">${renderObsidianEvidence(
            content,
            path
          )}</div>`
        : `<pre class="obsidian-plain-text">${esc(content)}</pre>`
      : `<div class="obsidian-detail-loading" role="status"><span class="detail-loading-spinner" aria-hidden="true"></span><span>${readerFocusCopy("正在读取本地文件…", "Reading local file…")}</span></div>`;
    const breadcrumbs = parts
      .map(
        (part, index) =>
          `<span>${index ? icon("chevron") : ""}<span>${esc(part)}</span></span>`
      )
      .join("");
    panel.classList.add("obsidian-detail-panel");
    if (options.citation)
      panel.classList.add("focus-source-detail-drawer", "detail-panel");
    panel.innerHTML = `<div class="obsidian-note-detail" data-obsidian-note="${esc(
      path
    )}"><div class="obsidian-detail-topbar"><span class="obsidian-detail-label">${previewLabel}</span><div class="obsidian-detail-actions"><span class="obsidian-sync-state">${icon(
      "check"
    )} ${readerFocusCopy("本地知识库", "Local knowledge base")}</span><button class="text-btn obsidian-focus-button" type="button" data-add-entry-focus="${esc(
      entry.key
    )}" aria-label="${readerFocusCopy("加入焦点", "Add to focus")}">${icon("plus")}<span>${readerFocusCopy("加入焦点", "Add to focus")}</span></button><button class="icon-btn" type="button" data-close-detail aria-label="${readerFocusCopy("关闭笔记详情", "Close note details")}" title="${readerFocusCopy("关闭笔记详情", "Close note details")}" >${icon(
      "close"
    )}</button></div></div><nav class="obsidian-breadcrumb" aria-label="${readerFocusCopy("笔记路径", "Note path")}">${breadcrumbs}</nav><div class="obsidian-note-heading"><span class="obsidian-detail-mark">${icon(
      format === "image" ? "picture" : "obsidian"
    )}</span><div><h2>${esc(obsidianDisplayName(name))}</h2><p>${readerFocusCopy("修改于", "Modified")} ${esc(
      formatDate(item.modified_at || item.observed_at || entryDate(entry))
    )}</p></div></div><article class="obsidian-markdown">${body}</article></div>`;
    if (options.citation) {
      const closeButton = panel.querySelector("[data-close-detail]");
      if (closeButton) {
        closeButton.removeAttribute("data-close-detail");
        closeButton.setAttribute("data-close-focus-citation", "");
      }
    } else if (typeof bindDetailEvents === "function") bindDetailEvents();
    return true;
  }

  function articleRendererFor(entry, content) {
    const source = text(entry?.item?.source_id).toLowerCase();
    const sourceURL = text(content?.source_url || entry?.item?.url);
    const host = articleSourceHost(sourceURL);
    const format = text(content?.content_format).toLowerCase();
    const hasHTML = Boolean(articleHTML(content));
    if (
      hasHTML &&
      (source.includes("wechat") ||
        host === "mp.weixin.qq.com" ||
        host === "wechat2rss.xlab.app" ||
        host.endsWith(".mmbiz.qpic.cn"))
    )
      return articleRenderers.wechat;
    if (hasHTML) return articleRenderers.html;
    if (format === "markdown") return articleRenderers.markdown;
    return articleRenderers.text;
  }

  function renderArticleContent(entry, content) {
    const renderer = articleRendererFor(entry, content);
    return renderer.render(
      content,
      entry,
      content?.lead_image_index
    );
  }

  function articleBlocksFor(entry, content = null) {
    const candidates = [
      content?.blocks,
      content?.article_blocks,
      entry?.item?.blocks,
      entry?.item?.article_blocks,
    ];
    for (const value of candidates) {
      if (!Array.isArray(value) || !value.length) continue;
      return value
        .map((block, index) => {
          if (typeof block === "string")
            return {
              block_id: `b-${String(index + 1).padStart(3, "0")}`,
              ordinal: index,
              text: block,
            };
          if (!block || typeof block !== "object") return null;
          const blockID = text(block.block_id || block.id);
          const valueText = text(block.text || block.content || block.quote);
          if (!blockID || !valueText) return null;
          return {
            block_id: blockID.slice(0, 128),
            ordinal: Number.isFinite(Number(block.ordinal))
              ? Number(block.ordinal)
              : index,
            text: valueText.slice(0, 12000),
            text_hash: text(block.text_hash),
            type: text(block.type, "paragraph"),
          };
        })
        .filter(Boolean)
        .slice(0, 500);
    }
    return [];
  }

  function normalizeArticleBlockText(value) {
    return text(value)
      .replace(/\s+/g, " ")
      .replace(/[\u200b\u00a0]/g, " ")
      .trim()
      .toLowerCase();
  }

  // Renderers intentionally stay small and sanitizer-driven. This pass adds
  // stable block anchors after rendering, using the server's exact block text
  // to match sanitized paragraphs. Older articles simply remain unanchored.
  function decorateArticleBlocks(root, entry, content = null) {
    if (!root) return [];
    const blocks = articleBlocksFor(entry, content);
    if (!blocks.length) return [];
    const richRoot = root.matches?.(".fulltext-rich")
      ? root
      : root.querySelector?.(".fulltext-rich") || root;
    richRoot.dataset.articleBlocksVersion = text(
      content?.blocks_version || entry?.item?.blocks_version,
      "article-blocks-v1"
    );
    const candidates = Array.from(
      richRoot.querySelectorAll(
        "p,h1,h2,h3,h4,li,blockquote,pre,figcaption,td,th"
      )
    ).filter((node) => text(node.textContent));
    let cursor = 0;
    const anchored = [];
    for (const block of blocks) {
      const blockText = normalizeArticleBlockText(block.text);
      if (!blockText) continue;
      const existing = richRoot.querySelector(
        `[data-block-id="${CSS.escape(block.block_id)}"]`
      );
      if (existing) {
        anchored.push({ block, node: existing });
        continue;
      }
      let matchIndex = -1;
      for (let index = cursor; index < candidates.length; index += 1) {
        const candidateText = normalizeArticleBlockText(
          candidates[index].textContent
        );
        if (
          candidateText === blockText ||
          candidateText.includes(blockText) ||
          blockText.includes(candidateText)
        ) {
          matchIndex = index;
          break;
        }
      }
      // A feed may reorder a heading or list item. A second pass is safe only
      // when the normalized text uniquely identifies one rendered element.
      if (matchIndex < 0) {
        const matches = candidates.filter((candidate) => {
          const candidateText = normalizeArticleBlockText(candidate.textContent);
          return candidateText === blockText || candidateText.includes(blockText);
        });
        if (matches.length === 1) matchIndex = candidates.indexOf(matches[0]);
      }
      if (matchIndex < 0) continue;
      const node = candidates[matchIndex];
      node.dataset.blockId = block.block_id;
      node.dataset.blockOrdinal = String(block.ordinal);
      if (block.text_hash) node.dataset.blockTextHash = block.text_hash;
      anchored.push({ block, node });
      cursor = matchIndex + 1;
    }
    return anchored;
  }

  function articleContentStatusNotice(content) {
    const status = text(content?.status).toLowerCase();
    if (status === "summary") {
      const reason = text(content?.error_message);
      return `<div class="article-fallback article-summary-fallback" role="status"><strong>当前显示摘要</strong><p>${esc(
        reason || "正文服务暂时不可用，当前先保留摘要。"
      )} 正文恢复后可点击“重试读取正文”。</p></div>`;
    }
    if (status === "stale" && text(content?.error_message)) {
      return `<div class="article-fallback article-summary-fallback" role="status"><strong>已显示缓存正文</strong><p>${esc(
        text(content.error_message)
      )}</p></div>`;
    }
    return "";
  }

  function renderArticleInto(root, entry, content) {
    if (!root) return [];
    root.innerHTML = renderArticleContent(entry, content);
    const notice = articleContentStatusNotice(content);
    if (notice) root.insertAdjacentHTML("afterbegin", notice);
    const button = root.closest?.("#detail-panel")?.querySelector("[data-load-fulltext]");
    if (button) {
      button.disabled = false;
      button.textContent = text(content?.status).toLowerCase() === "summary"
        ? "重试读取正文"
        : "重新获取正文";
    }
    return decorateArticleBlocks(root, entry, content);
  }

  function inlineArticleContent(entry) {
    const item = entry?.item || {};
    return {
      content_html: text(item.safe_html),
      content_text: text(item.summary_text),
      content_format: text(item.content_format, "text"),
      lead_image_index: -1,
      source_url: text(item.url),
      content_hash: text(item.content_hash),
      blocks_version: text(item.blocks_version || item.article_blocks_version),
      blocks_hash: text(item.blocks_hash || item.article_blocks_hash),
      blocks_truncated: Boolean(
        item.blocks_truncated ?? item.article_blocks_truncated
      ),
      blocks: Array.isArray(item.blocks)
        ? item.blocks
        : Array.isArray(item.article_blocks)
        ? item.article_blocks
        : [],
    };
  }

  function replaceFailedDetailCover(image) {
    if (!image || image.dataset.detailCoverFailed === "1") return;
    image.dataset.detailCoverFailed = "1";
    image.remove();
  }

  function replaceFailedArticleImage(image) {
    if (!image || image.dataset.articleImageFailed === "1") return;
    image.dataset.articleImageFailed = "1";
    const fallback = document.createElement("span");
    fallback.className = "article-image-fallback";
    fallback.setAttribute("role", "img");
    fallback.setAttribute("aria-label", "文章图片暂时无法加载");
    fallback.textContent = "图片暂时无法加载";
    image.replaceWith(fallback);
  }

  function readableSummaryExcerpt(raw) {
    const value = text(raw).replace(/\s+/g, " ").trim();
    return value.length > 420 ? `${value.slice(0, 420)}…` : value;
  }

  function cachedFullText(entryID, contentHash = "") {
    const key = text(entryID);
    if (!key) return null;
    const value = parity.fullText.get(key);
    const valueHash = text(value?.content_hash);
    const requestedHash = text(contentHash);
    if (value && (text(value.status) === "summary" || articleContentLooksBlocked(value))) {
      parity.fullText.delete(key);
      return null;
    }
    if (value && Boolean(valueHash || requestedHash) && valueHash !== requestedHash) {
      parity.fullText.delete(key);
      return null;
    }
    if (value) {
      // Map insertion order gives us a tiny bounded LRU without introducing
      // another cache structure on the client.
      parity.fullText.delete(key);
      parity.fullText.set(key, value);
    }
    return value || null;
  }

  function storeFullText(entryID, value) {
    const key = text(entryID);
    if (
      !key ||
      !value ||
      text(value.status, "ok") === "summary" ||
      articleContentLooksBlocked(value)
    )
      return;
    parity.fullText.delete(key);
    parity.fullText.set(key, value);
    while (parity.fullText.size > fullTextCacheLimit)
      parity.fullText.delete(parity.fullText.keys().next().value);
  }

  async function loadFullText(entry, automatic = false) {
    if (!entry || entry.kind !== "news" || !entry.item?.id) return;
    const entryID = text(entry.item.id);
    const attemptKey = `${entryID}\u0000${text(entry.item.content_hash)}`;
    if (automatic && parity.fullTextAttempted.has(attemptKey)) return;
    if (parity.fullTextLoading.has(entryID)) return;
    parity.fullTextLoading.add(entryID);
    if (automatic) {
      parity.fullTextAttempted.add(attemptKey);
      while (parity.fullTextAttempted.size > fullTextAttemptLimit)
        parity.fullTextAttempted.delete(parity.fullTextAttempted.values().next().value);
    }
    const button = document.querySelector(
      `[data-load-fulltext="${CSS.escape(entryID)}"]`
    );
    if (button) {
      button.disabled = true;
      button.textContent = "正在整理原文…";
    }
    try {
      const result = await request(
        `./api/entries/content?id=${encodeURIComponent(entryID)}${
          automatic ? "" : "&refresh=1"
        }`,
        { timeoutMs: 30000 }
      );
      const content = text(result.content_text);
      if (!content) throw new Error("原文没有可读正文");
      if (articleContentLooksBlocked(result))
        throw new Error("来源返回验证页，正文暂时不可用");
      if (result.status !== "summary") {
        storeFullText(entryID, result);
        void writeBrowserFullText(entry, result);
        if (state.selected === entry.key) {
          const body = $("detail-panel")?.querySelector(".detail-body");
          if (body) renderArticleInto(body, entry, result);
        }
      } else if (state.selected === entry.key) {
        // A summary fallback must stay retryable; do not put it in either
        // full-text cache as if it were a rich projection.
        const body = $("detail-panel")?.querySelector(".detail-body");
        if (body) renderArticleInto(body, entry, result);
      }
      if (result.status === "summary" && button) {
        button.disabled = false;
        button.textContent = "重试读取正文";
      }
      if (!automatic)
        showToast(
          result.cached || result.status === "stale"
            ? "已显示设备缓存的全文"
            : "已读取全文（已保存到设备并缓存到浏览器）"
        );
    } catch (error) {
      if (button) {
        button.disabled = false;
        button.textContent = "重试读取正文";
      }
      if (automatic && state.selected === entry.key) {
        const body = $("detail-panel")?.querySelector(".detail-body");
        if (body)
          body.innerHTML = articleProjectionLooksBlocked(entry.item)
            ? `<div class="article-fallback article-summary-fallback"><strong>当前显示摘要</strong><p>来源返回了验证页，正文暂时无法读取。请稍后点击“重试读取正文”，或打开原文。</p></div>`
            : `<div class="article-fallback article-summary-fallback"><strong>当前显示摘要</strong><p>正文服务暂时不可用，先保留可读摘要。${esc(
                readableSummaryExcerpt(entry.item.summary_text) ||
                  "请稍后点击“重试读取正文”，或打开原文阅读。"
              )}</p></div>`;
      }
      // Automatic extraction is best-effort: keep the summary readable and
      // reserve the prominent error surface for an explicit user retry.
      if (!automatic) showError(error.message || "全文暂时读取失败");
    } finally {
      parity.fullTextLoading.delete(entry.item.id);
    }
  }

  function applyXFeedAvatar(entry) {
    const item = entry?.item || {};
    const sourceID = text(item.source_id);
    // Pinned X accounts use the source-level portrait resolver. It is stable
    // across posts and does not confuse reply/media image indexes with the
    // author's avatar.
    if (sourceID) return;
    const entryID = text(item.id);
    const avatarEntryID = text(parity.xFeedAvatarEntryID || entryID);
    if (!avatarEntryID) return;
    const avatarURL = `./api/entries/content/image?id=${encodeURIComponent(
      avatarEntryID
    )}&index=0`;
    document
      .querySelectorAll(
        `.x-entry .source-avatar-image[data-x-author-image]${
          sourceID
            ? `, .source-avatar-image[data-source-icon-id="${CSS.escape(
                sourceID
              )}"]`
            : ""
        }`
      )
      .forEach((image) => {
        image.hidden = false;
        image.dataset.sourceIconSrc = "";
        image.dataset.retrySrc = "";
        image.src = avatarURL;
      });
  }

  function applyXFeedFullText(entry, value) {
    const entryID = text(entry?.item?.id);
    if (!entryID) return;
    const card = document.querySelector(
      `.x-entry[data-x-entry-id="${CSS.escape(entryID)}"]`
    );
    if (!card) return;
    const post = xPostTextFromContent(value, entry) || xEntryFallbackText(entry);
    const body = card.querySelector(".x-entry-text");
    if (body) body.textContent = post;
    card.dataset.xNeedsFulltext = "0";
    card.removeAttribute("aria-busy");
    const status = card.querySelector(".x-entry-load-state");
    if (status) status.textContent = "";
    applyXFeedAvatar(entry);
    applyXFeedMedia(entry, value);
  }

  function xFeedMediaIndices(value) {
    const raw = text(value?.content_html);
    if (!raw) return [];
    const doc = new DOMParser().parseFromString(raw, "text/html");
    const post = doc.querySelector("li");
    if (!post) return [];
    return [...post.querySelectorAll("img[data-entry-image]")]
      .map((image) => ({
        index: Number(image.getAttribute("data-entry-image")),
        alt: text(image.getAttribute("alt")).toLowerCase(),
      }))
      .filter(
        (image) =>
          Number.isInteger(image.index) &&
          image.index >= 0 &&
          image.index < 24 &&
          !image.alt.includes("avatar") &&
          image.index !== 0
      )
      .map((image) => image.index)
      .slice(0, 4);
  }

  function applyXFeedMedia(entry, value) {
    const entryID = text(entry?.item?.id);
    if (!entryID) return;
    const card = document.querySelector(
      `.x-entry[data-x-entry-id="${CSS.escape(entryID)}"]`
    );
    const content = card?.querySelector(".x-entry-content");
    const foot = content?.querySelector(".x-entry-foot");
    if (!content || !foot) return;
    content.querySelector("[data-x-rich-media]")?.remove();
    const indices = xFeedMediaIndices(value);
    if (!indices.length) return;
    foot.insertAdjacentHTML(
      "beforebegin",
      `<div class="x-entry-rich-media" data-x-rich-media>${indices
        .map(
          (index) =>
            `<a class="x-entry-media x-entry-photo" href="${esc(
              entry.item.url
            )}" target="_blank" rel="noopener noreferrer"><img src="./api/entries/content/image?id=${encodeURIComponent(
              entryID
            )}&index=${index}" alt="${esc(
              (entry.item.author || entry.item.source || "公开账号") +
                " 的动态图片"
            )}" loading="lazy" decoding="async"></a>`
        )
        .join("")}</div>`
    );
  }

  async function hydrateXFeedEntry(entry) {
    const entryID = text(entry?.item?.id);
    if (!entryID) return;
    const card = document.querySelector(
      `.x-entry[data-x-entry-id="${CSS.escape(entryID)}"]`
    );
    if (!card || card.dataset.xNeedsFulltext !== "1") return;
    const cached = cachedFullText(entryID, entry.item.content_hash);
    if (cached) {
      applyXFeedFullText(entry, cached);
      return;
    }
    if (parity.fullTextLoading.has(entryID)) return;
    parity.fullTextLoading.add(entryID);
    card.setAttribute("aria-busy", "true");
    try {
      let result = await readBrowserFullText(entry);
      if (!result) {
        result = await request(
          `./api/entries/content?id=${encodeURIComponent(entryID)}`,
          { timeoutMs: 30000 }
        );
      }
      if (
        !text(result?.content_text) ||
        text(result?.status) === "summary" ||
        articleContentLooksBlocked(result)
      )
        throw new Error("完整动态暂时不可用");
      storeFullText(entryID, result);
      void writeBrowserFullText(entry, result);
      applyXFeedFullText(entry, result);
    } catch {
      card.removeAttribute("aria-busy");
      card.dataset.xNeedsFulltext = "0";
      const status = card.querySelector(".x-entry-load-state");
      if (status) status.textContent = "";
    } finally {
      parity.fullTextLoading.delete(entryID);
    }
  }

  function pumpXFeedHydration() {
    while (
      parity.xFeedHydrationActive < 2 &&
      parity.xFeedHydrationQueue.length
    ) {
      const entry = parity.xFeedHydrationQueue.shift();
      const entryID = text(entry?.item?.id);
      parity.xFeedHydrationQueued.delete(entryID);
      if (!entryID) continue;
      parity.xFeedHydrationActive += 1;
      void hydrateXFeedEntry(entry).finally(() => {
        parity.xFeedHydrationActive = Math.max(
          0,
          parity.xFeedHydrationActive - 1
        );
        pumpXFeedHydration();
      });
    }
  }

  function queueXFeedHydration(entry) {
    const entryID = text(entry?.item?.id);
    if (!entryID || parity.xFeedHydrationQueued.has(entryID)) return;
    parity.xFeedHydrationQueued.add(entryID);
    parity.xFeedHydrationQueue.push(entry);
    pumpXFeedHydration();
  }

  function observeXFeedFullText(entries) {
    parity.xFeedObserver?.disconnect();
    parity.xFeedObserver = null;
    parity.xFeedHydrationQueue.length = 0;
    parity.xFeedHydrationQueued.clear();
    if (!document.body.classList.contains("x-feed")) {
      parity.xFeedAvatarEntryID = "";
      return;
    }
    parity.xFeedAvatarEntryID = text(
      entries.find(
        (entry) => entry?.kind === "news" && xAccountForItem(entry.item)
      )?.item?.id
    );
    const byID = new Map(
      entries
        .filter((entry) => entry?.kind === "news" && xAccountForItem(entry.item))
        .map((entry) => [text(entry.item.id), entry])
    );
    const observer = new IntersectionObserver(
      (observations) => {
        // disconnect() does not guarantee that an already queued callback is
        // discarded. Ignore callbacks from a projection that no longer owns
        // the observer or the shell before they can enqueue hydration work.
        if (
          parity.xFeedObserver !== observer ||
          !document.body.classList.contains("x-feed")
        )
          return;
        observations.forEach((observation) => {
          if (!observation.isIntersecting) return;
          observer.unobserve(observation.target);
          const entry = byID.get(text(observation.target.dataset.xEntryId));
          if (entry) queueXFeedHydration(entry);
        });
      },
      { root: $("timeline"), rootMargin: "480px 0px", threshold: 0.01 }
    );
    parity.xFeedObserver = observer;
    document.querySelectorAll(".x-entry[data-x-entry-id]").forEach((card) => {
      const entry = byID.get(text(card.dataset.xEntryId));
      if (!entry) return;
      const cached = cachedFullText(entry.item.id, entry.item.content_hash);
      if (cached) {
        applyXFeedFullText(entry, cached);
        return;
      }
      if (card.dataset.xNeedsFulltext === "1")
        parity.xFeedObserver?.observe(card);
    });
  }

  function installFullTextControl(entry) {
    if (!entry || entry.kind !== "news" || !entry.item?.id) return;
    if (xAccountForItem(entry.item)) return;
    const panel = $("detail-panel");
    const body = panel.querySelector(".detail-body");
    if (!body) return;
    const controls = panel.querySelector(".detail-tools-row");
    if (controls && !panel.querySelector("[data-load-fulltext]")) controls.insertAdjacentHTML("afterbegin", `<button class="text-btn" type="button" data-load-fulltext="${esc(text(entry.item.id))}">读取全文</button>`);
    const cached = cachedFullText(entry.item.id, entry.item.content_hash);
    if (cached) {
      renderArticleInto(body, entry, cached);
      return;
    }
    const blocked = articleProjectionLooksBlocked(entry.item);
    if (entry.item.safe_html && !blocked) {
      renderArticleInto(body, entry, inlineArticleContent(entry));
      return;
    }
    const tools = panel.querySelector(".detail-tools-row");
    if (!tools) return;
    const entryID = text(entry.item.id);
    const attemptKey = `${entryID}\u0000${text(entry.item.content_hash)}`;
    const loading =
      parity.fullTextLoading.has(entryID) ||
      parity.fullTextHydrating.has(entryID);
    if (!panel.querySelector("[data-load-fulltext]")) {
      tools.insertAdjacentHTML(
        "afterbegin",
        `<button class="text-btn" type="button" data-load-fulltext="${esc(
          entryID
        )}" ${loading ? "disabled" : ""}>${
          loading ? "正在整理原文…" : "读取全文"
        }</button>`
      );
    }
    if (!blocked && (loading || !parity.fullTextAttempted.has(attemptKey))) {
      body.innerHTML = `<div class="article-fallback" role="status" aria-busy="true"><strong>正在恢复正文排版…</strong><p>首次读取可能需要几秒。</p></div>`;
    }
    if (blocked && !loading) {
      body.innerHTML = `<div class="article-fallback"><strong>原文暂时不可用</strong><p>来源返回了验证页，未将其作为正文或 AI 概览。可以稍后重试或打开原文。</p></div>`;
    }
    if (loading) {
      return;
    }
    if (parity.fullTextAttempted.has(attemptKey)) return;

    // First open: restore the bounded browser cache first, then ask the AC
    // for a sanitized projection. The summary remains the final fallback,
    // while the retry button stays available if the public page is blocked.
    parity.fullTextHydrating.add(entryID);
    const hydrate = async () => {
      try {
        const browserCached = await readBrowserFullText(entry);
        if (browserCached && !articleContentLooksBlocked(browserCached)) {
          storeFullText(entryID, browserCached);
          if (state.selected === entry.key) {
            const currentBody = $("detail-panel")?.querySelector(".detail-body");
            if (currentBody) renderArticleInto(currentBody, entry, browserCached);
          }
          return;
        }
        if (state.selected === entry.key) await loadFullText(entry, true);
      } finally {
        parity.fullTextHydrating.delete(entryID);
      }
    };
    void hydrate();
  }

  function installProductArticleDetailPresentation(entry) {
    if (!entry || entry.kind !== "news" || !entry.item) return false;
    if (xAccountForItem(entry.item) || videoForItem(entry.item)) return false;
    const panel = $("detail-panel");
    const top = panel?.querySelector(".detail-top");
    const topCopy = top?.querySelector(".detail-top-copy");
    const tools = top?.querySelector(".detail-tools");
    const body = panel?.querySelector(".detail-body");
    if (!panel || !top || !topCopy || !tools || !body) return false;

    panel.classList.add("article-v3-detail");
    top.classList.add("article-v3-heading");
    topCopy.querySelector(".detail-kicker")?.classList.add("article-v3-kicker");
    topCopy.querySelector(".detail-title")?.classList.add("article-v3-title");
    topCopy.querySelector(".detail-subtitle")?.remove();

    const bottomTools = panel.querySelector(".detail-tools-row");
    const external = bottomTools?.querySelector("a");
    if (external) {
      external.className = "icon-btn article-v3-external";
      external.setAttribute("aria-label", "打开原文");
      external.setAttribute("title", "打开原文");
      external.innerHTML = icon("external");
    }
    tools.querySelector("[data-context-entry]")?.remove();

    const close = tools.querySelector("[data-close-detail]");
    const star = tools.querySelector("[data-star-entry]");
    const later = tools.querySelector("[data-later-entry]");
    let focus = tools.querySelector("[data-add-entry-focus]");
    if (!focus) {
      focus = document.createElement("button");
      focus.type = "button";
      focus.dataset.addEntryFocus = entry.key;
    }
    const descriptor = focusLinkDescriptor(entry.key);
    const linkedCount = descriptor
      ? parity.focusSpaces.filter((item) => focusHasLinkedEntry(item, descriptor)).length
      : 0;
    focus.className = `article-v3-focus-picker${linkedCount ? " is-linked" : ""}`;
    focus.setAttribute(
      "aria-label",
      linkedCount ? `管理焦点，已添加 ${linkedCount} 个` : "添加到焦点"
    );
    focus.setAttribute(
      "title",
      linkedCount ? `已添加 ${linkedCount} 个焦点` : "添加到焦点"
    );
    focus.innerHTML = `${icon("focus")}<span>焦点${
      linkedCount ? ` ${linkedCount}` : ""
    }</span>${icon("chevron")}`;
    const actions = document.createElement("div");
    actions.className = "article-v3-actions";
    [close, star, later, external, focus].forEach((control) => {
      if (control) actions.appendChild(control);
    });
    const topbar = document.createElement("div");
    topbar.className = "article-v3-topbar";
    topbar.innerHTML = '<span class="article-v3-column-label">文章详情</span>';
    topbar.appendChild(actions);
    const progress = panel.querySelector(".reading-progress");
    if (progress) progress.insertAdjacentElement("afterend", topbar);
    else panel.prepend(topbar);
    tools.remove();

    const byline = document.createElement("p");
    byline.className = "article-v3-byline";
    byline.textContent = `${text(entry.item.author, "订阅来源")} · ${
      text(entry.item.permission_scope) === "public_read"
        ? "公开可读"
        : text(entry.item.permission_scope, "来源可读")
    }`;
    body.insertAdjacentElement("beforebegin", byline);
    bottomTools?.querySelector("[data-dismiss-news]")?.remove();
    if (bottomTools && !bottomTools.childElementCount) bottomTools.remove();
    return true;
  }

  function meetingReportObject(raw) {
    if (raw && typeof raw === "object" && !Array.isArray(raw)) return raw;
    const value = text(raw).trim();
    if (!value || (!value.startsWith("{") && !value.startsWith("["))) return {};
    try {
      const parsed = JSON.parse(value);
      return parsed && typeof parsed === "object" && !Array.isArray(parsed)
        ? parsed
        : {};
    } catch {
      return {};
    }
  }

  function meetingReportText(item) {
    if (typeof item === "string" || typeof item === "number") return text(item);
    if (!item || typeof item !== "object") return "";
    return text(
      item.text || item.content || item.detail || item.title || item.task ||
        item.recommendation || item.summary
    );
  }

  function meetingReportList(...values) {
    const output = [];
    values.flatMap((value) => (Array.isArray(value) ? value : [])).forEach((item) => {
      const value = meetingReportText(item).trim();
      if (value && !output.includes(value)) output.push(value);
    });
    return output.slice(0, 24);
  }

  function meetingStructuredMarkdown(raw) {
    const sections = { overview: [], highlights: [], actions: [] };
    let active = "overview";
    text(raw)
      .replace(/\r\n?/g, "\n")
      .split("\n")
      .forEach((sourceLine) => {
        const line = sourceLine.trim();
        if (!line) return;
        const heading = line.match(/^#{1,6}\s+(.+)$/)?.[1]?.trim() || "";
        if (heading) {
          if (/行动|待办|任务|action/i.test(heading)) active = "actions";
          else if (/关键|讨论|结论|决议|要点|highlight|conclusion|decision/i.test(heading)) active = "highlights";
          else active = "overview";
          return;
        }
        const cleaned = line
          .replace(/^[-*+]\s+/, "")
          .replace(/^\d+[.)、]\s*/, "")
          .replace(/^>\s*/, "")
          .replace(/\*\*/g, "")
          .replace(/`/g, "")
          .trim();
        if (!cleaned) return;
        if ((active === "highlights" || active === "actions") && !sections[active].includes(cleaned)) {
          sections[active].push(cleaned);
          return;
        }
        sections.overview.push(cleaned);
      });
    return {
      overview: sections.overview.join(" ").replace(/\s+/g, " ").slice(0, 900),
      highlights: sections.highlights.slice(0, 12),
      actions: sections.actions.slice(0, 12),
    };
  }

  function meetingActionItems(...values) {
    const output = [];
    values.flatMap((value) => (Array.isArray(value) ? value : [])).forEach((item) => {
      const title = meetingReportText(item).trim();
      if (!title || output.some((entry) => entry.title === title)) return;
      const meta = item && typeof item === "object"
        ? [text(item.owner || item.assignee), text(item.due || item.due_at || item.deadline)].filter(Boolean).join(" · ")
        : "";
      output.push({ title, meta });
    });
    return output.slice(0, 12);
  }

  function meetingStatusKind(value) {
    const status = text(value).toLowerCase();
    if (["done", "completed", "summarized", "ready"].includes(status)) return "ready";
    if (["failed", "error", "cancelled", "canceled"].includes(status)) return "failed";
    return "processing";
  }

  function meetingTranscriptSegments(detail) {
    const names = detail?.speaker_names && typeof detail.speaker_names === "object"
      ? detail.speaker_names
      : {};
    const segments = (Array.isArray(detail?.segments) ? detail.segments : [])
      .map((segment) => ({
        at: Number(segment?.start ?? segment?.start_seconds) || 0,
        speaker: text(names[segment?.speaker] || segment?.speaker, "发言人"),
        text: text(segment?.text).trim(),
      }))
      .filter((segment) => segment.text);
    if (segments.length) return segments.slice(0, 1000);
    return text(detail?.transcript)
      .replace(/\r\n?/g, "\n")
      .split(/\n{2,}|\n(?=\S)/)
      .map((line) => line.trim())
      .filter(Boolean)
      .slice(0, 500)
      .map((line) => ({ at: null, speaker: "逐字稿", text: line }));
  }

  function meetingPlayIcon(playing = false) {
    return playing
      ? '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 5h4v14H7zM13 5h4v14h-4z" fill="currentColor"/></svg>'
      : '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m8 5 11 7-11 7z" fill="currentColor"/></svg>';
  }

  function renderMeetingDetail(entry, panelOverride = null, options = {}) {
    if (entry?.kind !== "recording") return false;
    const panel = panelOverride || $("detail-panel");
    const detail = state.recordingDetails?.get(text(entry.id)) || entry.item || {};
    const report = meetingReportObject(detail.report);
    const status = meetingStatusKind(detail.status);
    const duration = Math.max(0, Number(detail.duration_seconds) || 0);
    const speakerCount = Math.max(
      0,
      Number(detail.speaker_count) || Object.keys(detail.speaker_names || {}).length
    );
    const rawSummary = text(
      detail.summary || report.summary || report.overview || report.headline,
      "暂无 AI 总结"
    );
    const structuredSummary = meetingStructuredMarkdown(rawSummary);
    const summary = text(report.overview || report.headline || structuredSummary.overview, "暂无 AI 总结");
    const reportHighlights = meetingReportList(
      report.key_points,
      report.highlights,
      report.decisions,
      report.conclusions
    );
    const highlights = reportHighlights.length
      ? reportHighlights
      : structuredSummary.highlights;
    const reportActions = meetingActionItems(report.action_items, report.actions, report.tasks);
    const actions = reportActions.length
      ? reportActions
      : structuredSummary.actions.map((title) => ({ title, meta: "" }));
    const segments = meetingTranscriptSegments(detail);
    const preference = pref(entry);
    const source = audioURL(detail.audio_ref);
    const date = formatDate(entryDate(entry));
    const summaryMarkup = status === "ready"
      ? `<div class="meeting-v3-summary-view"><section class="meeting-v3-overview"><div class="meeting-v3-section-title">${icon("sparkles")}<h3>AI 摘要</h3></div><p>${esc(summary)}</p></section><section><div class="meeting-v3-section-title">${icon("action")}<h3>关键结论</h3></div>${highlights.length ? `<ul class="meeting-v3-highlights">${highlights.map((item) => `<li>${esc(item)}</li>`).join("")}</ul>` : '<p class="meeting-v3-empty">暂无结构化关键结论</p>'}</section><section><div class="meeting-v3-section-title">${icon("check")}<h3>行动项</h3></div>${actions.length ? `<div class="meeting-v3-actions-list">${actions.map((item) => `<article><span>${icon("check")}</span><div><strong>${esc(item.title)}</strong>${item.meta ? `<small>${esc(item.meta)}</small>` : ""}</div></article>`).join("")}</div>` : '<p class="meeting-v3-empty">本次会议没有识别到行动项</p>'}</section></div>`
      : `<div class="meeting-v3-state ${status === "failed" ? "is-failed" : ""}">${status === "failed" ? icon("close") : icon("refresh")}<strong>${esc(text(detail.meeting_stage, status === "failed" ? "处理失败" : "正在生成会议内容"))}</strong><p>${esc(text(detail.error_message, status === "failed" ? "处理暂时失败，原始录音仍保存在设备中。" : "录音已安全保存，处理完成后会自动更新。"))}</p></div>`;
    const transcriptMarkup = segments.length
      ? `<div class="meeting-v3-transcript">${segments.map((segment) => `<article data-meeting-segment-at="${segment.at === null ? "" : esc(String(segment.at))}">${segment.at === null ? '<span class="meeting-v3-time is-static">--:--</span>' : `<button class="meeting-v3-time" type="button" data-meeting-seek="${esc(String(segment.at))}" aria-label="跳转到 ${esc(secondsLabel(segment.at))}">${esc(secondsLabel(segment.at))}</button>`}<span class="meeting-v3-speaker">${esc(segment.speaker)}</span><p>${esc(segment.text)}</p></article>`).join("")}${detail.transcript_truncated || detail.segments_truncated ? '<p class="meeting-v3-truncated">逐字稿较长，当前显示设备返回的可用部分。</p>' : ""}</div>`
      : '<div class="meeting-v3-state"><strong>暂无转录内容</strong><p>设备尚未返回可展示的逐字稿。</p></div>';
    const player = status === "ready"
      ? source
        ? `<footer class="meeting-v3-player" aria-label="录音播放器"><audio id="meeting-v3-audio" preload="metadata" src="${esc(source)}"></audio><div class="meeting-v3-progress"><span data-meeting-current>0:00</span><input type="range" min="0" max="${esc(String(duration || 1))}" step="0.1" value="0" data-meeting-progress aria-label="播放进度"><span data-meeting-duration>${esc(secondsLabel(duration))}</span></div><div class="meeting-v3-player-controls"><button type="button" data-meeting-back aria-label="后退 15 秒" title="后退 15 秒">${icon("refresh")}<small>15</small></button><button class="meeting-v3-play" type="button" data-meeting-play aria-label="播放录音" title="播放录音">${meetingPlayIcon(false)}</button><button type="button" data-meeting-forward aria-label="前进 15 秒" title="前进 15 秒">${icon("refresh")}<small>15</small></button><button class="meeting-v3-rate" type="button" data-meeting-rate aria-label="播放速度" title="播放速度">1x</button></div></footer>`
        : '<div class="meeting-v3-no-audio">当前录音没有可播放音频</div>'
      : "";
    panel.classList.add("meeting-v3-detail-panel");
    if (options.citation)
      panel.classList.add("focus-source-detail-drawer", "detail-panel");
    panel.innerHTML = `<div class="meeting-v3-topbar"><span class="meeting-v3-column-label">会议记录</span><div class="meeting-v3-head-actions"><button class="icon-btn ${preference.starred ? "starred" : ""}" type="button" data-star-entry="${esc(entry.key)}" aria-label="${preference.starred ? "取消收藏" : "收藏"}" title="${preference.starred ? "取消收藏" : "收藏"}">${preference.starred ? icon("starFill") : icon("star")}</button><button class="article-v3-focus-picker" type="button" data-add-entry-focus="${esc(entry.key)}" aria-label="添加到焦点" title="添加到焦点">${icon("focus")}<span>焦点</span>${icon("chevron")}</button><button class="icon-btn" type="button" data-close-detail aria-label="关闭会议详情" title="关闭会议详情">${icon("close")}</button></div></div><div class="meeting-v3-scroll"><div class="meeting-v3-meta"><span>${icon("audio")}${esc(secondsLabel(duration))}</span><span>${icon("meeting")}${esc(String(speakerCount || 1))} 人</span><span>${esc(date)}</span></div><h2>${esc(entry.title)}</h2>${status === "ready" ? '<div class="meeting-v3-tabs" role="tablist" aria-label="会议详情内容"><button class="is-active" type="button" role="tab" aria-selected="true" data-meeting-tab="summary">AI 总结</button><button type="button" role="tab" aria-selected="false" data-meeting-tab="transcript">转录内容</button></div>' : ""}<div class="meeting-v3-panel" data-meeting-panel="summary">${summaryMarkup}</div><div class="meeting-v3-panel" data-meeting-panel="transcript" hidden>${transcriptMarkup}</div>${player}</div>`;
    if (options.citation) {
      const closeButton = panel.querySelector("[data-close-detail]");
      if (closeButton) {
        closeButton.removeAttribute("data-close-detail");
        closeButton.setAttribute("data-close-focus-citation", "");
      }
    } else bindDetailEvents();
    bindMeetingDetailEvents(panel, entry);
    return true;
  }

  function bindMeetingDetailEvents(panel, entry) {
    const audio = panel.querySelector("#meeting-v3-audio");
    let pendingSeek = null;
    let autoplayAfterMetadata = false;
    const seekTo = (seconds, autoplay = false) => {
      if (!audio) return;
      const target = Math.max(0, Number(seconds) || 0);
      if (audio.readyState < 1) {
        pendingSeek = target;
        autoplayAfterMetadata = autoplayAfterMetadata || autoplay;
        audio.load();
        return;
      }
      const knownDuration =
        Number.isFinite(audio.duration) && audio.duration > 0;
      audio.currentTime = Math.min(
        knownDuration ? audio.duration : target,
        target
      );
      sync();
      if (autoplay) void audio.play().catch(() => {});
    };
    panel.querySelectorAll("[data-meeting-tab]").forEach((button) => {
      button.addEventListener("click", () => {
        const selected = button.dataset.meetingTab;
        panel.querySelectorAll("[data-meeting-tab]").forEach((tab) => {
          const active = tab.dataset.meetingTab === selected;
          tab.classList.toggle("is-active", active);
          tab.setAttribute("aria-selected", String(active));
        });
        panel.querySelectorAll("[data-meeting-panel]").forEach((content) => {
          content.hidden = content.dataset.meetingPanel !== selected;
        });
      });
    });
    panel.querySelectorAll("[data-meeting-seek]").forEach((button) => {
      button.addEventListener("click", () => {
        seekTo(button.dataset.meetingSeek, true);
      });
    });
    if (!audio) return;
    const stored = liveMeetingPreference(entry.id);
    const progress = panel.querySelector("[data-meeting-progress]");
    const current = panel.querySelector("[data-meeting-current]");
    const duration = panel.querySelector("[data-meeting-duration]");
    const play = panel.querySelector("[data-meeting-play]");
    const rate = panel.querySelector("[data-meeting-rate]");
    const sync = () => {
      const seconds = Number(audio.currentTime) || 0;
      if (current) current.textContent = secondsLabel(seconds);
      if (progress) {
        progress.max = String(
          Number.isFinite(audio.duration) && audio.duration > 0
            ? audio.duration
            : Number(progress.max) || 1
        );
        progress.value = String(seconds);
      }
      panel.querySelectorAll("[data-meeting-segment-at]").forEach((segment) => {
        const at = Number(segment.dataset.meetingSegmentAt);
        segment.classList.toggle("is-active", segment.dataset.meetingSegmentAt !== "" && Number.isFinite(at) && at <= seconds);
      });
    };
    const restorePlayback = () => {
      if (duration && Number.isFinite(audio.duration) && audio.duration > 0)
        duration.textContent = secondsLabel(audio.duration);
      const resumeAt = pendingSeek === null
        ? stored.playback_seconds
        : pendingSeek;
      if (resumeAt > 0) {
        const maxResume =
          Number.isFinite(audio.duration) && audio.duration > 0
            ? Math.max(0, audio.duration - 0.1)
            : resumeAt;
        audio.currentTime = Math.min(
          resumeAt,
          maxResume
        );
      }
      pendingSeek = null;
      sync();
      if (autoplayAfterMetadata) {
        autoplayAfterMetadata = false;
        void audio.play().catch(() => {});
      }
    };
    if (audio.readyState >= 1) restorePlayback();
    else audio.addEventListener("loadedmetadata", restorePlayback, { once: true });
    audio.addEventListener("timeupdate", sync);
    let lastSavedAt = 0;
    const savePlayback = (force = false) => {
      const now = Date.now();
      if (!force && now - lastSavedAt < 1000) return;
      lastSavedAt = now;
      storeLiveMeetingPreference(entry.id, {
        playback_seconds: audio.currentTime,
      });
    };
    audio.addEventListener("timeupdate", () => savePlayback(false));
    audio.addEventListener("play", () => {
      if (!play) return;
      play.innerHTML = meetingPlayIcon(true);
      play.setAttribute("aria-label", "暂停播放");
      play.title = "暂停播放";
    });
    audio.addEventListener("pause", () => {
      savePlayback(true);
      if (!play) return;
      play.innerHTML = meetingPlayIcon(false);
      play.setAttribute("aria-label", "播放录音");
      play.title = "播放录音";
    });
    play?.addEventListener("click", () => {
      if (audio.paused) void audio.play().catch(() => {});
      else audio.pause();
    });
    audio.addEventListener("ended", () => {
      storeLiveMeetingPreference(entry.id, {
        read: true,
        playback_seconds: audio.currentTime,
      });
    });
    panel.querySelector("[data-meeting-back]")?.addEventListener("click", () => {
      seekTo(audio.currentTime - 15);
    });
    panel.querySelector("[data-meeting-forward]")?.addEventListener("click", () => {
      seekTo(audio.currentTime + 15);
    });
    progress?.addEventListener("input", () => {
      seekTo(progress.value);
      sync();
    });
    rate?.addEventListener("click", () => {
      audio.playbackRate = audio.playbackRate === 1 ? 1.5 : audio.playbackRate === 1.5 ? 2 : 1;
      rate.textContent = `${audio.playbackRate}x`;
    });
  }

  function detailRenderFingerprint(entry) {
    const item = entry?.item || {};
    const overview = item.ai_overview || {};
    const preference = pref(entry);
    const overviewKey = articleOverviewKey(item);
    const recording =
      entry?.kind === "recording"
        ? state.recordingDetails?.get(text(item.id)) || {}
        : {};
    return [
      entry?.key,
      text(item.content_hash),
      text(item.content_format),
      text(item.content),
      text(item.asset_url),
      text(item.blocks_version || item.article_blocks_version),
      text(item.blocks_hash || item.article_blocks_hash),
      Array.isArray(item.blocks)
        ? item.blocks.length
        : Array.isArray(item.article_blocks)
        ? item.article_blocks.length
        : 0,
      text(item.title),
      text(item.summary_text || item.summary),
      text(item.thumbnail_url),
      text(item.media_url),
      text(item.duration_seconds),
      text(item.view_type),
      text(item.media_mime),
      text(videoProviderForItem(item)),
      text(item.status),
      text(item.confirmation_status),
      text(item.schedule_state),
      text(item.job_id),
      text(item.client_request_id),
      text(item.revision),
      text(overview.status),
      text(overview.generated_at),
      text(recording.status),
      text(recording.updated_at),
      text(recording.duration_seconds),
      text(recording.audio_ref),
      text(recording.summary),
      text(recording.transcript),
      text(recording.per_speaker_summary),
      text(recording.report),
      Array.isArray(recording.segments) ? recording.segments.length : 0,
      contextIsActive(entry) ? "context-active" : "context-inactive",
      preference.starred ? "starred" : "unstarred",
      preference.read_later ? "later" : "not-later",
    ].join("\u0000");
  }

  renderDetail = function renderParityDetail() {
    const panel = $("detail-panel");
    if (parity.detailLoading) {
      if (panel.dataset.detailFingerprint === "loading") return;
      panel.classList.remove("x-detail-panel", "youtube-detail-panel", "obsidian-detail-panel", "meeting-v3-detail-panel");
      panel.setAttribute("aria-busy", "true");
      panel.innerHTML = detailLoadingMarkup();
      panel.dataset.detailFingerprint = "loading";
      return;
    }
    restoreCachedSelectedEntry();
    const entry =
      state.selected &&
      makeEntries(state.dashboard || {}).find(
        (item) => item.key === state.selected
      );
    if (!entry) {
      const overviewFingerprint = [
        "overview",
        state.view,
        text(state.dashboard?.generated_at),
        String((state.dashboard?.actions || []).length),
        String((state.dashboard?.decisions || []).length),
        String((state.dashboard?.signals || []).length),
      ].join("\u0000");
      if (panel.dataset.detailFingerprint === overviewFingerprint) return;
      panel.classList.remove("x-detail-panel", "youtube-detail-panel", "obsidian-detail-panel", "meeting-v3-detail-panel");
      panel.removeAttribute("aria-busy");
      panel.dataset.detailFingerprint = overviewFingerprint;
      baseRenderDetail();
      return;
    }
    const fingerprint = detailRenderFingerprint(entry);
    if (panel.dataset.detailFingerprint === fingerprint && panel.childElementCount)
      return;
    panel.classList.remove("x-detail-panel", "youtube-detail-panel", "meeting-v3-detail-panel");
    panel.removeAttribute("aria-busy");
    panel.dataset.detailFingerprint = fingerprint;
    if (renderObsidianDetail(entry)) return;
    if (renderMeetingDetail(entry)) return;
    panel.classList.remove("obsidian-detail-panel");
    baseRenderDetail();
    const videoEntry =
      entry.kind === "news" && Boolean(videoForItem(entry.item));
    const isYouTube =
      entry.kind === "news" && Boolean(youtubeVideoForItem(entry.item));
    const isBilibili =
      entry.kind === "news" && videoProviderForItem(entry.item) === "bilibili";
    installYouTubeDetailPresentation(entry);
    installBilibiliDetailPresentation(entry);
    const xDetail = installXDetailPresentation(entry);
    if (!isYouTube && !isBilibili && !videoEntry) installArticleAIOverview(entry);
    installSignalCounterevidence(entry);
    installContextControl(entry);
    if (entry.kind === "recording") return;
    installActionWorkflowControl(entry);
    // The base renderer only knows the feed's public URLs. Rebind every
    // stored media URL through the host-owned proxy before the browser can
    // request it; a bare `localAppMediaURL(source)` call would reject an
    // external URL because it has no entry id and would remove valid media.
    $("detail-panel")
      .querySelectorAll(
        ".detail-media, .detail-cover, .detail-body img, .detail-body video, .detail-body audio"
      )
      .forEach((node) => {
        const source = node.getAttribute("src") || "";
        if (source) {
          const kind = node.tagName === "IMG" ? "thumbnail" : "media";
          const trustedXVideo =
            node.tagName === "VIDEO"
              ? trustedXVideoURL(source, entry.item)
              : "";
          const directXImage =
            node.tagName === "IMG"
              ? trustedXImageURL(source, entry.item)
              : "";
          const local =
            (trustedXVideo
              ? localAppMediaURL(
                  trustedXVideo,
                  entry.item?.id || "",
                  "media"
                )
              : "") ||
            directXImage ||
            localAppMediaURL(source, entry.item?.id || "", kind);
          if (!local) node.remove();
          else {
            node.setAttribute("src", local);
            if (trustedXVideo) {
              node.classList.add("x-detail-video");
              node.dataset.xEntryUrl = entry.item.url;
              if (!node.closest("[data-x-media-shell]")) {
                const shell = document.createElement("div");
                shell.className = "x-entry-media x-detail-video-shell";
                shell.dataset.xMediaShell = "";
                node.parentNode?.insertBefore(shell, node);
                shell.appendChild(node);
                shell.insertAdjacentHTML(
                  "beforeend",
                  xVideoFallbackMarkup(
                    entry.item,
                    trustedXImageURL(entry.item.thumbnail_url, entry.item) ||
                      localAppMediaURL(
                        entry.item.thumbnail_url,
                        entry.item.id,
                        "thumbnail"
                      )
                  )
                );
              }
            }
          }
        }
        const poster = node.getAttribute("poster");
        if (poster) {
          const localPoster =
            trustedXImageURL(poster, entry.item) ||
            localAppMediaURL(poster, entry.item?.id || "", "thumbnail");
          if (!localPoster) node.removeAttribute("poster");
          else node.setAttribute("poster", localPoster);
        }
      });
    const preference = pref(entry);
    const tools = $("detail-panel").querySelector(".detail-tools");
    if (tools && !tools.querySelector("[data-later-entry]")) {
      tools.insertAdjacentHTML(
        "afterbegin",
        `<button class="icon-btn ${
          preference.read_later ? "active" : ""
        }" type="button" data-later-entry="${esc(entry.key)}" aria-label="${
          preference.read_later ? "移出稍后读" : "稍后读"
        }" title="${preference.read_later ? "移出稍后读" : "稍后读"}">${icon(
          "inbox"
        )}</button>`
      );
    }
    if (
      !xDetail &&
      !videoEntry &&
      ["news", "evidence"].includes(entry.kind)
    ) {
      $("detail-panel").insertAdjacentHTML(
        "afterbegin",
        `<div class="reading-progress"><i style="width:${Math.round(
          (preference.reading_progress || 0) * 100
        )}%"></i></div>`
      );
      if (parity.restoredKey !== entry.key && preference.reading_progress > 0) {
        parity.restoredKey = entry.key;
        requestAnimationFrame(() => {
          const panel = $("detail-panel");
          panel.scrollTop =
            preference.reading_progress *
            Math.max(0, panel.scrollHeight - panel.clientHeight);
        });
      }
    }
    if (!videoEntry) installFullTextControl(entry);
    installProductArticleDetailPresentation(entry);
  };

  async function toggleStableContext(key) {
    const active = contextIsActive(key);
    try {
      const result = await jsonRequest(
        "./api/context",
        active ? "DELETE" : "POST",
        active
          ? { item_key: key }
          : {
              item_key: key,
              state: "active",
              added_by: "context_action",
              confirmed: true,
            }
      );
      if (active) {
        parity.contextMemberships.delete(key);
        parity.contextMutationKeys.delete(key);
      } else {
        parity.contextMemberships.set(key, result);
        if (parity.contextMutationKeys.size >= contextMutationLimit) {
          const oldest = parity.contextMutationKeys.values().next().value;
          if (oldest) parity.contextMutationKeys.delete(oldest);
        }
        parity.contextMutationKeys.add(key);
      }
      rebindReaderNewsProjection();
      render();
      showToast(active ? t("removeFromKnowledge") : t("addToKnowledge"));
    } catch (error) {
      showError(error.message || "上下文状态没有保存");
    }
  }

  refresh = async function refreshParityState(silent = false) {
    const refreshView = state.view;
    const selectedKey = state.selected;
    const selectedScope = entryScopeForView();
    // A global dashboard refresh is not authoritative for a source-scoped
    // timeline. Keep the bounded typed-page snapshot across background and
    // source-warmup refreshes so an open article is not mistaken for a
    // deleted row merely because it is absent from the global projection.
    const scopedSnapshot =
      selectedScope && Array.isArray(state.dashboard?.news)
        ? {
            news: state.dashboard.news.map((item) => ({ ...item })),
            cursor: parity.entryCursor,
            pagesRead: parity.entryPagesRead,
            hasMore: parity.entryHasMore,
          }
        : null;
    const selectedScrollTop = selectedKey ? $("detail-panel")?.scrollTop || 0 : 0;
    const selectedEntry =
      selectedKey &&
      makeEntries(state.dashboard || {}).find(
        (entry) => entry.key === selectedKey
      );
    if (selectedEntry) cacheDetailEntry(selectedEntry);
    await baseRefresh({ deferRender: true });
    await loadFocusSpaces(silent);
    if (state.view !== refreshView) { renderNav(); return; }
    if (state.dashboard?.news_included !== false) {
    resetEntryPagination();
    if (selectedScope && scopedSnapshot) {
      parity.entryScopeSnapshots.set(selectedScope, scopedSnapshot);
      parity.entryScope = selectedScope;
      restoreEntryScopeSnapshot(selectedScope);
    } else if (!selectedScope) {
      const currentNewsCount = loadedAggregateEntryCount(state.view);
      const dashboardTotal = entryTotalForView(state.view);
      // The fresh dashboard snapshot is authoritative. Do not union pages
      // from the previous projection here: those rows may have been deleted
      // or dismissed while the reader was open. The next scroll starts from
      // the new keyset cursor and can rehydrate only rows the server returns.
      parity.entryCursor = "0";
      parity.entryPagesRead = 0;
      parity.entryHasMore =
        currentNewsCount < readerNewsTotalLimit &&
        (Number.isFinite(dashboardTotal)
          ? dashboardTotal > currentNewsCount
          : currentNewsCount >= parity.entryPageSize);
      rememberAggregateEntrySnapshot();
    }
    }
    // Keep a selected detail only when the authoritative projection contains
    // it. For source views that projection is the restored typed-page
    // snapshot; for aggregate views it is the fresh dashboard response.
    const selectedStillPresent =
      selectedKey &&
      makeEntries(state.dashboard || {}).some(
        (entry) => entry.key === selectedKey
      );
    // The base refresh intentionally asks for a lightweight dashboard with
    // news_included=false.  Its empty news array is not evidence that the
    // selected article disappeared; the scoped snapshot/detail cache above is
    // authoritative until the typed reader request settles.  Only an
    // authoritative news projection may clear a selected detail.
    const dashboardNewsIncluded = state.dashboard?.news_included !== false;
    if (selectedStillPresent || (selectedKey && !dashboardNewsIncluded))
      state.selected = selectedKey;
    else if (selectedKey) state.selected = null;
    restoreCachedSelectedEntry();
    restorePendingSchedules();
    render();
    // Cloud content is ready at this point. Device projections and context
    // memberships are optional enrichments and must never hold the first
    // article paint hostage.
    scheduleLiveDeviceDataRefresh(false, silent);
    void loadContextMemberships(true).then((changed) => {
      if (changed && state.dashboard && !document.hidden) render();
    });
    if (selectedKey && state.selected === selectedKey)
      requestAnimationFrame(() => {
        if (state.selected === selectedKey) $("detail-panel").scrollTop = selectedScrollTop;
      });
  };

  persistReader = function persistParityReader() {
    return jsonRequest("./api/reader", "PATCH", {
      sidebar_width: state.reader.sidebar_width,
      timeline_width: state.reader.timeline_width,
      view_mode: state.reader.view_mode || "comfortable",
      mark_read_on_scroll: Boolean(state.reader.mark_read_on_scroll),
    }).catch((error) => showError(error.message || "阅读设置没有保存"));
  };

  applyReaderWidths = function applyParityReaderWidths() {
    baseApplyReaderWidths();
    enforcePanelCollapseWidths();
    document.body.classList.toggle(
      "compact-mode",
      state.reader.view_mode === "compact"
    );
    syncScrollReadButton();
  };

  async function toggleViewMode() {
    state.reader.view_mode =
      state.reader.view_mode === "compact" ? "comfortable" : "compact";
    applyReaderWidths();
    render();
    await persistReader();
  }

  // --- mark-as-read-on-scroll (opt-in) -------------------------------------
  // One observer for the whole timeline; observeTimelineForScrollRead()
  // disconnects and re-observes the current card set after every timeline
  // render, so replaced DOM nodes never pile up inside the observer.
  let markReadOnScrollObserver = null;

  function ensureMarkReadOnScrollObserver() {
    if (markReadOnScrollObserver) return markReadOnScrollObserver;
    if (typeof IntersectionObserver !== "function") return null;
    markReadOnScrollObserver = new IntersectionObserver(
      (records) => {
        if (!state.reader?.mark_read_on_scroll) return;
        for (const record of records) {
          if (!record.isIntersecting || record.intersectionRatio < 0.6) continue;
          const key = text(record.target?.dataset?.entryKey);
          if (!key || key.startsWith("recording:")) continue;
          const entry = makeEntries(state.dashboard || {}).find(
            (candidate) => candidate.key === key
          );
          if (entry && !pref(entry).read)
            void markReaderEntryReadLocally(entry);
        }
      },
      { threshold: [0.6] }
    );
    return markReadOnScrollObserver;
  }

  function observeTimelineForScrollRead() {
    const observer = state.reader?.mark_read_on_scroll
      ? ensureMarkReadOnScrollObserver()
      : null;
    if (!observer) return;
    observer.disconnect();
    $("timeline")
      ?.querySelectorAll("[data-entry-key]")
      .forEach((card) => {
        if (!String(card.dataset.entryKey || "").startsWith("recording:"))
          observer.observe(card);
      });
  }

  function syncScrollReadButton() {
    const button = $("toggle-scroll-read");
    if (!button) return;
    const active = Boolean(state.reader?.mark_read_on_scroll);
    button.classList.toggle("active", active);
    const label = active
      ? readerFocusCopy("滚动时标记已读：开", "Mark as read on scroll: on")
      : readerFocusCopy("滚动时标记已读：关", "Mark as read on scroll: off");
    button.setAttribute("aria-label", label);
    button.title = label;
  }

  async function toggleScrollRead() {
    state.reader = {
      ...(state.reader || {}),
      mark_read_on_scroll: !state.reader?.mark_read_on_scroll,
    };
    syncScrollReadButton();
    if (!state.reader.mark_read_on_scroll && markReadOnScrollObserver)
      markReadOnScrollObserver.disconnect();
    else observeTimelineForScrollRead();
    await persistReader();
  }

  // showToastAction keeps the base showToast text behavior and adds one
  // bounded action button (e.g. 撤销). The next plain showToast call replaces
  // the text and removes the button, so the action can never outlive its toast.
  function dismissToastAction(toast, button) {
    window.clearTimeout(showToast.timer);
    toast.hidden = true;
    delete toast.dataset.hasAction;
    button.hidden = true;
    button.onclick = null;
  }

  function showToastAction(message, actionLabel, action) {
    showToast(message);
    const toast = $("toast");
    if (!toast || typeof action !== "function") return;
    let button = toast.querySelector("[data-toast-action]");
    if (!button) {
      button = document.createElement("button");
      button.type = "button";
      button.className = "toast-action";
      button.dataset.toastAction = "";
      toast.appendChild(button);
    }
    button.textContent = actionLabel;
    button.hidden = false;
    toast.dataset.hasAction = "1";
    button.onclick = () => {
      dismissToastAction(toast, button);
      action();
    };
    window.clearTimeout(showToast.timer);
    showToast.timer = window.setTimeout(
      () => dismissToastAction(toast, button),
      6000
    );
  }

  // undoMarkVisibleRead restores exactly the entries that were unread before a
  // bulk mark-all; entries already read before the bulk stay read.
  async function undoMarkVisibleRead(newsKeys, recordingIDs, liveIDs) {
    try {
      for (let index = 0; index < newsKeys.length; index += 200) {
        await jsonRequest("./api/items/bulk", "PATCH", {
          keys: newsKeys.slice(index, index + 200),
          read: false,
        });
      }
      await Promise.all(
        recordingIDs.map((id) =>
          jsonRequest("./api/recordings/preferences", "PATCH", {
            recording_id: id,
            read: false,
          })
        )
      );
      // Live device meetings keep their read state in local storage only; the
      // recordings endpoint does not know them (markVisibleRead treats them
      // the same way).
      liveIDs.forEach((id) => storeLiveMeetingPreference(id, { read: false }));
      await refresh();
      showToast(`已恢复 ${newsKeys.length + recordingIDs.length + liveIDs.length} 条未读`);
    } catch (error) {
      showError(error.message || "撤销没有保存");
      await refresh();
    }
  }

  async function markVisibleRead() {
    const entries = filteredEntries();
    const previouslyUnreadNews = entries.filter(
      (entry) => entry.kind !== "recording" && !pref(entry).read
    );
    const previouslyUnreadRecordings = entries.filter(
      (entry) =>
        entry.kind === "recording" &&
        !entry.item?.live_device &&
        !pref(entry).read
    );
    const previouslyUnreadLive = entries.filter(
      (entry) =>
        entry.kind === "recording" &&
        entry.item?.live_device &&
        !pref(entry).read
    );
    const keys = entries
      .filter((entry) => entry.kind !== "recording")
      .map((entry) => entry.key);
    const recordings = entries.filter(
      (entry) => entry.kind === "recording" && !entry.item?.live_device
    );
    entries
      .filter(
        (entry) => entry.kind === "recording" && entry.item?.live_device
      )
      .forEach((entry) =>
        storeLiveMeetingPreference(entry.id, { read: true })
      );
    try {
      // /api/items/bulk 每次最多 200 个 key；撤销路径一直分批，标记路径也
      // 必须分批，长列表全部已读才不会在 200 条处整体失败。
      for (let index = 0; index < keys.length; index += 200) {
        await jsonRequest("./api/items/bulk", "PATCH", {
          keys: keys.slice(index, index + 200),
          read: true,
        });
      }
      await Promise.all(
        recordings.map((entry) =>
          jsonRequest("./api/recordings/preferences", "PATCH", {
            recording_id: entry.id,
            read: true,
          })
        )
      );
      await refresh();
      const undoCount =
        previouslyUnreadNews.length +
        previouslyUnreadRecordings.length +
        previouslyUnreadLive.length;
      if (undoCount)
        showToastAction(
          `已标记 ${entries.length} 条内容`,
          "撤销",
          () =>
            void undoMarkVisibleRead(
              previouslyUnreadNews.map((entry) => entry.key),
              previouslyUnreadRecordings.map((entry) => entry.id),
              previouslyUnreadLive.map((entry) => entry.id)
            )
        );
      else showToast(`已标记 ${entries.length} 条内容`);
    } catch (error) {
      showError(error.message || "批量状态没有保存");
    }
  }

  function toggleLater(key) {
    const current = pref({ key });
    return itemMutation(key, "read_later", !current.read_later);
  }

  function onLaterClick(event) {
    const button = event.target.closest("[data-later-entry]");
    if (!button) return;
    event.preventDefault();
    event.stopPropagation();
    void toggleLater(button.dataset.laterEntry);
  }

  function onContextClick(event) {
    const button = event.target.closest("[data-context-entry]");
    if (!button) return;
    event.preventDefault();
    event.stopPropagation();
    void toggleStableContext(button.dataset.contextEntry);
  }

  function onFullTextClick(event) {
    const button = event.target.closest("[data-load-fulltext]");
    if (!button) return;
    event.preventDefault();
    event.stopPropagation();
    const entry =
      state.selected &&
      makeEntries(state.dashboard || {}).find(
        (item) => item.key === state.selected
      );
    if (entry) void loadFullText(entry);
  }

  function onArticleAIOverviewClick(event) {
    const button = event.target.closest(
      "[data-generate-article-overview], [data-retry-article-ai-overview]"
    );
    if (!button) return;
    event.preventDefault();
    event.stopPropagation();
    const entry = selectedNewsEntry();
    const targetID =
      button.dataset.generateArticleOverview ||
      button.dataset.retryArticleAiOverview ||
      "";
    if (!entry || entry.item?.id !== targetID)
      return;
    void requestArticleAIOverview(entry, true);
  }

  function onFocusRationaleClick(event) {
    const trigger = event.target?.closest?.("[data-focus-rationale]");
    if (!trigger) return false;
    event.preventDefault();
    event.stopPropagation();
    const card = trigger.closest("[data-focus-result-card]");
    const item = focusResultFromCard(card);
    if (item) openRationaleDrawer(item);
    return true;
  }

  function onDecisionHistoryClick(event) {
    const trigger = event.target?.closest?.("[data-decision-history]");
    if (!trigger) return false;
    event.preventDefault();
    event.stopPropagation();
    const card = trigger.closest("[data-focus-result-card]");
    const item = focusResultFromCard(card);
    if (item) openDecisionHistoryDrawer(item);
    return true;
  }

  function citationFromButton(button) {
    if (!button) return null;
    return normalizeFocusCitation({
      source_id: button.dataset.citationSourceId,
      source_kind: button.dataset.citationSourceKind,
      entry_id: button.dataset.citationEntryId,
      asset_id: button.dataset.citationAssetId,
      mime: button.dataset.citationMime,
      block_id: button.dataset.citationBlockId,
      content_hash: button.dataset.citationContentHash,
      text_hash: button.dataset.citationTextHash,
      quote: button.dataset.citationQuote,
      title: button.dataset.citationTitle,
      locator: button.dataset.citationLocator,
      source_locator: button.dataset.citationSourceLocator,
      role: button.dataset.citationRole,
    });
  }

  function onFocusCitationClick(event) {
    const trigger = event.target?.closest?.("[data-focus-citation]");
    if (!trigger) return false;
    event.preventDefault();
    event.stopPropagation();
    const citation = citationFromButton(trigger);
    if (citation) void openCitationDrawer(citation);
    return true;
  }

  function onFocusDrawerClick(event) {
    if (onDecisionHistoryClick(event)) return;
    if (onFocusRationaleClick(event)) return;
    onFocusCitationClick(event);
  }

  function saveReadingProgress() {
    const panel = $("detail-panel");
    const entry =
      state.selected &&
      makeEntries(state.dashboard || {}).find(
        (item) => item.key === state.selected
      );
    if (!entry || !["news", "evidence"].includes(entry.kind)) return;
    const maxScroll = panel.scrollHeight - panel.clientHeight;
    if (maxScroll <= 0) return;
    const progress = Math.min(1, Math.max(0, panel.scrollTop / maxScroll));
    const current = pref(entry);
    state.prefs.set(entry.key, { ...current, reading_progress: progress });
    const bar = panel.querySelector(".reading-progress i");
    if (bar) bar.style.width = `${Math.round(progress * 100)}%`;
    window.clearTimeout(parity.progressTimer);
    parity.progressTimer = window.setTimeout(() => {
      void jsonRequest("./api/items", "PATCH", {
        key: entry.key,
        reading_progress: progress,
      }).catch(() => {});
    }, 700);
  }

  function keyboardHandler(event) {
    if (event.key === "Escape" && !$("discover-modal").hidden) {
      event.preventDefault();
      closeDiscover();
      restoreModalFocus();
      return;
    }
    if (
      event.key === "Escape" &&
      document.body.classList.contains("mobile-nav-open")
    ) {
      event.preventDefault();
      closeMobileNavigation();
      return;
    }
    if (event.key === "Escape" && isShortcutsOverlayOpen()) {
      event.preventDefault();
      closeShortcutsOverlay();
      return;
    }
    if (event.key === "Escape" && isPictureLightboxOpen()) {
      event.preventDefault();
      closePictureLightbox();
      return;
    }
    if (isPictureLightboxOpen()) {
      // While the lightbox owns the screen, arrows page images and every
      // other reader shortcut stays inert.
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        stepPictureLightbox(-1);
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        stepPictureLightbox(1);
      }
      return;
    }
    if (
      event.defaultPrevented ||
      event.ctrlKey ||
      event.metaKey ||
      event.altKey
    )
      return;
    if (
      !$("discover-modal").hidden ||
      isShortcutsOverlayOpen() ||
      isPictureLightboxOpen() ||
      /INPUT|TEXTAREA|SELECT/.test(document.activeElement?.tagName || "")
    )
      return;
    if (event.key === "/") {
      event.preventDefault();
      $("search").focus();
      return;
    }
    if (event.key === "?") {
      event.preventDefault();
      toggleShortcutsOverlay();
      return;
    }
    if (event.key === "A") {
      event.preventDefault();
      void markVisibleRead();
      return;
    }
    const entries = filteredEntries();
    const currentIndex = entries.findIndex(
      (entry) => entry.key === state.selected
    );
    if (event.key === "j" || event.key === "k") {
      if (!entries.length) return;
      event.preventDefault();
      const delta = event.key === "j" ? 1 : -1;
      const next =
        entries[
          Math.min(
            entries.length - 1,
            Math.max(0, currentIndex < 0 ? 0 : currentIndex + delta)
          )
        ];
      if (next?.kind === "news" && videoForItem(next.item)) {
        state.selected = null;
        document.body.classList.remove("detail-open");
        render();
        openFloatingVideoPlayer(next);
        if (!pref(next).read) void itemMutation(next.key, "read", true);
      } else {
        selectEntry(next.key);
      }
      document
        .querySelector(`[data-entry-key="${CSS.escape(next.key)}"]`)
        ?.scrollIntoView({ block: "nearest" });
      return;
    }
    const entry = entries[currentIndex];
    if (!entry) {
      if (event.key === "r") {
        event.preventDefault();
        if (typeof window.requestDashboardRefresh === "function")
          void window.requestDashboardRefresh();
        else void Promise.all([refresh(), loadParityData(true)]);
      }
      return;
    }
    if (event.key === "m") {
      event.preventDefault();
      if (entry.kind === "recording")
        void recordingMutation(entry.id, { read: !pref(entry).read });
      else void itemMutation(entry.key, "read", !pref(entry).read);
    } else if (event.key === "s") {
      event.preventDefault();
      void itemMutation(entry.key, "starred", !pref(entry).starred);
    } else if (event.key === "l" && entry.kind !== "recording") {
      event.preventDefault();
      void toggleLater(entry.key);
    } else if (
      event.key === "o" &&
      entry.kind === "news" &&
      publicURL(entry.item?.url || "")
    ) {
      event.preventDefault();
      window.open(entry.item.url, "_blank", "noopener,noreferrer");
    } else if (event.key === "r") {
      event.preventDefault();
      if (typeof window.requestDashboardRefresh === "function")
        void window.requestDashboardRefresh();
      else void Promise.all([refresh(), loadParityData(true)]);
    }
  }

  // --- keyboard shortcuts overlay -------------------------------------------
  function ensureShortcutsOverlay() {
    let overlay = $("shortcuts-overlay");
    if (overlay) return overlay;
    overlay = document.createElement("div");
    overlay.id = "shortcuts-overlay";
    overlay.className = "shortcuts-overlay";
    overlay.hidden = true;
    const row = (keys, label) =>
      `<div class="shortcut-row"><span class="shortcut-keys">${keys
        .map((key) => `<kbd>${esc(key)}</kbd>`)
        .join("")}</span><span class="shortcut-label">${esc(label)}</span></div>`;
    const copy = (zh, en) => readerFocusCopy(zh, en);
    overlay.innerHTML = `<div class="shortcuts-panel" role="dialog" aria-modal="true" aria-label="${esc(
      copy("键盘快捷键", "Keyboard shortcuts")
    )}"><header><strong>${esc(
      copy("键盘快捷键", "Keyboard shortcuts")
    )}</strong><button class="icon-btn" type="button" data-close-shortcuts aria-label="${esc(
      copy("关闭", "Close")
    )}">${icon("close")}</button></header><div class="shortcut-rows">${row(
      ["j", "k"],
      copy("下一条 / 上一条", "Next / previous entry")
    )}${row(["Enter", "o"], copy("打开条目 / 原文", "Open entry / original"))}${row(
      ["s"],
      copy("收藏 / 取消收藏", "Toggle favorite")
    )}${row(["m"], copy("已读 / 未读", "Toggle read"))}${row(
      ["l"],
      copy("稍后读", "Read later")
    )}${row(
      ["Shift", "A"],
      copy("当前列表全部已读（可撤销）", "Mark list read (undoable)")
    )}${row(["r"], copy("刷新内容", "Refresh"))}${row(
      ["/"],
      copy("聚焦搜索", "Focus search")
    )}${row(["?"], copy("本面板", "This panel"))}${row(
      ["Esc"],
      copy("关闭浮层 / 详情", "Close overlay / detail")
    )}</div></div>`;
    overlay.addEventListener("click", (event) => {
      if (
        event.target === overlay ||
        event.target.closest("[data-close-shortcuts]")
      )
        closeShortcutsOverlay();
    });
    document.body.appendChild(overlay);
    return overlay;
  }

  function isShortcutsOverlayOpen() {
    const overlay = $("shortcuts-overlay");
    return Boolean(overlay && !overlay.hidden);
  }

  function closeShortcutsOverlay() {
    const overlay = $("shortcuts-overlay");
    if (overlay) overlay.hidden = true;
  }

  function toggleShortcutsOverlay() {
    const overlay = ensureShortcutsOverlay();
    overlay.hidden = !overlay.hidden;
  }

  // --- picture lightbox -------------------------------------------------------
  // A minimal full-image viewer for the picture wall. It walks the same
  // filtered entry projection the wall renders, so j/k selection, unread state
  // and the media proxy contract stay on one list.
  let pictureLightboxState = null; // { entries: Entry[], index: number }

  function ensurePictureLightbox() {
    let overlay = $("picture-lightbox");
    if (overlay) return overlay;
    overlay = document.createElement("div");
    overlay.id = "picture-lightbox";
    overlay.className = "picture-lightbox";
    overlay.hidden = true;
    overlay.innerHTML = `<div class="picture-lightbox-stage" data-picture-lightbox-stage></div><div class="picture-lightbox-bar"><button class="icon-btn" type="button" data-picture-prev aria-label="${esc(
      readerFocusCopy("上一张", "Previous")
    )}">${icon("chevron")}</button><span class="picture-lightbox-caption"><strong data-picture-title></strong><small data-picture-counter></small></span><a class="icon-btn" data-picture-open-source target="_blank" rel="noopener noreferrer" aria-label="${esc(
      readerFocusCopy("打开原文", "Open original")
    )}">${icon("external")}</a><button class="icon-btn" type="button" data-close-picture-lightbox aria-label="${esc(
      readerFocusCopy("关闭", "Close")
    )}">${icon("close")}</button><button class="icon-btn" type="button" data-picture-next aria-label="${esc(
      readerFocusCopy("下一张", "Next")
    )}">${icon("chevron")}</button></div>`;
    overlay.addEventListener("click", (event) => {
      if (event.target === overlay) {
        closePictureLightbox();
        return;
      }
      if (event.target.closest("[data-close-picture-lightbox]"))
        closePictureLightbox();
      else if (event.target.closest("[data-picture-prev]"))
        stepPictureLightbox(-1);
      else if (event.target.closest("[data-picture-next]"))
        stepPictureLightbox(1);
    });
    document.body.appendChild(overlay);
    return overlay;
  }

  function isPictureLightboxOpen() {
    const overlay = $("picture-lightbox");
    return Boolean(overlay && !overlay.hidden);
  }

  function openPictureLightbox(key) {
    const entries = filteredEntries().filter((entry) => entry.kind === "news");
    const index = entries.findIndex((entry) => entry.key === key);
    if (index < 0) return;
    pictureLightboxState = { entries, index };
    const overlay = ensurePictureLightbox();
    syncPictureLightbox();
    overlay.hidden = false;
  }

  function closePictureLightbox() {
    pictureLightboxState = null;
    const overlay = $("picture-lightbox");
    if (overlay) overlay.hidden = true;
  }

  // Clamp at both ends instead of wrapping: the disabled arrow is the clearest
  // signal that the wall has no more images in that direction.
  function stepPictureLightbox(delta) {
    if (!pictureLightboxState) return;
    const next = pictureLightboxState.index + (delta > 0 ? 1 : -1);
    if (next < 0 || next >= pictureLightboxState.entries.length) return;
    pictureLightboxState.index = next;
    syncPictureLightbox();
  }

  function syncPictureLightbox() {
    const overlay = $("picture-lightbox");
    const current = pictureLightboxState;
    if (!overlay || !current) return;
    const entry = current.entries[current.index];
    const item = entry?.item || {};
    const image = pictureImageForItem(item) || pictureEntryFallbackImage(item);
    const stage = overlay.querySelector("[data-picture-lightbox-stage]");
    const title = overlay.querySelector("[data-picture-title]");
    const counter = overlay.querySelector("[data-picture-counter]");
    const source = overlay.querySelector("[data-picture-open-source]");
    const prev = overlay.querySelector("[data-picture-prev]");
    const next = overlay.querySelector("[data-picture-next]");
    if (stage)
      stage.innerHTML = image
        ? `<img src="${esc(image)}" alt="${esc(entry.title)}">`
        : "";
    if (title) title.textContent = entry?.title || "";
    if (counter)
      counter.textContent = `${current.index + 1} / ${current.entries.length}`;
    if (source) {
      if (item.url) {
        source.hidden = false;
        source.setAttribute("href", item.url);
      } else {
        source.hidden = true;
      }
    }
    if (prev) prev.disabled = current.index <= 0;
    if (next) next.disabled = current.index >= current.entries.length - 1;
  }

  // --- per-source timeline display mode --------------------------------------
  // Overrides only the timeline presentation of one source. "gallery" is
  // accepted and rendered as the video grid until the picture view lands.
  function sourceIDForCurrentView() {
    const view = text(state.view);
    return view.startsWith("source:") ? view.slice("source:".length) : "";
  }

  function viewModeForSourceID(sourceID) {
    if (!sourceID) return "auto";
    return parity.sourceDisplayPrefs?.get(sourceID) || "auto";
  }

  async function loadSourceDisplayPrefs() {
    try {
      const prefs = await jsonRequest("./api/source-display", "GET");
      parity.sourceDisplayPrefs = new Map(Object.entries(prefs || {}));
    } catch {
      parity.sourceDisplayPrefs = new Map();
    }
  }

  async function setSourceDisplayMode(sourceID, mode) {
    if (!sourceID || !validSourceDisplayMode(mode)) return;
    parity.sourceDisplayPrefs.set(sourceID, mode);
    render();
    try {
      await jsonRequest("./api/source-display", "PUT", {
        source_id: sourceID,
        view_mode: mode,
      });
    } catch (error) {
      showError(error.message || "视图模式没有保存");
      await loadSourceDisplayPrefs();
      render();
    }
  }

  function validSourceDisplayMode(mode) {
    return ["auto", "article", "video", "gallery"].includes(mode);
  }

  function syncSourceDisplaySelector() {
    const sourceID = sourceIDForCurrentView();
    const row = document.querySelector(".timeline-title-row");
    if (!row) return;
    let select = $("source-display-mode");
    if (!sourceID || !row.isConnected) {
      if (select) select.remove();
      return;
    }
    if (!select) {
      select = document.createElement("select");
      select.id = "source-display-mode";
      select.className = "source-display-select";
      select.setAttribute("aria-label", readerFocusCopy("时间线视图", "Timeline view"));
      select.innerHTML = ["auto", "article", "video"].map(
        (mode) =>
          `<option value="${mode}">${esc(
            sourceDisplayModeLabel(mode)
          )}</option>`
      );
      select.addEventListener("change", () => {
        void setSourceDisplayMode(sourceIDForCurrentView(), select.value);
      });
      row.appendChild(select);
    }
    const current = viewModeForSourceID(sourceID);
    select.value = validSourceDisplayMode(current) && current !== "gallery" ? current : "auto";
  }

  function sourceDisplayModeLabel(mode) {
    switch (mode) {
      case "article":
        return readerFocusCopy("文章列表", "Article list");
      case "video":
        return readerFocusCopy("视频网格", "Video grid");
      case "gallery":
        return readerFocusCopy("媒体墙", "Media wall");
      default:
        return readerFocusCopy("自动", "Auto");
    }
  }

  installNavigation();
  installFloatingVideoSelection();
  installPanelCollapseControls();
  rebuildModal();
  void loadSourceDisplayPrefs();
  for (const trigger of [$("discover"), $("sidebar-add")]) {
    trigger?.addEventListener(
      "click",
      (event) => {
        parity.modalReturnFocus = event.currentTarget;
      },
      { capture: true }
    );
  }
  $("close-discover")?.addEventListener("click", restoreModalFocus);
  $("discover-modal")?.addEventListener("click", (event) => {
    if (event.target === $("discover-modal")) restoreModalFocus();
  });

  // ---- 订阅源对齐：来源级操作的 document 级委托 ------------------------------
  // 来源页头、订阅管理行和已退订区都在 #timeline / #parity-content 里，不在
  // onModalClick 的 modal 作用域内。这里统一委托；事件仍会冒泡到 modal 的
  // 处理器，所以先排除 modal 内的目标，避免 data-sync-source / data-toggle-
  // source 双触发。
  function onSourceAlignmentClick(event) {
    // 导出 OPML 在发现弹窗和订阅管理工具栏都有入口，先于 modal 排除处理。
    if (event.target.closest("[data-export-opml]")) {
      const anchor = document.createElement("a");
      anchor.href = "./api/sources/opml";
      anchor.download = "subscriptions.opml";
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      return;
    }
    if (event.target.closest?.("#discover-modal")) return;
    const openSite = event.target.closest("[data-open-site-source]");
    if (openSite) {
      const source = sourceByID(openSite.dataset.openSiteSource);
      const site = publicURL(text(source?.site_url))
        ? text(source.site_url)
        : publicURL(text(source?.url))
        ? text(source.url)
        : "";
      if (site) window.open(site, "_blank", "noopener,noreferrer");
      else showError(readerFocusCopy("该来源没有可打开的网站地址", "This source has no site URL"));
      return;
    }
    const copyFeed = event.target.closest("[data-copy-source-url]");
    if (copyFeed) {
      const source = sourceByID(copyFeed.dataset.copySourceUrl);
      const feedURL = text(source?.url);
      if (feedURL && navigator.clipboard?.writeText)
        void navigator.clipboard
          .writeText(feedURL)
          .then(() => showToast(readerFocusCopy("已复制订阅链接", "Feed URL copied")))
          .catch(() => showError(readerFocusCopy("复制失败，请手动复制", "Copy failed")));
      return;
    }
    const sync = event.target.closest("[data-sync-source]");
    if (sync) return void syncSource(sync.dataset.syncSource);
    const toggle = event.target.closest("[data-toggle-source]");
    if (toggle) return void toggleSource(toggle.dataset.toggleSource);
    const renameEntry = event.target.closest("[data-rename-source]");
    if (renameEntry) return beginRenameSource(renameEntry.dataset.renameSource);
    if (event.target.closest("[data-rename-cancel]"))
      return cancelRenameSource();
    const renameSave = event.target.closest("[data-rename-save]");
    if (renameSave) return void saveRenameSource(renameSave.dataset.renameSave);
    const readSource = event.target.closest("[data-read-source]");
    if (readSource) return void markSourceRead(readSource.dataset.readSource);
    if (event.target.closest("[data-rename-folder]"))
      return void renameFolder();
    if (event.target.closest("[data-delete-folder]"))
      return void deleteFolder();
  }
  document.addEventListener("click", onSourceAlignmentClick);

  // 订阅管理排序与重命名输入：document 级委托。select 一次 change 生效；重命名
  // 输入 Enter 保存、Esc 取消，不触发其它快捷键。
  document.addEventListener("change", (event) => {
    const sortMode = event.target.closest?.("#manager-sort-mode");
    if (!sortMode) return;
    const mode = text(sortMode.value) || "manual";
    if (mode === parity.managerSortMode) return;
    parity.managerSortMode = ["manual", "name", "unread", "sync"].includes(mode)
      ? mode
      : "manual";
    if (parity.tab === "subscriptions") renderCatalog();
  });
  document.addEventListener("keydown", (event) => {
    const input = event.target.closest?.("#rename-source-input");
    if (!input) return;
    if (event.key === "Enter") {
      event.preventDefault();
      const save = document.querySelector("[data-rename-save]");
      if (save) return void saveRenameSource(save.dataset.renameSave);
    } else if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      cancelRenameSource();
    }
  });

  // 订阅管理搜索：document 级 input 委托（订阅管理每次重绘都会重建工具栏），
  // 重绘后恢复焦点与光标位置。
  document.addEventListener("input", (event) => {
    const search = event.target.closest?.("#manager-source-search");
    if (!search) return;
    parity.managerSourceQuery = text(search.value);
    const wasActive = document.activeElement === search;
    const cursor = search.selectionStart;
    if (parity.tab === "subscriptions") renderSubscriptions();
    const restored = $("manager-source-search");
    if (restored && wasActive) {
      restored.focus();
      try {
        const position = Math.min(
          Number(cursor) || restored.value.length,
          restored.value.length
        );
        restored.setSelectionRange(position, position);
      } catch (_) {}
    }
  });

  // 拖拽订阅入分组：侧栏来源项可拖动，分组标题作为放置目标（默认分组 = 移出
  // 分组）。全部走既有 PATCH /api/subscriptions，与下拉选择同一契约。
  function installSourceDragToFolder() {
    document.addEventListener("dragstart", (event) => {
      const item = event.target.closest?.("[data-drag-source-id]");
      if (!item) return;
      event.dataTransfer?.setData("text/plain", text(item.dataset.dragSourceId));
      if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
    });
    document.addEventListener("dragover", (event) => {
      const zone = event.target.closest?.("[data-folder-drop]");
      if (!zone) return;
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
      zone.classList.add("drag-over");
    });
    document.addEventListener("dragleave", (event) => {
      const zone = event.target.closest?.("[data-folder-drop]");
      if (zone) zone.classList.remove("drag-over");
    });
    document.addEventListener("drop", (event) => {
      const zone = event.target.closest?.("[data-folder-drop]");
      if (!zone) return;
      event.preventDefault();
      zone.classList.remove("drag-over");
      const sourceID = event.dataTransfer?.getData("text/plain") || "";
      if (!sourceID) return;
      void patchSubscription(sourceID, {
        folder_id: text(zone.dataset.folderDrop),
      });
    });
  }
  installSourceDragToFolder();

  // OPML 导入：读取本地文件后原文 POST，服务端解析并复用订阅路径。2 MB 与
  // 服务端 sourceOPMLMaxBytes 对齐。发现 tab 每次切换都会重建模板，所以必须
  // document 级委托而不是直绑节点。
  function installOPMLImport() {
    document.addEventListener("click", (event) => {
      if (!event.target.closest?.("#opml-import-button")) return;
      $("opml-file-input")?.click();
    });
    document.addEventListener("change", (event) => {
      const input = event.target.closest?.("#opml-file-input");
      if (!input) return;
      const file = input.files?.[0];
      input.value = "";
      if (!file) return;
      if (file.size > 2 * 1024 * 1024) {
        showError(readerFocusCopy("OPML 文件超过 2 MB", "OPML file exceeds 2 MB"));
        return;
      }
      void (async () => {
        try {
          const body = await file.text();
          const result = await request("./api/sources/opml", {
            method: "POST",
            body,
            timeoutMs: 30000,
          });
          showToast(
            readerFocusCopy(
              `导入完成：新增 ${result?.imported ?? 0} 个订阅，跳过 ${
                result?.skipped ?? 0
              } 个`,
              `Imported ${result?.imported ?? 0} feeds, skipped ${result?.skipped ?? 0}`
            )
          );
          await loadParityData();
        } catch (error) {
          showError(error?.message || readerFocusCopy("OPML 导入失败", "OPML import failed"));
        }
      })();
    });
  }
  installOPMLImport();
  $("timeline").addEventListener("click", onLaterClick);
  $("timeline").addEventListener("click", (event) => {
    const retryAggregate = event.target.closest("[data-retry-aggregate]");
    if (retryAggregate) {
      event.preventDefault();
      const view = text(retryAggregate.dataset.retryAggregate);
      if (isAggregateEntryView(view) && state.view === view) {
        parity.entryViewErrors.delete(view);
        parity.entryPageError = "";
        document.querySelector("#timeline")?.removeAttribute("data-render-fingerprint");
        void loadAggregateEntriesOnDemand(view);
      }
      return;
    }
    const retryEntries = event.target.closest("[data-retry-entries]");
    if (retryEntries) {
      event.preventDefault();
      parity.entryPageError = "";
      parity.entryHasMore = Boolean(
        parity.entryCursor && parity.entryPagesRead < readerEntryPageLimit
      );
      document.querySelector("#timeline")?.removeAttribute("data-render-fingerprint");
      void loadMoreEntries();
      return;
    }
    const retrySourceEntry = event.target.closest("[data-retry-source-entry]");
    if (retrySourceEntry) {
      event.preventDefault();
      const sourceID = text(retrySourceEntry.dataset.retrySourceEntry);
      if (sourceID && state.view === `source:${sourceID}`) {
        parity.entryScopeError = "";
        document.querySelector("#timeline")?.removeAttribute("data-render-fingerprint");
        void switchEntryScope(sourceID, { force: true });
      }
      return;
    }
    const retry = event.target.closest("[data-retry-source]");
    if (retry) {
      event.preventDefault();
      void syncSource(retry.dataset.retrySource);
      return;
    }
    const button = event.target.closest("[data-load-more-entries]");
    if (button) {
      event.preventDefault();
      void loadMoreEntries();
      return;
    }
    const sourceHistory = event.target.closest("[data-load-more-source]");
    if (sourceHistory) {
      event.preventDefault();
      void loadMoreSourceHistory(sourceHistory.dataset.loadMoreSource);
    }
  });
  $("detail-panel").addEventListener("click", onLaterClick);
  $("detail-panel").addEventListener("click", onContextClick);
  $("detail-panel").addEventListener("click", onFullTextClick);
  $("detail-panel").addEventListener("click", onArticleAIOverviewClick);
  $("detail-panel").addEventListener("click", onFocusDrawerClick);
  $("detail-panel").addEventListener("click", onYouTubeDetailTabClick);
  $("detail-panel").addEventListener("scroll", saveReadingProgress, {
    passive: true,
  });
  document.addEventListener(
    "error",
    (event) => {
      if (
        event.target instanceof HTMLImageElement &&
        event.target.matches(".source-avatar-image")
      ) {
        event.target.classList.remove("is-loaded");
        retrySourceIcon(event.target);
        return;
      }
      if (
        event.target instanceof HTMLImageElement &&
        event.target.matches(".discovery-avatar img")
      ) {
        const fallback = text(event.target.dataset.fallbackSrc);
        if (fallback && event.target.src !== fallback) {
          event.target.src = fallback;
        } else {
          event.target.hidden = true;
        }
      }
      if (
        event.target instanceof HTMLImageElement &&
        event.target.matches(
          ".youtube-entry-media img, .video-entry-media img, .youtube-detail-poster"
        )
      ) {
        event.target.classList.remove("is-loaded");
        const next = text(event.target.dataset.fallbackSrc);
        const second = text(event.target.dataset.secondFallbackSrc);
        const current = event.target.currentSrc || event.target.src;
        const nextURL = next ? new URL(next, location.href).href : "";
        const secondURL = second ? new URL(second, location.href).href : "";
        if (
          nextURL &&
          nextURL !== current &&
          !event.target.dataset.fallbackAttempted
        ) {
          event.target.dataset.fallbackAttempted = "1";
          event.target.src = nextURL;
        } else if (
          secondURL &&
          secondURL !== current &&
          secondURL !== nextURL &&
          !event.target.dataset.secondFallbackAttempted
        ) {
          event.target.dataset.secondFallbackAttempted = "1";
          event.target.src = secondURL;
        } else {
          event.target.hidden = true;
          const placeholder = event.target.parentElement?.querySelector(
            ".youtube-thumbnail-placeholder"
          );
          if (placeholder) {
            placeholder.hidden = false;
            // 两级回退都失败是终态，别让占位一直自称"加载中"。
            const label = placeholder.querySelector(
              ".youtube-thumbnail-placeholder-label"
            );
            if (label) label.textContent = readerFocusCopy("封面加载失败", "Cover unavailable");
          }
        }
      }
      if (
        event.target instanceof HTMLImageElement &&
        event.target.matches(".fulltext-rich img[data-article-image]")
      ) {
        replaceFailedArticleImage(event.target);
      }
      if (
        event.target instanceof HTMLImageElement &&
        event.target.matches(".x-entry-media img")
      ) {
        event.target.closest(".x-entry-media")?.remove();
      }
      if (
        event.target instanceof HTMLImageElement &&
        event.target.matches(".entry-media-wrap img")
      ) {
        event.target.closest(".entry-media-wrap")?.remove();
      }
      if (
        event.target instanceof HTMLImageElement &&
        event.target.matches(".detail-cover")
      ) {
        replaceFailedDetailCover(event.target);
      }
      if (
        event.target instanceof HTMLVideoElement &&
        event.target.matches(".x-entry-video, .x-detail-video")
      ) {
        showXVideoFallback(event.target);
      }
    },
    true
  );
  document.addEventListener(
    "load",
    (event) => {
      if (
        event.target instanceof HTMLImageElement &&
        (event.target.matches(".source-avatar-image") ||
          event.target.matches(".discovery-avatar img") ||
          event.target.matches(
            ".youtube-entry-media img, .video-entry-media img, .youtube-detail-poster"
          ))
      ) {
        if (event.target.matches(".source-avatar-image")) {
          // Runtime icon state (src/is-loaded) lives on the DOM node only —
          // see sourceAvatar: baked state would change later markup builds
          // and force full-nav re-renders.
          event.target.classList.add("is-loaded");
          finishSourceIconRequest(event.target);
        } else event.target.classList.add("is-loaded");
        if (event.target.matches(".source-avatar-image"))
          finishSourceIconRequest(event.target);
        if (
          event.target.matches(
            ".youtube-entry-media img, .video-entry-media img, .youtube-detail-poster"
          )
        ) {
          const placeholder = event.target.parentElement?.querySelector(
            ".youtube-thumbnail-placeholder"
          );
          if (placeholder) placeholder.hidden = true;
        }
      }
    },
    true
  );
  installScheduleBridge();
  $("discover").addEventListener("click", () => void loadParityData());
  $("sidebar-add").addEventListener("click", () => void loadParityData());
  document.addEventListener("keydown", keyboardHandler);
  // Evidence/source panels are a stack. Escape only dismisses the top layer,
  // leaving the underlying rationale or article detail available.
  document.addEventListener(
    "keydown",
    (event) => {
      if (event.key !== "Escape") return;
      const drawers = citationDrawerElements();
      if (!drawers.length) return;
      const topDrawer = drawers[drawers.length - 1];
      const decisionWorkspace = topDrawer?.__decisionWorkspace;
      if (decisionWorkspace?.filtersOpen) {
        event.preventDefault();
        event.stopPropagation();
        decisionWorkspace.filtersOpen = false;
        renderDecisionHistoryDrawer(topDrawer, decisionWorkspace);
        return;
      }
      if (decisionWorkspace?.fullscreen) {
        event.preventDefault();
        event.stopPropagation();
        decisionWorkspace.fullscreen = false;
        renderDecisionHistoryDrawer(topDrawer, decisionWorkspace);
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      closeTopCitationDrawer();
    },
    true
  );
  document.addEventListener("click", onFocusDrawerClick);
  // The inline bootstrap refresh starts before this extension script is
  // loaded. Wait briefly for that bounded dashboard response, then enable the
  // first "load more" cursor without issuing a second competing refresh.
  let paginationBootstrapAttempts = 0;
  const bootstrapPagination = () => {
    if (state.dashboard) {
      resetEntryPagination();
      render();
      // Meeting and Obsidian projections are loaded only when their views are
      // entered. A cloud-feed or Focus startup must not recursively scan the
      // device filesystem and compete with article/detail requests.
      scheduleLiveDeviceDataRefresh(false, true);
      return;
    }
    if (paginationBootstrapAttempts++ < 100)
      window.setTimeout(bootstrapPagination, 50);
  };
  bootstrapPagination();
  // Global/unread/video badges are independent metadata; do not wait for the
  // article page or source catalog to become available before refreshing them.
  void loadEntryCounts(true);
  void loadDeviceCounts(2);
  // The inline bootstrap already fetched the dashboard and sources. Load only
  // optional settings here; the heavier catalog endpoints wait until the
  // source modal is opened.
  void loadParityData(true, true);
  window.__plaudParityReady = true;
})();
