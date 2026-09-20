package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aakash/godrop/internal/pool"
	"github.com/aakash/godrop/internal/processor"
	"github.com/joho/godotenv"
)

func main() {

	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading env")
	}

	filePath := os.Getenv("FILE_PATH")
	workers, err := strconv.Atoi(os.Getenv("WORKERS"))
	if err != nil {
		workers = 5
	}

	file := flag.String("file", filePath, "file with one URL per line")
	workerCount := flag.Int("workers", workers, "number of concurrent workers")
	timeout := flag.Duration("timeout", 5*time.Second, "per-job timeout")
	flag.Parse()

	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeout must be positive")
		os.Exit(1)
	}

	if *workerCount < 1 {
		fmt.Fprintln(os.Stderr, "workers must be at least 1")
		os.Exit(1)
	}

	urls, err := readURLs(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Read Urls:", err)
		os.Exit(1)
	}

	ctx := context.Background()
	stopCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-stopCtx.Done()
		stop() // restore default signal behaviour: a second Ctrl+C kills the process
	}()

	fetcher := processor.NewFetcher()

	jobs := make(chan string)
	results := make(chan pool.JobResult[string, processor.Page])

	var wg sync.WaitGroup
	for range *workerCount {
		wg.Add(1)
		go pool.RunWorker(ctx, fetcher, *timeout, jobs, results, &wg)
	}

	go pool.Feed(stopCtx, urls, jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	processed := 0
	for r := range results {
		printResult(r)
		processed++
	}

	if stopCtx.Err() != nil {
		fmt.Fprintf(os.Stderr, "interrupted: processed %d of %d URLs\n", processed, len(urls))
	}
}

func readURLs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var urls []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		urls = append(urls, line)
	}
	return urls, sc.Err()
}

func printResult(r pool.JobResult[string, processor.Page]) {
	fmt.Printf(
		"URL=%s status=%d duration=%v err=%v\n",
		r.Job,
		r.Value.StatusCode,
		r.Duration,
		r.Err,
	)
}
