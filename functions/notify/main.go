package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"path"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/k0kubun/pp"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

const (
	CRAWL_BASE_URL = "http://www.tokyoipo.com"
	CRAWL_PATH     = "ipo/schedule.php"
)

var (
	debug = flag.Bool("debug", false, "run debug mode")
)

type IPOSchedule struct {
	Index       int
	LatestError error

	CompanyName string // 企業名
	DetailURL   string // IPO情報詳細URL
	ChartURL    string // 株価・チャートURL
	ReleaseURL  string // 開示情報URL

	StockReleaseDate string // 公開日
	Code             string // コード
	StockCount       string // 公開株数

	ProvisionalCondition  string // 仮条件
	ReleasePrice          string // 公開価格
	BookBuildingDateRange string // BB期間

	InitialPrice string // 初値
	RiseRate     string // 騰落率
	Secretary    string // 主幹事

	BusinessDescription string // 事業内容
}

func Handler(ctx context.Context) error {
	fmt.Println("START: book-building-notify-lambda")

	u, _ := url.Parse(CRAWL_BASE_URL)
	u.Path = path.Join(CRAWL_PATH)

	doc, err := goquery.NewDocument(u.String())
	if err != nil {
		return err
	}

	var schedules []*IPOSchedule
	var ss []*goquery.Selection

	doc.Find(".iposchedulelist tr").Each(func(i int, s *goquery.Selection) {
		if s.HasClass(".iposchedulelist_tr1") {
			return
		}

		ss = append(ss, s)
	})

	for i, s := range ss {
		schedule := &IPOSchedule{}
		if !s.HasClass("iposchedulelist_tr_top") {
			continue
		}

		// 企業名、IPO情報詳細URLを取得
		s.Find("h2 a").Each(func(_ int, s *goquery.Selection) {
			dpath, _ := s.Attr("href")
			schedule.CompanyName = EUCJP2UTF8(s.Text())
			schedule.DetailURL = CRAWL_BASE_URL + dpath
		})

		// 株価・チャートURLを取得
		s.Find("div a.minkabubtn").Each(func(_ int, s *goquery.Selection) {
			btnurl, _ := s.Attr("href")
			schedule.ChartURL = btnurl
		})

		// 開示情報URLを取得
		s.Find("div a.kaijibtn").Each(func(_ int, s *goquery.Selection) {
			btnurl, _ := s.Attr("href")
			schedule.ReleaseURL = btnurl
		})

		s1 := ss[i+1] // 1行目
		s1.Find("td").Each(func(i int, s *goquery.Selection) {
			if i == 0 {
				// 公開日
				schedule.StockReleaseDate = EUCJP2UTF8(s.Text())
			} else if i == 1 {
				// コード
				schedule.Code = EUCJP2UTF8(s.Text())
			} else if i == 2 {
				// 公開株数
				schedule.StockCount = EUCJP2UTF8(s.Text())
			}
		})

		s2 := ss[i+2] // 2行目
		s2.Find("td").Each(func(i int, s *goquery.Selection) {
			if i == 0 {
				// 仮条件
				schedule.ProvisionalCondition = EUCJP2UTF8(s.Text())
			} else if i == 1 {
				// 価格公開日
				schedule.ReleasePrice = EUCJP2UTF8(s.Text())
			} else if i == 2 {
				// BB期間
				schedule.BookBuildingDateRange = EUCJP2UTF8(s.Text())
			}
		})

		s3 := ss[i+3] // 3行目
		s3.Find("td").Each(func(i int, s *goquery.Selection) {
			if i == 0 {
				// 初値
				schedule.InitialPrice = EUCJP2UTF8(s.Text())
			} else if i == 1 {
				// 騰落率
				schedule.RiseRate = EUCJP2UTF8(s.Text())
			} else if i == 2 {
				// 主幹事
				schedule.Secretary = EUCJP2UTF8(s.Text())
			}
		})

		s5 := ss[i+5] // 5行目(4行目は隠し要素)
		s5.Find("td").Each(func(i int, s *goquery.Selection) {
			// 事業内容
			schedule.BusinessDescription = EUCJP2UTF8(s.Text())
		})

		schedules = append(schedules, schedule)
	}

	pp.Println(schedules)

	// 1. 新規で追加された企業を通知する
	// 2. 翌日が仮条件、公開価格、BB期間開始、上場日の場合に通知する

	//for _, schedule := range schedules {
	//}

	return nil
}

func EUCJP2UTF8(str string) string {
	ret, err := ioutil.ReadAll(transform.NewReader(strings.NewReader(str), japanese.EUCJP.NewDecoder()))
	if err != nil {
		fmt.Println("WARNING: string encoding error")
		fmt.Println(err)
		return str
	}
	return strings.TrimSpace(string(ret))
}

func main() {
	flag.Parse()
	if *debug {
		if err := Handler(context.Background()); err != nil {
			panic(err)
		}
	} else {
		lambda.Start(Handler)
	}
}
