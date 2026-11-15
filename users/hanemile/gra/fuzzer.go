package main

import (
	"time"
	"encoding/json"
	"math/rand"
	"fmt"
)

// Mock Fuzzer Engine
func runFuzzerInstance(jobID string, target string) {
	paths := []string{"/admin", "/config", "/.env", "/api/v1", "/backup", "/login", "/wp-admin"}

	for i := 0; i <= 100; i += 5 {
		mu.RLock()
		// Find current job to check thread count/status
		var currentJob *Job
		for idx := range jobs {
			if jobs[idx].ID == jobID {
				currentJob = &jobs[idx]
				break
			}
		}
		mu.RUnlock()

		if currentJob == nil || currentJob.Status == "cancelled" { return }
		if currentJob.Status == "paused" {
			time.Sleep(1 * time.Second)
			i -= 5
			continue
		}

		// Simulate finding a path
		path := paths[rand.Intn(len(paths))]
		logMsg := fmt.Sprintf("[%s] Found: %s%s (200 OK)", time.Now().Format("15:04:05"), target, path)

		// Broadcast update
		update := map[string]interface{}{
			"type":     "fuzz_update",
			"job_id":   jobID,
			"progress": i,
			"log":      logMsg,
		}
		msg, _ := json.Marshal(update)
		wshub.broadcast <- msg

		// Speed controlled by threads (more threads = less sleep)
		sleepDuration := time.Duration(1000/currentJob.Threads) * time.Millisecond
		time.Sleep(sleepDuration)
	}
}
