package conn

import (
	"os"
	"strconv"
)

func getInterval() int {
	i := 3
	if interval := os.Getenv("WSS_INTERVAL"); interval != "" {
		j, err := strconv.Atoi(interval)
		if err == nil {
			i = j
		}
	}
	return i
}
func getTimeout() int {
	i := 18
	if interval := os.Getenv("WSS_TIME_OUT"); interval != "" {
		j, err := strconv.Atoi(interval)
		if err == nil {
			i = j
		}
	}
	return i
}
