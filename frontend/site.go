package frontend

import (
	"bytes"
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
}

func (cr *CacheResponse) free() {
	cr.Header = make(http.Header)
	if len(cr.Body) > 1<<20 {
		cr.Body = nil
	} else {
		cr.Body = cr.Body[:0]
	}
}

var cachePool = sync.Pool{New: func() any {
	return new(CacheResponse)
}}

var needIdAttrTags = []string{"address", "th", "tfoot", "tbody", "pre", "legend", "form", "h5", "h6", "h4", "h3", "h2", "h1", "dd", "dl", "dt", "fieldset", "caption", "div", "ol", "ul", "li", "p", "table", "tr", "td", "article", "aside", "nav", "header", "main", "section", "footer", "hgroup"}

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
func (site *Site) addNodeIdAttr(node *html.Node) {
	hasId := false
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "id") {
			hasId = true
		}
	}
	if !hasId && slices.Contains(needIdAttrTags, node.Data) {
		node.Attr = append(node.Attr, html.Attribute{Key: "id", Val: helper.RandStr(4, 8)})
	}
}
func (site *Site) addRandomHTML(node *html.Node) {
	if node.Data != "body" {
		return
	}
	node.InsertBefore(&html.Node{
		Type: html.TextNode,
		Data: "{{random_html}}",
	}, node.FirstChild)
}
func (site *Site) preDealNode(node *html.Node) {
	site.addNodeIdAttr(node)
	site.addRandomHTML(node)
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		site.preDealNode(c)
	}
}
func (site *Site) PreHandleHTML(document *html.Node, randomHtml string) ([]byte, error) {
	for c := document.FirstChild; c != nil; c = c.NextSibling {
		site.preDealNode(c)
	}

	var buff bytes.Buffer
	err := html.Render(&buff, document)
	if err != nil {
		return nil, err
	}
	content := bytes.Replace(buff.Bytes(), []byte("{{random_html}}"), []byte(randomHtml), 1)
	return content, nil
}

func (site *Site) handleHtmlResponse(document *html.Node, scheme, requestHost, requestPath string, randomHtml string, isIndexPage bool) ([]byte, error) {
	site.handleHtmlContent(document, requestHost, scheme, requestPath, isIndexPage)
	var buff bytes.Buffer
	err := html.Render(&buff, document)
	if err != nil {
		return nil, err
	}
	content := site.ParseTemplateTags(buff.Bytes(), scheme, requestHost, randomHtml, isIndexPage)
	return content, nil

}

func (site *Site) handleHtmlContent(document *html.Node, scheme, requestHost, requestPath string, isIndexPage bool) {
	replacedH1 := false
	for c := document.FirstChild; c != nil; c = c.NextSibling {
		site.handleHtmlNode(c, requestHost, requestPath, scheme, isIndexPage, &replacedH1)
		if !replacedH1 && c.FirstChild != nil && c.FirstChild.NextSibling != nil {
			c.FirstChild.NextSibling.InsertBefore(&html.Node{
				Type: html.ElementNode,
				Data: "h1",
				FirstChild: &html.Node{
					Type: html.TextNode,
					Data: "{{h1_replace:default<--null-->}}",
				},
			}, c.FirstChild.NextSibling.FirstChild)

		}
	}
}
func (site *Site) handleHtmlNode(node *html.Node, requestHost string, requestPath, scheme string, isIndexPage bool, replacedH1 *bool) {
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
		if node.Data == "h1" && node.FirstChild != nil && node.FirstChild.Type == html.TextNode {
			node.FirstChild.Data = fmt.Sprintf("{{h1_replace:default<--%s-->}}", node.FirstChild.Data)
			*replacedH1 = true
		}
		site.transformNodeAttr(node)

	default:

	}
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		site.handleHtmlNode(c, requestHost, requestPath, scheme, isIndexPage, replacedH1)
	}
}

func (site *Site) ParseTemplateTags(content []byte, scheme, requestHost, randomHtml string, isIndexPage bool) []byte {
	contentStr := string(content)
	contentStr = site.replaceHost(contentStr, scheme, requestHost)
	injectJs := ""
	if config.Conf.AdDomains[site.Domain] {
		injectJs = fmt.Sprintf(`<script type="text/javascript" src="%s"></script>`, helper.GetInjectJsPath(requestHost))
	}
	contentStr = strings.Replace(contentStr, "{{inject_js}}", injectJs, 1)

	if isIndexPage {
		friendLink := site.friendLink(site.Domain)
		contentStr = strings.Replace(contentStr, "{{friend_links}}", friendLink, 1)
		contentStr = strings.Replace(contentStr, "{{index_title}}", site.IndexTitle, 1)
		contentStr = strings.Replace(contentStr, "{{index_keywords}}", site.IndexKeywords, 1)
		contentStr = strings.Replace(contentStr, "{{index_description}}", site.IndexDescription, 1)
	}
	contentStr = strings.Replace(contentStr, "{{random_html}}", randomHtml, 1)
	h1regexp, _ := regexp.Compile(`\{\{h1_replace:default<--(.*?)-->}}`)
	h1Tag := h1regexp.FindStringSubmatch(contentStr)
	if h1Tag[1] == "null" {
		if site.H1Replace == "" {
			contentStr = strings.Replace(contentStr, "<h1>"+h1Tag[0]+"</h1>", site.H1Replace, 1)
		} else {
			contentStr = strings.Replace(contentStr, h1Tag[0], site.H1Replace, 1)
		}

	} else {
		if site.H1Replace == "" {
			contentStr = strings.Replace(contentStr, h1Tag[0], h1Tag[1], 1)
		} else {
			contentStr = strings.Replace(contentStr, h1Tag[0], site.H1Replace, 1)
		}
	}

	keywordRegexp, _ := regexp.Compile(`\{\{keyword:(\d+)}}`)
	keywordTags := keywordRegexp.FindAllStringSubmatch(contentStr, -1)
	for _, keywordTag := range keywordTags {
		index, err := strconv.Atoi(keywordTag[1])
		if err != nil {
			continue
		}
		contentStr = strings.ReplaceAll(contentStr, keywordTag[0], config.Conf.Keywords[index])
	}
	replaceRegexp, _ := regexp.Compile(`\{\{replace:(\d+)}}`)
	replaceTags := replaceRegexp.FindAllStringSubmatch(contentStr, -1)
	for _, replaceTag := range replaceTags {
		index, err := strconv.Atoi(replaceTag[1])
		if err != nil {
			continue
		}
		contentStr = strings.ReplaceAll(contentStr, replaceTag[0], site.Replaces[index])
	}
	return []byte(contentStr)
}

func (site *Site) transformText(text string) string {
	for index, find := range site.Finds {
		tag := fmt.Sprintf("{{replace:%d}}", index)
		text = strings.ReplaceAll(text, find, tag)
	}
	if site.S2t {
		chineseRegexp, _ := regexp.Compile("^[\u4e00-\u9fa5]+")
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
	for i, attr := range node.Attr {
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
	}

}
func (site *Site) transformBodyNode(node *html.Node, isIndexPage bool) {
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
		if u.Path == "" {
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

func (site *Site) replaceHost(content string, scheme, requestHost string) string {
	u := site.targetUrl
	originHost := u.Host
	content = strings.ReplaceAll(content, originHost, requestHost)
	if scheme == "https" {
		content = strings.ReplaceAll(content, "http://"+requestHost, "https://"+requestHost)
	} else {
		content = strings.ReplaceAll(content, "https://"+requestHost, "http://"+requestHost)
	}

	hostParts := strings.Split(originHost, ".")
	if len(hostParts) >= 3 {
		originHost = strings.Join(hostParts[1:], ".")
	}
	subDomainRegexp, _ := regexp.Compile(`[a-zA-Z0-9]+\.` + originHost)
	content = subDomainRegexp.ReplaceAllString(content, "")
	content = strings.ReplaceAll(content, originHost, site.Domain)
	return content
}

func (site *Site) friendLink(domain string) string {
	if len(config.Conf.FriendLinks[domain]) <= 0 {
		return ""
	}
	var friendLink string
	for _, link := range config.Conf.FriendLinks[domain] {
		linkItem := strings.Split(link, ",")
		if len(linkItem) != 2 {
			continue
		}
		friendLink += fmt.Sprintf("<a href='%s' target='_blank'>%s</a>", linkItem[0], linkItem[1])
	}
	return fmt.Sprintf("<div style='display:none'>%s</div>", friendLink)
}
