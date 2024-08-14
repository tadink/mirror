package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"seo/mirror/helper"
	"strings"

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
		conf.Keywords = strings.Split(strings.Replace(helper.HtmlEntities(string(keywordData)), "\r", "", -1), "\n")
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
	return &authInfo, nil
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

func IsCrawler(ua string) bool {

	ua = strings.ToLower(ua)
	for _, value := range Conf.Spider {
		spider := strings.ToLower(value)
		if strings.Contains(ua, spider) {
			return true
		}
	}
	return false
}
func IsGoodCrawler(ua string) bool {
	ua = strings.ToLower(ua)
	for _, value := range Conf.GoodSpider {
		spider := strings.ToLower(value)
		if strings.Contains(ua, spider) {
			return true
		}
	}
	return false
}
func FriendLink(domain string) string {
	if len(Conf.FriendLinks[domain]) < 1 {
		return ""
	}
	var friendLink string
	for _, link := range Conf.FriendLinks[domain] {
		linkItem := strings.Split(link, ",")
		if len(linkItem) != 2 {
			continue
		}
		friendLink += fmt.Sprintf("<a href='%s' target='_blank'>%s</a>", linkItem[0], linkItem[1])
	}
	return fmt.Sprintf("<div style='display:none'>%s</div>", friendLink)
}
