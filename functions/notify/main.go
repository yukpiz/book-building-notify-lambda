package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/k0kubun/pp"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

const (
	CRAWL_WEB_URL = "http://www.tokyoipo.com/ipo/schedule.php"
)

var (
	debug = flag.Bool("debug", false, "run debug mode")
)

type IPOSchedule struct {
	Index       int
	CompanyName string
}

func Handler(ctx context.Context) error {
	fmt.Println("START: book-building-notify-lambda")

	doc, err := goquery.NewDocument(CRAWL_WEB_URL)
	if err != nil {
		return err
	}

	var ss []*IPOSchedule

	doc.Find(".h2_ipolist_name > a").Each(func(i int, s *goquery.Selection) {
		ss = append(ss, &IPOSchedule{
			Index:       i,
			CompanyName: EUCJP2UTF8(s.Text()),
		})
	})

	pp.Println(ss)

	return nil
}

func EUCJP2UTF8(str string) string {
	ret, err := ioutil.ReadAll(transform.NewReader(strings.NewReader(str), japanese.EUCJP.NewDecoder()))
	if err != nil {
		fmt.Println("WARNING: string encoding error")
		fmt.Println(err)
		return str
	}
	return string(ret)
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
