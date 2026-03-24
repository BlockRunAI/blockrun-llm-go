package blockrun

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CostLogEntry represents a single cost log entry written to the JSONL file.
type CostLogEntry struct {
	Timestamp float64 `json:"ts"`
	Endpoint  string  `json:"endpoint"`
	CostUSD   float64 `json:"cost_usd"`
}

// CostSummary represents an aggregate summary of cost log entries.
type CostSummary struct {
	TotalUSD   float64            `json:"total_usd"`
	Calls      int                `json:"calls"`
	ByEndpoint map[string]float64 `json:"by_endpoint"`
}

// CostLog provides persistent cost logging to a JSONL file.
type CostLog struct {
	mu   sync.Mutex
	path string
}

// NewCostLog creates a new CostLog that writes to ~/.blockrun/cost_log.jsonl.
func NewCostLog() *CostLog {
	dir := filepath.Join(os.Getenv("HOME"), ".blockrun")
	os.MkdirAll(dir, 0755)
	return &CostLog{path: filepath.Join(dir, "cost_log.jsonl")}
}

// newCostLogWithPath creates a CostLog writing to a custom path (for testing).
func newCostLogWithPath(path string) *CostLog {
	return &CostLog{path: path}
}

// Append writes a cost log entry to the JSONL file.
func (cl *CostLog) Append(endpoint string, costUSD float64) error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	entry := CostLogEntry{
		Timestamp: float64(time.Now().UnixMilli()) / 1000.0,
		Endpoint:  endpoint,
		CostUSD:   costUSD,
	}

	f, err := os.OpenFile(cl.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	_, err = f.Write(append(data, '\n'))
	return err
}

// Summary reads all log entries and returns an aggregate CostSummary.
func (cl *CostLog) Summary() (*CostSummary, error) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	summary := &CostSummary{
		ByEndpoint: make(map[string]float64),
	}

	f, err := os.Open(cl.path)
	if err != nil {
		if os.IsNotExist(err) {
			return summary, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry CostLogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip malformed lines
		}

		summary.TotalUSD += entry.CostUSD
		summary.Calls++
		summary.ByEndpoint[entry.Endpoint] += entry.CostUSD
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return summary, nil
}
