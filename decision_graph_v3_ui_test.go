package main

import (
	"strings"
	"testing"
)

func TestFocusV3SecondaryDrawersPreservePrototypeStack(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	script, styles := string(scriptBytes), string(stylesBytes)

	for _, expected := range []string{
		`actionDrawer.id = "focus-v3-action-drawer-layer"`,
		`const layer = ensureFocusV3Surface() && $("focus-v3-action-drawer-layer")`,
		`openFocusV3Drawer(focusID, "overview")`,
		`focusLayer?.classList.toggle("is-above-action", actionOpen)`,
		`drawer.__focusCitationMask = mask`,
		`if (citationDrawerElements().length) return;`,
		`if (!$("focus-v3-action-drawer-layer")?.hidden) closeFocusV3ActionDrawer();`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("V3 drawer stack is missing %q", expected)
		}
	}
	if strings.Contains(script, `closeFocusV3ActionDrawer();\n      openFocusV3Drawer(focusID, "overview");`) {
		t.Error("opening a focus from an action must preserve the action drawer underneath")
	}

	for _, expected := range []string{
		`.focus-v3-drawer-layer.is-above-action{z-index:75}`,
		`.focus-citation-mask{position:fixed;inset:0;`,
		`.focus-citation-drawer{position:fixed;top:0;right:0;bottom:0;z-index:81;width:55vw;`,
		`.focus-v3-action-status-row>span{padding:2px 7px;border:1px solid var(--line);border-radius:999px;`,
		`body.focus-v3-drawer-open{overflow:hidden}`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("V3 drawer layer CSS is missing %q", expected)
		}
	}
}

func TestDecisionGraphUsesPrototypeThemeTokens(t *testing.T) {
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	for _, expected := range []string{
		"--canvas:#f7f8f9",
		"--surface:#fff",
		"--line:#e0e3e6",
		"--ink:#202327",
		"--green:#2f8f55",
		"--green-soft:#eef8f2",
		`html[data-theme="dark"] .decision-workspace-drawer`,
		"--canvas:#101214",
		"--surface:#15181b",
		"--green:#63d28a",
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("decision workspace is missing prototype theme token %q", expected)
		}
	}
}

func TestDecisionGraphNodeInspectorMatchesPrototypeStructure(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	script, styles := string(scriptBytes), string(stylesBytes)

	for _, expected := range []string{
		`function decisionNodeInspectorIcon(node)`,
		`function decisionNodeSourceLabel(sourceKind)`,
		`function decisionInspectorDefinitionList(rows)`,
		`class="decision-node-inspector-head"`,
		`class="decision-node-inspector-detail"`,
		`class="decision-node-inspector-meta"`,
		`class="decision-node-open-source"`,
		`decisionNodeTypeLabel(node.type))} · ${esc(author)}`,
		`readerFocusCopy("确认人", "Confirmed by")`,
		`readerFocusCopy("确认时间", "Confirmed at")`,
		`readerFocusCopy("思考模板", "Thinking model")`,
		`readerFocusCopy("预期结果", "Expected outcome")`,
		`readerFocusCopy("回顾时间", "Review at")`,
		`readerFocusCopy("期限", "Due")`,
		`readerFocusCopy("状态", "Status")`,
		`readerFocusCopy("保存记录", "Save record")`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("V3 decision node inspector is missing %q", expected)
		}
	}

	for _, expected := range []string{
		`.decision-node-inspector-head{display:grid;grid-template-columns:30px minmax(0,1fr) 28px;gap:9px;align-items:start}`,
		`.decision-node-inspector-head>span{width:30px;height:30px;`,
		`.decision-node-inspector-detail{margin:14px 0 0;`,
		`.decision-node-inspector-meta>div{display:grid;grid-template-columns:58px minmax(0,1fr);`,
		`.decision-node-open-source{width:100%;min-height:34px;`,
		`.decision-node-actions{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:6px;`,
		`.decision-node-inline-editor{display:grid;gap:8px;margin-top:9px;`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("V3 decision node inspector CSS is missing %q", expected)
		}
	}
}

// TestDecisionGraphRenderKeepsCanvasOnStateOnlyUpdates 决策图只变选中/横幅等
// 外围状态时不得销毁重建画布：大图上 destroy+recreate 会整屏清空重画，用户
// 看到的就是"一闪一闪"。渲染入口不再无条件 destroy，图实例按形状指纹复用。
func TestDecisionGraphRenderKeepsCanvasOnStateOnlyUpdates(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		`const shapeSignature = JSON.stringify([`,
		`if (existing && !sameContainer) existing.mount(container);`,
		`const cy = existing || window.cytoscape({`,
		`drawer.__decisionGraphSignature !== shapeSignature`,
		`cy.nodes().removeClass("is-selected");`,
		`drawer.__decisionGraphSignature = shapeSignature;`,
		`drawer.__decisionGraphSignature = "";`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("decision graph canvas reuse guard is missing %q", expected)
		}
	}
	renderStart := `function renderDecisionHistoryDrawer(drawer, workspace) {
    if (!drawer?.isConnected) return;`
	if !strings.Contains(script, renderStart) {
		t.Fatal("cannot locate renderDecisionHistoryDrawer head")
	}
	head := script[strings.Index(script, renderStart)+len(renderStart) : strings.Index(script, renderStart)+len(renderStart)+220]
	if strings.Contains(head, "destroyDecisionGraph(drawer, workspace);") {
		t.Error("renderDecisionHistoryDrawer must not destroy the graph up front; the shape fingerprint in initializeDecisionGraph decides rebuilds")
	}
}

// TestDecisionGraphGraphViewPreservesScrollSubtreeAcrossRender 图视图整树重渲时
// 必须保留 [data-decision-graph-scroll] 子树：cytoscape 画布 DOM 挂在这棵子树里，
// 随 drawer.innerHTML 直写被拔掉后，至少到下一帧 requestAnimationFrame 重挂前
// 画布都是空白的——大图上每次点选节点、busy/error 横幅变化都空一帧，就是用户
// 看到的"一闪一闪"。渲染入口要在直写前按图视图条件取走旧 scroll 子树，直写后
// 把新树里模板生成的 scroll 占位换回存活子树。
func TestDecisionGraphGraphViewPreservesScrollSubtreeAcrossRender(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		// 只在仍处于图视图时捕获，避免把旧画布搬进概览/时间线视图。
		`const preservedGraphScroll = currentView === "graph" ? drawer.querySelector("[data-decision-graph-scroll]") : null;`,
		// 搬回存活子树：画布 DOM 全程不离开文档，rAF 挂载看到的容器不变。
		`freshScroll.replaceWith(preservedGraphScroll);`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("decision graph scroll subtree preservation is missing %q", expected)
		}
	}
}

// TestDecisionGraphRenderPreservesBeforeOverwriteAndAdoptsBeforeMount 捕获必须
// 发生在整树 innerHTML 直写之前、搬回必须发生在直写之后且先于 rAF 挂载：顺序
// 错了保留下来的只是一具已离线节点，或挂载仍落在空容器上，闪屏照旧。
func TestDecisionGraphRenderPreservesBeforeOverwriteAndAdoptsBeforeMount(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	renderStart := `function renderDecisionHistoryDrawer(drawer, workspace) {
    if (!drawer?.isConnected) return;`
	renderEnd := `async function loadDecisionHistoryDrawer(drawer, workspace, revisionID = "") {`
	start := strings.Index(script, renderStart)
	end := strings.Index(script, renderEnd)
	if start < 0 {
		t.Fatal("cannot locate renderDecisionHistoryDrawer head")
	}
	if end < start {
		t.Fatal("cannot locate loadDecisionHistoryDrawer after renderDecisionHistoryDrawer")
	}
	body := script[start:end]

	capture := strings.Index(body, `const preservedGraphScroll = currentView === "graph"`)
	overwrite := strings.LastIndex(body, `drawer.innerHTML = `)
	adopt := strings.Index(body, `replaceWith(preservedGraphScroll)`)
	mount := strings.Index(body, `window.requestAnimationFrame`)
	if capture < 0 || overwrite < 0 || adopt < 0 || mount < 0 {
		t.Fatalf("preserve/overwrite/adopt/mount anchors incomplete: capture=%d overwrite=%d adopt=%d mount=%d", capture, overwrite, adopt, mount)
	}
	if capture > overwrite {
		t.Error("the live graph scroll subtree must be captured before the drawer innerHTML overwrite")
	}
	if adopt < overwrite {
		t.Error("the preserved scroll subtree must be adopted after the drawer innerHTML overwrite")
	}
	if adopt > mount {
		t.Error("the preserved scroll subtree must be adopted before the rAF canvas mount so initializeDecisionGraph sees the live container")
	}
}

// TestDecisionGraphShapeChangesUpdateCanvasInPlace 形状指纹变化（展开推理链条、
// 换版本、类型筛选）不再销毁重建整个实例：destroy + 重新 window.cytoscape(...)
// 会把画布清空再整体重排。存活实例上批量做元素增删改（cy.batch + getElementById
// 定位 + cy.add），只有首次进入图视图才新建实例。
func TestDecisionGraphShapeChangesUpdateCanvasInPlace(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	initStart := `function initializeDecisionGraph(drawer, workspace) {`
	initEnd := `function applyDecisionGraphZoom(drawer, workspace, nextZoom, clientX = null, clientY = null) {`
	start := strings.Index(script, initStart)
	end := strings.Index(script, initEnd)
	if start < 0 || end < start {
		t.Fatal("cannot locate initializeDecisionGraph body")
	}
	initBody := script[start:end]

	if strings.Contains(initBody, "destroyDecisionGraph(") {
		t.Error("initializeDecisionGraph must not destroy the live canvas on shape changes; update the existing instance in place")
	}
	for _, expected := range []string{
		`const cy = existing || window.cytoscape({`,
		`const sameContainer = Boolean(existing && existing.container() === container);`,
		`syncDecisionGraphElements(cy, elements, positions);`,
		`drawer.__decisionGraphSignature = shapeSignature;`,
		// 用户拖拽/缩放实时回写 workspace，兜底还原永远拿新鲜视口。
		`cy.on("pan zoom", () => {`,
	} {
		if !strings.Contains(initBody, expected) {
			t.Errorf("in-place canvas update is missing %q", expected)
		}
	}
	// 反回归：捆绑版 cytoscape 3.34.3 的 mount() 非幂等（无条件销毁 renderer
	// 重建空白画布并全量重排样式表），同容器复用路径绝不允许退回无条件 mount。
	if strings.Contains(initBody, "existing.mount(container);\n    }") {
		t.Error("initializeDecisionGraph must mount only when the container actually changed; unconditional mount reintroduces the renderer-swap flicker")
	}
	for _, expected := range []string{
		`function syncDecisionGraphElements(cy, elements, positions)`,
		`cy.batch(`,
		`cy.getElementById(`,
		`cy.add(`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("decision graph element sync helper is missing %q", expected)
		}
	}
}

// TestDecisionGraphClosedDrawerDestroysCanvasInstance 抽屉整树移除（close 路径）
// 必须销毁 cytoscape 实例：渲染方现在跨渲染复用实例，若关闭时不销毁，实例连同
// rAF 渲染循环会跟随已摘下的 DOM 泄漏（内存纪律 HR1 精神）。
func TestDecisionGraphClosedDrawerDestroysCanvasInstance(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	closeStart := `function closeCitationDrawer(drawer) {`
	start := strings.Index(script, closeStart)
	if start < 0 {
		t.Fatal("cannot locate closeCitationDrawer")
	}
	body := script[start : start+520]
	for _, expected := range []string{
		`drawer.__decisionGraphCy?.destroy?.();`,
		`drawer.__decisionGraphCy = null;`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("citation drawer close must destroy the live cytoscape instance: missing %q", expected)
		}
	}
	// 关窗竞态：渲染方挂载 rAF 的句柄必须落到 close 已取消的属性上，且回调
	// 首行守卫 isConnected——否则渲染后同帧关抽屉会在游离 drawer 上重建实例。
	for _, expected := range []string{
		`drawer.__decisionGraphResizeFrame = window.requestAnimationFrame(() => {`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("decision graph mount rAF must be cancellable from the close path: missing %q", expected)
		}
	}
	mountCallback := script[strings.Index(script, `drawer.__decisionGraphResizeFrame = window.requestAnimationFrame(() => {`):]
	guardWindow := mountCallback[:220]
	if !strings.Contains(guardWindow, `if (!drawer.isConnected) return;`) {
		t.Error("the decision graph mount rAF callback must bail out when the drawer was closed before the frame fired")
	}
}

// TestDecisionRunRefreshLoopIsBrokenOnTerminalStatus 静置闪屏的根因是一个以网络
// 往返速度自持的刷新环：loadDecisionRunState 看到终态就 reloadFocusProjection →
// refreshOpenFocusV3Drawer → workspace 面板无条件再触发 loadDecisionRunState →
// 又看到终态……实测 ~10Hz，思考与行动链图每圈销毁重建一次。三处断环锚点：
// 终态只在"转入终态"的那一次 reload；数据刷新路径不强制读运行状态（只有用户
// 真正切到 workspace tab 才补读）；同 id 的投影 reload 在飞行中去重。
func TestDecisionRunRefreshLoopIsBrokenOnTerminalStatus(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		// 断点一：终态响应只在"非终态→终态"的这一次刷新投影，持续终态不再触发。
		`const decisionRunTerminalNow = next && ["completed", "failed_retryable", "failed_terminal", "superseded"].includes(next.status);`,
		`if (decisionRunTerminalNow && !decisionRunTerminalBefore)`,
		// 断点二：只有用户切换到 workspace tab 才补读决策运行，数据刷新路径不得强制触发。
		`if (panel === "workspace" && options.tabSwitch)`,
		// 断点三：同 id 投影 reload 飞行中去重，并发触发合并为一次。
		`parity.focusProjectionReloads ||= new Map();`,
		`if (parity.focusProjectionReloads.has(id)) return parity.focusProjectionReloads.get(id);`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("decision run refresh loop break is missing %q", expected)
		}
	}
}

// TestDecisionGraphLabelsUseRealCanvasMetrics 节点文字必须按真实像素测量折行：
// 旧实现按"字符单位 × 8.6px"估宽，中文按 11px 字号实渲染普遍更宽，画布端
// text-wrap 二次折行折出比预估更多的行，节点高度按较少行数算，文字纵向
// 溢出方框（"方块中的文字超出了文本"）。测量字体串与 cytoscape 节点样式
// 完全一致，行数即渲染行数；textMaxWidth 比最宽实测行宽 6px，画布端不再
// 二次折行。估宽常数 8.6/9.4 不允许回流。
func TestDecisionGraphLabelsUseRealCanvasMetrics(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		"function decisionGraphMeasureWidth(value, fontSize) {",
		"decisionGraphMeasureContext.font = `600 ${fontSize}px Inter, ui-sans-serif, system-ui, sans-serif`",
		`function decisionGraphFallbackWidth(value, fontSize)`,
		`function decisionGraphWrapTextPx(value, maxPx, measure)`,
		`const measure = (value) => decisionGraphMeasureWidth(value, fontSize) ?? decisionGraphFallbackWidth(value, fontSize);`,
		`const label = decisionGraphWrapTextPx(rawLabel, wrapPx, measure);`,
		`textMaxWidth: Math.max(168, width - 28),`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("canvas-metric label wrapping is missing %q", expected)
		}
	}

	presStart := `function decisionGraphNodePresentation(node, workspace) {`
	start := strings.Index(script, presStart)
	if start < 0 {
		t.Fatal("cannot locate decisionGraphNodePresentation")
	}
	body := script[start : start+1800]
	for _, forbidden := range []string{
		`decisionGraphWrapText(rawLabel, wrapUnits)`,
		`* (fullscreen ? 9.4 : 8.6)`,
		`Math.min(148, naturalHeight)`,
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("decisionGraphNodePresentation must not regress to unit-estimated widths: found %q", forbidden)
		}
	}
}

// TestDecisionGraphRoutesEdgesAroundIntermediateColumns "线条太乱"的三个来源：
// ai_judgment 与 gap/alternative 混列还 5+1 切块、列内顺序不按邻接重心排、
// 所有边固定在源右侧 46px 转弯且跨列长边横穿中间列节点。修法是语义分层 +
// barycenter 列内排序 + 相邻列边在列间通道错开转弯 + 跨列边 unbundled-bezier
// 绕过中间列。创建与形状同步两条路径都必须重挂路由。
func TestDecisionGraphRoutesEdgesAroundIntermediateColumns(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		`function decisionGraphLayerCategory(node)`,
		`if (type === "ai_judgment") return 2;`,
		`function decisionGraphBarycenterOrder(layers, edges)`,
		`function decisionGraphOptimizeColumnOrder(layers, edges)`,
		`const order = decisionGraphOptimizeColumnOrder(layers, graph.edges);`,
		`const columns = order.flatMap((layerIndex) => decisionGraphChunkColumn(layers[layerIndex], maxRows)).filter((column) => column.length);`,
		`function decisionGraphLayeredLayout(graph, presentations, workspace)`,
		`const layout = decisionGraphLayeredLayout(graph, presentations, workspace);`,
		`function decisionGraphEdgeRoutes(graph, layout)`,
		`"curve-style": "unbundled-bezier",`,
		`"control-point-weights": weights.map((weight) => Math.round(weight * 100) / 100).join(" "),`,
		`const turn = Math.round(gutterStart + gutterWidth * ((index + 0.5) / edges.length) - source.x);`,
		`if (to <= from) return;`,
		`const upward = pairIndex % 2 === 0;`,
		`return Math.round(peak - lineY);`,
		`cy.fit(cy.nodes(), workspace.fullscreen ? 64 : 40);`,
		`function applyDecisionGraphEdgeRoutes(cy, routes)`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("decision graph edge routing is missing %q", expected)
		}
	}
	if got := strings.Count(script, "applyDecisionGraphEdgeRoutes(cy, decisionGraphEdgeRoutes(graph, layout));"); got < 2 {
		t.Errorf("edge routes must be applied on both canvas creation and shape sync, found %d call sites", got)
	}
	// 反回归：不允许退回把 ai_judgment 混进 gap/alternative 的旧混列。
	if strings.Contains(script, `const analysis = graph.nodes.filter((node) => ["gap", "alternative", "ai_judgment"].includes(text(node.type)));`) {
		t.Error("ai_judgment must own its own semantic layer instead of being lumped with gaps/alternatives")
	}
}

// TestDecisionGraphEdgeRoutesApplyByIDCall 真实翻车案例：apply 循环里把
// cytoscape 元素的 id 当字段读（edge.id 少了调用括号），text(函数引用) 永远
// 匹配不到路由键，13 条路由全部静默落空、图上仍是无路由默认线。这里的键
// 必须取 element.id() 的调用值。
func TestDecisionGraphEdgeRoutesApplyByIDCall(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	applyStart := `function applyDecisionGraphEdgeRoutes(cy, routes) {`
	start := strings.Index(script, applyStart)
	if start < 0 {
		t.Fatal("cannot locate applyDecisionGraphEdgeRoutes")
	}
	body := script[start : start+400]
	if !strings.Contains(body, `const route = routes.get(text(edge.id()));`) {
		t.Error("edge route lookup must call element.id() — reading edge.id as a field silently drops every route")
	}
	if strings.Contains(body, `routes.get(text(edge.id))`) {
		t.Error("edge.id without call parens leaks a function reference into the route lookup key")
	}
}

// TestFocusWorkflowGraphReusesDecisionGraphTidy 思考与行动链(焦点详情里的
// 工作流图)此前自成一套:单位估宽(中文溢出)、插入序列直排、全员固定
// 42px taxi 转弯——展开依据后"依据→备选"直接横穿目标方框。修法是复用决
// 策图的 px 实测换行、barycenter、列序优化与边路由;焦点详情关闭时整个
// 注销 per-focus 图状态,全屏档位不再泄漏到下一次打开。
func TestFocusWorkflowGraphReusesDecisionGraphTidy(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, expected := range []string{
		`function focusWorkflowLayout(graph, presentations, fullscreen = false)`,
		`decisionGraphBarycenterOrder(orderedLayers, graph.edges);`,
		`const order = decisionGraphOptimizeColumnOrder(orderedLayers, graph.edges);`,
		`decisionGraphWrapTextPx(decisionGraphShortText(raw, fullscreen ? 260 : 180), maxWidth - 28, measure)`,
		`textMaxWidth: Math.max(168, width - 28), fontSize`,
		`applyDecisionGraphEdgeRoutes(cy, decisionGraphEdgeRoutes(projection, layout));`,
		`parity.focusWorkflowStates.delete(parity.selectedFocusID);`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("focus workflow graph tidy port is missing %q", expected)
		}
	}
	// 反回归:不得退回单位估宽。
	if strings.Contains(script, `Math.round(widest * 8.6 + 48)`) {
		t.Error("focus workflow nodes must not regress to unit-estimated widths")
	}
}
