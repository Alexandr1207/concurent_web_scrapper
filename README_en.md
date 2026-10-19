# Concurrent Web Scraper v2 (Worker Pool + context)

A command-line tool written in Go. It takes a list of URLs, fetches the pages with a pool of workers, and prints a results table (status, size, page title, response time) plus summary statistics. This is the second version of the project: instead of "one goroutine per URL + a semaphore" it uses a Worker Pool, and the whole run is controlled through `context`.

## Features

- Worker Pool: a fixed number of workers takes URLs from a shared queue
- Per-request timeout (`-timeout`, 5 s by default)
- Overall timeout for the whole run via `context.WithTimeout` (`-total`, 30 s by default)
- When the overall timeout expires, in-flight requests are aborted and no new jobs are handed out
- Results come back in the same order as the URLs in the list
- Errors (DNS, connection refused, timeout, read failure) never crash the program; they appear in the table
- A request counts as a success only if it returns a 2xx status with no error. 404 and 500 count as failures
- URLs the run never reached because of the overall timeout are marked `not started`
- Summary: number of successes and failures, slowest page, total size
- URLs come from a file (blank lines and lines starting with `#` are skipped) or from a built-in default list

## Requirements

Go 1.21 or newer. Standard library only.

## Usage

```bash
# built-in URL list, 5 workers, 5 s per request, 30 s overall
go run .

# URLs from a file, 10 workers, 10 s overall
go run . -file=./urls.txt -workers=10 -total=10s
```

| Flag | Default | Description |
|---|---|---|
| `-file` | empty (built-in list) | file with a list of URLs, one per line |
| `-workers` | `5` | number of pool workers (a value below 1 stops the program with an error) |
| `-timeout` | `5s` | timeout for a single request (`500ms`, `3s`, etc.) |
| `-total` | `30s` | overall timeout for the whole run |

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

`go run -race . -workers=5 -total=10s` over 8 URLs:

```
OK times: 5
Failed times: 3
Slowest URL: https://books.toscrape.com ,with time: 774.4394ms
Total size: 134007
https://books.toscrape.com          200   51294 B  774ms    All products | Books to Scrape - Sandbox
https://quotes.toscrape.com         200   11064 B  624ms    Quotes to Scrape
https://example.com                 200     577 B  204ms    Example Domain
https://go.dev                      200   64188 B  326ms    The Go Programming Language
https://gobyexample.com             200    6884 B  177ms    Go by Example
https://httpstat.us/404             ERROR  696ms    Get "https://httpstat.us/404": EOF
https://localhost:1                 ERROR  1ms      Get "https://localhost:1": dial tcp [::1]:1: connectex: No connection could be made because the target machine actively refused it.
https://httpbin.org/delay/10        ERROR  5s       Get "https://httpbin.org/delay/10": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
Concurrent completed with time:  5.2101465s
```

The "slowest page" statistic only considers requests that finished without an error, so `delay/10`, which is cut off by the timeout, is not counted there.

If the overall timeout is shorter than the list needs, an in-flight request is aborted with `context deadline exceeded` (without the `Client.Timeout` suffix: the overall limit fired, not the per-request one), and the program ends with `pool stopped: context deadline exceeded`.

## Results: number of workers

Same 8 URLs, `-total=30s`:

| Workers | Total time |
|---|---|
| 1 | ≈ 9.7 s |
| 5 | ≈ 5.4 s |
| 10 | ≈ 5.0 s |

With one worker the time is the sum of all requests. With five or ten it is capped by the slowest request: `httpbin.org/delay/10` is cut off by the timeout at 5 seconds. The difference between 5 and 10 workers is within noise, because there are only 8 URLs and the extra workers sit idle. The network was unstable during measurement: in two runs `gobyexample.com` missed its timeout, so the success count may differ.

## How it works

- **Worker Pool.** `runPool` starts `-workers` workers. Each one runs `for j := range jobs`, takes a job, calls `fetch`, and sends the result to the `results` channel.
- **Job dispatcher.** A separate goroutine sends URLs into `jobs` and closes the channel (`defer close(jobs)`). If the context is cancelled, it stops handing out jobs.
- **Closing `results`.** A separate goroutine closes it after `wg.Wait()`, that is, once every worker has finished. Closing it earlier is not possible: workers are still writing, and sending on a closed channel panics.
- **Result order.** Each job and each result carries an index (`Id`, `JobId`). The main goroutine reads `results` and stores each answer in `out[JobId]`, so the order matches the URL order and the slice needs no mutex.
- **`Stats` guarded by `sync.Mutex`.** The counters, the total size and the slowest page are shared by all workers. `s.ok++` is really three steps (read, add, write), so without a lock increments get lost.
- **`context`.** `fetch(ctx, client, url)` builds the request with `http.NewRequestWithContext`, so cancelling the context aborts an in-flight request. The overall timeout is set once in `main` with `context.WithTimeout`.
- **A dedicated `http.Client` with a timeout.** The default client has no timeout, so one hung server could block a worker forever.
- **Status is checked separately from the error.** `err == nil` does not mean success: a 404 arrives as an ordinary `resp`.

## Verification

```bash
# race detector (-race is a flag of the go command, so it goes before the program's arguments)
go run -race . -file=./urls.txt -workers=5 -total=10s

# overall timeout shorter than the list needs
go run -race . -workers=1 -total=3s

# static analysis
go vet ./...
```

## Limitations and ideas for improvement

- No retries on transient errors and no per-host rate limiting.
- URLs marked `not started` are not counted in the summary: only workers update the statistics.
- No graceful shutdown on Ctrl+C (the context could be wrapped with `signal.NotifyContext`).
- The page title is extracted with a regular expression. An HTML parser would be more reliable for complex pages.
- Output goes to the console only. JSON or CSV export could be added.

## What I learned

Channels (unbuffered and buffered), closing channels and `range` over a channel, `select`, the Worker Pool pattern (fan-out and fan-in), preserving result order with a job index, `context` (cancellation, timeouts, aborting HTTP requests), synchronizing shared data with `Mutex`, and the race detector `-race`.