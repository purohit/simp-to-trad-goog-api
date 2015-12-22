// Package main translates all strings from stdin,
// one per line, from simplified to traditional
// Chinese using the Google Translate API.
// Outputs the strings to stdout at the end, in order.
// Your API key needs to be set as an environment var.

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

	maxRequestsPerSec = 100 // 100/s is the maximum rate limit, specified by Google
	jobs              = 20  // 20 concurrent network requests.
)

// Data comes from this nested JSON structure:
//{"data": {"translations": [{"translatedText": "你覺得緊張嗎？"]}}}

type respJSON struct {
	Data tJSON `json:"data"`
}

type tJSON struct {
	Translations []ttJSON `json:"translations"`
}

type ttJSON struct {
	Text string `json:"translatedText"`
}

type sourceText struct {
	line int
	text string
}

type result struct {
	line int
	text string
	err  error
}

type byLine []result

func (a byLine) Len() int           { return len(a) }
func (a byLine) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byLine) Less(i, j int) bool { return a[i].line < a[j].line }

func translate(s sourceText, apiKey string) (r result) {
	r.line = s.line
	resp, err := http.Get(fmt.Sprintf("%s?q=%s&target=%s&source=%s&key=%s", baseURL, url.QueryEscape(s.text), to, from, apiKey))
	defer resp.Body.Close()
	if err != nil {
		r.err = err
		return
	}
	// Parse it.
	dec := json.NewDecoder(resp.Body)
	var j respJSON
	if r.err = dec.Decode(&j); r.err != nil {
		return
	}
	// Got some translated text.
	r.text = j.Data.Translations[0].Text
	return
}

func startWorkers(from <-chan sourceText, to chan result, wg *sync.WaitGroup) {
	apiKey := os.Getenv(apiKeyEnvVar)
	if apiKey == "" {
		log.Fatalf("No %s supplied", apiKeyEnvVar)
	}
	limiter := rate.NewLimiter(maxRequestsPerSec, 1)
	for i := 0; i < jobs; i++ { // Start workers
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range from {
				// Throttle & perform request.
				limiter.Wait(context.TODO())
				to <- translate(f, apiKey)
			}
		}()
	}

}

func main() {
	total := make(chan int)
	toTranslate := make(chan sourceText)
	translated := make(chan result)
	var wg sync.WaitGroup
	startWorkers(toTranslate, translated, &wg)
	var results byLine
	wg.Add(1)
	// Collect results as they arrive
	go func() {
		defer wg.Done()
		expected := -1
		for {
			select {
			case r := <-translated:
				results = append(results, r)
				if len(results) == expected {
					return
				}
			case expected = <-total:
				if len(results) == expected {
					return
				}
			}
		}
	}()
	// Read input and enqueue jobs
	i := 0
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		toTranslate <- sourceText{line: i, text: scanner.Text()}
		i++
	}
	close(toTranslate)
	total <- i
	wg.Wait()
	// Sort and print all results
	sort.Sort(results)
	for _, r := range results {
		fmt.Println(r.text)
	}
}
