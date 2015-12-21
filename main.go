// Package main translates all strings from stdin,
// one per line, from simplified to traditional
// Chinese using the Google Translate API.
// Outputs the strings to stdout at the end, in order.
// Your API key needs to be set as an environment var.
// Uses request limiting (100/s as specified by Google),
// and 20 concurrent network requests.

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"sync"

	"golang.org/x/net/context"
	"golang.org/x/time/rate"
)

const (
	apiKeyEnvVar = "GOOGLE_API_KEY"
	baseURL      = "https://www.googleapis.com/language/translate/v2"

	from = "zh-CN"
	to   = "zh-TW"
)

// Data comes from this nested JSON structure:
//{
//"data": {
//  "translations": [{
//    "translatedText": "你覺得緊張嗎？"
//  ]}
//  }
//}

type ttJSON struct {
	Text string `json:"translatedText"`
}

type tJSON struct {
	Translations []ttJSON `json:"translations"`
}

type dJSON struct {
	Data tJSON `json:"data"`
}

type sourceText struct {
	line int
	text string
}

type doneText struct {
	line int
	text string
	err  error
}

type byLine []doneText

func (a byLine) Len() int           { return len(a) }
func (a byLine) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byLine) Less(i, j int) bool { return a[i].line < a[j].line }

func translate(s sourceText, key string) doneText {
	res := doneText{line: s.line}
	resp, err := http.Get(fmt.Sprintf("%s?q=%s&target=%s&source=%s&key=%s", baseURL, url.QueryEscape(s.text), to, from, key))
	defer resp.Body.Close()
	if err != nil {
		res.err = err
		return res
	}
	// Let's parse it.
	dec := json.NewDecoder(resp.Body)
	var d dJSON
	if err := dec.Decode(&d); err != nil {
		res.err = err
		return res
	}
	// Got some translated text.
	res.text = d.Data.Translations[0].Text
	return res
}

func main() {
	apiKey := os.Getenv(apiKeyEnvVar)
	if apiKey == "" {
		log.Fatalf("No %s supplied", apiKeyEnvVar)
	}
	total := make(chan int)
	toTranslate := make(chan sourceText)
	translated := make(chan doneText)
	limiter := rate.NewLimiter(100, 1)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ { // Start workers
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range toTranslate {
				// Throttle & perform request.
				limiter.Wait(context.TODO())
				translated <- translate(t, apiKey)
			}
		}()
	}
	var allResults byLine
	wg.Add(1)
	go func() { // Put results into a slice as they arrive
		defer wg.Done()
		j := -1
		for {
			select {
			case r := <-translated:
				allResults = append(allResults, r)
				if len(allResults) == j {
					return
				}
			case t := <-total:
				j = t
			}
		}
	}()
	i := 0
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if i > 10 {
			break
		}
		toTranslate <- sourceText{line: i, text: scanner.Text()}
		i++
	}
	close(toTranslate)
	total <- i
	wg.Wait()
	// We can sort and print all the jobs now since we're guaranteed they're all done.
	sort.Sort(allResults)
	for _, r := range allResults {
		fmt.Println(r.text)
	}
}
