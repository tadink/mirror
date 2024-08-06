package frontend

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"github.com/liuzl/gocc"
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
	"seo/mirror/logger"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Key uint

const (
	ORIGIN_UA Key = iota
	REQUEST_HOST
	ORIGIN_SCHEME
	SITE
	TARGET
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
			logger.Error(err.Error())
		}
		return
	}
	if r.URL.Path == helper.GetInjectJsPath(host) {
		w.Header().Set("Content-Type", "text/javascript;charset=utf-8")
		_, err := w.Write([]byte(config.Conf.InjectJs))
		if err != nil {
			logger.Error(err.Error())
		}
		return
	}

	ua := r.UserAgent()
	if helper.IsCrawler(ua) && !helper.IsGoodCrawler(ua) { //如果是蜘蛛但不是好蜘蛛
		w.WriteHeader(404)
		_, _ = w.Write([]byte("页面未找到"))
		return
	}
	requestHost := helper.GetHost(r)
	scheme := r.Header.Get("scheme")
	ctx := context.WithValue(r.Context(), SITE, site)
	ctx = context.WithValue(ctx, ORIGIN_UA, ua)
	ctx = context.WithValue(ctx, ORIGIN_SCHEME, scheme)
	ctx = context.WithValue(ctx, REQUEST_HOST, requestHost)
	ctx = context.WithValue(ctx, TARGET, site.targetUrl)
	r = r.WithContext(ctx)
	f.Route(w, r)
}

func (f *Frontend) Route(writer http.ResponseWriter, request *http.Request) {
	site := request.Context().Value(SITE).(*Site)
	//ua := request.Context().Value(ORIGIN_UA).(string)
	cacheKey := site.Domain + request.URL.Path + request.URL.RawQuery
	if site.CacheEnable {
		if cacheResponse := f.getCache(cacheKey, site.Domain, site.CacheTime, false); cacheResponse != nil {
			f.handleCacheResponse(cacheResponse, site, writer, request)
			return
		}
	}
	if config.Conf.UserAgent != "" {
		request.Header.Set("User-Agent", config.Conf.UserAgent)
	}
	f.proxy.ServeHTTP(writer, request)

}

func (f *Frontend) ErrorHandler(writer http.ResponseWriter, request *http.Request, e error) {
	logger.Error(request.URL.String(), e.Error())

	requestHost := request.Context().Value(REQUEST_HOST).(string)
	scheme := request.Context().Value(ORIGIN_SCHEME).(string)
	site := request.Context().Value(SITE).(*Site)
	cacheKey := site.Domain + request.URL.Path + request.URL.RawQuery
	cacheResponse := f.getCache(cacheKey, site.Domain, site.CacheTime, true)
	if cacheResponse == nil {
		writer.WriteHeader(404)
		_, _ = writer.Write([]byte("请求出错，请检查源站"))
		return
	}
	var content = cacheResponse.Body
	contentType := strings.ToLower(cacheResponse.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/html") {
		isIndexPage := helper.IsIndexPage(request.URL)
		content = site.handleHtmlResponse(content, isIndexPage, requestHost, scheme)
	} else if strings.Contains(contentType, "css") || strings.Contains(contentType, "javascript") {
		content = helper.GBK2UTF8(content, contentType)
		for index, find := range site.Finds {
			content = bytes.ReplaceAll(content, []byte(find), []byte(site.Replaces[index]))
		}
		contentStr := site.replaceHost(string(content), requestHost, scheme)
		content = []byte(contentStr)
	}

	for s, i := range cacheResponse.Header {
		writer.Header()[s] = i
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
		logger.Error("写出错误：", err.Error(), request.URL)
	}

}

func (f *Frontend) ModifyResponse(response *http.Response) error {
	requestHost := response.Request.Context().Value(REQUEST_HOST).(string)
	scheme := response.Request.Context().Value(ORIGIN_SCHEME).(string)
	site := response.Request.Context().Value(SITE).(*Site)
	if response.StatusCode == 301 || response.StatusCode == 302 {
		return site.handleRedirectResponse(response, requestHost)
	}

	cacheKey := site.Domain + response.Request.URL.Path + response.Request.URL.RawQuery
	if response.StatusCode == 200 {
		content, err := helper.ReadResponse(response)
		if err != nil {
			return err
		}
		contentType := strings.ToLower(response.Header.Get("Content-Type"))

		if strings.Contains(contentType, "text/html") {
			content = bytes.ReplaceAll(content, []byte("\u200B"), []byte(""))
			content = bytes.ReplaceAll(content, []byte("\uFEFF"), []byte(""))
			content = bytes.ReplaceAll(content, []byte("\u200D"), []byte(""))
			content = bytes.ReplaceAll(content, []byte("\u200C"), []byte(""))
			content = helper.GBK2UTF8(content, contentType)
			randomHtml := helper.RandHtml(site.Domain, scheme)

			requestPath := response.Request.URL.Path
			isIndex := helper.IsIndexPage(response.Request.URL)
			content = site.PreHandleHTML(content, isIndex, requestHost, requestPath, scheme, randomHtml)
			_ = f.setCache(cacheKey, site.Domain, response.StatusCode, response.Header, content)
			content = site.handleHtmlResponse(content, isIndex, requestHost, scheme)
			helper.WrapResponseBody(response, content)
			return nil
		} else if strings.Contains(contentType, "css") || strings.Contains(contentType, "javascript") {
			_ = f.setCache(cacheKey, site.Domain, response.StatusCode, response.Header, content)
			content = helper.GBK2UTF8(content, contentType)
			for index, find := range site.Finds {
				content = bytes.ReplaceAll(content, []byte(find), []byte(site.Replaces[index]))
			}
			contentStr := site.replaceHost(string(content), requestHost, scheme)

			content = []byte(contentStr)
			helper.WrapResponseBody(response, content)
			return nil

		}

		_ = f.setCache(cacheKey, site.Domain, response.StatusCode, response.Header, content)
		helper.WrapResponseBody(response, content)
		return nil

	}
	if response.StatusCode > 400 && response.StatusCode < 500 {
		content := []byte("访问的页面不存在")
		response.Header.Set("Content-Type", "text/plain")
		_ = f.setCache(cacheKey, site.Domain, response.StatusCode, response.Header, content)
		helper.WrapResponseBody(response, content)
	}
	return nil
}

func (site *Site) handleRedirectResponse(response *http.Response, host string) error {
	redirectUrl, err := response.Request.URL.Parse(response.Header.Get("Location"))
	scheme := response.Request.Context().Value(ORIGIN_SCHEME).(string)
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
	if config.Conf.AuthInfo == nil {
		return errors.New("已到期，请重新续期")
	}
	t, err := time.Parse("2006-01-02", config.Conf.AuthInfo.Date)
	if err != nil {
		return errors.Join(errors.New("日期格式错误"), err)
	}
	if time.Since(t) > 0 {
		return errors.New("有效期超时")
	}
	return nil
}

func (f *Frontend) handleCacheResponse(cacheResponse *CacheResponse, site *Site, writer http.ResponseWriter, request *http.Request) {
	contentType := strings.ToLower(cacheResponse.Header.Get("Content-Type"))
	requestHost := helper.GetHost(request)
	scheme := request.Context().Value(ORIGIN_SCHEME).(string)
	var content = cacheResponse.Body
	if strings.Contains(contentType, "text/html") {
		isIndexPage := helper.IsIndexPage(request.URL)
		content = site.handleHtmlResponse(content, isIndexPage, requestHost, scheme)
	} else if strings.Contains(contentType, "css") || strings.Contains(contentType, "javascript") {
		content = helper.GBK2UTF8(content, contentType)
		for index, find := range site.Finds {
			content = bytes.ReplaceAll(content, []byte(find), []byte(site.Replaces[index]))
		}
		contentStr := site.replaceHost(string(content), requestHost, scheme)
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
		logger.Error("写出错误：", err.Error(), requestHost, request.URL)
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
		target := request.In.Context().Value(TARGET).(*url.URL)
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

func (f *Frontend) getCache(requestUrl string, domain string, cacheTime int64, force bool) *CacheResponse {
	sum := sha1.Sum([]byte(requestUrl))
	hash := hex.EncodeToString(sum[:])
	dir := path.Join(config.Conf.CachePath, domain, hash[:2])
	filename := path.Join(dir, hash)
	fileInfo, err := os.Stat(filename)
	if err != nil {
		return nil
	}
	if modTime := fileInfo.ModTime(); !force && time.Now().Unix() > modTime.Unix()+cacheTime*60 {
		return nil
	}

	if file, err := os.Open(filename); err == nil {
		resp := new(CacheResponse)
		err = gob.NewDecoder(file).Decode(resp)
		if err != nil {
			logger.Error(err.Error())
			return nil
		}
		err = file.Close()
		if err != nil {
			logger.Error(err.Error())
			return nil
		}
		return resp
	}
	return nil
}

func (f *Frontend) setCache(url string, domain string, statusCode int, header http.Header, content []byte) error {
	contentType := header.Get("Content-Type")
	if strings.Contains(strings.ToLower(contentType), "charset") {
		contentPartArr := strings.Split(contentType, ";")
		header.Set("Content-Type", contentPartArr[0]+"; charset=utf-8")
	}
	header.Del("Content-Encoding")
	header.Del("Content-Security-Policy")
	resp := &CacheResponse{
		Body:       content,
		StatusCode: statusCode,
		Header:     header,
	}
	sum := sha1.Sum([]byte(url))
	hash := hex.EncodeToString(sum[:])
	dir := path.Join(config.Conf.CachePath, domain, hash[:2])
	if !helper.IsExist(dir) {
		err := os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			logger.Error("MkdirAll error", dir, err.Error())
			return err
		}
	}
	filename := path.Join(dir, hash)
	file, err := os.Create(filename)
	if err != nil {
		logger.Error("os.Create error", filename, err.Error())
		return err
	}
	defer func() {
		_ = file.Close()
	}()
	if err := gob.NewEncoder(file).Encode(resp); err != nil {
		logger.Error("gob.NewEncoder error", filename, err.Error())
		return err
	}
	return nil
}
