package frontend

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/net/html"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"seo/mirror/config"
	"seo/mirror/db"
	"seo/mirror/helper"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type Site struct {
	*db.SiteConfig
	targetUrl *url.URL
}

type CacheResponse struct {
	StatusCode int
	Body       []byte
	Header     http.Header
	RandomHtml string
	RandomIds  []string
}

func (cr *CacheResponse) free() {
	cr.Header = make(http.Header)
	if cap(cr.Body) > 1<<20 {
		cr.Body = nil
	} else {
		cr.Body = cr.Body[:0]
	}
}

var cachePool = sync.Pool{New: func() any {
	return new(CacheResponse)
}}
var bufferPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
var needIdAttrTags = []string{"address", "th", "tfoot", "tbody", "pre", "legend", "form", "h5", "h6", "h4", "h3", "h2", "h1", "dd", "dl", "dt", "fieldset", "caption", "div", "ol", "ul", "li", "p", "table", "tr", "td", "article", "aside", "nav", "header", "main", "section", "footer", "hgroup"}
var chineseRegexp = regexp.MustCompile("^[\u4e00-\u9fa5]+")
var keywordRegexp = regexp.MustCompile(`\{\{keyword:(\d+)}}`)
var replaceRegexp = regexp.MustCompile(`\{\{replace:(\d+)}}`)

func NewSite(siteConfig *db.SiteConfig) (*Site, error) {
	u, err := url.Parse(siteConfig.Url)
	if err != nil {
		return nil, errors.Join(errors.New("源站url错误"), err)
	}
	siteConfig.IndexTitle = helper.HtmlEntities(siteConfig.IndexTitle)
	siteConfig.IndexKeywords = helper.HtmlEntities(siteConfig.IndexKeywords)
	siteConfig.IndexDescription = helper.HtmlEntities(siteConfig.IndexDescription)
	for _, item := range config.Conf.GlobalReplace {
		siteConfig.Replaces = append(siteConfig.Replaces, item["replace"])
		siteConfig.Finds = append(siteConfig.Finds, item["needle"])
	}
	for i, replace := range siteConfig.Replaces {
		siteConfig.Replaces[i] = helper.HtmlEntities(replace)
	}

	site := &Site{SiteConfig: siteConfig, targetUrl: u}

	return site, nil
}
func (site *Site) handleHtmlResponse(document *html.Node, scheme, requestHost, requestPath, randomHtml string, isIndexPage bool, buffer *bytes.Buffer) ([]byte, error) {
	site.handleHtmlNode(document, scheme, requestHost, requestPath, isIndexPage)
	err := html.Render(buffer, document)
	if err != nil {
		return nil, err
	}
	content := site.ParseTemplateTags(buffer.Bytes(), scheme, requestHost, randomHtml, isIndexPage)
	return content, nil

}

func (site *Site) handleHtmlNode(node *html.Node, scheme, requestHost, requestPath string, isIndexPage bool) {
	switch node.Type {
	case html.TextNode, html.CommentNode, html.RawNode:
		node.Data = site.transformText(node.Data)
	case html.ElementNode:
		if node.Data == "a" {
			site.transformANode(node, scheme, requestHost, requestPath)
		}
		if node.Data == "link" {
			site.transformLinkNode(node, requestHost)
		}
		if node.Data == "title" {
			site.transformTitleNode(node, isIndexPage)
		}
		if node.Data == "script" {
			site.transformScriptNode(node)
		}
		if node.Data == "meta" {
			site.transformMetaNode(node, isIndexPage)
		}
		if node.Data == "body" {
			site.transformBodyNode(node, isIndexPage)
		}
		if node.Data == "head" {
			site.transformHeadNode(node)
		}
		if node.Data == "h1" && node.FirstChild != nil && node.FirstChild.Type == html.TextNode && site.H1Replace != "" {
			node.FirstChild.Data = site.H1Replace
		}
		site.transformNodeAttr(node)
	default:
	}
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		site.handleHtmlNode(c, scheme, requestHost, requestPath, isIndexPage)
	}

}

func (site *Site) ParseTemplateTags(content []byte, scheme, requestHost, randomHtml string, isIndexPage bool) []byte {
	content = site.replaceHost(content, scheme, requestHost)
	contentStr := string(content)
	injectJs := ""
	if scheme == "https" {
		injectJs += `<meta http-equiv="Content-Security-Policy" content="upgrade-insecure-requests">`
	}
	if config.Conf.AdDomains[site.Domain] {
		injectJs += fmt.Sprintf(`<script type="text/javascript" src="%s"></script>`, helper.GetInjectJsPath(requestHost))
	}
	contentStr = strings.Replace(contentStr, "{{inject_js}}", injectJs, 1)
	if isIndexPage {
		friendLink := helper.FriendLink(site.Domain)
		contentStr = strings.Replace(contentStr, "{{friend_links}}", friendLink, 1)
		contentStr = strings.Replace(contentStr, "{{index_title}}", site.IndexTitle, 1)
		if strings.Contains(contentStr, "{{index_description}}") {
			contentStr = strings.Replace(contentStr, "{{index_description}}", site.IndexDescription, 1)
		} else {
			r := fmt.Sprintf(`</title><meta name="description" content="%s">`, site.IndexDescription)
			contentStr = strings.Replace(contentStr, "</title>", r, 1)
		}
		if strings.Contains(contentStr, "{{index_keywords}}") {
			contentStr = strings.Replace(contentStr, "{{index_keywords}}", site.IndexKeywords, 1)
		} else {
			r := fmt.Sprintf(`</title><meta name="keywords" content="%s">`, site.IndexKeywords)
			contentStr = strings.Replace(contentStr, "</title>", r, 1)
		}
	}
	randomHtml = strings.ReplaceAll(randomHtml, "{{scheme}}", scheme)
	contentStr = strings.Replace(contentStr, "{{random_html}}", randomHtml, 1)
	if !strings.Contains(contentStr, "<h1") {
		contentStr = strings.Replace(contentStr, "{{h1_tag}}", site.H1Replace, 1)
	}
	keywordTags := keywordRegexp.FindAllStringSubmatch(contentStr, -1)
	for _, keywordTag := range keywordTags {
		index, err := strconv.Atoi(keywordTag[1])
		if err != nil {
			continue
		}
		contentStr = strings.ReplaceAll(contentStr, keywordTag[0], config.Conf.Keywords[index])
	}
	replaceTags := replaceRegexp.FindAllStringSubmatch(contentStr, -1)
	for _, replaceTag := range replaceTags {
		index, err := strconv.Atoi(replaceTag[1])
		if err != nil {
			continue
		}
		contentStr = strings.ReplaceAll(contentStr, replaceTag[0], site.Replaces[index])
	}
	result := []byte(contentStr)
	return result
}

func (site *Site) transformText(text string) string {
	for index, find := range site.Finds {
		tag := fmt.Sprintf("{{replace:%d}}", index)
		text = strings.ReplaceAll(text, find, tag)
	}
	if site.S2t {
		text = chineseRegexp.ReplaceAllStringFunc(text, func(s string) string {
			result, _ := S2T.Convert(s)
			return result
		})
	}
	return text
}
func (site *Site) transformHeadNode(node *html.Node) {
	node.AppendChild(&html.Node{
		Type: html.TextNode,
		Data: "{{inject_js}}",
	})

}

func (site *Site) transformNodeAttr(node *html.Node) {
	hasId := false
	var attrString bytes.Buffer
	attrString.WriteString(node.Data)
	for i, attr := range node.Attr {
		attrString.WriteString(attr.Key + attr.Val)
		if strings.EqualFold(attr.Key, "title") ||
			strings.EqualFold(attr.Key, "alt") ||
			strings.EqualFold(attr.Key, "value") ||
			strings.EqualFold(attr.Key, "placeholder") ||
			strings.EqualFold(attr.Key, "content") {
			for index, find := range site.Finds {
				tag := fmt.Sprintf("{{replace:%d}}", index)
				attr.Val = strings.ReplaceAll(attr.Val, find, tag)
			}
			node.Attr[i].Val = attr.Val
			if site.S2t {
				node.Attr[i].Val, _ = S2T.Convert(attr.Val)
			}

		}
		if strings.EqualFold(attr.Key, "id") {
			hasId = true
		}

	}
	if slices.Contains(needIdAttrTags, node.Data) && !hasId {
		sum := md5.Sum(attrString.Bytes())
		h := hex.EncodeToString(sum[:])
		id := ""
		if len(site.Domain) > 16 {
			id = h[:8]
		} else {
			id = h[:6]
		}
		node.Attr = append(node.Attr, html.Attribute{Key: "id", Val: id})
	}

}
func (site *Site) transformBodyNode(node *html.Node, isIndexPage bool) {
	tag := "{{random_html}}"
	if site.H1Replace != "" {
		tag = "{{h1_tag}}{{random_html}}"
	}
	node.InsertBefore(&html.Node{
		Type: html.TextNode,
		Data: tag,
	}, node.FirstChild)

	if !isIndexPage {
		return
	}
	node.AppendChild(&html.Node{
		Type: html.TextNode,
		Data: "{{friend_links}}",
	})
}
func (site *Site) transformANode(node *html.Node, scheme, requestHost, requestPath string) {
	ou, _ := url.Parse(site.Url)
	ou.Path = requestPath
	for i, attr := range node.Attr {
		if !strings.EqualFold(attr.Key, "href") || attr.Val == "" {
			continue
		}

		u, _ := ou.Parse(attr.Val)
		if u == nil {
			break
		}
		if u.Host == ou.Host {
			u.Scheme = scheme
			u.Host = requestHost
			node.Attr[i].Val = u.String()
			break
		}
		if u.Path == "" || u.Path == "/" {
			//path为空，是友情链接，全部删除
			//node.Attr[i].Val = "#"
			node.Attr[i].Val = "#"
			break
		}
		//不是友情链接，只删除链接，不删除文字
		node.Attr[i].Val = "#"
		break
	}
}

func (site *Site) transformLinkNode(node *html.Node, requestHost string) {
	isAlternate := false
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "rel") && strings.EqualFold(attr.Val, "alternate") {
			isAlternate = true
			break
		}
	}
	if !isAlternate {
		return
	}
	for i, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "href") {
			node.Attr[i].Val = "//" + requestHost
			break
		}
	}
}

func (site *Site) transformTitleNode(node *html.Node, isIndexPage bool) {
	if isIndexPage {
		node.FirstChild = &html.Node{
			Type: html.TextNode,
			Data: "{{index_title}}",
		}
		return
	}
	if !isIndexPage &&
		site.TitleReplace &&
		len(config.Conf.Keywords) > 0 &&
		node.FirstChild != nil &&
		node.FirstChild.Type == html.TextNode {
		title := node.FirstChild.Data
		randIndex := rand.IntN(len(config.Conf.Keywords))
		d := []rune(title)
		length := strings.Count(title, "")
		n := rand.IntN(length)
		tag := fmt.Sprintf("{{keyword:%d}}", randIndex)
		title = string(d[:n]) + tag + string(d[n:])
		node.FirstChild.Data = title
	}
}

func (site *Site) transformScriptNode(node *html.Node) {
	if site.NeedJs {
		return
	}
	if node.FirstChild != nil && node.FirstChild.Type == html.TextNode {
		node.FirstChild.Data = ""
	}
	for i, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "src") {
			node.Attr[i].Val = ""
			break
		}
	}

}

func (site *Site) transformMetaNode(node *html.Node, isIndexPage bool) {
	content := ""
	for i, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "name") && strings.EqualFold(attr.Val, "keywords") && isIndexPage {
			content = "{{index_keywords}}"
			break
		}
		if strings.EqualFold(attr.Key, "name") && strings.EqualFold(attr.Val, "description") && isIndexPage {
			content = "{{index_description}}"
			break
		}
		if strings.EqualFold(attr.Key, "http-equiv") && strings.EqualFold(attr.Val, "content-type") {
			content = "text/html; charset=UTF-8"
			break
		}
		if strings.EqualFold(attr.Key, "http-equiv") && strings.EqualFold(attr.Val, "Content-Security-Policy") {
			content = "*"
			break
		}
		if strings.EqualFold(attr.Key, "charset") {
			node.Attr[i].Val = "UTF-8"
		}
	}
	if content == "" {
		return
	}
	for i, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "content") {
			node.Attr[i].Val = content
		}
	}

}

func (site *Site) replaceHost(content []byte, scheme, requestHost string) []byte {
	originHost := site.targetUrl.Host
	content = bytes.ReplaceAll(content, []byte(originHost), []byte(requestHost))
	if scheme == "https" {
		content = bytes.ReplaceAll(content, []byte("http://"+requestHost), []byte("https://"+requestHost))
	} else {
		content = bytes.ReplaceAll(content, []byte("https://"+requestHost), []byte("http://"+requestHost))
	}
	hostParts := strings.Split(originHost, ".")
	if len(hostParts) >= 3 {
		originHost = strings.Join(hostParts[1:], ".")
	}
	s := []byte("." + originHost)
	if bytes.Contains(content, s) {
		content = bytes.ReplaceAll(content, s, []byte(""))
	}
	content = bytes.ReplaceAll(content, []byte(originHost), []byte(site.Domain))
	return content
}
