package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/aakash/godrop/internal/pool"
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
	flag.Parse()

	urls, err := readURLs(*file)
	if err != nil {
		fmt.Println("Read Urls:", err)
		os.Exit(1)
	}

	jobs := make(chan string)
	results := make(chan pool.Result)

	var wg sync.WaitGroup
	for range *workerCount {
		wg.Add(1)
		go pool.Worker(jobs, results, &wg)
	}

	go func() {
		defer close(jobs)
		for _, u := range urls {
			jobs <- u
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		pool.PrintResult(r)
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
