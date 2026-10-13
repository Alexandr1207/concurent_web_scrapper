package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Result struct {
	URL      string
	Status   int
	Size     int
	Title    string
	Duration time.Duration
	Err      error
}

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func fetch(client *http.Client, url string) Result {
	start := time.Now()

	res := Result{URL: url}

	resp, err := client.Get(url)
	if err != nil {
		res.Err = err
		res.Duration = time.Since(start)
		return res
	}
	defer resp.Body.Close()
	res.Status = resp.StatusCode

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		res.Err = err
		res.Duration = time.Since(start)
		return res
	}

	if m := titleRe.FindSubmatch(body); m != nil {
		res.Title = strings.TrimSpace(string(m[1]))
	}
	res.Size = len(body)
	res.Duration = time.Since(start)

	return res
}

func printResults(results []Result) {
	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("%-35s ERROR  %-8v %v\n", r.URL, r.Duration.Round(time.Millisecond), r.Err)
			continue
		}
		fmt.Printf("%-35s %d  %6d B  %-8v %s\n", r.URL, r.Status, r.Size, r.Duration.Round(time.Millisecond), r.Title)
	}
}

type Stats struct {
	mu        sync.Mutex
	ok        int
	failed    int
	slowest   Result
	totalSize int
}

func (s *Stats) Add(r Result) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if r.Err == nil && r.Status >= 200 && r.Status <= 299 {
		s.ok++
	} else {
		s.failed++
	}

	if r.Err == nil && (s.slowest.URL == "" || r.Duration > s.slowest.Duration) {
		s.slowest = r
	}

	s.totalSize += r.Size
}

func (s *Stats) PrintStats() {
	fmt.Println("OK times:", s.ok)
	fmt.Println("Failed times:", s.failed)
	fmt.Println("Slowest URL:", s.slowest.URL, ",with time:", s.slowest.Duration)
	fmt.Println("Total size:", s.totalSize)
}

func runSequential(client *http.Client, urls []string, s *Stats) []Result {
	results := make([]Result, len(urls))
	for i, url := range urls {
		r := fetch(client, url)
		results[i] = r
		s.Add(r)
	}

	return results
}

func runConcurrent(client *http.Client, urls []string, s *Stats) []Result {
	var wg sync.WaitGroup

	sem := make(chan struct{}, 5)

	results := make([]Result, len(urls))
	for i, url := range urls {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			r := fetch(client, url) // no races
			results[i] = r
			s.Add(r)
		})

	}
	wg.Wait()

	return results
}

func LoadUrls(file string) []string {
	if file == "" {
		return []string{
			"https://books.toscrape.com", "https://quotes.toscrape.com", "https://example.com", "https://go.dev", "https://gobyexample.com", "https://httpstat.us/404", "https://localhost:1", "https://httpbin.org/delay/10",
		}
	}

	data, err := os.ReadFile(file)
	if err != nil {
		log.Fatalf("File is not readable %q: %v", file, err)
	}

	var urls []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	if len(urls) == 0 {
		log.Fatalf("файл %q пуст — нечего запрашивать", file)
	}
	return urls
}

func main() {
	timeout := flag.Duration("timeout", 5*time.Second, "таймаут одного запроса")
	file := flag.String("file", "", "файл со списком URL (по одному в строке)")
	flag.Parse()

	client := &http.Client{Timeout: *timeout}
	// urls := []string{"https://books.toscrape.com", "https://quotes.toscrape.com", "https://example.com", "https://go.dev", "https://gobyexample.com", "https://httpstat.us/404", "https://localhost:1", "https://httpbin.org/delay/10"}
	urls := LoadUrls(*file)

	start := time.Now()
	seqStats := &Stats{}
	seq := runSequential(client, urls, seqStats)
	seqStats.PrintStats()
	printResults(seq)
	end := time.Since(start)
	fmt.Println("Sequential completed with time: ", end) // 8.4217687s

	start = time.Now()
	concStats := &Stats{}
	conc := runConcurrent(client, urls, concStats)
	concStats.PrintStats()
	printResults(conc)
	end = time.Since(start)
	fmt.Println("Concurrent completed with time: ", end) // 5.0018447s
}
