package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/antchfx/htmlquery"
)

type Node struct {
	// ID defines the unique identifier of a node
	ID   int    `json:"id"`

	// Name defines the name of a node
	Name string `json:"name"`

	// Base defines the domain of a path
	Base string `json:"base"`

	// Type defines the type of a node
	Type string `json:"type"`
}

type GraphData struct {
	Nodes map[int]Node  `json:"nodes"`
	OutLinks map[int][]int `json:"links"`
}

type Job struct {
	ID        string    `json:"id"`
	NodeID    int       `json:"node_id"`
	NodeName  string    `json:"node_name"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Progress  int       `json:"progress"`
	Threads   int       `json:"threads"`
	StartTime time.Time `json:"start_time"`
	Machine   string    `json:"machine"`
}

type JobConfig struct {
	MaxConcurrent int      `json:"max_concurrent"`
	Workers       []Worker `json:"workers"`
}

type Worker struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Status   string `json:"status"`
	Capacity int    `json:"capacity"`
}

var (
	graphData = GraphData{
		Nodes: map[int]Node{
			1: {
				ID:   1,
				Name: "https://emile.space",
				Base: "emile.space",
				Type: "domain",
			},
			2: {
				ID:   2,
				Name: "/abc",
				Base: "emile.space",
				Type: "path",
			},
			3: {
				ID:   3,
				Name: "/def",
				Base: "emile.space",
				Type: "path",
			},
		},
		Links: map[int][]int{
			1: {2, 3},
		},
	}
	jobs      = []Job{}
	jobConfig = JobConfig{
		MaxConcurrent: 5,
		Workers: []Worker{
			{
				ID:       0,
				Name:     "Local Machine",
				IP:       "127.0.0.1",
				Status:   "active",
				Capacity: 10,
			},
		},
	}
	mu sync.RWMutex
)

func main() {
	go wshub.run() // Start the hub

	http.HandleFunc("/ws", handleWebSocket) // New WS endpoint

	http.Handle("/", http.FileServer(http.Dir("./static")))

	http.HandleFunc("/api/graph", handleGraph)
	http.HandleFunc("/api/nodes", handleNodes)
	http.HandleFunc("/api/links", handleLinks)
	http.HandleFunc("/api/jobs", handleJobs)
	http.HandleFunc("/api/jobs/", handleJobUpdate)
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/workers", handleWorkers)

	http.HandleFunc("/spider", spiderHandler)

	log.Println("Server starting on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

type SpiderRequest struct {
	Node     Node `json:"node"`
	MaxDepth int  `json:"maxDepth"`
}

func spiderHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Spidering")

	var spiderRequest SpiderRequest
	if err := json.NewDecoder(r.Body).Decode(&spiderRequest); err != nil {
		log.Println("err: ", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Println("Target: ", spiderRequest.Node)
	log.Println("MaxDepth: ", spiderRequest.MaxDepth)

	if spiderRequest.Node.Type != "domain" {
		log.Println("Can't spider a non domain")
		return
	}

	target, err := url.Parse(spiderRequest.Node.Name)
	if err != nil {
		log.Println("err: ", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	currentNodeId := spiderRequest.Node.ID

	err = fetchURL(*target, currentNodeId, spiderRequest.MaxDepth)
	if err != nil {
		log.Println("err: ", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spiderRequest)
}

func fetchURL(url url.URL, nodeId int, depth int) error {
	log.Println("getting", url.String())

	if depth > 0 {
		doc, err := htmlquery.LoadURL(url.String())
		if err != nil {
			return err
		}

		list := htmlquery.Find(doc, "//a/@href")

		// for each url found
		for i, n := range list {
			linkText := htmlquery.InnerText(n)

			// disect the url into individual pieces that are inserted as needed
			parts := strings.Split(linkText, "/")
			log.Printf("[+] %s", linkText)
			log.Printf("[%d] parts: %v", i, parts)

			// Handle raw domains
			if parts[0] == "http:" || parts[0] == "https:" {
				domain := parts[2]

				alreadyExists := false
				for _, n := range graphData.Nodes {
					if n.Name == domain {
						alreadyExists = true
					}
				}

				if alreadyExists == false {
					node := Node{
						Name: domain,
						Type: "domain",
						ID:   len(graphData.Nodes) + 1,
					}
					mu.Lock()
					graphData.Nodes[node.ID] = node
					mu.Unlock()

					mu.Lock()
					graphData.Links[nodeId] = append(graphData.Links[nodeId], node.ID)
					mu.Unlock()
				}
			}

			log.Println("linkText: ", linkText)

			// the root itself
			if linkText == "/" {
				continue
			}

			// insert the node
			node := Node{
				Name: linkText,
				Type: "path",
				ID:   len(graphData.Nodes) + 1,
			}
			mu.Lock()
			graphData.Nodes[node.ID] = node
			mu.Unlock()

			mu.Lock()
			graphData.Links[nodeId] = append(graphData.Links[nodeId], node.ID)
			mu.Unlock()

			// build path for further fetching

			var newPathStr string
			if strings.HasPrefix(linkText, "/") {
				newPathStr = url.Hostname() + linkText
			} else if strings.HasPrefix(linkText, "https://") {
				newPathStr = linkText
			} else {
				newPathStr = url.String() + linkText
			}

			newPath, err := url.Parse(newPathStr)
			if err != nil {
				log.Println("err: ", err)
				continue
			}
			log.Println("newPath: ", newPath)

			if newPath.Hostname() != node.Base {
				log.Println("hostname mismatch")
				continue
			}

			err = fetchURL(*newPath, node.ID, depth-1)
			if err != nil {
				log.Println(err)
				return err
			}
		}
	} else {
		return nil
	}

	return nil
}

// handleGraph returns the graph data in JSON format.
func handleGraph(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(graphData)
}

// handleNodes allows modifying nodes in the graph.
func handleNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		var node Node
		if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		graphData.Nodes[node.ID] = node
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(node)

	case "PUT":
		var node Node
		if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		for i, n := range graphData.Nodes {
			if n.ID == node.ID {
				graphData.Nodes[i] = node
				break
			}
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(node)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleLinks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		var links map[int][]int
		if err := json.NewDecoder(r.Body).Decode(&links); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		for k, v := range links {
			graphData.Links[k] = v
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(links)

	case "PUT":
		var links map[int][]int
		if err := json.NewDecoder(r.Body).Decode(&links); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		for k, v := range links {
			graphData.Links[k] = v
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(links)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		mu.RLock()
		defer mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jobs)

	case "POST":
		var job Job
		if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		job.StartTime = time.Now()
		if job.Status == "" {
			job.Status = "queued"
		}
		if job.Machine == "" {
			job.Machine = "local"
		}

		mu.Lock()
		jobs = append(jobs, job)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleJobUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" && r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Path[len("/api/jobs/"):]

	if r.Method == "DELETE" {
		mu.Lock()
		for i, job := range jobs {
			if job.ID == jobID {
				jobs = append(jobs[:i], jobs[i+1:]...)
				break
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	mu.Lock()
	for i, job := range jobs {
		if job.ID == jobID {
			if status, ok := updates["status"].(string); ok {
				jobs[i].Status = status
			}
			if threads, ok := updates["threads"].(float64); ok {
				jobs[i].Threads = int(threads)
			}
			if progress, ok := updates["progress"].(float64); ok {
				jobs[i].Progress = int(progress)
			}
			if machine, ok := updates["machine"].(string); ok {
				jobs[i].Machine = machine
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jobs[i])
			mu.Unlock()
			return
		}
	}
	mu.Unlock()

	http.Error(w, "Job not found", http.StatusNotFound)
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		mu.RLock()
		defer mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jobConfig)

	case "PUT":
		var config JobConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		jobConfig = config
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jobConfig)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleWorkers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		mu.RLock()
		defer mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jobConfig.Workers)

	case "POST":
		var worker Worker
		if err := json.NewDecoder(r.Body).Decode(&worker); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		jobConfig.Workers = append(jobConfig.Workers, worker)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(worker)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
