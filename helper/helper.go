package helper

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
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

func IsIndexPage(path string) bool {
	return path == "" ||
		strings.EqualFold(path, "/") ||
		strings.EqualFold(path, "/index.php") ||
		strings.EqualFold(path, "/index.asp") ||
		strings.EqualFold(path, "/index.jsp") ||
		strings.EqualFold(path, "/index.htm") ||
		strings.EqualFold(path, "/index.html") ||
		strings.EqualFold(path, "/index.shtml")

}
func GBK2UTF8(content []byte, contentType string) []byte {
	temp := content
	if len(content) > 1024 {
		temp = content[:1024]
	}
	if !IsUTF8(temp) {
		e, name, _ := charset.DetermineEncoding(content, contentType)
		if !strings.EqualFold(name, "utf-8") {
			content, _ = e.NewDecoder().Bytes(content)
		}
	}
	return content
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

func RandHtml(domain string) string {
	htmlTags := []string{"abbr", "address", "area", "article", "aside", "b", "base", "bdo", "blockquote", "button", "cite", "code", "dd", "del", "details", "dfn", "dl", "dt", "em", "figure", "font", "i", "ins", "kbd", "label", "legend", "li", "mark", "meter", "ol", "option", "p", "q", "progress", "rt", "ruby", "samp", "section", "select", "small", "strong", "tt", "u"}
	var result string
	for i := 0; i < 100; i++ {
		if domainParts := strings.Split(domain, "."); ((IsDoubleSuffixDomain(domain) && len(domainParts) == 3) || len(domainParts) == 2) && rand.IntN(100) < 20 {
			result = result + fmt.Sprintf(`<a href="%s" target="_blank">%s</a>`, "{{scheme}}://"+RandStr(3, 5)+"."+domain, RandStr(6, 16))
			continue
		}
		t := htmlTags[rand.IntN(len(htmlTags))]
		result = result + fmt.Sprintf(`<%s id="%s" class="%s">%s</%s>`, t, RandStr(4, 8), RandStr(4, 8), RandStr(6, 16), t)
	}
	return "<div style=\"display:none\">" + result + "</div>"
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
	suffixes := []string{"com.cn", "net.cn", "org.cn"}
	for _, suffix := range suffixes {
		if strings.Contains(host, suffix) {
			return true
		}
	}
	return false
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
	defer response.Body.Close()
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
	return err
}

func WrapResponseBody(response *http.Response, content []byte) {
	readAndCloser := io.NopCloser(bytes.NewReader(content))
	contentLength := int64(len(content))
	response.Body = readAndCloser
	response.ContentLength = contentLength
	response.Header.Set("Content-Length", strconv.FormatInt(contentLength, 10))
}
