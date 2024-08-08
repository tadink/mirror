package config

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"time"

	"github.com/wenzhenxi/gorsa"
)

type Config struct {
	Port          string              `json:"port"`
	AdminPort     string              `json:"admin_port"`
	CachePath     string              `json:"cache_path"`
	Spider        []string            `json:"spider"`
	GoodSpider    []string            `json:"good_spider"`
	AdminUri      string              `json:"admin_uri"`
	UserAgent     string              `json:"user_agent"`
	GlobalReplace []map[string]string `json:"global_replace"`
	InjectJsPath  string              `json:"inject_js_path"`
	Keywords      []string
	InjectJs      string
	FriendLinks   map[string][]string
	AdDomains     map[string]bool
	AuthInfo      *AuthInfo
}

type AuthInfo struct {
	IPList []string `json:"ip_list"`
	Date   string   `json:"date"`
}

var Conf *Config

func Init() error {
	var err error
	Conf, err = parseAppConfig()
	if err != nil {
		return err
	}
	return nil

}

func parseAppConfig() (*Config, error) {
	var conf = new(Config)
	data, err := os.ReadFile("config/config.json")
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data, &conf)
	if err != nil {
		return nil, err
	}
	//关键字文件
	keywordData, err := os.ReadFile("config/keywords.txt")
	if err == nil && len(keywordData) > 0 {
		conf.Keywords = strings.Split(strings.Replace(string(keywordData), "\r", "", -1), "\n")
	}
	//统计js
	js, err := os.ReadFile("config/inject.js")
	if err == nil {
		conf.InjectJs = string(js)
	}
	authInfo, err := getAuthInfo()
	if err != nil {
		return nil, err
	}
	conf.AuthInfo = authInfo
	//友情链接文本
	conf.FriendLinks = readLinks()
	conf.AdDomains = adDomains()

	return conf, nil
}

func adDomains() map[string]bool {
	adDomainData, err := os.ReadFile("config/ad_domains.txt")
	adDomains := make(map[string]bool)
	if err != nil || len(adDomainData) == 0 {
		return adDomains

	}
	domains := strings.Split(strings.ReplaceAll(string(adDomainData), "\r", ""), "\n")
	for _, domain := range domains {
		adDomains[domain] = true
	}
	return adDomains
}

func getAuthInfo() (*AuthInfo, error) {
	pubKey := `-----BEGIN PUBLIC KEY-----
	MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAsfUtexjm9RVM5CpijrNF
	NDI4NfCyMIxW9q+/QaBXiNbqoguWYh1Mmkt+tal6QqObyvmufAbMfJpj0b+cGm96
	KYgAOXUntYAKkTvQLQoQQl9aGY/rxEPuVu+nvN0zsVHrDteaWpMu+7O6OyYS0aKL
	nWhCYpobTp6MTheMfnlMi7p2pJmGxyvUvZNvv6O6OZelOyr7Pb1FeYzpc/8+vkmK
	BGnbyK6EVbZ5vwTaw/X2DI4uDOneKU2qVUyq2nd7pSvbX9aSuQZq1xwWhIXcEY6l
	XzFBxZbhjXaZkaO2CWTHLwcKtSCCd3PkXNCRWQeHM4OelRZJajKSxwcWWTqbusGC
	2wIDAQAB
	-----END PUBLIC KEY-----`
	data, err := os.ReadFile("config/auth.cert")
	if err != nil {
		return nil, errors.Join(errors.New("鉴权文件读取错误"), err)
	}
	result, err := gorsa.PublicDecrypt(string(data), pubKey)
	if err != nil {
		return nil, errors.Join(errors.New("解密错误"), err)
	}
	var authInfo AuthInfo
	err = json.Unmarshal([]byte(result), &authInfo)
	if err != nil {
		return nil, errors.Join(errors.New("json 解析错误"), err)
	}
	t, err := time.Parse("2006-01-02", authInfo.Date)
	if err != nil {
		return nil, errors.Join(errors.New("日期格式错误"), err)
	}
	if time.Since(t) > 0 {
		return nil, errors.New("有效期超时")
	}
	localIP, _ := getLocalAddresses()
	if len(localIP) < 1 {
		return nil, errors.New("未获取到有效IP")
	}
	if !Intersection(localIP, authInfo.IPList) {
		return nil, errors.New("IP地址验证不通过")
	}
	return &AuthInfo{Date: "2025-12-31"}, nil
}

func readLinks() map[string][]string {
	result := make(map[string][]string)
	linkData, err := os.ReadFile("config/links.txt")
	if err != nil && len(linkData) <= 0 {
		return result
	}
	linkLines := strings.Split(strings.ReplaceAll(string(linkData), "\r", ""), "\n")
	for _, line := range linkLines {
		linkArr := strings.Split(line, "||")
		if len(linkArr) < 2 {
			continue
		}
		result[linkArr[0]] = linkArr[1:]
	}
	return result
}
func getLocalAddresses() ([]string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	var ips []string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if v4 := ipNet.IP.To4(); v4 != nil {
			ips = append(ips, v4.String())
		}
	}

	return ips, nil
}
func Intersection(a, b []string) bool {
	m := make(map[string]bool)
	for _, x := range a {
		m[x] = true
	}
	for _, y := range b {
		if m[y] {
			return true
		}
	}
	return false
}
