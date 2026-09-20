package pool

import "fmt"

func PrintResult(result Result) {
	fmt.Printf(
		"URL=%s status=%d duration=%v err=%v\n",
		result.URL,
		result.StatusCode,
		result.Duration,
		result.Err,
	)
}
