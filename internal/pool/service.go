package pool

import (
	"net/http"
	"sync"
	"time"
)

type Result struct {
	URL        string
	StatusCode int
	Duration   time.Duration
	Err        error
}

func Fetch(url string) Result {
	start := time.Now()

	client := http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(url)

	result := Result{
		URL:      url,
		Duration: time.Since(start),
	}

	if err != nil {
		result.Err = err
		return result
	}

	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode

	return result
}

func Worker(
	jobs <-chan string,
	results chan<- Result,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for url := range jobs {
		result := Fetch(url)
		results <- result
	}
}
