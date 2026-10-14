# Concurrent Web Scraper

A command-line tool written in Go. It takes a list of URLs, fetches the pages both sequentially and concurrently, and prints a results table (status, size, page title, response time) plus summary statistics. The goal is to compare the two approaches side by side and show how to collect data from goroutines safely.

## Features

- Sequential and concurrent crawling of a URL list, with total time measured for each
- Per-request timeout (5 s by default)
- Limit on simultaneous requests (a semaphore with 5 slots)
- Errors (DNS, connection refused, timeout, read failure) never crash the program; they appear in the table
- A request counts as a success only if it returns a 2xx status with no error. 404 and 500 count as failures
- Summary: number of successes and failures, slowest page, total size
- URLs come from a file (blank lines and lines starting with `#` are skipped) or from a built-in default list

## Requirements

Go 1.25 or newer (the code uses `sync.WaitGroup.Go`).

## Usage

```bash
# built-in URL list, 5-second timeout
go run .

# URLs from a file with a custom timeout
go run . -file=./urls.txt -timeout=3s
```

| Flag | Default | Description |
|---|---|---|
| `-file` | empty (built-in list) | file with a list of URLs, one per line |
| `-timeout` | `5s` | timeout for a single request (`500ms`, `3s`, etc.) |

Example `urls.txt`:

```
# sandbox sites made for scraping practice
https://books.toscrape.com
https://quotes.toscrape.com
https://example.com
https://go.dev
https://localhost:1
```

## Example output

A concurrent run over 8 URLs:

```
OK times: 5
Failed times: 3
Slowest URL: https://books.toscrape.com ,with time: 589.8799ms
Total size: 134004
https://books.toscrape.com          200   51294 B  590ms    All products | Books to Scrape - Sandbox
https://quotes.toscrape.com         200   11064 B  406ms    Quotes to Scrape
https://example.com                 200     577 B  79ms     Example Domain
https://go.dev                      200   64185 B  214ms    The Go Programming Language
https://gobyexample.com             200    6884 B  45ms     Go by Example
https://httpstat.us/404             ERROR  679ms    Get "https://httpstat.us/404": EOF
https://localhost:1                 ERROR  2ms      Get "https://localhost:1": dial tcp [::1]:1: connectex: No connection could be made because the target machine actively refused it.
https://httpbin.org/delay/10        ERROR  5s       Get "https://httpbin.org/delay/10": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
Concurrent completed with time:  5.128114s
```

The "slowest page" statistic only considers requests that finished without an error, so `delay/10`, which is cut off by the timeout, is not counted there.

## Results: sequential vs. concurrent

| Mode | Total time |
|---|---|
| Sequential | 8.4 – 9.8 s |
| Concurrent | ≈ 5.0 – 5.1 s |

The concurrent version takes about as long as the slowest request, not the sum of all of them. In this set the slowest request is `httpbin.org/delay/10`, which is cut off by the timeout at 5 seconds. So 5 seconds is not a limit of goroutines but a ceiling set by one slow URL. On a list without hanging sites the speedup is even bigger.

## How it works

- **Writing to `results[i]` without a mutex.** The results slice is created up front with the right length. Each goroutine writes only to its own cell `results[i]`, so no two goroutines ever touch the same cell. Results stay in the same order as the URLs.
- **`Stats` guarded by `sync.Mutex`.** The success and failure counters, the total size and the slowest page are shared by all goroutines. `s.ok++` is really three steps (read, add, write), so without a lock increments get lost. `Add` takes the lock and releases it with `defer Unlock`.
- **`WaitGroup`.** `main` waits for every goroutine with `wg.Wait()` and only then reads the results and prints the statistics.
- **Semaphore on a buffered channel.** `sem := make(chan struct{}, 5)` lets at most 5 requests run at once, so the tool does not overload sites or hit system limits.
- **A dedicated `http.Client` with a timeout.** The default client has no timeout, so one hung server could block a goroutine forever.
- **Status is checked separately from the error.** `err == nil` does not mean success: a 404 arrives as an ordinary `resp`.

## Verification

```bash
# race detector (-race is a flag of the go command, so it goes before the program's arguments)
go run -race . -file=./urls.txt -timeout=5s

# static analysis
go vet ./...
```

## Limitations and ideas for improvement

- One goroutine is started per URL, and the semaphore only limits how many requests run at the same time. For large lists a Worker Pool with a fixed number of workers would be better (next step).
- No retries on transient errors and no per-host rate limiting.
- The page title is extracted with a regular expression. An HTML parser would be more reliable for complex pages.
- Output goes to the console only. JSON or CSV export could be added.

## What I learned

Goroutines and `WaitGroup`, synchronizing shared data with `Mutex`, writing to independent slice cells without locks, working with `net/http` (timeouts, closing the response body, status codes), command-line flags, reading a file, and handling errors carefully.