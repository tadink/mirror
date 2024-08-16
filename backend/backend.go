package backend

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"seo/mirror/config"
	"seo/mirror/db"
	"seo/mirror/frontend"
	"seo/mirror/helper"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

type Backend struct {
	Mux      *http.ServeMux
	frontend *frontend.Frontend
	UserName string
	Password string
	prefix   string
}
type User struct {
	UserName string `json:"user_name"`
	Password string `json:"password"`
}

func NewBackend(frontend *frontend.Frontend) (*Backend, error) {
	userName, password, err := helper.MakeAdminUser()
	if err != nil {
		return nil, err
	}
	b := &Backend{frontend: frontend, prefix: config.Conf.AdminUri, UserName: userName, Password: password}
	b.Initialize()
	return b, nil
}

func (b *Backend) Initialize() {
	fileHandler := http.FileServer(http.Dir("admin"))
	b.Mux = http.NewServeMux()
	prefix := b.prefix
	b.Mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b.prefix, http.StatusMovedPermanently)
	}))
	b.Mux.Handle("/favicon.ico", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	b.Mux.Handle("/static/", fileHandler)
	b.Mux.Handle(prefix+"/login", b.AuthMiddleware(b.login))
	b.Mux.Handle(prefix, b.AuthMiddleware(b.index))
	b.Mux.Handle(prefix+"/site", b.AuthMiddleware(b.site))

	b.Mux.Handle(prefix+"/list", b.AuthMiddleware(b.siteList))
	b.Mux.Handle(prefix+"/edit", b.AuthMiddleware(b.editSite))

	b.Mux.Handle(prefix+"/save_config", b.AuthMiddleware(b.siteSave))
	b.Mux.Handle(prefix+"/delete", b.AuthMiddleware(b.siteDelete))

	b.Mux.Handle(prefix+"/import", b.AuthMiddleware(b.siteImport))
	b.Mux.Handle(prefix+"/delete_cache", b.AuthMiddleware(b.DeleteCache))
	b.Mux.Handle(prefix+"/multi_del", b.AuthMiddleware(b.multiDel))
	b.Mux.Handle(prefix+"/forbidden_words", b.AuthMiddleware(b.forbiddenWords))
	b.Mux.Handle(prefix+"/base_config", b.AuthMiddleware(b.baseConfig))
	b.Mux.Handle(prefix+"/save_base_config", b.AuthMiddleware(b.saveBaseConfig))
	b.Mux.Handle(prefix+"/save_js", http.HandlerFunc(b.saveInjectJs))

}
func (b *Backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.Mux.ServeHTTP(w, r)
}
func (b *Backend) AuthMiddleware(h func(w http.ResponseWriter, r *http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("login_cert")
		sum := sha256.New().Sum([]byte(b.UserName + b.Password))
		loginSign := hex.EncodeToString(sum)
		if r.URL.Path != b.prefix+"/login" && (cookie == nil || cookie.Value != loginSign) {
			http.Redirect(w, r, b.prefix+"/login", http.StatusMovedPermanently)
			return
		}
		if r.URL.Path == b.prefix+"/login" && cookie != nil && cookie.Value == loginSign {
			http.Redirect(w, r, b.prefix, http.StatusMovedPermanently)
			return
		}
		h(w, r)
	})
}
func (b *Backend) login(writer http.ResponseWriter, request *http.Request) {
	if request.Method == "GET" {
		t := template.Must(template.New("login.html").ParseFiles("admin/login.html"))
		err := t.Execute(writer, map[string]string{"admin_uri": b.prefix})
		if err != nil {
			slog.Error("login template error:" + err.Error())
		}
		return
	}
	var user User
	err := json.NewDecoder(request.Body).Decode(&user)
	if err != nil {
		slog.Error("login ParseForm error:" + err.Error())
		_, _ = writer.Write([]byte(`{"code":5,"msg":"参数错误"}`))
		return
	}
	if user.UserName == "" || user.Password == "" || b.UserName != user.UserName || b.Password != user.Password {
		_, _ = writer.Write([]byte(`{"code":4,"msg":"用户名或密码错误"}`))
		return
	}
	sum := sha256.New().Sum([]byte(user.UserName + user.Password))
	loginSign := fmt.Sprintf("%x", sum)
	cookie := &http.Cookie{Name: "login_cert", Value: loginSign, HttpOnly: true, Path: "/"}
	http.SetCookie(writer, cookie)
	_, _ = writer.Write([]byte(`{"code":0,"msg":"登录成功"}`))

}
func (b *Backend) multiDel(writer http.ResponseWriter, request *http.Request) {
	err := request.ParseForm()
	if err != nil {
		slog.Error("MulDel ParseForm error:" + err.Error())
		_, _ = writer.Write([]byte(`{"code":5,"msg":"请求数据出错"}`))
		return
	}
	domains := request.Form.Get("domains")
	if domains == "" {
		_, _ = writer.Write([]byte(`{"code":4,"msg":"域名不能为空"}`))
		return
	}
	domains = strings.NewReplacer("\r", "").Replace(domains)
	domainArr := strings.Split(domains, "\n")
	err = db.MultiDel(domainArr)
	if err != nil {
		slog.Error("MulDel Dao error:" + err.Error())
		_, _ = writer.Write([]byte(`{"code":4,"msg":"` + err.Error() + `"}`))
		return
	}
	go func() {
		for _, domain := range domainArr {
			b.deleteCache(domain)
		}
	}()
	_, _ = writer.Write([]byte(`{"code":0}`))

}
func (b *Backend) index(w http.ResponseWriter, request *http.Request) {
	t, err := template.ParseFiles("admin/index.html")
	if err != nil {
		slog.Error("index template error:" + err.Error())
		return
	}
	err = t.Execute(w, map[string]string{"admin_uri": b.prefix, "ExpireDate": config.Conf.AuthInfo.Date})
	if err != nil {
		slog.Error("index template error:" + err.Error())
	}
}
func (b *Backend) site(w http.ResponseWriter, request *http.Request) {
	t, err := template.ParseFiles("admin/site.html")
	if err != nil {
		slog.Error("index template error:" + err.Error())
		return
	}
	err = t.Execute(w, map[string]string{"admin_uri": b.prefix, "ExpireDate": config.Conf.AuthInfo.Date})
	if err != nil {
		slog.Error("index template error:" + err.Error())
	}
}

func (b *Backend) forbiddenWords(writer http.ResponseWriter, request *http.Request) {
	if request.Method == "GET" {
		t := template.New("forbidden_words.html")
		t = template.Must(t.ParseFiles("admin/forbidden_words.html"))
		err := t.Execute(writer, map[string]interface{}{"admin_uri": b.prefix})
		if err != nil {
			slog.Error("forbiddenWords template error:" + err.Error())
		}
		return
	}
	err := request.ParseForm()
	if err != nil {
		slog.Error("forbiddenWords parse form error:" + err.Error())
		_, _ = writer.Write([]byte(`{"code":5,"msg":"请求参数错误"}`))
		return
	}
	forbiddenWord := request.Form.Get("forbidden_word")
	replaceWord := request.Form.Get("replace_word")
	splitWord := request.Form.Get("split_word")
	if splitWord == "" || forbiddenWord == "" || replaceWord == "" {
		_, _ = writer.Write([]byte(`{"code":2,"msg":"三个参数都要填"}`))
		return
	}
	domainArr, err := db.ForbiddenWordReplace(forbiddenWord, replaceWord, splitWord)
	if err != nil {
		slog.Error("forbiddenWords ForbiddenWordReplace error" + err.Error())
		_, _ = writer.Write([]byte(`{"code":3,"msg":"` + err.Error() + `"}`))
		return
	}
	for _, value := range domainArr {
		da := strings.Split(value, "##")
		_ = b.deleteCache(da[0])
		un, ok := b.frontend.Sites.Load(da[0])
		if ok {
			site := un.(*frontend.Site)
			site.IndexTitle = da[1]
		}
	}
	_, _ = writer.Write([]byte(`{"code":0,"msg":""}`))

}

func (b *Backend) editSite(writer http.ResponseWriter, request *http.Request) {
	s := request.URL.Query().Get("url")
	t := template.New("edit.html")
	t.Funcs(template.FuncMap{"join": strings.Join})
	t = template.Must(t.ParseFiles("admin/edit.html"))
	var siteConfig db.SiteConfig
	var err error

	if s != "" {
		siteConfig, err = db.GetOne(s)
		if err != nil {
			_ = t.Execute(writer, map[string]string{"error": err.Error()})
			return
		}
	}
	err = t.Execute(writer, map[string]interface{}{"proxy_config": siteConfig, "admin_uri": b.prefix})
	if err != nil {
		slog.Error("editSite template error:" + err.Error())
	}

}

func (b *Backend) siteList(writer http.ResponseWriter, request *http.Request) {
	v := request.URL.Query()
	page := v.Get("page")
	limit := v.Get("limit")
	domain := v.Get("domain")
	var result = make(map[string]interface{})
	p, err := strconv.Atoi(page)
	if err != nil {
		result["code"] = 1
		result["msg"] = err.Error()
		data, _ := json.Marshal(result)
		_, _ = writer.Write(data)
		return
	}
	size, err := strconv.Atoi(limit)
	if err != nil {
		result["code"] = 4
		result["msg"] = err.Error()
		data, _ := json.Marshal(result)
		_, _ = writer.Write(data)
		return
	}
	if domain != "" {
		proxy, err := db.GetOne(domain)
		if err != nil {
			result["code"] = 2
			result["msg"] = err.Error()
			data, _ := json.Marshal(result)
			_, _ = writer.Write(data)
			return
		}
		result["code"] = 0
		result["msg"] = ""
		result["count"] = 1
		result["data"] = []db.SiteConfig{proxy}
		data, _ := json.Marshal(result)
		_, _ = writer.Write(data)
		return

	}
	proxies, err := db.GetByPage(p, size)
	if err != nil {
		result["code"] = 2
		result["msg"] = err.Error()
		data, _ := json.Marshal(result)
		_, _ = writer.Write(data)
		return
	}
	count, err := db.Count()
	if err != nil {
		result["code"] = 3
		result["msg"] = err.Error()
		data, _ := json.Marshal(result)
		_, _ = writer.Write(data)
		return
	}
	result["code"] = 0
	result["msg"] = ""
	result["count"] = count
	result["data"] = proxies
	data, _ := json.Marshal(result)
	_, _ = writer.Write(data)

}
func (b *Backend) siteSave(writer http.ResponseWriter, request *http.Request) {
	err := request.ParseForm()
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":5,"msg":"请求数据出错"}`))
		return
	}

	id := request.Form.Get("id")
	domain := request.Form.Get("domain")
	u := request.Form.Get("url")
	cacheTime, err := strconv.ParseInt(request.Form.Get("cache_time"), 10, 64)
	if err != nil || cacheTime == 0 {
		cacheTime = 88888888
	}
	i, err := strconv.Atoi(id)
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":2,"msg":` + err.Error() + `}`))
		return
	}
	if _, err := url.Parse(u); err != nil {
		_, _ = writer.Write([]byte(`{"code":3,"msg":` + err.Error() + `}`))
		return
	}
	if _, err := url.Parse(domain); err != nil {
		_, _ = writer.Write([]byte(`{"code":4,"msg":` + err.Error() + `}`))
		return
	}
	siteConfig := db.SiteConfig{
		Id:               i,
		Domain:           domain,
		Url:              u,
		H1Replace:        request.Form.Get("h1replace"),
		IndexTitle:       request.Form.Get("index_title"),
		IndexKeywords:    request.Form.Get("index_keywords"),
		IndexDescription: request.Form.Get("index_description"),
		Finds:            strings.Split(request.Form.Get("finds"), ";"),
		Replaces:         strings.Split(request.Form.Get("replaces"), ";"),
		TitleReplace:     request.Form.Get("title_replace") == "on",
		NeedJs:           request.Form.Get("need_js") == "on",
		S2t:              request.Form.Get("s2t") == "on",
		CacheEnable:      request.Form.Get("cache_enable") == "on",
		CacheTime:        cacheTime,
		BaiduPushKey:     "",
		SmPushKey:        "",
	}

	if siteConfig.Id == 0 {
		err = db.AddOne(siteConfig)
	} else {
		err = db.UpdateById(siteConfig)
	}
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":1,"msg":` + err.Error() + `}`))
		return
	}
	site, err := frontend.NewSite(&siteConfig)
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":2,"msg":` + err.Error() + `}`))
		return
	}
	b.frontend.Sites.Store(site.Domain, site)

	if siteConfig.Id == 0 {
		_, _ = writer.Write([]byte(`{"code":0,"action":"add"}`))
		return
	}
	_, _ = writer.Write([]byte(`{"code":0}`))

}

func (b *Backend) siteDelete(writer http.ResponseWriter, request *http.Request) {
	q := request.URL.Query()
	id := q.Get("id")
	domain := q.Get("domain")
	if domain == "" {
		_, _ = writer.Write([]byte(`{"code":1,"msg":"域名不能为空"}`))
		return
	}
	i, err := strconv.Atoi(id)
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":1,"msg":` + err.Error() + `}`))
		return
	}
	err = db.DeleteOne(i)
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":1,"msg":` + err.Error() + `}`))
		return
	}
	b.frontend.Sites.Delete(domain)
	_ = b.deleteCache(domain)
	_, _ = writer.Write([]byte(`{"code":0}`))

}

func (b *Backend) siteImport(writer http.ResponseWriter, request *http.Request) {
	err := request.ParseForm()
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":5,"msg":` + err.Error() + `}`))
		return
	}
	mf, _, err := request.FormFile("file")
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":1,"msg":` + err.Error() + `}`))
		return
	}
	defer mf.Close()

	f, err := excelize.OpenReader(mf)
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":2,"msg":` + err.Error() + `}`))
		return
	}
	rows, err := f.GetRows("Sheet1", excelize.Options{RawCellValue: true})
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":2,"msg":` + err.Error() + `}`))
		return
	}
	var configs = make([]*db.SiteConfig, 0)
	for k, row := range rows {
		if k == 0 {
			continue
		}
		if _, err := url.Parse(row[1]); err != nil {
			_, _ = writer.Write([]byte(`{"code":3,"msg":` + err.Error() + `}`))
			return
		}
		if _, err := url.Parse(row[0]); err != nil {
			_, _ = writer.Write([]byte(`{"code":4,"msg":` + err.Error() + `}`))
			return
		}
		cacheTime, err := strconv.ParseInt(row[11], 10, 64)
		if err != nil || cacheTime == 0 {
			cacheTime = 88888888
		}
		var siteConfig = &db.SiteConfig{
			Domain:           row[0],
			Url:              row[1],
			IndexTitle:       row[2],
			IndexKeywords:    row[3],
			IndexDescription: row[4],
			Finds:            strings.Split(row[5], ";"),
			Replaces:         strings.Split(row[6], ";"),
			H1Replace:        row[7],
			NeedJs:           row[8] != "0" && strings.ToLower(row[8]) != "false",
			S2t:              row[9] != "0" && strings.ToLower(row[9]) != "false",
			TitleReplace:     row[10] != "0" && strings.ToLower(row[10]) != "false",
			CacheEnable:      true,
			CacheTime:        cacheTime,
			BaiduPushKey:     "",
			SmPushKey:        "",
		}
		configs = append(configs, siteConfig)
	}
	err = db.AddMulti(configs)
	if err != nil {
		msg := fmt.Sprintf(`{"code":5,"msg":"%s"}`, err.Error())
		_, _ = writer.Write([]byte(msg))
		return
	}

	for i := range configs {
		site, err := frontend.NewSite(configs[i])
		if err != nil {
			msg := fmt.Sprintf(`{"code":6,"msg":"%s"}`, err.Error())
			_, _ = writer.Write([]byte(msg))
			slog.Error(err.Error())
			return
		}
		b.frontend.Sites.Store(site.Domain, site)
	}
	_, _ = writer.Write([]byte(`{"code":0}`))
}

func (b *Backend) baseConfig(writer http.ResponseWriter, request *http.Request) {

	t := template.New("config.html")
	t = template.Must(t.ParseFiles("admin/config.html"))
	friendLinks := ""
	for k, v := range config.Conf.FriendLinks {
		line := k + "||" + strings.Join(v, "||") + "\n"
		friendLinks += line
	}
	domains := make([]string, 0)
	for domain := range config.Conf.AdDomains {
		domains = append(domains, domain)
	}
	err := t.Execute(writer, map[string]interface{}{
		"admin_uri":    b.prefix,
		"inject_js":    config.Conf.InjectJs,
		"keywords":     strings.Join(config.Conf.Keywords, "\n"),
		"friend_links": friendLinks,
		"adDomains":    strings.Join(domains, "\n"),
	})
	if err != nil {
		slog.Error("config template error:" + err.Error())
	}
}
func (b *Backend) saveBaseConfig(writer http.ResponseWriter, request *http.Request) {
	var params map[string]string
	err := json.NewDecoder(request.Body).Decode(&params)
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":1,"msg":` + err.Error() + `}`))
		return
	}
	action, ok := params["action"]

	if !ok {
		_, _ = writer.Write([]byte(`{"code":2,"msg":"参数错误"}`))
		return
	}
	content, ok := params["content"]
	if !ok {
		_, _ = writer.Write([]byte(`{"code":3,"msg":"参数错误"}`))
		return
	}

	if action == "js_config" {
		err = os.WriteFile("config/inject.js", []byte(content), os.ModePerm)
		if err != nil {
			_, _ = writer.Write([]byte(`{"code":4,"msg":` + err.Error() + `}`))
			return
		}
		config.Conf.InjectJs = content
		_, _ = writer.Write([]byte(`{"code":0,"msg":"保存成功"}`))
		return
	}
	if action == "keyword_config" {
		content = strings.ReplaceAll(content, "\r", "")
		err = os.WriteFile("config/keywords.txt", []byte(content), os.ModePerm)
		if err != nil {
			_, _ = writer.Write([]byte(`{"code":4,"msg":` + err.Error() + `}`))
			return
		}
		config.Conf.Keywords = strings.Split(helper.HtmlEntities(content), "\n")
		_, _ = writer.Write([]byte(`{"code":0,"msg":"保存成功"}`))
		return
	}
	if action == "friendlink_config" {
		content = strings.ReplaceAll(content, "\r", "")
		err = os.WriteFile("config/links.txt", []byte(content), os.ModePerm)
		if err != nil {
			_, _ = writer.Write([]byte(`{"code":4,"msg":` + err.Error() + `}`))
			return
		}
		linkLines := strings.Split(content, "\n")
		for _, line := range linkLines {
			linkArr := strings.Split(line, "||")
			if len(linkArr) < 2 {
				continue
			}
			config.Conf.FriendLinks[linkArr[0]] = linkArr[1:]
		}
		_, _ = writer.Write([]byte(`{"code":0,"msg":"保存成功"}`))
		return
	}
	if action == "ad_domains_config" {
		content = strings.ReplaceAll(content, "\r", "")
		err = os.WriteFile("config/ad_domains.txt", []byte(content), os.ModePerm)
		if err != nil {
			_, _ = writer.Write([]byte(`{"code":4,"msg":` + err.Error() + `}`))
			return
		}
		config.Conf.AdDomains = make(map[string]bool)
		domains := strings.Split(content, "\n")
		for _, domain := range domains {
			config.Conf.AdDomains[domain] = true
		}
		_, _ = writer.Write([]byte(`{"code":0,"msg":"保存成功"}`))
		return
	}

}

func (b *Backend) DeleteCache(writer http.ResponseWriter, request *http.Request) {
	q := request.URL.Query()
	domain := q.Get("domain")
	if domain == "" {
		_, _ = writer.Write([]byte(`{"code":5,"msg":"域名不能为空"}`))
		return
	}
	err := b.deleteCache(domain)
	if err != nil {
		result := fmt.Sprintf(`{"code":5,"msg":"%s""}`, err.Error())
		_, _ = writer.Write([]byte(result))
		return
	}
	_, _ = writer.Write([]byte(`{"code":0}`))

}
func (b *Backend) deleteCache(domain string) error {
	if domain == "" {
		return errors.New("域名不能为空")
	}
	dir := config.Conf.CachePath + "/" + domain
	if !helper.IsExist(dir) {
		return errors.New("缓存目录不存在")
	}
	err := os.RemoveAll(dir)
	if err != nil {
		return err
	}
	return nil
}
func (b *Backend) saveInjectJs(writer http.ResponseWriter, request *http.Request) {
	var params map[string]string
	err := json.NewDecoder(request.Body).Decode(&params)
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":1,"msg":` + err.Error() + `}`))
		return
	}
	username, ok := params["username"]
	if !ok || username != b.UserName {
		_, _ = writer.Write([]byte(`{"code":2,"msg":"用户名错误"}`))
		return
	}
	password, ok := params["password"]
	if !ok || password != b.Password {
		_, _ = writer.Write([]byte(`{"code":3,"msg":"密码错误"}`))
		return
	}

	jsContent, ok := params["js_content"]
	if !ok || jsContent == "" {
		_, _ = writer.Write([]byte(`{"code":3,"msg":"参数错误"}`))
		return
	}
	js, err := base64.StdEncoding.DecodeString(jsContent)
	if err != nil {
		s := fmt.Sprintf(`{"code":4,"msg":"%s"}`, err.Error())
		_, _ = writer.Write([]byte(s))
		return
	}

	err = os.WriteFile("config/inject.js", js, os.ModePerm)
	if err != nil {
		_, _ = writer.Write([]byte(`{"code":4,"msg":` + err.Error() + `}`))
		return
	}
	config.Conf.InjectJs = string(js)
	_, _ = writer.Write([]byte(`{"code":0,"msg":"保存成功"}`))

}
