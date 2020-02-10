package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/guregu/dynamo"
	"github.com/k0kubun/pp"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

const (
	CrawlBaseURL = "http://www.tokyoipo.com"
	CrawlPath    = "ipo/schedule.php"
)

var (
	debug = flag.Bool("debug", false, "run debug mode")
)

type SlackPayload struct {
	Channel  string   `json:"channel"`
	UserName string   `json:"username"`
	Blocks   []*Block `json:"blocks"`
	Text     string   `json:"text"`
	Markdown bool     `json:"mrkdwn"`
}

type Block struct {
	Type string `json:"type"`
	Text *Text  `json:"text"`
}

type Text struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type IPOSchedule struct {
	Index       int   `dynamo:"index"`
	LatestError error `dynamo:"-"`

	CompanyName string `dynamo:"company_name"` // 企業名
	DetailURL   string `dynamo:"detail_url"`   // IPO情報詳細URL
	ChartURL    string `dynamo:"chart_url"`    // 株価・チャートURL
	ReleaseURL  string `dynamo:"release_url"`  // 開示情報URL

	StockReleaseDate string `dynamo:"stock_release_date"` // 公開日
	Code             string `dynamo:"code"`               // コード
	StockCount       string `dynamo:"stock_count"`        // 公開株数

	ProvisionalCondition  string `dynamo:"provisional_condition"`    // 仮条件
	ReleasePrice          string `dynamo:"release_price"`            // 公開価格
	BookBuildingDateRange string `dynamo:"book_building_date_range"` // BB期間

	InitialPrice string `dynamo:"initial_price"` // 初値
	RiseRate     string `dynamo:"rise_rate"`     // 騰落率
	Secretary    string `dynamo:"secretary"`     // 主幹事

	BusinessDescription string `dynamo:"business_description"` // 事業内容
}

func Handler(ctx context.Context) error {
	fmt.Println("START: book-building-notify-lambda")

	u, _ := url.Parse(CrawlBaseURL)
	u.Path = path.Join(CrawlPath)

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
			schedule.DetailURL = CrawlBaseURL + dpath
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

	db := dynamo.New(session.New(), &aws.Config{Region: aws.String(os.Getenv("AWS_DYNAMODB_REGION"))})
	table := db.Table(os.Getenv("DYNAMODB_TABLE"))
	for _, schedule := range schedules {
		var tempSS []*IPOSchedule
		if err := table.Scan().Filter("code = ?", schedule.Code).All(&tempSS); err != nil {
			return err
		}

		if len(tempSS) == 0 {
			if err := PostSlack(":hatching_chick: < 新規IPO情報が公開されましたよ！", schedule); err != nil {
				return err
			}
		}
		if err := table.Put(schedule).Run(); err != nil {
			return err
		}

		tomorrow := time.Now().Add(24 * time.Hour)
		if len(schedule.ProvisionalCondition) > 0 {
			// 仮条件が日付、かつ明日の場合
			layout := "01/02"
			t, err := time.Parse(layout, schedule.ProvisionalCondition)
			if err == nil && t.Month() == tomorrow.Month() && t.Day() == tomorrow.Day() {
				if err := PostSlack(":hatching_chick: < 明日、仮条件が公開されますよ！", schedule); err != nil {
					return err
				}
			}
		}
		sp := strings.Split(schedule.BookBuildingDateRange, "-")
		if len(sp) == 2 {
			// BB期間の開始が日付、かつ明日の場合
			layout := "01/02"
			t, err := time.Parse(layout, strings.TrimSpace(sp[0]))
			if err == nil && t.Month() == tomorrow.Month() && t.Day() == tomorrow.Day() {
				if err := PostSlack(":hatching_chick: < 明日、ブックビルが開始されます！", schedule); err != nil {
					return err
				}
			}
		}
		if len(schedule.ReleasePrice) > 0 {
			// 公開価格が日付、かつ明日の場合
			layout := "01/02"
			t, err := time.Parse(layout, schedule.ReleasePrice)
			if err == nil && t.Month() == tomorrow.Month() && t.Day() == tomorrow.Day() {
				if err := PostSlack(":hatching_chick: < 明日、公開価格が発表されます！", schedule); err != nil {
					return err
				}
			}
		}
		if len(schedule.StockReleaseDate) > 0 {
			// 株式公開日が日付、かつ明日の場合
			layout := "01/02"
			t, err := time.Parse(layout, schedule.StockReleaseDate)
			if err == nil && t.Month() == tomorrow.Month() && t.Day() == tomorrow.Day() {
				if err := PostSlack(":hatching_chick: < 明日、株式公開予定の企業があります！", schedule); err != nil {
					return err
				}
			}
		}
	}
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

func GetJST() *time.Location {
	tz := time.FixedZone("JST", 9*60*60)
	return tz
}

func PostSlack(headTitle string, schedule *IPOSchedule) error {

	payload := &SlackPayload{
		Channel:  os.Getenv("SLACK_CHANNEL"),
		UserName: os.Getenv("SLACK_USER_NAME"),
		Blocks: []*Block{
			{
				Type: "section",
				Text: &Text{
					Type: "mrkdwn",
					Text: headTitle,
				},
			},
			{
				Type: "section",
				Text: &Text{
					Type: "mrkdwn",
					Text: "```" + fmt.Sprintf(`
企業名   : %s
コード   : %s
仮条件   : %s
公開価格 : %s
ＢＢ期間 : %s
公開日   : %s
公開株数 : %s
主幹事   : %s
事業内容 : %s

IPO詳細 : %s
株価詳細: %s
開示情報: %s`, schedule.CompanyName, schedule.Code, schedule.ProvisionalCondition, schedule.ReleasePrice, schedule.BookBuildingDateRange, schedule.StockReleaseDate, schedule.StockCount, schedule.Secretary, schedule.BusinessDescription, schedule.DetailURL, schedule.ChartURL, schedule.ReleaseURL) + "```",
				},
			},
		},
		Markdown: true,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, os.Getenv("SLACK_WEBHOOK_URL"), bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return err
	}

	bb, err := ioutil.ReadAll(res.Body)
	log.Printf("%+v\n", string(bb))
	defer res.Body.Close()
	return nil
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
