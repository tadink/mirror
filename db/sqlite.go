package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"

	_ "github.com/glebarez/go-sqlite"
)

type SiteConfig struct {
	Id               int      `json:"id"`
	Domain           string   `json:"domain"`
	Url              string   `json:"url"`
	IndexTitle       string   `json:"index_title"`
	IndexKeywords    string   `json:"index_keywords"`
	IndexDescription string   `json:"index_description"`
	Finds            []string `json:"finds"`
	Replaces         []string `json:"replaces"`
	NeedJs           bool     `json:"need_js"`
	S2t              bool     `json:"s2t"`
	TitleReplace     bool     `json:"title_replace"`
	H1Replace        string   `json:"h1replace"`
	CacheTime        int64    `json:"cache_time"`
	CacheEnable      bool     `json:"cache_enable"`
	BaiduPushKey     string   `json:"baidu_push_key"`
	SmPushKey        string   `json:"sm_push_key"`
	ArticleType      string   `json:"article_type"`
}
type Article struct {
	Id        int    `json:"id"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	Pic       string `json:"pic"`
	Content   string `json:"content"`
	Author    string `json:"author"`
	TypeName  string `json:"type_name"`
	CreatedAt string `json:"created_at"`
}

var DB *sql.DB

func InitDB() error {
	var err error
	DB, err = sql.Open("sqlite", "config/data.db")
	if err != nil {
		return err
	}
	err = createSiteTable()
	if err != nil {
		return err
	}
	return nil
}

func GetOne(domain string) (SiteConfig, error) {
	domain = strings.TrimSpace(domain)
	var siteConfig SiteConfig
	rs, err := DB.Query("select id,domain,url,index_title,index_keywords,index_description,finds,replaces,need_js,s2t,cache_enable,title_replace,h1replace,cache_time,baidu_push_key,sm_push_key from website_config where domain=?", domain)
	if err != nil {
		return siteConfig, err
	}

	if rs.Next() {
		var findsStr, replStr string
		err = rs.Scan(
			&siteConfig.Id,
			&siteConfig.Domain,
			&siteConfig.Url,
			&siteConfig.IndexTitle,
			&siteConfig.IndexKeywords,
			&siteConfig.IndexDescription,
			&findsStr, &replStr, &siteConfig.NeedJs, &siteConfig.S2t,
			&siteConfig.CacheEnable, &siteConfig.TitleReplace, &siteConfig.H1Replace,
			&siteConfig.CacheTime, &siteConfig.BaiduPushKey, &siteConfig.SmPushKey)
		if err != nil {
			return siteConfig, err
		}
		siteConfig.Finds = strings.Split(findsStr, ";")
		siteConfig.Replaces = strings.Split(replStr, ";")

	}
	err = rs.Close()
	if err != nil {
		return siteConfig, err
	}
	if siteConfig.Id == 0 {
		return siteConfig, errors.New("无搜索结果")
	}
	return siteConfig, nil

}
func DeleteOne(id int) error {
	_, err := DB.Exec("delete from website_config where id=?", id)
	if err != nil {
		return err
	}
	return nil
}
func GetAll() ([]*SiteConfig, error) {
	rs, err := DB.Query("select id, domain,url,index_title,index_keywords,index_description,finds,replaces,need_js,s2t,cache_enable,title_replace,h1replace,cache_time,baidu_push_key,sm_push_key from website_config")
	if err != nil {
		return nil, err
	}
	var results = make([]*SiteConfig, 0)
	for rs.Next() {
		var siteConfig SiteConfig
		var findsStr, replStr string
		err := rs.Scan(
			&siteConfig.Id, &siteConfig.Domain, &siteConfig.Url,
			&siteConfig.IndexTitle, &siteConfig.IndexKeywords, &siteConfig.IndexDescription,
			&findsStr, &replStr, &siteConfig.NeedJs, &siteConfig.S2t, &siteConfig.CacheEnable,
			&siteConfig.TitleReplace, &siteConfig.H1Replace, &siteConfig.CacheTime,
			&siteConfig.BaiduPushKey, &siteConfig.SmPushKey)
		if err != nil {
			return nil, err
		}
		siteConfig.Finds = strings.Split(findsStr, ";")
		siteConfig.Replaces = strings.Split(replStr, ";")
		results = append(results, &siteConfig)
	}
	_ = rs.Close()
	return results, nil

}
func AddOne(data SiteConfig) error {
	insertSql := `insert  into website_config(domain,url,index_title,index_keywords,index_description,finds,replaces,need_js,s2t,cache_enable,title_replace,h1replace,cache_time,baidu_push_key,sm_push_key)values (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := DB.Exec(insertSql, data.Domain, data.Url, data.IndexTitle, data.IndexKeywords, data.IndexDescription, strings.Join(data.Finds, ";"), strings.Join(data.Replaces, ";"), data.NeedJs, data.S2t, data.CacheEnable, data.TitleReplace, data.H1Replace, data.CacheTime, data.BaiduPushKey, data.SmPushKey)
	if err != nil {
		return err
	}
	return nil
}
func UpdateById(data SiteConfig) error {
	updateSql := "update website_config set url=?,domain=?,index_title=?,index_keywords=?,index_description=?,finds=?,replaces=?,need_js=?,s2t=?,cache_enable=?,title_replace=?,h1replace=?,cache_time=?,baidu_push_key=?,sm_push_key=? where id=?"
	_, err := DB.Exec(updateSql, data.Url, data.Domain, data.IndexTitle, data.IndexKeywords, data.IndexDescription, strings.Join(data.Finds, ";"), strings.Join(data.Replaces, ";"), data.NeedJs, data.S2t, data.CacheEnable, data.TitleReplace, data.H1Replace, data.CacheTime, data.BaiduPushKey, data.SmPushKey, data.Id)
	if err != nil {
		return err
	}
	return nil

}
func GetByPage(page, limit int) ([]SiteConfig, error) {
	start := (page - 1) * limit
	querySql := fmt.Sprintf("select * from website_config limit %d,%d", start, limit)
	rs, err := DB.Query(querySql)
	if err != nil {
		return nil, err
	}
	var results = make([]SiteConfig, 0)
	for rs.Next() {
		var siteConfig SiteConfig
		var findsStr, replStr string
		err := rs.Scan(
			&siteConfig.Id, &siteConfig.Domain, &siteConfig.Url,
			&siteConfig.IndexTitle, &siteConfig.IndexKeywords, &siteConfig.IndexDescription,
			&findsStr, &replStr, &siteConfig.NeedJs, &siteConfig.S2t, &siteConfig.CacheEnable,
			&siteConfig.TitleReplace, &siteConfig.H1Replace, &siteConfig.CacheTime,
			&siteConfig.BaiduPushKey, &siteConfig.SmPushKey)
		if err != nil {
			return nil, err
		}
		siteConfig.Finds = strings.Split(findsStr, ";")
		siteConfig.Replaces = strings.Split(replStr, ";")

		results = append(results, siteConfig)
	}
	_ = rs.Close()
	return results, nil
}
func AddMulti(configs []*SiteConfig) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	insetSql := `insert into website_config(domain,url,index_title,index_keywords,index_description,finds,replaces,need_js,s2t,cache_enable,title_replace,h1replace,cache_time,baidu_push_key,sm_push_key)values (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	for _, data := range configs {
		_, err := tx.Exec(insetSql, data.Domain, data.Url, data.IndexTitle, data.IndexKeywords, data.IndexDescription, strings.Join(data.Finds, ";"), strings.Join(data.Replaces, ";"), data.NeedJs, data.S2t, data.CacheEnable, data.TitleReplace, data.H1Replace, data.CacheTime, data.BaiduPushKey, data.SmPushKey)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	err = tx.Commit()
	if err != nil {
		return err
	}
	return nil

}
func MultiDel(domains []string) error {
	args := make([]interface{}, len(domains))
	for i, id := range domains {
		args[i] = id
	}
	delSql := fmt.Sprintf(`delete from website_config where domain in (?%s)`, strings.Repeat(",?", len(args)-1))
	_, err := DB.Exec(delSql, args...)
	if err != nil {
		return err
	}
	return nil

}

func Count() (int, error) {
	countSql := `select count(*) as count from website_config`
	rs, err := DB.Query(countSql)
	if err != nil {
		return 0, err
	}
	var count int
	rs.Next()
	err = rs.Scan(&count)
	if err != nil {
		return 0, err
	}
	err = rs.Close()
	if err != nil {
		return 0, err
	}
	return count, nil

}
func ForbiddenWordReplace(forbiddenWord, replaceWord, splitWord string) ([]string, error) {
	forbiddenSql := "select domain,index_title from website_config where index_title like ?"
	rs, err := DB.Query(forbiddenSql, "%"+forbiddenWord+"%")
	if err != nil {
		return nil, err
	}
	var indexTitleArr = make(map[string]string)
	var temp string
	var tempDomain string
	for rs.Next() {
		err = rs.Scan(&tempDomain)
		if err != nil {
			return nil, err
		}
		err = rs.Scan(&temp)
		if err != nil {
			return nil, err
		}
		indexTitleArr[tempDomain] = temp
	}
	_ = rs.Close()
	if len(indexTitleArr) == 0 {
		return nil, errors.New("没有找到要替换的禁词")
	}
	var domainArr = make([]string, 0)
	updateSql := `update website_config set index_title=? where index_title=?`
	for domain, title := range indexTitleArr {
		if strings.Contains(title, forbiddenWord+splitWord) || strings.Contains(title, splitWord+forbiddenWord) {
			words := strings.Split(title, splitWord)
			for i, word := range words {
				if word == forbiddenWord {
					words[i] = replaceWord
				}
			}
			newTitle := strings.Join(words, splitWord)
			_, err := DB.Exec(updateSql, newTitle, title)
			if err != nil {
				return nil, err
			}
			dn := domain + "##" + newTitle
			domainArr = append(domainArr, dn)
		}
	}
	return domainArr, err
}
func GetArticle(articleType string) (*Article, error) {
	var times int = 1
	articelId := 0
	for {
		articelId, _ = GetRandomArticleId(articleType)
		if articelId > 0 || times > 4 {
			break
		}
		times += 1
	}
	if articelId <= 0 {
		return nil, errors.New("获取随机ID错误")
	}
	return QueryArticle(articelId)

}
func GetRandomArticleId(articleType string) (int, error) {
	count, err := QueryArticleCount(articleType)
	if err != nil {
		return 0, err
	}
	order := "order by id "
	switch rand.IntN(3) {
	case 1:
		order = "order by title "
	case 2:
		order = "order by created_at "
	}
	offset := rand.IntN(count)
	s := fmt.Sprintf("select id from article %s limit %d,1", order, offset)
	stmt, err := DB.Prepare(s)
	if err != nil {
		return 0, err
	}
	defer func() {
		err = stmt.Close()
		if err != nil {
			panic(err)
		}
	}()
	row := stmt.QueryRow()
	if row.Err() != nil {
		return 0, err
	}
	var articleId int
	err = row.Scan(&articleId)
	if err != nil {
		return 0, err
	}
	return articleId, nil
}
func QueryArticleCount(articleType string) (int, error) {
	stmt, err := DB.Prepare("select count(id) from article where type_name=?")
	if err != nil {
		return 0, err
	}
	row := stmt.QueryRow(articleType)
	if row.Err() != nil {
		return 0, err
	}
	var count int
	err = row.Scan(&count)
	if err != nil {
		return count, err
	}
	err = stmt.Close()
	if err != nil {
		slog.Error(err.Error())
	}
	return count, err
}
func QueryArticle(articleId int) (*Article, error) {
	stmt, err := DB.Prepare(`select * from article where id=?`)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = stmt.Close()
		if err != nil {
			panic(err)
		}
	}()
	row := stmt.QueryRow(articleId)
	if row.Err() != nil {
		return nil, err
	}
	a := new(Article)
	err = row.Scan(&a.Id, &a.Title, &a.Summary, &a.Pic, &a.Content, &a.Author, &a.TypeName, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func GetArticleList(typeName string, size int) ([]*Article, error) {
	order := "order by id "
	switch rand.IntN(3) {
	case 1:
		order = "order by title "
	case 2:
		order = "order by created_at "
	}
	count, err := QueryArticleCount(typeName)
	if err != nil {
		return nil, err
	}
	offset := rand.IntN(count)
	if count <= size {
		offset = 0
	}
	if offset > 0 && offset > count-size {
		offset = count - size
	}
	s := fmt.Sprintf("select * from article where type_name=? %s limit ?,?", order)
	stmt, err := DB.Prepare(s)
	if err != nil {
		return nil, err
	}

	rows, err := stmt.Query(typeName, offset, size)
	if err != nil {
		return nil, err
	}
	var articles []*Article
	for rows.Next() {
		a := new(Article)
		err = rows.Scan(&a.Id, &a.Title, &a.Summary, &a.Pic, &a.Content, &a.Author, &a.TypeName, &a.CreatedAt)
		if err != nil {
			return nil, err
		}
		articles = append(articles, a)
	}
	return articles, nil
}
func createSiteTable() error {

	_, err := DB.Exec(`create table if not exists website_config  (
		id integer primary key AUTOINCREMENT,
		domain varchar(30) not null unique ,
		url varchar(50),
		index_title varchar(50),
		index_keywords varchar(100),
		index_description varchar(255),
		finds varchar(100),
		replaces varchar(100),
		need_js boolean default false ,
		s2t boolean default false ,
		cache_enable boolean default true,
		title_replace boolean default false ,
		h1replace varchar(20),
		cache_time integer,
		baidu_push_key varchar(255),
		sm_push_key varchar(255),
		article_type varchar(100)
)`)
	if err != nil {
		return err
	}
	_, err = DB.Exec(`CREATE TABLE IF NOT EXISTS article (
  id INTEGER PRIMARY KEY AUTOINCREMENT ,
  title varchar(255)  NOT NULL,
  summary varchar(255)  NOT NULL DEFAULT '',
  pic varchar(255)  NOT NULL DEFAULT '',
  content text  NOT NULL,
  author varchar(255)  NOT NULL DEFAULT '',
  type_name varchar(255)  NOT NULL DEFAULT '',
  created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
);
CREATE INDEX IF NOT EXISTS  title_idx on article(title);
CREATE INDEX IF NOT EXISTS  created_at_idx on article(created_at);`)
	return err
}
