package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	htmlcharset "golang.org/x/net/html/charset"
)

const (
	feedFetchTimeout           = 15 * time.Second
	feedFetchMaxBytes          = 2 << 20
	feedFetchFoloMaxBytes      = 8 << 20
	feedFetchMaxHeaderBytes    = 64 << 10
	feedFetchMaxItems          = 100
	feedFetchTrustedMaxItems   = 1500
	feedFetchRedirectLimit     = 4
	feedUserAgent              = "Zettlab-Plaud-Action-Dashboard/1"
	feedSyncMaxConcurrent      = 3
	feedSyncSourceStripes      = 16
	feedSyncRetryBase          = 5 * time.Minute
	feedSyncRetryMax           = time.Hour
	feedSyncRecoveryBatchSize  = 64
	feedSyncRecoveryInterval   = 5 * time.Second
	feedSyncWorkerPanicMessage = "来源同步内部异常，稍后自动重试"
	bilibiliPublicUserAgent    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
)

var feedSyncWriteMu sync.Mutex
var publicDialRound uint32
var feedSyncSlots = make(chan struct{}, feedSyncMaxConcurrent)
var feedSyncSourceSlots = newFeedSyncSourceSlots()
var feedSyncRuntimeEnabled bool
var feedSyncQueue chan string
var localCustomFeedFetch feedDiscoveryFetcher = fetchPublicURLWithCache

// feedSyncQueueState tracks both queued and in-flight source IDs.  A source
// request is persisted before it reaches this in-memory queue, so a full queue
// must leave the row pending for the recovery scan instead of silently losing
// the request.  The channel reference is part of the state because tests and
// process restarts replace the queue instance.
var feedSyncQueueState struct {
	mu     sync.Mutex
	queue  chan string
	queued map[string]struct{}
}

// Source scheduling is deliberately bounded. Startup handles one small batch
// so the first page stays responsive; regular ticks continue draining due
// sources in bounded batches without creating an unbounded backlog.
const (
	feedSyncWarmupBatchSize  = 8
	feedSyncWarmupMaxSources = 64
	// The first pass must not compete with dashboard reads for minutes on a
	// weak network. Remaining due sources are picked up by the regular ticker.
	feedSyncStartupMaxSources = feedSyncWarmupBatchSize
	feedSyncStartupBudget     = 20 * time.Second
)

type feedSourceSyncFunc func(context.Context, string) error

type feedSyncBatchResult struct {
	Processed int
	Failures  int
}

func newFeedSyncSourceSlots() [feedSyncSourceStripes]chan struct{} {
	var slots [feedSyncSourceStripes]chan struct{}
	for index := range slots {
		slots[index] = make(chan struct{}, 1)
	}
	return slots
}

func prepareFeedSyncQueueStateLocked(queue chan string) {
	if feedSyncQueueState.queue != queue || feedSyncQueueState.queued == nil {
		feedSyncQueueState.queue = queue
		feedSyncQueueState.queued = make(map[string]struct{})
	}
}

func releaseFeedSyncQueueItem(queue chan string, sourceID string) {
	feedSyncQueueState.mu.Lock()
	defer feedSyncQueueState.mu.Unlock()
	if feedSyncQueueState.queue != queue {
		return
	}
	delete(feedSyncQueueState.queued, strings.TrimSpace(sourceID))
}

func clearFeedSyncQueueState(queue chan string) {
	feedSyncQueueState.mu.Lock()
	defer feedSyncQueueState.mu.Unlock()
	if feedSyncQueueState.queue == queue {
		feedSyncQueueState.queued = make(map[string]struct{})
	}
}

type fetchedFeed struct {
	Title        string
	Description  string
	SiteURL      string
	FeedURL      string
	IconURL      string
	Format       string
	Items        []NewsItem
	ETag         string
	LastModified string
}

type rssDocument struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string
	Description string
	Link        string
	ImageURL    string
	ITunesImage string
	Items       []rssItem
}

type rssChannelImage struct {
	URL string `xml:"url"`
}

// RSS channel images use the same local element name for the standard
// <image><url> form and the iTunes <image href> form. Decode them explicitly
// so feeds can provide their real source icon without confusing encoding/xml.
func (channel *rssChannel) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	*channel = rssChannel{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch strings.ToLower(value.Name.Local) {
			case "title":
				if err := decoder.DecodeElement(&channel.Title, &value); err != nil {
					return err
				}
			case "description":
				if err := decoder.DecodeElement(&channel.Description, &value); err != nil {
					return err
				}
			case "link":
				var link string
				if err := decoder.DecodeElement(&link, &value); err != nil {
					return err
				}
				if strings.TrimSpace(link) != "" {
					channel.Link = link
				}
			case "image":
				if isITunesNamespace(value.Name.Space) {
					channel.ITunesImage = firstNonEmpty(xmlAttr(&value, "href"), xmlAttr(&value, "url"))
					if err := decoder.Skip(); err != nil {
						return err
					}
				} else {
					var image rssChannelImage
					if err := decoder.DecodeElement(&image, &value); err != nil {
						return err
					}
					channel.ImageURL = image.URL
				}
			case "item":
				var item rssItem
				if err := decoder.DecodeElement(&item, &value); err != nil {
					return err
				}
				channel.Items = append(channel.Items, item)
			default:
				if err := decoder.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return nil
			}
		}
	}
}

type rssItem struct {
	Title          string        `xml:"title"`
	Link           string        `xml:"link"`
	GUID           string        `xml:"guid"`
	PubDate        string        `xml:"pubDate"`
	Published      string        `xml:"published"`
	Updated        string        `xml:"updated"`
	Description    string        `xml:"description"`
	Summary        string        `xml:"summary"`
	Content        string        `xml:"content"`
	Encoded        string        `xml:"encoded"`
	Author         string        `xml:"author"`
	Creator        string        `xml:"creator"`
	Enclosure      *rssEnclosure `xml:"enclosure"`
	Media          []rssMedia    `xml:"-"`
	Thumbnail      []rssMedia    `xml:"-"`
	ITunesImage    string        `xml:"-"`
	ITunesDuration string        `xml:"-"`
}

// RSS commonly mixes core <content> fields with media:content and
// content:encoded. encoding/xml treats the namespace prefix as part of the
// local tag unless we decode the item explicitly, which otherwise causes a
// duplicate-field error and drops the whole feed.
func (item *rssItem) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	*item = rssItem{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.StartElement:
			local := strings.ToLower(value.Name.Local)
			switch local {
			case "title":
				if err := decoder.DecodeElement(&item.Title, &value); err != nil {
					return err
				}
			case "link":
				if err := decoder.DecodeElement(&item.Link, &value); err != nil {
					return err
				}
			case "guid", "id":
				if err := decoder.DecodeElement(&item.GUID, &value); err != nil {
					return err
				}
			case "pubdate":
				if err := decoder.DecodeElement(&item.PubDate, &value); err != nil {
					return err
				}
			case "published":
				if err := decoder.DecodeElement(&item.Published, &value); err != nil {
					return err
				}
			case "updated":
				if err := decoder.DecodeElement(&item.Updated, &value); err != nil {
					return err
				}
			case "description":
				if err := decoder.DecodeElement(&item.Description, &value); err != nil {
					return err
				}
			case "summary":
				if err := decoder.DecodeElement(&item.Summary, &value); err != nil {
					return err
				}
			case "encoded":
				if err := decoder.DecodeElement(&item.Encoded, &value); err != nil {
					return err
				}
			case "content":
				if isMediaNamespace(value.Name.Space) {
					var media rssMedia
					if err := decoder.DecodeElement(&media, &value); err != nil {
						return err
					}
					item.Media = append(item.Media, media)
				} else if err := decoder.DecodeElement(&item.Content, &value); err != nil {
					return err
				}
			case "thumbnail":
				var media rssMedia
				if err := decoder.DecodeElement(&media, &value); err != nil {
					return err
				}
				item.Thumbnail = append(item.Thumbnail, media)
			case "image":
				if isITunesNamespace(value.Name.Space) {
					item.ITunesImage = firstNonEmpty(xmlAttr(&value, "href"), xmlAttr(&value, "url"))
					if item.ITunesImage == "" {
						var image struct {
							URL string `xml:"url"`
						}
						if err := decoder.DecodeElement(&image, &value); err != nil {
							return err
						}
						item.ITunesImage = image.URL
					}
				} else if err := decoder.Skip(); err != nil {
					return err
				}
			case "duration":
				if isITunesNamespace(value.Name.Space) {
					if err := decoder.DecodeElement(&item.ITunesDuration, &value); err != nil {
						return err
					}
				} else if err := decoder.Skip(); err != nil {
					return err
				}
			case "author", "creator":
				var textValue string
				if err := decoder.DecodeElement(&textValue, &value); err != nil {
					return err
				}
				if local == "creator" {
					item.Creator = textValue
				} else {
					item.Author = textValue
				}
			case "enclosure":
				var enclosure rssEnclosure
				if err := decoder.DecodeElement(&enclosure, &value); err != nil {
					return err
				}
				item.Enclosure = &enclosure
			default:
				if err := decoder.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return nil
			}
		}
	}
}

type rssEnclosure struct {
	URL    string `xml:"url,attr"`
	Type   string `xml:"type,attr"`
	Length string `xml:"length,attr"`
}

type rssMedia struct {
	URL      string `xml:"url,attr"`
	Href     string `xml:"href,attr"`
	Type     string `xml:"type,attr"`
	Medium   string `xml:"medium,attr"`
	Duration string `xml:"duration,attr"`
	Width    string `xml:"width,attr"`
}

func xmlAttr(start *xml.StartElement, name string) string {
	for _, attr := range start.Attr {
		if strings.EqualFold(attr.Name.Local, name) {
			return strings.TrimSpace(attr.Value)
		}
	}
	return ""
}

func isMediaNamespace(space string) bool {
	value := strings.ToLower(strings.TrimSpace(space))
	return strings.Contains(value, "media") || strings.Contains(value, "mrss")
}

func isITunesNamespace(space string) bool {
	value := strings.ToLower(strings.TrimSpace(space))
	return strings.Contains(value, "itunes")
}

type atomDocument struct {
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle"`
	Icon     string      `xml:"icon"`
	Logo     string      `xml:"logo"`
	Links    []atomLink  `xml:"link"`
	Entries  []atomEntry `xml:"entry"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type atomEntry struct {
	Title          string       `xml:"title"`
	ID             string       `xml:"id"`
	Summary        string       `xml:"summary"`
	Content        string       `xml:"content"`
	Published      string       `xml:"published"`
	Updated        string       `xml:"updated"`
	Authors        []atomAuthor `xml:"author"`
	Links          []atomLink   `xml:"link"`
	Media          []rssMedia   `xml:"-"`
	Thumbnail      []rssMedia   `xml:"-"`
	ITunesImage    string       `xml:"-"`
	ITunesDuration string       `xml:"-"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

func (entry *atomEntry) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	*entry = atomEntry{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.StartElement:
			local := strings.ToLower(value.Name.Local)
			switch local {
			case "title":
				if err := decoder.DecodeElement(&entry.Title, &value); err != nil {
					return err
				}
			case "id":
				if err := decoder.DecodeElement(&entry.ID, &value); err != nil {
					return err
				}
			case "summary":
				if err := decoder.DecodeElement(&entry.Summary, &value); err != nil {
					return err
				}
			case "content":
				if isMediaNamespace(value.Name.Space) {
					var media rssMedia
					if err := decoder.DecodeElement(&media, &value); err != nil {
						return err
					}
					entry.Media = append(entry.Media, media)
				} else if err := decoder.DecodeElement(&entry.Content, &value); err != nil {
					return err
				}
			case "thumbnail":
				var media rssMedia
				if err := decoder.DecodeElement(&media, &value); err != nil {
					return err
				}
				entry.Thumbnail = append(entry.Thumbnail, media)
			case "image":
				if isITunesNamespace(value.Name.Space) {
					entry.ITunesImage = firstNonEmpty(xmlAttr(&value, "href"), xmlAttr(&value, "url"))
					if entry.ITunesImage == "" {
						var image struct {
							URL string `xml:"url"`
						}
						if err := decoder.DecodeElement(&image, &value); err != nil {
							return err
						}
						entry.ITunesImage = image.URL
					}
				} else if err := decoder.Skip(); err != nil {
					return err
				}
			case "duration":
				if isITunesNamespace(value.Name.Space) {
					if err := decoder.DecodeElement(&entry.ITunesDuration, &value); err != nil {
						return err
					}
				} else if err := decoder.Skip(); err != nil {
					return err
				}
			case "published":
				if err := decoder.DecodeElement(&entry.Published, &value); err != nil {
					return err
				}
			case "updated":
				if err := decoder.DecodeElement(&entry.Updated, &value); err != nil {
					return err
				}
			case "author":
				var author atomAuthor
				if err := decoder.DecodeElement(&author, &value); err != nil {
					return err
				}
				entry.Authors = append(entry.Authors, author)
			case "link":
				var link atomLink
				if err := decoder.DecodeElement(&link, &value); err != nil {
					return err
				}
				entry.Links = append(entry.Links, link)
			default:
				if err := decoder.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return nil
			}
		}
	}
}

type jsonFeedDocument struct {
	Version     string         `json:"version"`
	Title       string         `json:"title"`
	HomePageURL string         `json:"home_page_url"`
	FeedURL     string         `json:"feed_url"`
	Icon        string         `json:"icon"`
	Favicon     string         `json:"favicon"`
	Description string         `json:"description"`
	Items       []jsonFeedItem `json:"items"`
}

type jsonFeedItem struct {
	ID            string           `json:"id"`
	URL           string           `json:"url"`
	ExternalURL   string           `json:"external_url"`
	Title         string           `json:"title"`
	ContentHTML   string           `json:"content_html"`
	ContentText   string           `json:"content_text"`
	Summary       string           `json:"summary"`
	DatePublished string           `json:"date_published"`
	DateModified  string           `json:"date_modified"`
	Author        jsonFeedAuthor   `json:"author"`
	Image         string           `json:"image"`
	BannerImage   string           `json:"banner_image"`
	Attachments   []jsonAttachment `json:"attachments"`
}

type jsonFeedAuthor struct {
	Name string `json:"name"`
}

type jsonAttachment struct {
	URL      string `json:"url"`
	MimeType string `json:"mime_type"`
	Duration int    `json:"duration_in_seconds"`
}

type feedFetchResponse struct {
	Body         []byte
	ContentType  string
	FinalURL     string
	ETag         string
	LastModified string
	NotModified  bool
}

var autodiscoveryLinkRE = regexp.MustCompile(`(?is)<link\b[^>]*>`)
var htmlMetaRE = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
var htmlAttributeRE = regexp.MustCompile(`(?is)([a-zA-Z_:][-a-zA-Z0-9_:]*)\s*=\s*["']([^"']*)["']`)
var htmlTitleRE = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
var htmlTagRE = regexp.MustCompile(`(?is)<[^>]+>`)

// Only maintained, fixed sources may use the host-configured egress proxy.
// User-added feeds stay on the IP-validating direct dialer so a proxy cannot
// turn attacker-controlled DNS into access to the device's private network.
var feedProxyHostAllowlist = map[string]struct{}{
	"about.fb.com":             {},
	"api.bilibili.com":         {},
	"api.crossref.org":         {},
	"api.folo.is":              {},
	"blog.cloudflare.com":      {},
	"blog.google":              {},
	"blog.playstation.com":     {},
	"data.epo.org":             {},
	"efts.sec.gov":             {},
	"eur-lex.europa.eu":        {},
	"export.arxiv.org":         {},
	"feeds.bbci.co.uk":         {},
	"feeds.npr.org":            {},
	"github.blog":              {},
	"hacks.mozilla.org":        {},
	"hn.algolia.com":           {},
	"img.folo.is":              {},
	"mp.weixin.qq.com":         {},
	"myanimelist.net":          {},
	"openai.com":               {},
	"patentscope.wipo.int":     {},
	"query.sse.com.cn":         {},
	"rsshub.rssforever.com":    {},
	"space.bilibili.com":       {},
	"sspai.com":                {},
	"www.bilibili.com":         {},
	"www.cninfo.com.cn":        {},
	"www.federalregister.gov":  {},
	"www.gov.cn":               {},
	"www.gutenberg.org":        {},
	"www.hkex.com.hk":          {},
	"www.nasa.gov":             {},
	"www.nature.com":           {},
	"www.openai.com":           {},
	"www.sec.gov":              {},
	"www.smashingmagazine.com": {},
	"www.technologyreview.com": {},
	"www.theverge.com":         {},
	"www.youtube.com":          {},
	"wechat2rss.xlab.app":      {},
	"weixin.sogou.com":         {},
}

func feedProxyURLForRequest(proxy *url.URL, req *http.Request) (*url.URL, error) {
	if proxy == nil || req == nil || req.URL == nil {
		return nil, nil
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(req.URL.Hostname()), "."))
	_, hostAllowed := feedProxyHostAllowlist[host]
	if !hostAllowed && !isVerifiedWeChatDirectFeedURL(req.URL.String()) {
		return nil, nil
	}
	return proxy, nil
}

func isVerifiedWeChatDirectFeedURL(raw string) bool {
	_, ok := weChatVerifiedDirectFeedPolicies[raw]
	return ok
}

func publicFeedHTTPClient() *http.Client {
	proxy := configuredHTTPSProxyURL()
	transport := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			return feedProxyURLForRequest(proxy, req)
		},
		DisableCompression:     false,
		ForceAttemptHTTP2:      true,
		DialContext:            publicDialContext,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  feedFetchTimeout,
		ExpectContinueTimeout:  1 * time.Second,
		IdleConnTimeout:        30 * time.Second,
		MaxIdleConns:           8,
		MaxIdleConnsPerHost:    2,
		MaxResponseHeaderBytes: feedFetchMaxHeaderBytes,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   feedFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= feedFetchRedirectLimit {
				return errors.New("公开来源重定向次数过多")
			}
			if !validPublicFeedURL(req.URL.String()) {
				return errors.New("公开来源重定向到不安全地址")
			}
			return nil
		},
	}
}

func publicDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	configuredProxy := isConfiguredProxyEndpoint(host, port)
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) && !configuredProxy {
			return nil, errors.New("公开来源地址解析到受限网络")
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(host, port))
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(lookupCtx, "ip", host)
	if err != nil || len(ips) == 0 {
		if err == nil {
			err = errors.New("域名没有公开地址")
		}
		return nil, err
	}
	// Prefer IPv4 edges for public feeds. Some providers (notably Bilibili)
	// return a false 412 challenge on otherwise valid IPv6 edges while their
	// IPv4 HTTP/2 edge serves the same public JSON normally.
	sort.SliceStable(ips, func(i, j int) bool {
		return ips[i].To4() != nil && ips[j].To4() == nil
	})
	if len(ips) > 1 {
		start := int(atomic.AddUint32(&publicDialRound, 1) % uint32(len(ips)))
		ips = append(append([]net.IP(nil), ips[start:]...), ips[:start]...)
	}
	var lastErr error
	for _, ip := range ips {
		if !isPublicIP(ip) && !configuredProxy {
			continue
		}
		conn, dialErr := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = errors.New("域名没有公开地址")
	}
	return nil, lastErr
}

// An explicitly configured local/system proxy is an allowed network hop. The
// public URL and redirect checks still protect the destination; this exception
// only prevents the dialer from rejecting the proxy itself as a private IP.
func isConfiguredProxyEndpoint(host, port string) bool {
	proxy := configuredHTTPSProxyURL()
	if proxy == nil {
		return false
	}
	proxyHost := strings.TrimSpace(proxy.Hostname())
	proxyPort := strings.TrimSpace(proxy.Port())
	if proxyPort == "" {
		if strings.EqualFold(proxy.Scheme, "https") {
			proxyPort = "443"
		} else {
			proxyPort = "80"
		}
	}
	if proxyHost == "" || strings.TrimSpace(port) != proxyPort {
		return false
	}
	if strings.EqualFold(host, proxyHost) {
		return true
	}
	ip, proxyIP := net.ParseIP(host), net.ParseIP(proxyHost)
	return ip != nil && proxyIP != nil && ip.Equal(proxyIP)
}

func isPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		return !(ip4[0] == 10 || ip4[0] == 127 || (ip4[0] == 169 && ip4[1] == 254) || (ip4[0] == 192 && ip4[1] == 168) || (ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) || (ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127) || (ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19)))
	}
	return true
}

func fetchPublicURL(ctx context.Context, raw string) (feedFetchResponse, error) {
	return fetchPublicURLWithCache(ctx, raw, "", "")
}

func fetchPublicURLWithCache(ctx context.Context, raw, etag, lastModified string) (feedFetchResponse, error) {
	normalized, err := normalizePublicURL(raw)
	if err != nil {
		return feedFetchResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return feedFetchResponse{}, err
	}
	req.Header.Set("User-Agent", feedUserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/feed+json, application/json, text/html;q=0.8, */*;q=0.1")
	hostname := strings.ToLower(req.URL.Hostname())
	if hostname == "api.bilibili.com" || hostname == "www.bilibili.com" || hostname == "space.bilibili.com" {
		// Bilibili's public search endpoint rejects service-identifying user agents
		// and requests without a first-party referer. This remains a public,
		// anonymous request; no account cookie or token is forwarded.
		req.Header.Set("User-Agent", bilibiliPublicUserAgent)
		req.Header.Set("Referer", "https://www.bilibili.com/")
		req.Header.Set("Origin", "https://www.bilibili.com")
		if hostname == "api.bilibili.com" {
			// The search API rejects the generic feed Accept header with a 412
			// response on some edge nodes; advertise the JSON contract explicitly.
			req.Header.Set("Accept", "application/json, text/plain, */*")
		}
	}
	if strings.TrimSpace(etag) != "" {
		req.Header.Set("If-None-Match", limitText(etag, 512))
	}
	if strings.TrimSpace(lastModified) != "" {
		req.Header.Set("If-Modified-Since", limitText(lastModified, 128))
	}
	client := publicFeedHTTPClient()
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return feedFetchResponse{}, err
	}
	defer response.Body.Close()
	finalURL := normalized
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL.String()
	}
	if response.StatusCode == http.StatusNotModified {
		return feedFetchResponse{FinalURL: finalURL, ETag: limitText(response.Header.Get("ETag"), 512), LastModified: limitText(response.Header.Get("Last-Modified"), 128), NotModified: true}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return feedFetchResponse{}, fmt.Errorf("来源返回 HTTP %d", response.StatusCode)
	}
	body, err := readPublicFeedResponseBody(response, normalized)
	if err != nil {
		return feedFetchResponse{}, err
	}
	return feedFetchResponse{Body: body, ContentType: strings.ToLower(limitText(response.Header.Get("Content-Type"), 256)), FinalURL: finalURL, ETag: limitText(response.Header.Get("ETag"), 512), LastModified: limitText(response.Header.Get("Last-Modified"), 128)}, nil
}

func readPublicFeedResponseBody(response *http.Response, requestedURL string) ([]byte, error) {
	if response == nil || response.Body == nil {
		return nil, errors.New("来源响应无内容")
	}
	maxBytes := publicFeedResponseMaxBytes(response, requestedURL)
	if response.ContentLength > maxBytes {
		return nil, errors.New("来源内容超过大小上限")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, errors.New("来源内容超过大小上限")
	}
	return body, nil
}

func publicFeedResponseMaxBytes(response *http.Response, requestedURL string) int64 {
	finalURL := requestedURL
	if response != nil && response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL.String()
	}
	if isVerifiedWeChatDirectFeedURL(finalURL) {
		return feedFetchFoloMaxBytes
	}
	parsed, err := url.Parse(finalURL)
	if err != nil {
		return feedFetchMaxBytes
	}
	switch strings.ToLower(strings.TrimSuffix(strings.TrimSpace(parsed.Hostname()), ".")) {
	case "api.folo.is", "wechat2rss.xlab.app":
		// These bounded feed adapters can include article/media HTML in each
		// item. The two verified direct feeds get the same finite allowance by
		// exact URL; arbitrary feeds retain the generic 2 MiB ceiling.
		return feedFetchFoloMaxBytes
	default:
		return feedFetchMaxBytes
	}
}

func parseFetchedFeed(response feedFetchResponse, inputURL string) (fetchedFeed, error) {
	return parseFetchedFeedWithContext(context.Background(), response, inputURL)
}

func parseFetchedFeedWithContext(ctx context.Context, response feedFetchResponse, inputURL string) (fetchedFeed, error) {
	return parseFetchedFeedWithDiscovery(ctx, response, inputURL, 0, map[string]struct{}{}, localCustomFeedFetch)
}

type feedDiscoveryFetcher func(context.Context, string, string, string) (feedFetchResponse, error)

const feedAutodiscoveryMaxDepth = 3

var errFeedAutodiscoveryCycle = errors.New("来源自动发现出现循环")
var errFeedAutodiscoveryDepth = errors.New("来源自动发现层级过深")

func parseFetchedFeedWithDiscovery(ctx context.Context, response feedFetchResponse, inputURL string, depth int, visited map[string]struct{}, fetch feedDiscoveryFetcher) (fetchedFeed, error) {
	var err error
	response, err = validateLocalCustomFeedResponse(response, inputURL)
	if err != nil {
		return fetchedFeed{}, err
	}
	currentURL := strings.TrimSpace(firstNonEmpty(response.FinalURL, inputURL))
	if normalized, err := normalizePublicURL(currentURL); err == nil {
		currentURL = normalized
	}
	if currentURL != "" {
		if _, exists := visited[currentURL]; exists {
			return fetchedFeed{}, errFeedAutodiscoveryCycle
		}
		visited[currentURL] = struct{}{}
	}
	trimmed := strings.TrimSpace(string(response.Body))
	if trimmed == "" {
		return fetchedFeed{}, errors.New("来源返回空内容")
	}
	if validFoloWechat2RSSFeedURL(currentURL) {
		feed, err := parseFoloWechat2RSSFeed(response.Body, currentURL)
		if err != nil {
			return fetchedFeed{}, err
		}
		feed.ETag, feed.LastModified = response.ETag, response.LastModified
		return feed, nil
	}
	if isFoloFeedAdapterURL(currentURL) {
		feed, err := parseFoloPublicFeed(response.Body, currentURL)
		if err != nil {
			return fetchedFeed{}, err
		}
		feed.ETag, feed.LastModified = response.ETag, response.LastModified
		return feed, nil
	}
	if strings.HasPrefix(trimmed, "{") {
		if feed, err := parseJSONFeed(response.Body, currentURL); err == nil {
			feed.ETag, feed.LastModified = response.ETag, response.LastModified
			return feed, nil
		}
	}
	if feed, err := parseXMLFeed(response.Body, currentURL); err == nil {
		feed.ETag, feed.LastModified = response.ETag, response.LastModified
		return feed, nil
	}
	if strings.Contains(response.ContentType, "html") || strings.HasPrefix(trimmed, "<") {
		candidate, title, description := discoverHTMLFeed(response.Body, currentURL)
		if candidate != "" && depth < feedAutodiscoveryMaxDepth {
			normalized, normalizeErr := normalizePublicURL(candidate)
			if normalizeErr != nil {
				return fetchedFeed{}, normalizeErr
			}
			if _, exists := visited[normalized]; exists {
				return fetchedFeed{}, errFeedAutodiscoveryCycle
			}
			if fetch == nil {
				fetch = fetchPublicURLWithCache
			}
			child, err := fetch(ctx, normalized, "", "")
			if err == nil {
				feed, parseErr := parseFetchedFeedWithDiscovery(ctx, child, normalized, depth+1, visited, fetch)
				if parseErr == nil {
					if feed.SiteURL == "" {
						feed.SiteURL = inputURL
					}
					if feed.Title == "" {
						feed.Title = title
					}
					if feed.Description == "" {
						feed.Description = description
					}
					return feed, nil
				}
				if errors.Is(parseErr, errFeedAutodiscoveryCycle) || errors.Is(parseErr, errFeedAutodiscoveryDepth) {
					return fetchedFeed{}, parseErr
				}
			}
		} else if candidate != "" {
			return fetchedFeed{}, errFeedAutodiscoveryDepth
		}
	}
	return fetchedFeed{}, errors.New("没有发现可解析的 RSS、Atom 或 JSON Feed")
}

func isFoloFeedAdapterURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(strings.TrimSuffix(parsed.Hostname(), "."), "api.folo.is") || parsed.Path != "/feeds" {
		return false
	}
	wrapped := strings.TrimSpace(parsed.Query().Get("url"))
	return wrapped != "" && validPublicFeedURL(wrapped)
}

// parseFoloPublicFeed adapts Folo's bounded public-feed response to the same
// local projection used by RSS, Atom and JSON Feed. All returned links are
// independently revalidated as public URLs before they can reach SQLite.
// Account-pinned X sources keep using parseFoloOperationalFeed, which applies
// the stricter handle and media-host allowlists required for those defaults.
func parseFoloPublicFeed(body []byte, inputURL string) (fetchedFeed, error) {
	if !isFoloFeedAdapterURL(inputURL) {
		return fetchedFeed{}, errors.New("Folo 来源地址不合法")
	}
	var response foloResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fetchedFeed{}, errors.New("Folo JSON 无法解析")
	}
	if response.Code != 0 {
		return fetchedFeed{}, errors.New("Folo 返回非成功状态")
	}
	if len(response.Data.Entries) == 0 {
		return fetchedFeed{}, errors.New("Folo 没有返回条目")
	}
	feed := fetchedFeed{
		Title:       cleanPlain(response.Data.Feed.Title),
		Description: cleanPlain(response.Data.Feed.Description),
		SiteURL:     resolvePublicLink("", response.Data.Feed.SiteURL),
		FeedURL:     inputURL,
		IconURL:     resolvePublicLink("", response.Data.Feed.Image),
		Format:      "json",
	}
	seen := make(map[string]struct{}, minInt(len(response.Data.Entries), feedFetchMaxItems))
	for _, entry := range response.Data.Entries {
		link := resolvePublicLink("", entry.URL)
		externalID := strings.TrimSpace(firstNonEmpty(entry.ID, link, entry.Title))
		if externalID == "" || link == "" {
			continue
		}
		if _, exists := seen[externalID]; exists {
			continue
		}
		seen[externalID] = struct{}{}
		thumbnailURL, mediaURL, mediaMIME, duration := "", "", "", 0
		for _, media := range entry.Media {
			mediaKind := strings.ToLower(strings.TrimSpace(media.Type))
			candidate := resolvePublicLink("", media.URL)
			if candidate == "" {
				continue
			}
			switch mediaKind {
			case "photo", "image":
				if thumbnailURL == "" {
					thumbnailURL = candidate
				}
			case "video":
				if mediaURL == "" {
					mediaURL, mediaMIME = candidate, normalizeMediaMIME("video/mp4", candidate)
					duration = parseFoloDuration(media.Duration, media.DurationSecs, media.DurationInSeconds, entry.Duration, entry.DurationSecs, entry.DurationInSeconds)
					if preview := resolvePublicLink("", media.PreviewImageURL); preview != "" {
						thumbnailURL = preview
					}
				}
			case "audio":
				if mediaURL == "" {
					mediaURL, mediaMIME = candidate, normalizeMediaMIME("audio/mpeg", candidate)
					duration = parseFoloDuration(media.Duration, media.DurationSecs, media.DurationInSeconds, entry.Duration, entry.DurationSecs, entry.DurationInSeconds)
				}
			}
		}
		item := makeFetchedNews(
			externalID,
			firstNonEmpty(entry.Title, entry.Summary, entry.Description),
			link,
			feed.Title,
			entry.Author,
			entry.PublishedAt,
			firstNonEmpty(entry.Content, entry.Description, entry.Summary),
			thumbnailURL,
			mediaURL,
			mediaMIME,
			duration,
			inputURL,
		)
		item.SourceType = "api"
		item.SourceLevel = "public_aggregator"
		feed.Items = append(feed.Items, item)
		if len(feed.Items) >= feedFetchMaxItems {
			break
		}
	}
	return finalizeFetchedFeed(feed)
}

func validateLocalCustomFeedResponse(response feedFetchResponse, requestedURL string) (feedFetchResponse, error) {
	finalURL, err := normalizePublicURL(firstNonEmpty(response.FinalURL, requestedURL))
	if err != nil {
		return feedFetchResponse{}, errors.New("来源重定向到不安全地址")
	}
	response.FinalURL = finalURL
	return response, nil
}

func parseXMLFeed(body []byte, inputURL string) (fetchedFeed, error) {
	return parseXMLFeedWithLimit(body, inputURL, feedFetchMaxItems)
}

func parseXMLFeedWithLimit(body []byte, inputURL string, itemLimit int) (fetchedFeed, error) {
	if itemLimit < 1 || itemLimit > feedFetchTrustedMaxItems {
		itemLimit = feedFetchMaxItems
	}
	var rss rssDocument
	rssDecoder := xml.NewDecoder(bytes.NewReader(body))
	rssDecoder.CharsetReader = htmlcharset.NewReaderLabel
	if err := rssDecoder.Decode(&rss); err == nil && (rss.Channel.Title != "" || len(rss.Channel.Items) > 0) {
		feed := fetchedFeed{Title: cleanPlain(rss.Channel.Title), Description: cleanPlain(rss.Channel.Description), SiteURL: resolvePublicLink(inputURL, rss.Channel.Link), FeedURL: inputURL, IconURL: resolvePublicLink(inputURL, firstNonEmpty(rss.Channel.ImageURL, rss.Channel.ITunesImage)), Format: "rss"}
		for _, item := range rss.Channel.Items {
			feed.Items = append(feed.Items, newsItemFromRSS(item, feed.Title, inputURL))
			if len(feed.Items) >= itemLimit {
				break
			}
		}
		return finalizeFetchedFeed(feed)
	}
	var atom atomDocument
	atomDecoder := xml.NewDecoder(bytes.NewReader(body))
	atomDecoder.CharsetReader = htmlcharset.NewReaderLabel
	if err := atomDecoder.Decode(&atom); err != nil {
		return fetchedFeed{}, err
	}
	if atom.Title == "" && len(atom.Entries) == 0 {
		return fetchedFeed{}, errors.New("XML 不是 RSS/Atom")
	}
	feed := fetchedFeed{Title: cleanPlain(atom.Title), Description: cleanPlain(atom.Subtitle), FeedURL: inputURL, IconURL: resolvePublicLink(inputURL, firstNonEmpty(atom.Icon, atom.Logo)), Format: "atom"}
	for _, link := range atom.Links {
		if link.Rel == "" || strings.EqualFold(link.Rel, "alternate") {
			if value := resolvePublicLink(inputURL, link.Href); value != "" {
				feed.SiteURL = value
				break
			}
		}
	}
	for _, item := range atom.Entries {
		feed.Items = append(feed.Items, newsItemFromAtom(item, feed.Title, inputURL))
		if len(feed.Items) >= itemLimit {
			break
		}
	}
	return finalizeFetchedFeed(feed)
}

func parseJSONFeed(body []byte, inputURL string) (fetchedFeed, error) {
	var document jsonFeedDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return fetchedFeed{}, err
	}
	if !strings.Contains(strings.ToLower(document.Version), "jsonfeed.org/version") || len(document.Items) == 0 && document.Title == "" {
		return fetchedFeed{}, errors.New("JSON 不是 JSON Feed")
	}
	feed := fetchedFeed{Title: cleanPlain(document.Title), Description: cleanPlain(document.Description), SiteURL: resolvePublicLink(inputURL, document.HomePageURL), FeedURL: inputURL, IconURL: resolvePublicLink(inputURL, firstNonEmpty(document.Icon, document.Favicon)), Format: "json"}
	for _, item := range document.Items {
		feed.Items = append(feed.Items, newsItemFromJSON(item, feed.Title, inputURL))
		if len(feed.Items) >= feedFetchMaxItems {
			break
		}
	}
	return finalizeFetchedFeed(feed)
}

func finalizeFetchedFeed(feed fetchedFeed) (fetchedFeed, error) {
	if strings.TrimSpace(feed.Title) == "" {
		feed.Title = urlHost(feed.FeedURL)
	}
	if feed.Format == "" {
		feed.Format = "rss"
	}
	if len(feed.Items) == 0 {
		return fetchedFeed{}, errors.New("来源没有可用条目")
	}
	return feed, nil
}

func newsItemFromRSS(item rssItem, source, baseURL string) NewsItem {
	content := firstNonEmpty(item.Encoded, item.Content, item.Description, item.Summary)
	link := resolvePublicLink(baseURL, item.Link)
	external := strings.TrimSpace(firstNonEmpty(item.GUID, link, item.Title))
	mediaURL, mediaMIME, duration := rssMediaValues(item.Enclosure, item.Media, baseURL)
	if duration == 0 {
		duration = parseMediaDuration(item.ITunesDuration)
	}
	thumbnailURL := rssThumbnailValue(item.Thumbnail, item.ITunesImage, baseURL)
	if thumbnailURL == "" {
		if item.Enclosure != nil {
			thumbnailURL = rssImageThumbnailValue([]rssMedia{{URL: item.Enclosure.URL, Type: item.Enclosure.Type}}, baseURL)
		}
	}
	if thumbnailURL == "" {
		thumbnailURL = rssImageThumbnailValue(item.Media, baseURL)
	}
	return makeFetchedNews(external, item.Title, link, source, firstNonEmpty(item.Creator, item.Author), firstNonEmpty(item.PubDate, item.Published, item.Updated), content, thumbnailURL, mediaURL, mediaMIME, duration, baseURL)
}

func newsItemFromAtom(item atomEntry, source, baseURL string) NewsItem {
	link := ""
	mediaURL, mediaMIME, duration := rssMediaValues(nil, item.Media, baseURL)
	enclosureImage := ""
	// Track the best enclosure across all link entries: an image enclosure is
	// the last-resort media attachment (picture wall) and only fills media_url
	// when nothing playable was found above.
	bestEnclosureURL, bestEnclosureMIME, bestEnclosurePriority := "", "", -1
	for _, candidate := range item.Links {
		if strings.EqualFold(candidate.Rel, "enclosure") {
			if resolved := resolvePublicLink(baseURL, candidate.Href); resolved != "" {
				mimeType := normalizeMediaMIME(candidate.Type, resolved)
				priority := mediaPriority(mimeType)
				if priority > bestEnclosurePriority {
					bestEnclosureURL, bestEnclosureMIME, bestEnclosurePriority = resolved, mimeType, priority
				}
				if enclosureImage == "" && priority == 0 {
					enclosureImage = resolved
				}
			}
			continue
		}
		if candidate.Rel == "" || strings.EqualFold(candidate.Rel, "alternate") {
			if link == "" {
				link = resolvePublicLink(baseURL, candidate.Href)
			}
		}
	}
	if mediaURL == "" && bestEnclosureURL != "" {
		mediaURL, mediaMIME = bestEnclosureURL, bestEnclosureMIME
	}
	author := ""
	if len(item.Authors) > 0 {
		author = item.Authors[0].Name
	}
	content := firstNonEmpty(item.Content, item.Summary)
	if duration == 0 {
		duration = parseMediaDuration(item.ITunesDuration)
	}
	thumbnailURL := rssThumbnailValue(item.Thumbnail, item.ITunesImage, baseURL)
	if thumbnailURL == "" {
		thumbnailURL = enclosureImage
	}
	if thumbnailURL == "" {
		thumbnailURL = rssImageThumbnailValue(item.Media, baseURL)
	}
	return makeFetchedNews(firstNonEmpty(item.ID, link, item.Title), item.Title, link, source, author, firstNonEmpty(item.Published, item.Updated), content, thumbnailURL, mediaURL, mediaMIME, duration, baseURL)
}

func newsItemFromJSON(item jsonFeedItem, source, baseURL string) NewsItem {
	link := resolvePublicLink(baseURL, firstNonEmpty(item.URL, item.ExternalURL))
	content := firstNonEmpty(item.ContentHTML, item.ContentText, item.Summary)
	mediaURL, mediaMIME, duration := jsonMediaValues(item.Attachments, baseURL)
	thumbnailURL := firstPublicLink(baseURL, item.Image, item.BannerImage)
	if thumbnailURL == "" {
		thumbnailURL = jsonImageThumbnailValue(item.Attachments, baseURL)
	}
	return makeFetchedNews(firstNonEmpty(item.ID, link, item.Title), item.Title, link, source, item.Author.Name, firstNonEmpty(item.DatePublished, item.DateModified), content, thumbnailURL, mediaURL, mediaMIME, duration, baseURL)
}

func firstPublicLink(baseURL string, values ...string) string {
	for _, value := range values {
		if resolved := resolvePublicLink(baseURL, value); resolved != "" {
			return resolved
		}
	}
	return ""
}

func makeFetchedNews(externalID, title, link, source, author, published, content, thumbnailURL, mediaURL, mediaMIME string, duration int, baseURL string) NewsItem {
	title = cleanPlain(title)
	if title == "" {
		title = "未命名条目"
	}
	if link == "" {
		link = resolvePublicLink(baseURL, "")
	}
	if link == "" {
		// A feed may expose only a GUID. Keep the item usable and auditable by
		// linking back to the public feed itself rather than inventing a host.
		link = resolvePublicLink(baseURL, baseURL)
	}
	var article extractedArticle
	parsedLink, _ := url.Parse(strings.TrimSpace(link))
	if isWeChatArticleURL(parsedLink) && strings.TrimSpace(content) != "" {
		contentType := "text/plain"
		if strings.Contains(content, "<") {
			contentType = "text/html"
		}
		article = renderEntryContent([]byte(content), contentType, firstNonEmpty(link, baseURL), link)
	}
	plain := firstNonEmpty(article.Text, cleanPlain(content))
	contentIdentity := plain
	if len([]rune(plain)) > 4000 {
		plain = string([]rune(plain)[:4000])
	}
	if published = normalizePublishedAt(published); published == "" {
		published = time.Now().UTC().Format(time.RFC3339)
	}
	mediaMIME = normalizeMediaMIME(mediaMIME, mediaURL)
	viewType := "article"
	if strings.HasPrefix(strings.ToLower(mediaMIME), "video/") {
		viewType = "video"
	} else if strings.HasPrefix(strings.ToLower(mediaMIME), "audio/") {
		viewType = "audio"
	} else if strings.HasPrefix(strings.ToLower(mediaMIME), "image/") {
		// An image media attachment classifies the entry for the picture wall.
		viewType = "picture"
	}
	hashInput := strings.Join([]string{externalID, title, link, contentIdentity, mediaURL}, "\x00")
	digest := sha256.Sum256([]byte(hashInput))
	hash := hex.EncodeToString(digest[:])
	return NewsItem{ID: stableObjectID("entry", externalID), ExternalID: limitText(externalID, 512), Title: limitText(title, 240), URL: link, Source: limitText(source, 120), Origin: "app-fetch", SourceType: "rss", ViewType: viewType, Author: limitText(cleanPlain(author), 120), PublishedAt: published, SummaryText: plain, ContentFormat: "text", SafeHTML: "", ThumbnailURL: firstNonEmpty(thumbnailURL, article.LeadImageURL), MediaURL: mediaURL, MediaMIME: limitText(mediaMIME, 120), DurationSecs: clampInt(duration, 0, 86400), ContentHash: hash, FetchedAt: time.Now().UTC().Format(time.RFC3339), feedArticle: article}
}

// persistFetchedFeedArticleTx keeps rich HTML that was already present in a
// trusted RSS item (notably WeChat2RSS content:encoded). The news projection
// deliberately stays summary-only; the bounded full-text cache is the reader
// surface and avoids fetching the WeChat page again, which is often protected
// by a captcha. Only WeChat article links are eligible.
func persistFetchedFeedArticleTx(tx *sql.Tx, entryID string, item NewsItem, now string) error {
	if strings.TrimSpace(entryID) == "" || strings.TrimSpace(item.feedArticle.SafeHTML) == "" {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(item.URL))
	if err != nil || !isWeChatArticleURL(parsed) {
		return nil
	}
	imageURLsJSON, err := json.Marshal(item.feedArticle.ImageURLs)
	if err != nil {
		return err
	}
	blocksJSON, blocksVersion, blocksHash, blocksTruncated, err := articleBlocksJSON(item.feedArticle)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_html,image_urls_json,content_format,lead_image_index,fetched_at,status,error_message,source_url,updated_at,content_hash,blocks_json,blocks_version,blocks_hash,blocks_truncated)
VALUES(?,?,?,?,?,?,?,'ok','',?,?,?,?,?,?,?)
ON CONFLICT(entry_id) DO UPDATE SET content_text=excluded.content_text,content_html=excluded.content_html,
image_urls_json=excluded.image_urls_json,content_format=excluded.content_format,lead_image_index=excluded.lead_image_index,
fetched_at=excluded.fetched_at,status='ok',error_message='',source_url=excluded.source_url,updated_at=excluded.updated_at,content_hash=excluded.content_hash,
blocks_json=excluded.blocks_json,blocks_version=excluded.blocks_version,blocks_hash=excluded.blocks_hash,blocks_truncated=excluded.blocks_truncated`,
		entryID, item.feedArticle.Text, item.feedArticle.SafeHTML, string(imageURLsJSON), extractedArticleFormat(item.feedArticle), item.feedArticle.LeadImageIndex, now, item.URL, now, item.ContentHash, blocksJSON, blocksVersion, blocksHash, boolToSQLiteInt(blocksTruncated)); err != nil {
		return err
	}
	_, _ = tx.Exec(`DELETE FROM entry_full_texts
WHERE entry_id IN (
    SELECT entry_id FROM entry_full_texts
    ORDER BY updated_at DESC, entry_id DESC
    LIMIT -1 OFFSET ?
)`, maxPersistedFullTexts)
	return nil
}

func rssMediaValues(enclosure *rssEnclosure, media []rssMedia, baseURL string) (string, string, int) {
	// Images (priority 0) are legitimate candidates: a photo feed's enclosure
	// is the entry payload and feeds the picture wall. They still lose to any
	// video/audio attachment, so playable media never regresses.
	bestURL, bestMIME, bestDuration, bestPriority := "", "", 0, -1
	consider := func(rawURL, rawType string, duration int) {
		resolved := resolvePublicLink(baseURL, rawURL)
		if resolved == "" {
			return
		}
		mimeType := normalizeMediaMIME(rawType, resolved)
		priority := mediaPriority(mimeType)
		if priority > bestPriority {
			bestURL, bestMIME, bestDuration, bestPriority = resolved, mimeType, duration, priority
		}
	}
	if enclosure != nil {
		consider(enclosure.URL, enclosure.Type, 0)
	}
	for _, item := range media {
		consider(firstNonEmpty(item.URL, item.Href), item.Type, parseMediaDuration(item.Duration))
	}
	return bestURL, bestMIME, bestDuration
}

func rssThumbnailValue(media []rssMedia, itunesImage, baseURL string) string {
	for _, item := range media {
		if resolved := resolvePublicLink(baseURL, firstNonEmpty(item.URL, item.Href)); resolved != "" {
			return resolved
		}
	}
	return resolvePublicLink(baseURL, itunesImage)
}

func rssImageThumbnailValue(media []rssMedia, baseURL string) string {
	for _, item := range media {
		resolved := resolvePublicLink(baseURL, firstNonEmpty(item.URL, item.Href))
		if resolved != "" && mediaPriority(normalizeMediaMIME(item.Type, resolved)) == 0 {
			if extension := strings.ToLower(path.Ext(firstNonEmpty(item.URL, item.Href))); extension == ".jpg" || extension == ".jpeg" || extension == ".png" || extension == ".webp" || extension == ".gif" {
				return resolved
			}
		}
	}
	return ""
}

func jsonMediaValues(attachments []jsonAttachment, baseURL string) (string, string, int) {
	var bestURL, bestMIME string
	bestDuration, bestPriority := 0, -1
	for _, attachment := range attachments {
		resolved := resolvePublicLink(baseURL, attachment.URL)
		if resolved == "" {
			continue
		}
		mimeType := normalizeMediaMIME(attachment.MimeType, resolved)
		priority := mediaPriority(mimeType)
		// Images qualify as last-resort media (picture wall); video and audio
		// still outrank them.
		if priority > bestPriority {
			bestURL, bestMIME, bestDuration, bestPriority = resolved, mimeType, attachment.Duration, priority
		}
	}
	return bestURL, bestMIME, bestDuration
}

func jsonImageThumbnailValue(attachments []jsonAttachment, baseURL string) string {
	for _, attachment := range attachments {
		resolved := resolvePublicLink(baseURL, attachment.URL)
		if resolved != "" && mediaPriority(normalizeMediaMIME(attachment.MimeType, resolved)) == 0 {
			return resolved
		}
	}
	return ""
}

func mediaPriority(mimeType string) int {
	switch {
	case strings.HasPrefix(strings.ToLower(mimeType), "video/"):
		return 3
	case strings.HasPrefix(strings.ToLower(mimeType), "audio/"):
		return 2
	case strings.HasPrefix(strings.ToLower(mimeType), "image/"):
		return 0
	default:
		return 1
	}
}

func parseMediaDuration(raw string) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}
	if strings.Contains(value, ":") {
		parts := strings.Split(value, ":")
		if len(parts) > 3 {
			return 0
		}
		seconds := 0
		for _, part := range parts {
			parsed, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || parsed < 0 || parsed > 3600 {
				return 0
			}
			seconds = seconds*60 + parsed
		}
		return seconds
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func normalizeMediaMIME(raw, mediaURL string) string {
	declared := strings.ToLower(strings.TrimSpace(strings.Split(raw, ";")[0]))
	extension := ""
	if parsed, err := url.Parse(mediaURL); err == nil {
		extension = strings.ToLower(path.Ext(parsed.Path))
	}
	inferred := map[string]string{
		".mp4": "video/mp4", ".m4v": "video/x-m4v", ".webm": "video/webm", ".mov": "video/quicktime", ".mkv": "video/x-matroska",
		".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".wav": "audio/wav", ".ogg": "audio/ogg", ".oga": "audio/ogg",
		".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp", ".gif": "image/gif",
	}[extension]
	if inferred != "" {
		// A feed's MIME is advisory.  When its broad category conflicts with a
		// well-known URL extension, prefer the extension so an image cannot be
		// rendered as a playable video (or vice versa).
		declaredCategory, inferredCategory := mediaCategory(declared), mediaCategory(inferred)
		if declared == "" || (declaredCategory != inferredCategory && !(extension == ".mp4" && declaredCategory == "audio" && inferredCategory == "video")) {
			return inferred
		}
	}
	if mediaCategory(declared) != "" {
		return declared
	}
	if inferred != "" {
		return inferred
	}
	return declared
}

func mediaCategory(mimeType string) string {
	value := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(value, "video/"):
		return "video"
	case strings.HasPrefix(value, "audio/"):
		return "audio"
	case strings.HasPrefix(value, "image/"):
		return "image"
	default:
		return ""
	}
}

func discoverHTMLFeed(body []byte, inputURL string) (string, string, string) {
	text := string(body)
	feedURL := ""
	for _, tag := range autodiscoveryLinkRE.FindAllString(text, 200) {
		attrs := map[string]string{}
		for _, match := range htmlAttributeRE.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(match[1])] = html.UnescapeString(strings.TrimSpace(match[2]))
		}
		rel := strings.ToLower(attrs["rel"])
		typeValue := strings.ToLower(attrs["type"])
		if strings.Contains(rel, "alternate") && (strings.Contains(typeValue, "rss") || strings.Contains(typeValue, "atom") || strings.Contains(typeValue, "json")) {
			feedURL = resolvePublicLink(inputURL, attrs["href"])
			if feedURL != "" {
				break
			}
		}
	}
	title := ""
	if match := htmlTitleRE.FindStringSubmatch(text); len(match) > 1 {
		title = cleanPlain(match[1])
	}
	description := ""
	for _, tag := range htmlMetaRE.FindAllString(text, 200) {
		attrs := map[string]string{}
		for _, match := range htmlAttributeRE.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(match[1])] = html.UnescapeString(strings.TrimSpace(match[2]))
		}
		name := strings.ToLower(firstNonEmpty(attrs["name"], attrs["property"]))
		if (name == "description" || name == "og:description") && attrs["content"] != "" {
			description = cleanPlain(attrs["content"])
			break
		}
	}
	return feedURL, title, description
}

func resolvePublicLink(baseURL, value string) string {
	value = strings.TrimSpace(html.UnescapeString(value))
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	if baseURL != "" {
		base, baseErr := url.Parse(baseURL)
		if baseErr == nil {
			parsed = base.ResolveReference(parsed)
		}
	}
	if !validPublicFeedURL(parsed.String()) {
		return ""
	}
	return parsed.String()
}

func normalizePublishedAt(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	layouts := []string{time.RFC3339, time.RFC3339Nano, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC850, "2006-01-02 15:04:05Z07:00", "2006-01-02"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return limitText(value, 80)
}

func cleanPlain(value string) string {
	value = html.UnescapeString(value)
	value = htmlTagRE.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

func limitText(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > max {
		return string(runes[:max])
	}
	return string(runes)
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func syncFeedSource(sourceID string) error {
	return syncFeedSourceContext(context.Background(), sourceID)
}

func feedSyncSourceSlot(sourceID string) chan struct{} {
	hash := uint32(2166136261)
	for index := 0; index < len(sourceID); index++ {
		hash ^= uint32(sourceID[index])
		hash *= 16777619
	}
	return feedSyncSourceSlots[int(hash%feedSyncSourceStripes)]
}

func withFeedSyncSourceTimeout(parent context.Context, timeout time.Duration, task func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return task(ctx)
}

// syncFeedSourceContext keeps the queue wait outside the source budget: a
// busy source cannot consume another source's 15-second network window. The
// fixed slots cap top-level fetches at three; including the existing bounded
// autodiscovery depth, raw response bodies stay below 24 MiB before parsing.
func syncFeedSourceContext(parent context.Context, sourceID string) error {
	if err := parent.Err(); err != nil {
		return err
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return errors.New("来源标识不能为空")
	}
	sourceSlot := feedSyncSourceSlot(sourceID)
	select {
	case sourceSlot <- struct{}{}:
		defer func() { <-sourceSlot }()
	case <-parent.Done():
		return parent.Err()
	}
	return withVerifiedWeChatHistorySourceSlot(parent, sourceID, func(runParent context.Context) error {
		select {
		case feedSyncSlots <- struct{}{}:
			defer func() { <-feedSyncSlots }()
		case <-runParent.Done():
			return runParent.Err()
		}
		return withFeedSyncSourceTimeout(runParent, feedSyncTimeoutForSource(sourceID), func(ctx context.Context) error {
			return syncFeedSourceWithinBudget(runParent, ctx, sourceID)
		})
	})
}

func syncFeedSourceWithinBudget(parent, ctx context.Context, sourceID string) error {
	source, err := feedSourceByID(sourceID)
	if err != nil {
		return err
	}
	switch source.CollectionMode {
	case "agent":
		return markSourceAgentRequired(sourceID, "该来源由 Agent/Connector 采集")
	case "source_gateway":
		return syncGatewaySourceWithinBudget(parent, ctx, source)
	case "app", "hybrid":
		return syncPublicFeedSourceWithinBudget(parent, ctx, source, "")
	default:
		return markSourceLocalCollectionDisabled(sourceID)
	}
}

type feedCollectionResult struct {
	response              feedFetchResponse
	feed                  fetchedFeed
	status                string
	message               string
	backfill              bool
	backfillCursor        string
	backfillCursorChanged bool
	nextSyncAfter         time.Duration
}

type feedCollectionFunc func(context.Context, FeedSource) (feedCollectionResult, error)

func collectLocalCustomFeed(ctx context.Context, source FeedSource) (feedCollectionResult, error) {
	if isRuanYiFengPublicSource(source) {
		return collectRuanYiFengPublicFeed(ctx, source)
	}
	response, err := fetchPublicURLWithRetryUsing(ctx, source.URL, source.ETag, source.LastModified, operationalFetchFunc(localCustomFeedFetch))
	if err != nil {
		return feedCollectionResult{}, err
	}
	response, err = validateLocalCustomFeedResponse(response, source.URL)
	if err != nil {
		return feedCollectionResult{}, err
	}
	return feedCollectionResult{response: response, status: "ok"}, nil
}

// collectOperationalSourceFixture keeps the removed compatibility adapters
// testable with synthetic responses. Production app/hybrid routing never calls
// this function.
func collectOperationalSourceFixture(ctx context.Context, source FeedSource) (feedCollectionResult, error) {
	operational, handled, err := collectOperationalSourceContext(ctx, source)
	if err != nil {
		return feedCollectionResult{}, err
	}
	if !handled {
		return feedCollectionResult{}, errors.New("来源没有兼容采集 fixture")
	}
	if operational.Status == "unavailable" {
		return feedCollectionResult{}, errors.New(operational.Message)
	}
	return feedCollectionResult{
		response: feedFetchResponse{
			ETag:         operational.Feed.ETag,
			LastModified: operational.Feed.LastModified,
			NotModified:  operational.NotModified,
		},
		feed:                  operational.Feed,
		status:                firstNonEmpty(operational.Status, "ok"),
		message:               operational.Message,
		backfill:              operational.Backfill,
		backfillCursor:        operational.BackfillCursor,
		backfillCursorChanged: operational.BackfillCursorChanged,
		nextSyncAfter:         operational.NextSyncAfter,
	}, nil
}

func syncPublicFeedSourceWithinBudget(parent, ctx context.Context, source FeedSource, fallbackMessage string) error {
	return syncCollectedFeedSourceWithinBudget(parent, ctx, source, fallbackMessage, collectLocalCustomFeed)
}

func syncOperationalSourceFixtureWithinBudget(parent, ctx context.Context, source FeedSource) error {
	return syncCollectedFeedSourceWithinBudget(parent, ctx, source, "", collectOperationalSourceFixture)
}

func syncCollectedFeedSourceWithinBudget(parent, ctx context.Context, source FeedSource, fallbackMessage string, collect feedCollectionFunc) error {
	sourceID := source.ID
	fail := func(message string) error {
		return markSourceSyncFailure(sourceID, joinSourceSyncMessages(fallbackMessage, message))
	}
	collected, err := collect(ctx, source)
	if err != nil {
		if parent.Err() != nil {
			return parent.Err()
		}
		return fail(errorText(err))
	}
	response, feed := collected.response, collected.feed
	collectionStatus, collectionMessage := firstNonEmpty(collected.status, "ok"), collected.message
	backfill, backfillCursor, backfillCursorChanged := collected.backfill, collected.backfillCursor, collected.backfillCursorChanged
	nextSyncAfter := collected.nextSyncAfter
	if err := ctx.Err(); err != nil {
		if parent.Err() != nil {
			return parent.Err()
		}
		return fail(err.Error())
	}
	if response.NotModified {
		now := time.Now().UTC().Format(time.RFC3339)
		next := nextFeedSyncAt(source, nextSyncAfter)
		status, lastError, requestMessage := firstNonEmpty(collectionStatus, "ok"), "", "内容未变化"
		if collectionMessage != "" {
			lastError, requestMessage = collectionMessage, collectionMessage
			if status == "ok" {
				status = "partial"
			}
		}
		if fallbackMessage != "" {
			status = "partial"
			lastError = joinSourceSyncMessages(fallbackMessage, lastError)
			requestMessage = lastError
		}
		feedSyncWriteMu.Lock()
		if _, updateErr := db.Exec(`UPDATE feed_sources SET status=?, last_sync_at=?, last_error=?, next_sync_at=?, etag=COALESCE(NULLIF(?, ''), etag), last_modified=COALESCE(NULLIF(?, ''), last_modified), updated_at=? WHERE id=?`, status, now, lastError, next, response.ETag, response.LastModified, now, sourceID); updateErr != nil {
			feedSyncWriteMu.Unlock()
			return updateErr
		}
		_ = persistSourceIconCandidate(sourceID, preferredSourceIconURL(source, fetchedFeed{}))
		_, _ = db.Exec(`UPDATE sync_requests SET status=?, completed_at=?, message=?, attempt_count=attempt_count+1, next_retry_at='' WHERE source_id=? AND status IN ('pending','retry_wait')`, status, now, requestMessage, sourceID)
		feedSyncWriteMu.Unlock()
		return nil
	}
	if feed.Title == "" && len(feed.Items) == 0 {
		var err error
		feed, err = parseFetchedFeedWithContext(ctx, response, source.URL)
		if err != nil {
			if parent.Err() != nil {
				return parent.Err()
			}
			return fail(err.Error())
		}
	}
	if err := ctx.Err(); err != nil {
		if parent.Err() != nil {
			return parent.Err()
		}
		return fail(err.Error())
	}
	if fallbackMessage != "" {
		collectionStatus = "partial"
		collectionMessage = joinSourceSyncMessages(fallbackMessage, collectionMessage)
	}
	firstSync := source.LastSyncAt == "" || source.RecordsSeen == 0 || backfill
	cutoff := time.Time{}
	if firstSync && source.BackfillDays > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(source.BackfillDays) * 24 * time.Hour)
	}
	selected := selectFetchedFeedItems(source, feed, cutoff)
	iconURL := preferredSourceIconURL(source, feed)
	now := time.Now().UTC().Format(time.RFC3339)
	feedSyncWriteMu.Lock()
	defer feedSyncWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	usedCount := 0
	for _, item := range selected {
		if item.historyMetadata {
			exists, existsErr := verifiedWeChatHistoryItemExistsTx(tx, item)
			if existsErr != nil {
				return existsErr
			}
			if exists {
				continue
			}
		} else if err := alignVerifiedWeChatItemIdentityTx(tx, &item); err != nil {
			return err
		}
		if err := upsertFetchedNewsTx(tx, item, now); err != nil {
			return err
		}
		usedCount++
	}
	next := nextFeedSyncAt(source, nextSyncAfter)
	status := collectionStatus
	message := collectionMessage
	if len(selected) == 0 {
		status = "partial"
		if message == "" {
			message = "来源可访问，但没有新的条目"
		}
	}
	if backfillCursorChanged && backfillCursor != "" && source.RetentionLimit > 0 {
		var retained int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=? AND deleted_at=''`, sourceID).Scan(&retained); err != nil {
			return err
		}
		backfillCursor, status, message = capBackfillAtRetentionLimit(retained, source.RetentionLimit, backfillCursor, status, message)
	}
	_, err = tx.Exec(`UPDATE feed_sources SET status=?, records_seen=records_seen+?, records_used=records_used+?, last_sync_at=?, last_error=?, next_sync_at=?, etag=?, last_modified=?, backfill_cursor=CASE WHEN ? THEN ? ELSE backfill_cursor END, updated_at=? WHERE id=?`, status, len(feed.Items), usedCount, now, message, next, feed.ETag, feed.LastModified, backfillCursorChanged, backfillCursor, now, sourceID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO feed_catalog_entries(source_id,site_url,icon_url,feed_format,validation_status,validated_at,catalog_origin)
VALUES(?,?,?,?,'verified',?,'app-public-fetch')
ON CONFLICT(source_id) DO UPDATE SET site_url=CASE WHEN excluded.site_url <> '' THEN excluded.site_url ELSE feed_catalog_entries.site_url END,
icon_url=CASE WHEN excluded.icon_url <> '' THEN excluded.icon_url ELSE feed_catalog_entries.icon_url END,
feed_format=excluded.feed_format, validation_status='verified', validated_at=excluded.validated_at,
catalog_origin='app-public-fetch'`, sourceID, feed.SiteURL, iconURL, feed.Format, now)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE sync_requests SET status=?, completed_at=?, message=?, attempt_count=attempt_count+1, next_retry_at='' WHERE source_id=? AND status IN ('pending','retry_wait')`, status, now, message, sourceID)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	pruneSourceNews(sourceID)
	return nil
}

func nextFeedSyncAt(source FeedSource, requested time.Duration) string {
	regular := time.Duration(source.SyncIntervalSecs) * time.Second
	if regular <= 0 {
		regular = time.Hour
	}
	if requested > 0 && requested < regular {
		regular = requested
	}
	return time.Now().UTC().Add(regular).Format(time.RFC3339)
}

func joinSourceSyncMessages(first, second string) string {
	first, second = strings.TrimSpace(first), strings.TrimSpace(second)
	if first == "" {
		return limitText(second, 500)
	}
	if second == "" || second == first {
		return limitText(first, 500)
	}
	return limitText(first+"；"+second, 500)
}

func capBackfillAtRetentionLimit(retained, retentionLimit int, cursor, status, message string) (string, string, string) {
	if retained < retentionLimit || retentionLimit <= 0 || strings.TrimSpace(cursor) == "" || verifiedWeChatHistoryCompletedCursor(cursor) {
		return cursor, status, message
	}
	cursor = ""
	if status == "partial" && strings.Contains(message, "后续同步将继续回填") {
		status, message = "ok", ""
	}
	return cursor, status, message
}

func selectFetchedFeedItems(source FeedSource, feed fetchedFeed, cutoff time.Time) []NewsItem {
	selected := make([]NewsItem, 0, len(feed.Items))
	for _, item := range feed.Items {
		if source.ID == "hacker-news" && isHackerNewsDiscussionURL(item.URL) {
			continue
		}
		item.SourceType = firstNonEmpty(item.SourceType, feed.Format, source.Kind)
		include := cutoff.IsZero()
		if !include {
			parsed, parseErr := time.Parse(time.RFC3339, item.PublishedAt)
			include = parseErr != nil || !parsed.Before(cutoff)
		}
		if include {
			item.SourceID = source.ID
			item.PermissionScope = firstNonEmpty(item.PermissionScope, "public_read")
			item.SourceLevel = firstNonEmpty(item.SourceLevel, sourceLevelForPublicFeed(source.ID))
			// Feed GUIDs are only unique within a feed. Scope the local row
			// identity by source so two feeds using the same GUID cannot
			// overwrite one another.
			item.ID = stableObjectID("entry", source.ID+"|"+item.ExternalID)
			item.Source = firstNonEmpty(source.Name, feed.Title)
			selected = append(selected, item)
		}
	}
	return selected
}

func isHackerNewsDiscussionURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	return host == "news.ycombinator.com" || host == "www.news.ycombinator.com"
}

func sourceLevelForPublicFeed(sourceID string) string {
	switch sourceID {
	case "sec-xbrl-filings", "hkex-regulatory-announcements", "cninfo-disclosures", "sse-periodic-reports",
		"govinfo-federal-register", "eurlex-legislation", "china-policy-library", "arxiv-ai",
		"crossref-research", "epo-publication-server", "cnipa-patent-data", "wipo-patentscope":
		return "official"
	default:
		return "public"
	}
}

func upsertFetchedNewsTx(tx *sql.Tx, item NewsItem, now string) error {
	if item.ExternalID == "" {
		item.ExternalID = item.ID
	}
	if item.SourceID == "" {
		return errors.New("条目缺少来源标识")
	}
	var existingID, existingHash, existingOrigin, currentTitle, currentSummary string
	var existingRevision int
	err := tx.QueryRow(`SELECT id,content_hash,origin,revision,title,summary_text FROM news WHERE source_id=? AND external_id=?`, item.SourceID, item.ExternalID).Scan(&existingID, &existingHash, &existingOrigin, &existingRevision, &currentTitle, &currentSummary)
	// A compatibility public collector may use the feed GUID while the gateway
	// uses its own entry id. Reuse the gateway row by canonical URL so fallback
	// never detaches read state, favorites or stable-context membership.
	if errors.Is(err, sql.ErrNoRows) && item.URL != "" {
		err = tx.QueryRow(`SELECT id,content_hash,origin,revision,title,summary_text FROM news
WHERE source_id=? AND url=? AND origin=?
ORDER BY published_at DESC,id LIMIT 1`, item.SourceID, item.URL, gatewayOrigin).Scan(&existingID, &existingHash, &existingOrigin, &existingRevision, &currentTitle, &currentSummary)
	}
	// Hacker News changed from RSS GUIDs to API object IDs. Reuse the old
	// app-fetched row by its canonical article URL so upgrades keep item
	// preferences and do not display the same article twice.
	if errors.Is(err, sql.ErrNoRows) && item.SourceID == "hacker-news" && item.URL != "" {
		err = tx.QueryRow(`SELECT id,content_hash,origin,revision,title,summary_text FROM news
WHERE source_id=? AND url=? AND origin='app-fetch'
ORDER BY published_at DESC,id LIMIT 1`, item.SourceID, item.URL).Scan(&existingID, &existingHash, &existingOrigin, &existingRevision, &currentTitle, &currentSummary)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	fetchedAt := firstNonEmpty(item.FetchedAt, now)
	if err == nil {
		if existingHash == item.ContentHash {
			_, updateErr := tx.Exec(`UPDATE news SET
thumbnail_url=CASE WHEN ?<>'' THEN ? ELSE thumbnail_url END,
media_url=CASE WHEN ?<>'' THEN ? ELSE media_url END,
media_mime=CASE WHEN ?<>'' THEN ? ELSE media_mime END,
view_type=CASE WHEN ?<>'' THEN ? ELSE view_type END,
fetched_at=?,permission_scope=?,source_level=?,failure_reason=?,updated_at=? WHERE id=?`,
				item.ThumbnailURL, item.ThumbnailURL, item.MediaURL, item.MediaURL,
				item.MediaMIME, item.MediaMIME, item.ViewType, item.ViewType,
				fetchedAt, item.PermissionScope, item.SourceLevel, item.FailureReason, now, existingID)
			if updateErr != nil {
				return updateErr
			}
			return persistFetchedFeedArticleTx(tx, existingID, item, now)
		}
		// A user/Agent-authored projection is authoritative over a later public
		// fetch. Preserve the current content and record the incoming version
		// for review instead of silently overwriting a deliberate edit.  Both
		// "app-fetch" and "source-gateway" are automated public-content origins
		// and may be refreshed by later automated fetches.
		if existingOrigin != "" && existingOrigin != "app-fetch" && existingOrigin != gatewayOrigin {
			currentRaw, _ := json.Marshal(map[string]any{
				"id": existingID, "source_id": item.SourceID, "external_id": item.ExternalID,
				"title": currentTitle, "summary_text": currentSummary, "content_hash": existingHash,
				"origin": existingOrigin, "revision": existingRevision,
			})
			incomingRaw, _ := json.Marshal(item)
			_, conflictErr := tx.Exec(`INSERT INTO entry_conflicts(source_id,external_id,current_revision,expected_revision,current_json,incoming_json,created_at) VALUES(?,?,?,?,?,?,?)`, item.SourceID, item.ExternalID, existingRevision, existingRevision, string(currentRaw), string(incomingRaw), now)
			if conflictErr != nil {
				return conflictErr
			}
			_, updateErr := tx.Exec(`UPDATE news SET conflict_state='needs_review',fetched_at=?,updated_at=? WHERE id=?`, fetchedAt, now, existingID)
			return updateErr
		}
		_, updateErr := tx.Exec(`UPDATE news SET external_id=?,title=?,url=?,source=?,origin=?,source_type=?,view_type=?,author=?,published_at=?,summary_text=?,content_format=?,safe_html=?,thumbnail_url=?,media_url=?,media_mime=?,duration_seconds=?,content_hash=?,fetched_at=?,permission_scope=?,source_level=?,failure_reason=?,updated_at=?,revision=revision+1 WHERE id=?`,
			item.ExternalID, item.Title, item.URL, item.Source, item.Origin, item.SourceType, item.ViewType, item.Author, item.PublishedAt, item.SummaryText, item.ContentFormat, item.SafeHTML, item.ThumbnailURL, item.MediaURL, item.MediaMIME, item.DurationSecs, item.ContentHash, fetchedAt, item.PermissionScope, item.SourceLevel, item.FailureReason, now, existingID)
		if updateErr != nil {
			return updateErr
		}
		return persistFetchedFeedArticleTx(tx, existingID, item, now)
	}
	if item.ID == "" {
		item.ID = stableObjectID("entry", item.SourceID+"|"+item.ExternalID)
	}
	_, err = tx.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.ExternalID, item.Title, item.URL, item.Source, item.SourceID, item.Origin, item.SourceType, item.ViewType, item.Author, item.PublishedAt, item.SummaryText, item.ContentFormat, item.SafeHTML, item.ThumbnailURL, item.MediaURL, item.MediaMIME, item.DurationSecs, item.ContentHash, fetchedAt, item.PermissionScope, item.SourceLevel, item.FailureReason, now)
	if err != nil {
		return err
	}
	return persistFetchedFeedArticleTx(tx, item.ID, item, now)
}

func feedSyncRetryBackoff(completedAttempts int) time.Duration {
	if completedAttempts < 0 {
		completedAttempts = 0
	}
	backoff := feedSyncRetryBase
	for attempt := 0; attempt < completedAttempts && backoff < feedSyncRetryMax; attempt++ {
		backoff *= 2
		if backoff >= feedSyncRetryMax {
			return feedSyncRetryMax
		}
	}
	return backoff
}

func markSourceSyncFailure(sourceID, message string) error {
	feedSyncWriteMu.Lock()
	defer feedSyncWriteMu.Unlock()
	completedAttempts := 0
	_ = db.QueryRow(`SELECT COALESCE(MAX(attempt_count),0) FROM sync_requests WHERE source_id=? AND status IN ('pending','retry_wait')`, sourceID).Scan(&completedAttempts)
	now := time.Now().UTC().Format(time.RFC3339)
	next := time.Now().UTC().Add(feedSyncRetryBackoff(completedAttempts)).Format(time.RFC3339)
	status := "unavailable"
	var existing int
	if countErr := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=? AND deleted_at=''`, sourceID).Scan(&existing); countErr == nil && existing > 0 {
		// A transient upstream failure must not hide a usable local snapshot.
		// Keep the retry scheduled while reporting the source as degraded.
		status = "partial"
	}
	_, err := db.Exec(`UPDATE feed_sources SET status=?, last_error=?, last_sync_at=?, next_sync_at=?, updated_at=? WHERE id=?`, status, limitText(message, 500), now, next, now, sourceID)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`UPDATE sync_requests SET status='retry_wait', completed_at=?, message=?, attempt_count=attempt_count+1, next_retry_at=? WHERE source_id=? AND status IN ('pending','retry_wait')`, now, limitText(message, 500), next, sourceID)
	return errors.New(message)
}

func markSourceAgentRequired(sourceID, message string) error {
	feedSyncWriteMu.Lock()
	defer feedSyncWriteMu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	next := time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE feed_sources SET status='partial', last_error=?, last_sync_at=?, next_sync_at=?, updated_at=? WHERE id=?`, limitText(message, 500), now, next, now, sourceID); err != nil {
		return err
	}
	_, _ = db.Exec(`UPDATE sync_requests SET status='agent_required', completed_at=?, message=?, attempt_count=attempt_count+1, next_retry_at=? WHERE source_id=? AND status IN ('pending','retry_wait')`, now, limitText(message, 500), next, sourceID)
	return nil
}

func markSourceLocalCollectionDisabled(sourceID string) error {
	feedSyncWriteMu.Lock()
	defer feedSyncWriteMu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	status := "unavailable"
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=? AND deleted_at=''`, sourceID).Scan(&existing); err == nil && existing > 0 {
		status = "partial"
	}
	if _, err := db.Exec(`UPDATE feed_sources SET status=?,last_error=?,next_sync_at='',updated_at=? WHERE id=?`, status, legacyLocalCollectionDisabledMessage, now, sourceID); err != nil {
		return err
	}
	_, _ = db.Exec(`UPDATE sync_requests SET status='superseded',completed_at=?,message=?,next_retry_at='' WHERE source_id=? AND status IN ('pending','retry_wait')`, now, legacyLocalCollectionDisabledMessage, sourceID)
	return nil
}

// triggerSourceSync keeps the historical fire-and-forget call contract. The
// result-aware helper below lets recovery distinguish a queued/covered request
// from one that must remain pending because the bounded queue was full.
func triggerSourceSync(sourceID string) {
	_ = tryTriggerSourceSync(sourceID)
}

// tryTriggerSourceSync returns true when the request is queued or already
// covered by an equivalent queued/in-flight item. False means the persisted
// request must remain for a later recovery pass because the bounded queue was
// full.
func tryTriggerSourceSync(sourceID string) bool {
	if !feedSyncRuntimeEnabled || feedSyncQueue == nil {
		return false
	}
	sourceID = strings.TrimSpace(sourceID)
	queue := feedSyncQueue
	queueBusy := false
	feedSyncQueueState.mu.Lock()
	prepareFeedSyncQueueStateLocked(queue)
	if _, alreadyQueued := feedSyncQueueState.queued[sourceID]; alreadyQueued {
		// Preserve the existing sync-all backpressure signal for callers that
		// issue a second refresh while the single sync-all job is still queued.
		queueBusy = sourceID == "" && len(queue) >= cap(queue)
		feedSyncQueueState.mu.Unlock()
		if queueBusy {
			markSyncAllQueueBusy()
		}
		return true
	}
	enqueued := false
	select {
	case queue <- sourceID:
		feedSyncQueueState.queued[sourceID] = struct{}{}
		enqueued = true
	default:
		// Source-specific requests remain pending and are picked up by the
		// bounded recovery scan. A sync-all request has no source row to use as
		// a due signal, so expose backpressure with a retry timestamp.
		queueBusy = sourceID == ""
	}
	feedSyncQueueState.mu.Unlock()
	if queueBusy {
		markSyncAllQueueBusy()
	}
	return enqueued
}

func markSyncAllQueueBusy() {
	if db == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	next := time.Now().UTC().Add(time.Minute).Format(time.RFC3339)
	_, _ = db.Exec(`UPDATE sync_requests SET status='retry_wait',completed_at=?,message='同步队列繁忙，请稍后重试',next_retry_at=? WHERE source_id='' AND status='pending'`, now, next)
}

func runFeedSyncWorker(ctx context.Context) {
	queue := feedSyncQueue
	defer clearFeedSyncQueueState(queue)
	runFeedSyncWorkers(ctx, queue, feedSyncMaxConcurrent, func(workerCtx context.Context, sourceID string) error {
		return runQueuedFeedSync(workerCtx, queue, sourceID, syncRequestedSourceContext)
	})
}

func runQueuedFeedSync(ctx context.Context, queue chan string, sourceID string, syncOne feedSourceSyncFunc) (err error) {
	// Register release first so it still runs if either the sync implementation
	// or the panic recovery bookkeeping itself panics.
	defer releaseFeedSyncQueueItem(queue, sourceID)
	defer func() {
		if recovered := recover(); recovered != nil {
			if db != nil {
				_ = markSourceSyncFailure(strings.TrimSpace(sourceID), feedSyncWorkerPanicMessage)
			}
			err = fmt.Errorf("来源同步 worker 异常: %v", recovered)
			log.Printf("%v", err)
		}
	}()
	err = syncOne(ctx, sourceID)
	if err != nil && ctx.Err() == nil {
		log.Printf("来源同步失败 source=%s: %v", sourceID, err)
	}
	return err
}

func runFeedSyncWorkers(ctx context.Context, queue <-chan string, workerCount int, syncOne feedSourceSyncFunc) {
	if workerCount <= 0 || workerCount > feedSyncMaxConcurrent {
		workerCount = feedSyncMaxConcurrent
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case sourceID, ok := <-queue:
					if !ok {
						return
					}
					_ = syncOne(ctx, sourceID)
				}
			}
		}()
	}
	workers.Wait()
}

func syncFeedSourcesBounded(ctx context.Context, sourceIDs []string, syncOne feedSourceSyncFunc) feedSyncBatchResult {
	workerCount := feedSyncMaxConcurrent
	if len(sourceIDs) < workerCount {
		workerCount = len(sourceIDs)
	}
	if workerCount == 0 {
		return feedSyncBatchResult{}
	}
	jobs := make(chan string)
	var workers sync.WaitGroup
	var resultMu sync.Mutex
	result := feedSyncBatchResult{}
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for sourceID := range jobs {
				err := syncOne(ctx, sourceID)
				resultMu.Lock()
				result.Processed++
				if err != nil {
					result.Failures++
				}
				resultMu.Unlock()
				if ctx.Err() != nil {
					return
				}
			}
		}()
	}
dispatch:
	for _, sourceID := range sourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			continue
		}
		select {
		case jobs <- sourceID:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	workers.Wait()
	return result
}

func syncRequestedSource(sourceID string) error {
	return syncRequestedSourceContext(context.Background(), sourceID)
}

func syncRequestedSourceContext(ctx context.Context, sourceID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(sourceID) != "" {
		pending, err := hasPendingSyncRequest(sourceID)
		if err != nil {
			return err
		}
		// Unsubscribe cancels queued work. A worker can already have received
		// the source id when the user clicks退订, so re-check the request row
		// before touching the network; an explicit manual sync creates a fresh
		// pending row and remains available even while unsubscribed.
		if !pending {
			return nil
		}
		return syncFeedSourceContext(ctx, sourceID)
	}
	rows, err := db.Query(`SELECT f.id FROM feed_sources f JOIN feed_subscriptions s ON s.source_id=f.id WHERE s.enabled=1 ORDER BY f.id LIMIT 100`)
	if err != nil {
		return err
	}
	ids := make([]string, 0, 32)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	result := syncFeedSourcesBounded(ctx, ids, syncFeedSourceContext)
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	status, message := "ok", fmt.Sprintf("已同步 %d 个来源", result.Processed)
	if result.Failures > 0 {
		status, message = "partial", fmt.Sprintf("已同步 %d 个来源，%d 个失败", result.Processed-result.Failures, result.Failures)
	}
	_, err = db.Exec(`UPDATE sync_requests SET status=?,completed_at=?,message=?,attempt_count=attempt_count+1,next_retry_at='' WHERE source_id='' AND status IN ('pending','retry_wait')`, status, now, message)
	return err
}

func hasPendingSyncRequest(sourceID string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM sync_requests WHERE source_id=? AND status IN ('pending','retry_wait')`, sourceID).Scan(&count)
	return count > 0, err
}

func cancelPendingSyncRequests(sourceID string) error {
	if strings.TrimSpace(sourceID) == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE sync_requests SET status='cancelled', completed_at=?, message='来源已退订', next_retry_at='' WHERE source_id=? AND status IN ('pending','retry_wait')`, now, sourceID)
	return err
}

func pruneSourceNews(sourceID string) {
	_, _ = db.Exec(`DELETE FROM news WHERE source_id=? AND published_at < datetime('now', '-' || COALESCE((SELECT retention_days FROM feed_sources WHERE id=?), 90) || ' days')
AND ('news:' || id) NOT IN (SELECT item_key FROM item_preferences WHERE starred=1 OR read_later=1)
AND NOT EXISTS (SELECT 1 FROM stable_context_items c WHERE c.owner_id=? AND c.profile_id=? AND c.state='active' AND c.item_key='news:' || news.id)`, sourceID, sourceID, appDataScope.OwnerID, appDataScope.ProfileID)
	_, _ = db.Exec(`DELETE FROM news WHERE source_id=? AND id NOT IN (SELECT id FROM news WHERE source_id=? ORDER BY published_at DESC, updated_at DESC LIMIT COALESCE((SELECT retention_limit FROM feed_sources WHERE id=?),1000))
AND ('news:' || id) NOT IN (SELECT item_key FROM item_preferences WHERE starred=1 OR read_later=1)
AND NOT EXISTS (SELECT 1 FROM stable_context_items c WHERE c.owner_id=? AND c.profile_id=? AND c.state='active' AND c.item_key='news:' || news.id)`, sourceID, sourceID, sourceID, appDataScope.OwnerID, appDataScope.ProfileID)
}

func runFeedSyncScheduler(ctx context.Context) {
	lazy := feedSyncSchedulerLazy()
	if recovered, err := recoverPendingFeedSyncRequests(ctx, feedSyncRecoveryBatchSize); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		log.Printf("来源同步待处理请求恢复失败: %v", err)
	} else if recovered > 0 {
		log.Printf("来源同步恢复 %d 个待处理请求", recovered)
	}
	if !lazy {
		startupCtx, cancelStartup := context.WithTimeout(ctx, feedSyncStartupBudget)
		syncDueFeedSourcesWith(startupCtx, feedSyncStartupMaxSources, syncDueFeedSourceBatch)
		cancelStartup()
	}
	metadataTicker := time.NewTicker(time.Minute)
	defer metadataTicker.Stop()
	recoveryTicker := time.NewTicker(feedSyncRecoveryInterval)
	defer recoveryTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-recoveryTicker.C:
			if recovered, err := recoverPendingFeedSyncRequests(ctx, feedSyncRecoveryBatchSize); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				log.Printf("来源同步待处理请求恢复失败: %v", err)
			} else if recovered > 0 {
				log.Printf("来源同步恢复 %d 个待处理请求", recovered)
			}
		case <-metadataTicker.C:
			// Metadata reconciliation is independent of article hydration.  Keep it
			// running even when lazy source sync is enabled so a cold install can
			// recover a missing feed_subscriptions projection without eagerly
			// downloading every source.
			if err := reconcileSourceGatewayWithinBudget(ctx); err != nil && !errors.Is(err, errSourceGatewayNotConfigured) {
				log.Printf("来源网关定时目录协调失败: %v", err)
			}
			if !lazy {
				syncDueFeedSources(ctx)
			}
		}
	}
}

// recoverPendingFeedSyncRequests re-enqueues persisted work that was not able
// to enter the bounded in-memory queue (or whose retry backoff has elapsed).
// It only schedules explicit sync requests; it never hydrates a source merely
// because the scheduler started, so lazy mode remains lazy.
func recoverPendingFeedSyncRequests(ctx context.Context, maxRequests int) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxRequests <= 0 || maxRequests > feedSyncRecoveryBatchSize {
		maxRequests = feedSyncRecoveryBatchSize
	}
	if db == nil || !feedSyncRuntimeEnabled || feedSyncQueue == nil {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	rows, err := db.QueryContext(ctx, `SELECT source_id
FROM sync_requests
WHERE status='pending'
   OR (status='retry_wait' AND (COALESCE(next_retry_at,'')='' OR julianday(next_retry_at) IS NULL OR julianday(next_retry_at) <= julianday('now')))
GROUP BY source_id
ORDER BY MIN(id)
LIMIT ?`, maxRequests)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, maxRequests)
	for rows.Next() {
		var sourceID string
		if err := rows.Scan(&sourceID); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, strings.TrimSpace(sourceID))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	recovered := 0
	for _, sourceID := range ids {
		if err := ctx.Err(); err != nil {
			return recovered, err
		}
		if tryTriggerSourceSync(sourceID) {
			recovered++
		}
	}
	return recovered, nil
}

// feedSyncSchedulerLazy defaults to on-demand source hydration.  The reader
// requests a source's entries when its navigation item is opened; an explicit
// POST /api/sync still queues that source immediately.  Keep an opt-in eager
// switch for legacy deployments that intentionally warm every due source at
// startup.
func feedSyncSchedulerLazy() bool {
	return strings.TrimSpace(os.Getenv("FOCUS_SPACE_LAZY_SYNC")) != "0"
}

func syncDueFeedSources(ctx context.Context) {
	syncDueFeedSourcesWith(ctx, feedSyncWarmupMaxSources, syncDueFeedSourceBatch)
}

type feedSyncBatchFunc func(context.Context, []string) feedSyncBatchResult

func syncDueFeedSourceBatch(ctx context.Context, ids []string) feedSyncBatchResult {
	return syncFeedSourcesBounded(ctx, ids, func(sourceCtx context.Context, sourceID string) error {
		err := syncFeedSourceContext(sourceCtx, sourceID)
		if err != nil && sourceCtx.Err() == nil {
			log.Printf("来源同步失败 source=%s: %v", sourceID, err)
		}
		return err
	})
}

func syncDueFeedSourcesWith(ctx context.Context, maxSources int, syncBatch feedSyncBatchFunc) {
	if maxSources <= 0 || syncBatch == nil {
		return
	}
	processed := 0
	for processed < maxSources {
		if ctx.Err() != nil {
			return
		}
		ids, err := dueFeedSourceIDs(feedSyncWarmupBatchSize)
		if err != nil || len(ids) == 0 {
			return
		}
		if remaining := maxSources - processed; remaining < len(ids) {
			ids = ids[:remaining]
		}
		result := syncBatch(ctx, ids)
		processed += result.Processed
		if result.Processed == 0 {
			return
		}
	}
}

func dueFeedSourceIDs(limit int) ([]string, error) {
	if limit <= 0 || limit > feedSyncWarmupBatchSize {
		limit = feedSyncWarmupBatchSize
	}
	rows, err := db.Query(`SELECT id FROM feed_sources WHERE collection_mode IN ('source_gateway','app','hybrid')
AND EXISTS (SELECT 1 FROM feed_subscriptions s WHERE s.source_id=feed_sources.id AND s.enabled=1)
AND (next_sync_at='' OR julianday(next_sync_at) <= julianday('now'))
ORDER BY CASE WHEN id='wechat-huxiu' THEN 0 WHEN id LIKE 'youtube-%' THEN 1 ELSE 2 END,
CASE WHEN next_sync_at='' THEN 0 ELSE 1 END, next_sync_at ASC, id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}
