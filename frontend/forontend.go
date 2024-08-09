package frontend

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/liuzl/gocc"
	"golang.org/x/net/html"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"seo/mirror/config"
	"seo/mirror/db"
	"seo/mirror/helper"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Key uint

const (
	OriginUA Key = iota
	RequestHost
	OriginScheme
	SITE
	TargetUrl
	BUFFER
)

type Frontend struct {
	Sites  *sync.Map
	IpList []net.IP
	proxy  *httputil.ReverseProxy
}

var S2T *gocc.OpenCC

func InitS2T() error {
	var err error
	S2T, err = gocc.New("s2t")
	if err != nil {

		return err
	}
	return nil
}
func NewFrontend() (*Frontend, error) {
	siteConfigs, err := db.GetAll()
	if err != nil {
		return nil, err
	}
	sites := new(sync.Map)
	for i := range siteConfigs {
		site, err := NewSite(siteConfigs[i])
		if err != nil {
			return nil, err
		}
		sites.Store(siteConfigs[i].Domain, site)
	}

	ipList, err := helper.GetIPList()
	if err != nil {
		return nil, err
	}

	f := &Frontend{Sites: sites, IpList: ipList}
	f.initProxy()
	return f, nil
}

func (f *Frontend) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if authErr := f.Auth(); authErr != nil {
		_, _ = w.Write([]byte(authErr.Error()))
		return
	}
	host := helper.GetHost(r)
	site, err := f.querySite(host)
	if err != nil {
		_, err = w.Write([]byte(err.Error()))
		if err != nil {
			slog.Error(err.Error())
		}
		return
	}
	if r.URL.Path == helper.GetInjectJsPath(host) {
		w.Header().Set("Content-Type", "text/javascript;charset=utf-8")
		_, err = w.Write([]byte(config.Conf.InjectJs))
		if err != nil {
			slog.Error(err.Error())
		}
		return
	}

	ua := r.UserAgent()
	if helper.IsCrawler(ua) && !helper.IsGoodCrawler(ua) { //如果是蜘蛛但不是好蜘蛛
		w.WriteHeader(404)
		_, _ = w.Write([]byte("页面未找到"))
		return
	}
	scheme := r.Header.Get("scheme")
	buffer := bufferPool.Get().(*bytes.Buffer)
	buffer.Reset()
	ctx := context.WithValue(r.Context(), SITE, site)
	ctx = context.WithValue(ctx, OriginUA, ua)
	ctx = context.WithValue(ctx, OriginScheme, scheme)
	ctx = context.WithValue(ctx, RequestHost, host)
	ctx = context.WithValue(ctx, TargetUrl, site.targetUrl)
	ctx = context.WithValue(ctx, BUFFER, buffer)
	r = r.WithContext(ctx)
	f.Route(w, r)
	if cap(buffer.Bytes()) < 1<<20 {
		bufferPool.Put(buffer)
	}

}

func (f *Frontend) Route(writer http.ResponseWriter, request *http.Request) {
	site := request.Context().Value(SITE).(*Site)
	cacheKey := site.Domain + request.URL.Path + request.URL.RawQuery
	if site.CacheEnable {
		cache := cachePool.Get().(*CacheResponse)
		defer cachePool.Put(cache)
		cache.free()
		err := f.getCache(cacheKey, site.Domain, site.CacheTime, false, cache)
		if err == nil {
			f.handleCacheResponse(cache, site, writer, request)
			return
		}
	}
	if config.Conf.UserAgent != "" {
		request.Header.Set("User-Agent", config.Conf.UserAgent)
	}
	f.proxy.ServeHTTP(writer, request)
}

func (f *Frontend) ErrorHandler(writer http.ResponseWriter, request *http.Request, e error) {
	slog.Error("error handler", request.URL.String(), e.Error())
	site := request.Context().Value(SITE).(*Site)
	cacheKey := site.Domain + request.URL.Path + request.URL.RawQuery
	cache := cachePool.Get().(*CacheResponse)
	defer cachePool.Put(cache)
	cache.free()
	err := f.getCache(cacheKey, site.Domain, site.CacheTime, true, cache)
	if err != nil {
		writer.WriteHeader(404)
		_, _ = writer.Write([]byte("请求出错，请检查源站"))
		return
	}
	f.handleCacheResponse(cache, site, writer, request)
}

func (f *Frontend) ModifyResponse(response *http.Response) error {
	requestHost := response.Request.Context().Value(RequestHost).(string)
	scheme := response.Request.Context().Value(OriginScheme).(string)
	site := response.Request.Context().Value(SITE).(*Site)
	if response.StatusCode == 301 || response.StatusCode == 302 {
		return f.handleRedirectResponse(response, requestHost)
	}

	cacheKey := site.Domain + response.Request.URL.Path + response.Request.URL.RawQuery
	if response.StatusCode == 200 {
		buffer := response.Request.Context().Value(BUFFER).(*bytes.Buffer)
		err := helper.ReadResponse(response, buffer)
		if err != nil {
			return err
		}
		content := buffer.Bytes()
		contentType := strings.ToLower(response.Header.Get("Content-Type"))
		if strings.Contains(contentType, "text/html") {
			content = helper.GBK2UTF8(content, contentType)
			randomHtml := helper.RandHtml(site.Domain)
			err = f.setCache(cacheKey, site.Domain, response.StatusCode, response.Header, content, randomHtml)
			if err != nil {
				return err
			}
			doc, err := html.Parse(buffer)
			if err != nil {
				return err
			}

			requestPath := response.Request.URL.Path
			isIndex := helper.IsIndexPage(requestPath)
			buffer.Reset()
			content, err = site.handleHtmlResponse(doc, scheme, requestHost, requestPath, randomHtml, isIndex, buffer)
			helper.WrapResponseBody(response, content)
			return nil
		} else if strings.Contains(contentType, "css") || strings.Contains(contentType, "javascript") {
			content = helper.GBK2UTF8(content, contentType)
			err = f.setCache(cacheKey, site.Domain, response.StatusCode, response.Header, content, "")
			if err != nil {
				return err
			}
			for index, find := range site.Finds {
				content = bytes.ReplaceAll(content, []byte(find), []byte(site.Replaces[index]))
			}
			content = site.replaceHost(content, scheme, requestHost)
			helper.WrapResponseBody(response, content)
			return nil
		}
		err = f.setCache(cacheKey, site.Domain, response.StatusCode, response.Header, content, "")
		if err != nil {
			return err
		}
		helper.WrapResponseBody(response, content)
		return nil
	}
	if response.StatusCode > 400 && response.StatusCode < 500 {
		content := []byte("访问的页面不存在")
		response.Header.Set("Content-Type", "text/plain")
		helper.WrapResponseBody(response, content)
	}
	return nil
}

func (f *Frontend) handleRedirectResponse(response *http.Response, host string) error {
	redirectUrl, err := response.Request.URL.Parse(response.Header.Get("Location"))
	scheme := response.Request.Context().Value(OriginScheme).(string)
	if err != nil {
		return err
	}
	if redirectUrl.Host == response.Request.URL.Host {
		redirectUrl.Host = host
		redirectUrl.Scheme = scheme
	}
	response.Header.Set("Location", redirectUrl.String())
	return nil
}

func (f *Frontend) Auth() error {
	if !helper.Intersection(config.Conf.AuthInfo.IPList, f.IpList) {
		return errors.New("IP地址不正确")
	}
	if config.Conf.AuthInfo == nil {
		return errors.New("已到期，请重新续期")
	}
	t, err := time.Parse("2006-01-02", config.Conf.AuthInfo.Date)
	if err != nil {
		return errors.Join(errors.New("日期格式错误"), err)
	}
	if time.Since(t) > 0 {
		return errors.New("已到期，请重新续期")
	}
	return nil
}

func (f *Frontend) handleCacheResponse(cacheResponse *CacheResponse, site *Site, writer http.ResponseWriter, request *http.Request) {
	contentType := strings.ToLower(cacheResponse.Header.Get("Content-Type"))
	requestHost := helper.GetHost(request)
	requestPath := request.URL.Path
	scheme := request.Context().Value(OriginScheme).(string)
	buffer := request.Context().Value(BUFFER).(*bytes.Buffer)
	var content = cacheResponse.Body
	if strings.Contains(contentType, "text/html") {
		isIndexPage := helper.IsIndexPage(requestPath)
		doc, _ := html.Parse(bytes.NewReader(content))
		content, _ = site.handleHtmlResponse(doc, scheme, requestHost, requestPath, cacheResponse.RandomHtml, isIndexPage, buffer)
	} else if strings.Contains(contentType, "css") || strings.Contains(contentType, "javascript") {
		for index, find := range site.Finds {
			content = bytes.ReplaceAll(content, []byte(find), []byte(site.Replaces[index]))
		}
		contentStr := site.replaceHost(content, scheme, requestHost)
		content = []byte(contentStr)
	}

	for key, values := range cacheResponse.Header {
		writer.Header()[key] = values
	}
	contentLength := int64(len(content))
	writer.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	if cacheResponse.StatusCode != 0 {
		writer.WriteHeader(cacheResponse.StatusCode)
	} else {
		writer.WriteHeader(200)
	}
	_, err := writer.Write(content)
	if err != nil {
		slog.Error("写出错误", err.Error(), request.URL.String())
	}
}

func (f *Frontend) initProxy() {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			localIp := net.IPv4(0, 0, 0, 0)
			if len(f.IpList) > 0 {
				ipIndex := rand.IntN(len(f.IpList))
				localIp = f.IpList[ipIndex]
			}
			localAddr := &net.TCPAddr{IP: localIp, Port: 0, Zone: ""}
			var dialer = net.Dialer{
				LocalAddr: localAddr,
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	rewrite := func(request *httputil.ProxyRequest) {
		target := request.In.Context().Value(TargetUrl).(*url.URL)
		request.Out.Header.Set("Referer", target.Scheme+"://"+target.Host)
		request.Out.Header.Del("If-Modified-Since")
		request.Out.Header.Del("If-None-Match")
		request.SetURL(target)
	}
	f.proxy = &httputil.ReverseProxy{Rewrite: rewrite, Transport: transport}
	f.proxy.ModifyResponse = f.ModifyResponse
	f.proxy.ErrorHandler = f.ErrorHandler
}
func (f *Frontend) querySite(host string) (*Site, error) {
	hostParts := strings.Split(host, ".")
	if len(hostParts) == 1 {
		return nil, errors.New("站点不存在，请检查配置")
	}
	item, ok := f.Sites.Load(host)
	if ok {
		return item.(*Site), nil
	}
	return f.querySite(strings.Join(hostParts[1:], "."))
}

func (f *Frontend) getCache(requestUrl string, domain string, cacheTime int64, force bool, cache *CacheResponse) error {
	sum := sha1.Sum([]byte(requestUrl))
	hash := hex.EncodeToString(sum[:])
	dir := path.Join(config.Conf.CachePath, domain, hash[:2])
	filename := path.Join(dir, hash)
	fileInfo, err := os.Stat(filename)
	if err != nil {
		return err
	}
	if modTime := fileInfo.ModTime(); !force && time.Now().Unix() > modTime.Unix()+cacheTime*60*60 {
		return fmt.Errorf("%s缓存已经过期 %s", domain, requestUrl)
	}
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	err = gob.NewDecoder(file).Decode(cache)
	if err != nil {

		return err
	}
	err = file.Close()
	if err != nil {

		return err
	}
	return nil
}

func (f *Frontend) setCache(url string, domain string, statusCode int, header http.Header, content []byte, randomHtml string) error {
	contentType := header.Get("Content-Type")
	if strings.Contains(strings.ToLower(contentType), "charset") {
		contentPartArr := strings.Split(contentType, ";")
		header.Set("Content-Type", contentPartArr[0]+"; charset=utf-8")
	}
	header.Del("Content-Encoding")
	header.Del("Content-Security-Policy")
	resp := new(CacheResponse)
	resp.Header = header
	resp.Body = content
	resp.StatusCode = statusCode
	resp.RandomHtml = randomHtml
	sum := sha1.Sum([]byte(url))
	hash := hex.EncodeToString(sum[:])
	dir := path.Join(config.Conf.CachePath, domain, hash[:2])
	if !helper.IsExist(dir) {
		err := os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			slog.Error("mkdirAll error", dir, err.Error())
			return err
		}
	}
	filename := path.Join(dir, hash)
	file, err := os.Create(filename)
	if err != nil {
		slog.Error("os.Create error", filename, err.Error())
		return err
	}
	defer func() {
		_ = file.Close()
	}()
	if err := gob.NewEncoder(file).Encode(resp); err != nil {
		slog.Error("gob.NewEncoder error", filename, err.Error())
		return err
	}
	return nil
}
