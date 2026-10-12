package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	stdhtml "html"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	htmlnode "golang.org/x/net/html"
)

const maxEntryFullHTMLBytes = 1 << 20

const (
	articleBlocksVersion  = "article-blocks-v1"
	maxEntryArticleBlocks = 256
	maxArticleBlockRunes  = 2_000
	maxArticleBlocksBytes = 256 << 10
)

var articleAllowedElements = map[string]bool{
	"a": true, "b": true, "blockquote": true, "br": true, "code": true, "em": true,
	"article": true, "div": true, "main": true, "section": true,
	"figcaption": true, "figure": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"hr": true, "i": true, "li": true, "ol": true, "p": true, "pre": true, "strong": true,
	"table": true, "tbody": true, "td": true, "th": true, "thead": true, "tr": true, "u": true, "ul": true,
}

var articleNoiseTokens = []string{
	"advert", "author", "breadcrumb", "charge", "comment", "copyright",
	"directory", "footer", "header", "menu", "nav", "popover", "promo",
	"recommend", "related", "share", "sidebar", "social", "sticky", "toolbar",
}

type extractedArticle struct {
	Text            string
	SafeHTML        string
	ImageURLs       []string
	LeadImageURL    string
	LeadImageIndex  int
	Format          string
	Blocks          []ArticleBlock
	BlocksHash      string
	BlocksVersion   string
	BlocksTruncated bool
}

// ArticleBlock is the bounded, source-derived representation used for
// paragraph-level citations.  It deliberately contains no source URL or
// owner information; those are resolved by the scoped evidence row.
// Block IDs are deterministic for a source identity, ordinal, type and
// normalized text. The content hash carried by the parent EntryContent keeps
// old citations from silently pointing at a newer article revision.
type ArticleBlock struct {
	BlockID    string `json:"block_id"`
	Type       string `json:"type"`
	Ordinal    int    `json:"ordinal"`
	Text       string `json:"text,omitempty"`
	TextHash   string `json:"text_hash,omitempty"`
	Level      int    `json:"level,omitempty"`
	ImageIndex int    `json:"image_index,omitempty"`
}

// finalizeArticleBlocks applies the same bounds to every renderer and derives
// the payload hash used by clients to detect a changed block projection.
// Returning a value (rather than mutating a shared slice in place) keeps the
// renderer safe when a feed item is reused by more than one persistence path.
func finalizeArticleBlocks(article extractedArticle) extractedArticle {
	if len(article.Blocks) > maxEntryArticleBlocks {
		article.Blocks = append([]ArticleBlock(nil), article.Blocks[:maxEntryArticleBlocks]...)
		article.BlocksTruncated = true
	}
	for index := range article.Blocks {
		block := &article.Blocks[index]
		if block.Ordinal < 1 {
			block.Ordinal = index + 1
		}
		block.Text = limitText(strings.TrimSpace(block.Text), maxArticleBlockRunes)
		if block.Text != "" {
			block.TextHash = articleTextHash(block.Text)
		}
		if block.BlockID == "" {
			block.BlockID = articleBlockID("article", block)
		}
	}
	article.BlocksVersion = articleBlocksVersion
	article.BlocksHash = articleBlocksHash(article.Blocks)
	return article
}

func articleTextHash(value string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(digest[:])
}

func articleBlockID(identity string, block *ArticleBlock) string {
	if strings.TrimSpace(identity) == "" {
		identity = "article"
	}
	seed := strings.Join([]string{identity, block.Type, strconv.Itoa(block.Ordinal), strings.TrimSpace(block.Text)}, "\x00")
	digest := sha256.Sum256([]byte(seed))
	return "b_" + hex.EncodeToString(digest[:])[:16]
}

func articleBlocksHash(blocks []ArticleBlock) string {
	raw, err := json.Marshal(blocks)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// DecodeArticleBlocks is intentionally small and strict so focus citation
// validation can reuse the exact cache representation. An empty JSON value is
// treated as an empty projection for legacy rows.
func DecodeArticleBlocks(raw string) ([]ArticleBlock, error) {
	if strings.TrimSpace(raw) == "" {
		return []ArticleBlock{}, nil
	}
	var blocks []ArticleBlock
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		return nil, err
	}
	if len(blocks) > maxEntryArticleBlocks {
		return nil, errors.New("article blocks exceed limit")
	}
	for index := range blocks {
		block := &blocks[index]
		if block.BlockID == "" || block.Ordinal < 1 || block.Ordinal > maxEntryArticleBlocks || !validArticleBlockType(block.Type) || len([]rune(block.Text)) > maxArticleBlockRunes {
			return nil, errors.New("article block is invalid")
		}
		if block.Text != "" && block.TextHash != articleTextHash(block.Text) {
			return nil, errors.New("article block text hash mismatch")
		}
	}
	return blocks, nil
}

func validArticleBlockType(value string) bool {
	switch value {
	case "heading", "paragraph", "quote", "list_item", "code", "image", "table":
		return true
	default:
		return false
	}
}

func articleBlocksJSON(article extractedArticle) (string, string, string, bool, error) {
	article = finalizeArticleBlocks(article)
	raw, err := json.Marshal(article.Blocks)
	if err != nil {
		return "", "", "", article.BlocksTruncated, err
	}
	if len(raw) > maxArticleBlocksBytes {
		// The per-block bound normally keeps this impossible. Keep a defensive
		// fallback so a future field addition cannot create an unbounded cache.
		bounded := article
		bounded.Blocks = bounded.Blocks[:0]
		for _, block := range article.Blocks {
			candidate := append(append([]ArticleBlock(nil), bounded.Blocks...), block)
			candidateRaw, marshalErr := json.Marshal(candidate)
			if marshalErr != nil || len(candidateRaw) > maxArticleBlocksBytes {
				break
			}
			bounded.Blocks = candidate
		}
		bounded.BlocksTruncated = true
		article = finalizeArticleBlocks(bounded)
		raw, err = json.Marshal(article.Blocks)
		if err != nil {
			return "", "", "", true, err
		}
	}
	return string(raw), article.BlocksVersion, article.BlocksHash, article.BlocksTruncated, nil
}

// entryContentRenderer is the extension boundary for source-specific reader
// compatibility. Renderers can select a content node, but all of them share
// the same bounded sanitizer and output contract.
type entryContentRenderer interface {
	Render(body []byte, contentType, baseURL string) extractedArticle
}

type genericEntryContentRenderer struct{}
type wechatEntryContentRenderer struct{}
type markdownEntryContentRenderer struct{}
type plainEntryContentRenderer struct{}

func entryContentRendererFor(body []byte, sourceURL, contentType string) entryContentRenderer {
	if entryContentLooksMarkdown(contentType) {
		return markdownEntryContentRenderer{}
	}
	parsed, err := url.Parse(strings.TrimSpace(sourceURL))
	if err == nil {
		host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		if isWechatRendererHost(host) {
			return wechatEntryContentRenderer{}
		}
	}
	if !entryContentLooksHTML(body, contentType) {
		return plainEntryContentRenderer{}
	}
	return genericEntryContentRenderer{}
}

func newArticleBlock(identity, blockType string, ordinal int, text string, level int) ArticleBlock {
	block := ArticleBlock{
		Type:    blockType,
		Ordinal: ordinal,
		Text:    limitText(strings.TrimSpace(normalizeArticleText(text)), maxArticleBlockRunes),
		Level:   level,
	}
	block.BlockID = articleBlockID(identity, &block)
	if block.Text != "" {
		block.TextHash = articleTextHash(block.Text)
	}
	return block
}

func plainArticleBlocks(raw, identity string) []ArticleBlock {
	value := stdhtml.UnescapeString(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"))
	// FieldsFunc intentionally preserves the existing plain-text reader
	// behavior while giving every non-empty line a safe citation target. When
	// the input has blank lines, merge adjacent lines into one paragraph.
	blocks := make([]ArticleBlock, 0, 16)
	var pending []string
	flush := func() {
		if len(pending) == 0 {
			return
		}
		text := strings.Join(pending, " ")
		blocks = append(blocks, newArticleBlock(identity, "paragraph", len(blocks)+1, text, 0))
		pending = pending[:0]
	}
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		pending = append(pending, line)
	}
	flush()
	return blocks
}

func markdownArticleBlocks(raw, identity string) []ArticleBlock {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"), "\n")
	blocks := make([]ArticleBlock, 0, len(lines))
	paragraph := make([]string, 0, 4)
	inFence := false
	code := make([]string, 0, 8)
	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		blocks = append(blocks, newArticleBlock(identity, "paragraph", len(blocks)+1, strings.Join(paragraph, " "), 0))
		paragraph = paragraph[:0]
	}
	flushCode := func() {
		if len(code) == 0 {
			return
		}
		blocks = append(blocks, newArticleBlock(identity, "code", len(blocks)+1, strings.Join(code, "\n"), 0))
		code = code[:0]
	}
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			if inFence {
				flushCode()
			} else {
				flushParagraph()
			}
			inFence = !inFence
			continue
		}
		if inFence {
			code = append(code, rawLine)
			continue
		}
		if line == "" {
			flushParagraph()
			continue
		}
		if level, heading := markdownHeading(line); heading != "" {
			flushParagraph()
			blocks = append(blocks, newArticleBlock(identity, "heading", len(blocks)+1, heading, level))
			continue
		}
		if quote := strings.TrimSpace(strings.TrimPrefix(line, ">")); quote != line {
			flushParagraph()
			blocks = append(blocks, newArticleBlock(identity, "quote", len(blocks)+1, quote, 0))
			continue
		}
		if item := markdownListItem(line); item != "" {
			flushParagraph()
			blocks = append(blocks, newArticleBlock(identity, "list_item", len(blocks)+1, item, 0))
			continue
		}
		paragraph = append(paragraph, stripMarkdownInline(line))
	}
	if inFence {
		flushCode()
	} else {
		flushParagraph()
	}
	return blocks
}

func markdownHeading(line string) (int, string) {
	trimmed := strings.TrimSpace(line)
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || (len(trimmed) > level && trimmed[level] != ' ') {
		return 0, ""
	}
	return level, stripMarkdownInline(strings.TrimSpace(trimmed[level:]))
}

func markdownListItem(line string) string {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) >= 2 && (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') && trimmed[1] == ' ' {
		return stripMarkdownInline(strings.TrimSpace(trimmed[2:]))
	}
	index := 0
	for index < len(trimmed) && trimmed[index] >= '0' && trimmed[index] <= '9' {
		index++
	}
	if index > 0 && index+1 < len(trimmed) && (trimmed[index] == '.' || trimmed[index] == ')') && trimmed[index+1] == ' ' {
		return stripMarkdownInline(strings.TrimSpace(trimmed[index+2:]))
	}
	return ""
}

func (plainEntryContentRenderer) Render(body []byte, _, baseURL string) extractedArticle {
	value := stdhtml.UnescapeString(string(body))
	// Keep the long-standing plain-text contract: line breaks collapse to a
	// single space. HTML/WeChat projections use normalizeArticleText below so
	// paragraph boundaries remain readable without changing plain notes.
	article := extractedArticle{Text: limitText(strings.Join(strings.Fields(value), " "), maxEntryFullTextRunes), Format: "text", Blocks: plainArticleBlocks(value, baseURL)}
	return finalizeArticleBlocks(article)
}

func (genericEntryContentRenderer) Render(body []byte, contentType, baseURL string) extractedArticle {
	return renderHTMLArticle(body, contentType, baseURL, nil)
}

func (wechatEntryContentRenderer) Render(body []byte, contentType, baseURL string) extractedArticle {
	return renderHTMLArticle(body, contentType, baseURL, findWechatArticleContent)
}

func (markdownEntryContentRenderer) Render(body []byte, _, baseURL string) extractedArticle {
	article := extractedArticle{
		Text:   limitText(normalizeMarkdownText(string(body)), maxEntryFullTextRunes),
		Format: "markdown",
		Blocks: markdownArticleBlocks(string(body), baseURL),
	}
	return finalizeArticleBlocks(article)
}

func extractEntryFullText(body []byte, contentType string) string {
	return renderEntryContent(body, contentType, "", "").Text
}

func extractEntryArticle(body []byte, contentType, baseURL string) extractedArticle {
	return renderEntryContent(body, contentType, baseURL, baseURL)
}

func renderEntryContent(body []byte, contentType, baseURL, sourceURL string) extractedArticle {
	return entryContentRendererFor(body, sourceURL, contentType).Render(body, contentType, baseURL)
}

func entryContentLooksHTML(body []byte, contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return true
	}
	// A few feed bridges return an HTML fragment with a text/plain header. Keep
	// the sniff bounded and conservative so ordinary notes containing a '<'
	// character remain plain text.
	value := strings.ToLower(strings.TrimSpace(string(body)))
	for _, prefix := range []string{
		"<!doctype html", "<html", "<head", "<body", "<article", "<main", "<section",
		"<div", "<p>", "<p ", "<h1", "<h2", "<h3", "<h4", "<figure", "<table",
		"<blockquote", "<ul", "<ol", "<pre", "<img", "<a ",
	} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func entryContentLooksMarkdown(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch mediaType {
	case "text/markdown", "text/x-markdown", "application/markdown", "text/commonmark":
		return true
	default:
		return false
	}
}

func htmlArticleBlockNodes(root *htmlnode.Node) []*htmlnode.Node {
	result := make([]*htmlnode.Node, 0, 32)
	var walk func(*htmlnode.Node)
	walk = func(node *htmlnode.Node) {
		if node == nil || nodeIsArticleNoise(node) {
			return
		}
		if node.Type == htmlnode.ElementNode {
			if node.Data == "img" {
				if resolveArticleImage(node, "") != "" || nodeAttribute(node, "alt") != "" {
					result = append(result, node)
				}
				return
			}
			if htmlArticleLeafBlock(node.Data) {
				if strings.TrimSpace(normalizeArticleText(visibleArticleText(node))) != "" || node.Data == "pre" {
					result = append(result, node)
				}
				return
			}
			before := len(result)
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
			// A fragment sometimes uses only div/section wrappers. Keep a
			// wrapper as a block only when it contains no more specific child.
			if len(result) == before && htmlArticleContainerBlock(node.Data) {
				if strings.TrimSpace(normalizeArticleText(visibleArticleText(node))) != "" {
					result = append(result, node)
				}
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return result
}

func htmlArticleLeafBlock(tag string) bool {
	switch tag {
	case "h1", "h2", "h3", "h4", "h5", "h6", "p", "blockquote", "pre", "li", "figcaption", "td", "th", "tr":
		return true
	default:
		return false
	}
}

func htmlArticleContainerBlock(tag string) bool {
	switch tag {
	case "article", "main", "section", "div", "figure":
		return true
	default:
		return false
	}
}

func htmlArticleBlockText(node *htmlnode.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == htmlnode.ElementNode && node.Data == "img" {
		return limitText(strings.TrimSpace(stdhtml.UnescapeString(nodeAttribute(node, "alt"))), maxArticleBlockRunes)
	}
	return limitText(strings.TrimSpace(normalizeArticleText(visibleArticleText(node))), maxArticleBlockRunes)
}

func htmlArticleBlockType(node *htmlnode.Node) string {
	if node != nil && node.Type == htmlnode.ElementNode {
		switch node.Data {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			return "heading"
		case "blockquote":
			return "quote"
		case "li":
			return "list_item"
		case "pre":
			return "code"
		case "img":
			return "image"
		case "td", "th", "tr", "table":
			return "table"
		}
	}
	return "paragraph"
}

func htmlArticleBlockLevel(node *htmlnode.Node) int {
	if node == nil || node.Type != htmlnode.ElementNode {
		return 0
	}
	if len(node.Data) == 2 && node.Data[0] == 'h' && node.Data[1] >= '1' && node.Data[1] <= '6' {
		return int(node.Data[1] - '0')
	}
	return 0
}

func extractHTMLArticleBlocks(root *htmlnode.Node, identity string) ([]ArticleBlock, map[*htmlnode.Node]string) {
	nodes := htmlArticleBlockNodes(root)
	blocks := make([]ArticleBlock, 0, len(nodes))
	ids := make(map[*htmlnode.Node]string, len(nodes))
	for _, node := range nodes {
		text := htmlArticleBlockText(node)
		block := newArticleBlock(identity, htmlArticleBlockType(node), len(blocks)+1, text, htmlArticleBlockLevel(node))
		if block.Type == "image" {
			block.Text = text
		}
		blocks = append(blocks, block)
		ids[node] = block.BlockID
		if len(blocks) >= maxEntryArticleBlocks {
			break
		}
	}
	return blocks, ids
}

func isWechatRendererHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return host == "mp.weixin.qq.com" || host == "wechat2rss.xlab.app" || strings.HasSuffix(host, ".mmbiz.qpic.cn")
}

func normalizeMarkdownText(raw string) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if strings.HasPrefix(line, ">") {
			line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		}
		if len(line) >= 2 && ((line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ') {
			line = strings.TrimSpace(line[2:])
		}
		for len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			line = line[1:]
		}
		if strings.HasPrefix(line, ". ") || strings.HasPrefix(line, ") ") {
			line = strings.TrimSpace(line[2:])
		}
		line = stripMarkdownInline(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n\n")
}

func stripMarkdownInline(value string) string {
	value = stdhtml.UnescapeString(value)
	for _, marker := range []string{"**", "__", "~~", "`", "*", "_"} {
		value = strings.ReplaceAll(value, marker, "")
	}
	for {
		start := strings.Index(value, "![")
		if start < 0 {
			break
		}
		end := strings.Index(value[start:], ")")
		if end < 0 {
			break
		}
		value = value[:start] + value[start+end+1:]
	}
	for {
		start := strings.Index(value, "[")
		if start < 0 {
			break
		}
		closeLabel := strings.Index(value[start:], "](")
		if closeLabel < 0 {
			break
		}
		closeLabel += start
		end := strings.Index(value[closeLabel+2:], ")")
		if end < 0 {
			break
		}
		label := value[start+1 : closeLabel]
		value = value[:start] + label + value[closeLabel+2+end+1:]
	}
	return strings.Join(strings.Fields(value), " ")
}

type articleNodeSelector func(*htmlnode.Node) *htmlnode.Node

func renderHTMLArticle(body []byte, contentType, baseURL string, selector articleNodeSelector) extractedArticle {
	if !entryContentLooksHTML(body, contentType) {
		return plainEntryContentRenderer{}.Render(body, contentType, baseURL)
	}
	document, err := htmlnode.Parse(bytes.NewReader(body))
	if err != nil {
		return plainEntryContentRenderer{}.Render(body, contentType, baseURL)
	}
	var content *htmlnode.Node
	if selector != nil {
		content = selector(document)
	}
	if content == nil {
		content = bestArticleNode(document)
	}
	if content == nil {
		content = findElement(document, "body")
	}
	if content == nil {
		return extractedArticle{}
	}

	leadImage := findLeadImage(document, baseURL)
	images := make([]string, 0, maxEntryArticleImages)
	imageIndex := map[string]int{}
	var rich strings.Builder
	leadInContent := leadImage != "" && articleNodeContainsImage(content, leadImage, baseURL)
	if leadInContent {
		imageIndex[leadImage] = 0
		images = append(images, leadImage)
	}
	bodyLeadImage := ""
	leadImageIndex := -1
	if leadInContent {
		bodyLeadImage = leadImage
		leadImageIndex = 0
	}
	blocks, blockIDs := extractHTMLArticleBlocks(content, baseURL)
	renderArticleNode(&rich, content, baseURL, bodyLeadImage, &images, imageIndex, blockIDs)
	text := limitText(normalizeArticleText(visibleArticleText(content)), maxEntryFullTextRunes)
	safeHTML := strings.TrimSpace(rich.String())
	format := "text"
	if safeHTML != "" {
		format = "html"
	}
	article := extractedArticle{Text: text, SafeHTML: safeHTML, ImageURLs: images, LeadImageURL: firstNonEmpty(leadImage, firstString(images)), LeadImageIndex: leadImageIndex, Format: format, Blocks: blocks}
	return finalizeArticleBlocks(article)
}

func articleNodeContainsImage(root *htmlnode.Node, expected, baseURL string) bool {
	if root == nil || expected == "" {
		return false
	}
	if root.Type == htmlnode.ElementNode && root.Data == "img" && resolveArticleImage(root, baseURL) == expected {
		return true
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if articleNodeContainsImage(child, expected, baseURL) {
			return true
		}
	}
	return false
}

func findWechatArticleContent(root *htmlnode.Node) *htmlnode.Node {
	// The outer #js_article shell often contains title, author and share
	// chrome. Prefer the actual #js_content / rich_media_content node first.
	selectors := []func(*htmlnode.Node) bool{
		func(node *htmlnode.Node) bool { return strings.EqualFold(nodeAttribute(node, "id"), "js_content") },
		func(node *htmlnode.Node) bool { return hasArticleClass(node, "rich_media_content") },
		func(node *htmlnode.Node) bool { return strings.EqualFold(nodeAttribute(node, "id"), "js_article") },
	}
	for _, matches := range selectors {
		if result := findArticleNode(root, matches); result != nil {
			return result
		}
	}
	return nil
}

func findArticleNode(root *htmlnode.Node, matches func(*htmlnode.Node) bool) *htmlnode.Node {
	if root.Type == htmlnode.ElementNode && matches(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if result := findArticleNode(child, matches); result != nil {
			return result
		}
	}
	return nil
}

func hasArticleClass(node *htmlnode.Node, expected string) bool {
	for _, className := range strings.Fields(nodeAttribute(node, "class")) {
		if strings.EqualFold(className, expected) {
			return true
		}
	}
	return false
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func bestArticleNode(root *htmlnode.Node) *htmlnode.Node {
	var best *htmlnode.Node
	bestScore := -1 << 30
	var walk func(*htmlnode.Node)
	walk = func(node *htmlnode.Node) {
		if node.Type == htmlnode.ElementNode && (node.Data == "article" || node.Data == "main" || node.Data == "div" || node.Data == "section") {
			textLength := len([]rune(normalizeArticleText(visibleArticleText(node))))
			if textLength >= 160 {
				score := textLength + articleCandidateBonus(node)
				if score > bestScore {
					best, bestScore = node, score
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return best
}

func articleCandidateBonus(node *htmlnode.Node) int {
	identifier := strings.ToLower(nodeAttribute(node, "id") + " " + nodeAttribute(node, "class"))
	bonus := 0
	if node.Data == "article" {
		bonus += 3_000
	} else if node.Data == "main" {
		bonus += 2_000
	}
	for _, token := range []string{"article__main__content", "article-content", "article_content", "entry-content", "main-content", "post-content", "post_content", "story-content"} {
		if strings.Contains(identifier, token) {
			bonus += 40_000
		}
	}
	for _, token := range []string{"article", "content", "entry", "post", "story", "wangeditor"} {
		if strings.Contains(identifier, token) {
			bonus += 2_000
		}
	}
	for _, token := range articleNoiseTokens {
		if strings.Contains(identifier, token) {
			bonus -= 30_000
		}
	}
	return bonus
}

func nodeIsArticleNoise(node *htmlnode.Node) bool {
	if node.Type != htmlnode.ElementNode {
		return false
	}
	switch node.Data {
	case "script", "style", "noscript", "svg", "template", "nav", "header", "footer", "aside", "form", "button", "iframe", "canvas":
		return true
	}
	identifier := strings.ToLower(nodeAttribute(node, "id") + " " + nodeAttribute(node, "class") + " " + nodeAttribute(node, "role"))
	for _, token := range articleNoiseTokens {
		if strings.Contains(identifier, token) {
			return true
		}
	}
	return false
}

func visibleArticleText(node *htmlnode.Node) string {
	var value strings.Builder
	var walk func(*htmlnode.Node)
	walk = func(current *htmlnode.Node) {
		if nodeIsArticleNoise(current) {
			return
		}
		if current.Type == htmlnode.TextNode {
			value.WriteString(current.Data)
			value.WriteByte(' ')
		}
		block := current.Type == htmlnode.ElementNode && isArticleBlock(current.Data)
		if block {
			value.WriteByte('\n')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if block {
			value.WriteByte('\n')
		}
	}
	walk(node)
	return value.String()
}

func normalizeArticleText(value string) string {
	lines := strings.Split(stdhtml.UnescapeString(value), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" && (len(cleaned) == 0 || cleaned[len(cleaned)-1] != line) {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n\n")
}

func renderArticleNode(out *strings.Builder, node *htmlnode.Node, baseURL, leadImage string, images *[]string, imageIndex map[string]int, blockIDs map[*htmlnode.Node]string) {
	if out.Len() >= maxEntryFullHTMLBytes || nodeIsArticleNoise(node) {
		return
	}
	if node.Type == htmlnode.TextNode {
		text := strings.Join(strings.Fields(node.Data), " ")
		if text != "" {
			writeArticleText(out, stdhtml.EscapeString(text)+" ")
		}
		return
	}
	if node.Type != htmlnode.ElementNode {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderArticleNode(out, child, baseURL, leadImage, images, imageIndex, blockIDs)
		}
		return
	}
	if node.Data == "img" {
		imageURL := resolveArticleImage(node, baseURL)
		if imageURL == "" {
			return
		}
		index, exists := imageIndex[imageURL]
		if !exists {
			if len(*images) >= maxEntryArticleImages {
				return
			}
			index = len(*images)
			imageIndex[imageURL] = index
			*images = append(*images, imageURL)
		}
		// The cover may also occur inside the source article. Keep its stable
		// index for the image proxy, but omit the duplicate from the body.
		if leadImage != "" && imageURL == leadImage {
			return
		}
		blockAttr := ""
		if blockID := blockIDs[node]; blockID != "" {
			blockAttr = ` data-block-id="` + stdhtml.EscapeString(blockID) + `"`
		}
		writeArticleExact(out, `<img data-entry-image="`+strconv.Itoa(index)+`" data-entry-image-url="`+stdhtml.EscapeString(imageURL)+`"`+blockAttr+` alt="`+stdhtml.EscapeString(limitText(nodeAttribute(node, "alt"), 240))+`">`)
		return
	}
	if !articleAllowedElements[node.Data] {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderArticleNode(out, child, baseURL, leadImage, images, imageIndex, blockIDs)
		}
		return
	}
	opening := "<" + node.Data
	if blockID := blockIDs[node]; blockID != "" {
		opening += ` data-block-id="` + stdhtml.EscapeString(blockID) + `"`
	}
	if node.Data == "a" {
		if href := resolvePublicLink(baseURL, nodeAttribute(node, "href")); href != "" {
			opening += ` href="` + stdhtml.EscapeString(href) + `"`
		}
	}
	if !writeArticleExact(out, opening+">") {
		return
	}
	if node.Data != "br" && node.Data != "hr" {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderArticleNode(out, child, baseURL, leadImage, images, imageIndex, blockIDs)
		}
		writeArticleExact(out, "</"+node.Data+">")
	}
}

func writeArticleExact(out *strings.Builder, value string) bool {
	if len(value) > maxEntryFullHTMLBytes-out.Len() {
		return false
	}
	out.WriteString(value)
	return true
}

func writeArticleText(out *strings.Builder, value string) bool {
	remaining := maxEntryFullHTMLBytes - out.Len()
	if remaining <= 0 {
		return false
	}
	if len(value) <= remaining {
		out.WriteString(value)
		return true
	}
	for _, runeValue := range value {
		runeBytes := utf8.RuneLen(runeValue)
		if runeBytes < 0 || runeBytes > remaining {
			break
		}
		out.WriteRune(runeValue)
		remaining -= runeBytes
		if remaining == 0 {
			break
		}
	}
	return false
}

func resolveArticleImage(node *htmlnode.Node, baseURL string) string {
	for _, name := range []string{"data-original", "data-src", "data-lazy-src", "src"} {
		if resolved := resolvePublicLink(baseURL, nodeAttribute(node, name)); resolved != "" {
			return resolved
		}
	}
	if srcset := nodeAttribute(node, "srcset"); srcset != "" {
		for _, candidate := range strings.Split(srcset, ",") {
			fields := strings.Fields(candidate)
			if len(fields) > 0 {
				if resolved := resolvePublicLink(baseURL, fields[0]); resolved != "" {
					return resolved
				}
			}
		}
	}
	return ""
}

func findLeadImage(root *htmlnode.Node, baseURL string) string {
	var result string
	var walk func(*htmlnode.Node)
	walk = func(node *htmlnode.Node) {
		if result != "" {
			return
		}
		if node.Type == htmlnode.ElementNode && node.Data == "meta" {
			key := strings.ToLower(firstNonEmpty(nodeAttribute(node, "property"), nodeAttribute(node, "name")))
			if key == "og:image" || key == "og:image:secure_url" || key == "twitter:image" {
				result = resolvePublicLink(baseURL, nodeAttribute(node, "content"))
			}
		}
		for child := node.FirstChild; child != nil && result == ""; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return result
}

func findElement(root *htmlnode.Node, tag string) *htmlnode.Node {
	if root.Type == htmlnode.ElementNode && root.Data == tag {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, tag); found != nil {
			return found
		}
	}
	return nil
}

func nodeAttribute(node *htmlnode.Node, name string) string {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, name) {
			return strings.TrimSpace(attribute.Val)
		}
	}
	return ""
}

func isArticleBlock(tag string) bool {
	switch tag {
	case "article", "blockquote", "br", "div", "figcaption", "figure", "h1", "h2", "h3", "h4", "hr", "li", "main", "ol", "p", "pre", "section", "table", "tr", "ul":
		return true
	default:
		return false
	}
}
