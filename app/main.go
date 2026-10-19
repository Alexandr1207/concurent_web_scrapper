package main

import (
	"context"
	"errors"
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

func fetch(ctx context.Context, client *http.Client, url string) Result {
	start := time.Now()

	res := Result{URL: url}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		res.Err = err
		res.Duration = time.Since(start)
		return res
	}
	resp, err := client.Do(req)
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

type FetchJob struct {
	Id  int
	URL string
}

type FetchResult struct {
	JobId int
	R     Result
}

func runPool(ctx context.Context, client *http.Client, urls []string, workers int, s *Stats) []Result {
	jobs := make(chan FetchJob)
	results := make(chan FetchResult)
	var wg sync.WaitGroup

	for i := 1; i <= workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				r := fetch(ctx, client, j.URL)
				results <- FetchResult{JobId: j.Id, R: r}
				s.Add(r)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for i, url := range urls {
			select {
			case jobs <- FetchJob{Id: i, URL: url}:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make([]Result, len(urls))
	for i, u := range urls {
		out[i] = Result{URL: u, Err: errors.New("not started")}
	}
	for fr := range results {
		out[fr.JobId] = fr.R
	}
	return out
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
	total := flag.Duration("total", 30*time.Second, "общий таймаут на весь пул")
	workers := flag.Int("workers", 5, "число воркеров пула")
	file := flag.String("file", "", "файл со списком URL")
	flag.Parse()

	if *workers <= 0 {
		log.Fatalf("-workers must be > 0, got %d", *workers)
	}

	client := &http.Client{Timeout: *timeout}
	urls := LoadUrls(*file)

	ctx, cancel := context.WithTimeout(context.Background(), *total)
	defer cancel()

	start := time.Now()
	concStats := &Stats{}
	conc := runPool(ctx, client, urls, *workers, concStats)
	concStats.PrintStats()
	printResults(conc)
	fmt.Println("Concurrent completed with time: ", time.Since(start))

	if err := ctx.Err(); err != nil {
		fmt.Println("pool stopped:", err)
	}
}
