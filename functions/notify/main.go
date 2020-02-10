package main

import (
	"context"
	"fmt"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-lambda-go/lambda"
)

const (
	CRAWL_WEB_URL = "http://www.tokyoipo.com/ipo/schedule.php"
)

func Handler(ctx context.Context) error {
	fmt.Println("START: book-building-notify-lambda")

	doc, err := goquery.NewDocument(CRAWL_WEB_URL)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	lambda.Start(Handler)
}
