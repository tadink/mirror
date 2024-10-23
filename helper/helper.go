package helper

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"seo/mirror/db"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html/charset"

	"golang.org/x/net/publicsuffix"
)

func GetHost(request *http.Request) string {
	host := request.Host
	if host == "" {
		host = request.Header.Get("Host")
	}
	if strings.Contains(host, ":") {
		host, _, _ = net.SplitHostPort(host)
	}
	return host
}
func GetInjectJsPath(host string) string {
	hash := md5.Sum([]byte(host))
	name := hex.EncodeToString(hash[:])
	if len(host) >= 6 {
		lastDotIndex := strings.LastIndex(host, ".")
		dirPart := host[:lastDotIndex]
		path := fmt.Sprintf("%x", md5.Sum([]byte(dirPart)))
		return fmt.Sprintf("/%s/%s.js", path, name)
	}
	return fmt.Sprintf("/%s.js", name)

}

func GetArticlePath(host string) string {
	hash := md5.Sum([]byte(host))
	name := hex.EncodeToString(hash[:])
	return name[0:16]
}

func IsIndexPage(path, query string) bool {
	return path == "" ||
		strings.EqualFold(path, "/") ||
		(strings.EqualFold(path, "/index.php") && query == "") ||
		(strings.EqualFold(path, "/index.asp") && query == "") ||
		(strings.EqualFold(path, "/index.jsp") && query == "") ||
		strings.EqualFold(path, "/index.htm") ||
		strings.EqualFold(path, "/index.html") ||
		strings.EqualFold(path, "/index.shtml")

}
func GBK2UTF8(content []byte, contentType string) ([]byte, error) {
	temp := content
	if len(content) > 1024 {
		temp = content[:1024]
	}
	if IsUTF8(temp) {
		return content, nil
	}
	e, name, _ := charset.DetermineEncoding(content, contentType)
	if !strings.EqualFold(name, "utf-8") {
		return e.NewDecoder().Bytes(content)
	}
	return content, nil
}

func GetIPList() ([]net.IP, error) {
	ipList := make([]net.IP, 0)
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return ipList, err
	}
	for _, address := range addresses {
		// 检查ip地址判断是否回环地址
		ipNet, ok := address.(*net.IPNet)
		if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil && isPublicIP(ipNet.IP) {
			ipList = append(ipList, ipNet.IP)
		}
	}
	return ipList, nil
}

func isPublicIP(IP net.IP) bool {
	if IP.IsLoopback() || IP.IsLinkLocalMulticast() || IP.IsLinkLocalUnicast() {
		return false
	}
	if ip4 := IP.To4(); ip4 != nil {
		switch true {
		case ip4[0] == 10:
			return false
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return false
		case ip4[0] == 192 && ip4[1] == 168:
			return false
		default:
			return true
		}
	}
	return false
}
func Intersection(a []string, b []net.IP) bool {
	m := make(map[string]bool)
	for _, x := range a {
		m[x] = true
	}
	for _, y := range b {
		if m[y.String()] {
			return true
		}
	}
	return false
}

func RandHtml(scheme, domain string, keywords []string, typeName string) string {
	var result strings.Builder
	result.WriteString(GetKeywordList(domain, keywords))
	result.WriteString(GetArticleList(scheme, domain, keywords, typeName))
	return "<div style=\"display:none\">" + result.String() + "</div>"
}
func RandStr(minLength int, maxLength int) string {
	chars := []rune("ABCDEFGHIJKLNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
	length := rand.IntN(maxLength-minLength) + minLength
	result := ""
	for i := 0; i < length; i++ {
		result = result + string(chars[rand.IntN(len(chars))])
	}
	return result

}

func genUserAndPass() (string, string) {
	chars := []rune("abcdefghijklmnopqrstuvwxyz")
	user := ""
	for i := 0; i < 8; i++ {
		user = user + string(chars[rand.IntN(len(chars))])
	}
	chars = []rune("abcdefghijklmnopqrstuvwxyz1234567890")
	pass := ""
	for i := 0; i < 18; i++ {
		pass = pass + string(chars[rand.IntN(len(chars))])
	}
	return user, pass
}
func MakeAdminUser() (string, string, error) {
	passBytes, err := os.ReadFile("config/passwd")
	if err != nil || len(passBytes) == 0 {
		userName, password := genUserAndPass()
		err = os.WriteFile("config/passwd", []byte(userName+":"+password), os.ModePerm)
		if err != nil {
			return "", "", errors.New("生成用户文件错误" + err.Error())
		}
		return userName, password, nil

	}
	userAndPass := strings.Split(string(passBytes), ":")
	if len(userAndPass) != 2 {
		return "", "", errors.New("用户文件内容错误")
	}
	return userAndPass[0], userAndPass[1], nil
}

func HtmlEntities(input string) string {
	var buffer bytes.Buffer
	for _, r := range input {
		inputUnicode := strconv.QuoteToASCII(string(r))
		if strings.Contains(inputUnicode, "\\u") {
			inputUnicode = strings.Replace(inputUnicode, `"`, "", 2)
			inputUnicode = strings.Replace(inputUnicode, "\\u", "", 1)
			code, _ := strconv.ParseUint(inputUnicode, 16, 64)
			entity := fmt.Sprintf("&#%d;", code)
			buffer.WriteString(entity)

		} else {
			buffer.WriteString(string(r))
		}
	}
	return buffer.String()
}

func IsDoubleSuffixDomain(host string) bool {
	suffix, _ := publicsuffix.PublicSuffix(host)
	return strings.Contains(suffix, ".")
}
func Escape(content string) string {
	content = strings.ReplaceAll(content, "&", "&amp;")
	content = strings.ReplaceAll(content, "'", "&#39;")
	content = strings.ReplaceAll(content, "<", "&lt;")
	content = strings.ReplaceAll(content, "\"", "&#34;")
	content = strings.ReplaceAll(content, "\r", "&#13;")
	return content
}
func IsUTF8(content []byte) bool {
	for i := len(content) - 1; i >= 0 && i > len(content)-4; i-- {
		b := content[i]
		if b < 0x80 {
			break
		}
		if utf8.RuneStart(b) {
			content = content[:i]
			break
		}
	}
	hasHighBit := false
	for _, c := range content {
		if c >= 0x80 {
			hasHighBit = true
			break
		}
	}
	if hasHighBit && utf8.Valid(content) {
		return true
	}
	return false
}

func IsExist(path string) bool {
	_, err := os.Stat(path) //os.Stat获取文件信息
	if err != nil {
		return os.IsExist(err)
	}
	return true

}

func ReadResponse(response *http.Response, buffer *bytes.Buffer) error {
	contentEncoding := response.Header.Get("Content-Encoding")

	if contentEncoding == "gzip" {
		reader, gzipErr := gzip.NewReader(response.Body)
		if gzipErr != nil {
			return gzipErr
		}
		_, err := io.Copy(buffer, reader)
		if err != nil {
			return err
		}
		return nil
	}
	_, err := io.Copy(buffer, response.Body)
	if err != nil {
		return err
	}
	return response.Body.Close()
}

func WrapResponseBody(response *http.Response, content []byte) {
	readAndCloser := io.NopCloser(bytes.NewReader(content))
	contentLength := int64(len(content))
	response.Body = readAndCloser
	response.ContentLength = contentLength
	response.Header.Set("Content-Length", strconv.FormatInt(contentLength, 10))
}
func RenderTemplate(data []byte, article *db.Article, scheme, domain string) string {
	replacer := strings.NewReplacer(
		"{{article_title}}", article.Title,
		"{{article_pic}}", article.Pic,
		"{{article_summary}}", article.Summary,
		"{{article_content}}", article.Content,
		"{{article_author}}", article.Author,
		"{{article_type_name}}", article.TypeName,
		"{{article_created_at}}", article.CreatedAt,
	)
	content := replacer.Replace(string(data))
	c := strings.Count(content, "{{article_url}}")
	for i := 0; i < c; i++ {
		content = strings.Replace(content, "{{article_url}}", GenerateArticleURL(scheme, domain), 1)
	}
	return content
}
func GetArticleCache(domain, requestPath string) (string, error) {
	sum := sha1.Sum([]byte(domain + requestPath))
	hash := hex.EncodeToString(sum[:])
	dir := path.Join("articleCache", domain, hash[:2])
	filename := path.Join(dir, hash)
	_, err := os.Stat(filename)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
func SetArticleCache(domain, requestPath, data string) error {
	sum := sha1.Sum([]byte(domain + requestPath))
	hash := hex.EncodeToString(sum[:])
	dir := path.Join("articleCache", domain, hash[:2])
	if !IsExist(dir) {
		err := os.MkdirAll(dir, os.ModePerm)
		if err != nil {

			return err
		}
	}
	filename := path.Join(dir, hash)
	err := os.WriteFile(filename, []byte(data), os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}
func GetArticleContent(scheme, domain, requestPath, articleType string, targetUrl *url.URL) (string, error) {
	c, err := GetArticleCache(domain, requestPath)
	if err == nil {
		return c, nil
	}
	templateFile := targetUrl.Host + ".html"
	data, _ := os.ReadFile("templates/" + templateFile)
	if data == nil {
		data, err = os.ReadFile("templates/common.html")
		if err != nil {
			return "", err
		}
	}

	article, err := db.GetArticle(articleType)
	if err != nil {

		return "", err
	}
	template := RenderTemplate(data, article, scheme, domain)
	SetArticleCache(domain, requestPath, template)
	return template, nil
}
func GetKeywordList(domain string, keywords []string) string {
	templateFile := domain + ".html"
	data, err := os.ReadFile("keyword_list/" + templateFile)
	if data == nil || err != nil {
		data, err = os.ReadFile("keyword_list/common.html")
		if err != nil {
			return ""
		}
	}
	content := string(data)
	c := strings.Count(content, "{{rand_str}}")
	for i := 0; i < c; i++ {
		content = strings.Replace(content, "{{rand_str}}", RandStr(4, 8), 1)
	}
	keywordRe, _ := regexp.Compile(`\{\{keyword:(\d+)\}\}`)
	matches := keywordRe.FindAllStringSubmatch(content, -1)
	if matches == nil {
		return content
	}
	for _, match := range matches {
		i, _ := strconv.Atoi(match[1])

		content = strings.ReplaceAll(content, match[0], keywords[i%len(keywords)])
	}
	return content

}
func GetArticleList(scheme, domain string, keywords []string, typeName string) string {
	templateFile := domain + ".html"
	data, err := os.ReadFile("article_list/" + templateFile)
	if data == nil || err != nil {
		data, err = os.ReadFile("article_list/common.html")
		if err != nil {
			return ""
		}
	}
	content := string(data)
	keywordRe, _ := regexp.Compile(`\{\{keyword:(\d+)\}\}`)
	matches := keywordRe.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		i, _ := strconv.Atoi(match[1])
		content = strings.ReplaceAll(content, match[0], keywords[i%len(keywords)])
	}

	c := strings.Count(content, "{{article_url}}")
	for i := 0; i < c; i++ {
		content = strings.Replace(content, "{{article_url}}", GenerateArticleURL(scheme, domain), 1)
	}
	c = strings.Count(content, "{{article_title}}")
	articles, err := db.GetArticleList(typeName, c)
	if err != nil {
		return content
	}
	for i := 0; i < c; i++ {
		content = strings.Replace(content, "{{article_title}}", articles[i].Title, 1)
	}

	return content

}
func GenerateArticleURL(scheme, domain string) string {
	return scheme + "://" + domain + "/__news__/" + RandStr(4, 8)
}
