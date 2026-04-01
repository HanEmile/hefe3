// Package rsspoll provides RSS feed polling functionality.
package rsspoll

import (
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"makhor/pkg/db"
	"makhor/pkg/models"
)

// Precompiled regex patterns for HTML stripping
var (
	multiNewlineRegex = regexp.MustCompile(`\n{3,}`)
	multiSpaceRegex   = regexp.MustCompile(`[ \t]+`)
)

// MaxConcurrentPolls limits concurrent feed polling goroutines.
const MaxConcurrentPolls = 10

// Poller polls RSS feeds and creates posts from new items.
type Poller struct {
	DB       *db.DB
	BotUser  int64 // User ID to create posts as
	Interval time.Duration
	Client   *http.Client
	stop     chan struct{}
	wg       sync.WaitGroup
	sem      chan struct{} // Semaphore for limiting concurrent polls
}

// New creates a new RSS poller.
func New(database *db.DB, botUserID int64, interval time.Duration) *Poller {
	return &Poller{
		DB:       database,
		BotUser:  botUserID,
		Interval: interval,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
		stop: make(chan struct{}),
		sem:  make(chan struct{}, MaxConcurrentPolls),
	}
}

// Start begins the polling loop.
func (p *Poller) Start() {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		// Poll immediately on start
		p.pollAll()

		ticker := time.NewTicker(p.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.pollAll()
			case <-p.stop:
				return
			}
		}
	}()
	log.Printf("RSS poller started (checking every %v)", p.Interval)
}

// Stop stops the polling loop.
func (p *Poller) Stop() {
	close(p.stop)
	p.wg.Wait()
	log.Println("RSS poller stopped")
}

// pollAll polls all feeds that are due (excludes poll_on_view feeds).
func (p *Poller) pollAll() {
	feeds, err := p.DB.GetFeedsDueForPolling()
	if err != nil {
		log.Printf("Error getting feeds due for polling: %v", err)
		return
	}

	for _, feed := range feeds {
		if err := p.pollFeed(feed); err != nil {
			log.Printf("Error polling feed %d (%s): %v", feed.ID, feed.URL, err)
			p.DB.UpdateFeedError(feed.ID, err.Error())
		} else {
			p.DB.UpdateFeedPolled(feed.ID)
		}
	}
}

// PollTagFeeds polls all poll_on_view feeds for a specific tag.
// Called when a tag page is viewed to refresh content.
func (p *Poller) PollTagFeeds(tagID int64) {
	feeds, err := p.DB.GetFeedsDueForViewPolling(tagID)
	if err != nil {
		log.Printf("Error getting view-poll feeds for tag %d: %v", tagID, err)
		return
	}

	var wg sync.WaitGroup
	for _, feed := range feeds {
		wg.Add(1)
		go func(f *db.RSSFeed) {
			defer wg.Done()
			// Acquire semaphore to limit concurrent polls
			p.sem <- struct{}{}
			defer func() { <-p.sem }()

			if err := p.pollFeed(f); err != nil {
				log.Printf("Error polling feed %d (%s): %v", f.ID, f.URL, err)
				p.DB.UpdateFeedError(f.ID, err.Error())
			} else {
				p.DB.UpdateFeedPolled(f.ID)
			}
		}(feed)
	}
	wg.Wait()
}

// PollFeedNow polls a specific feed immediately.
func (p *Poller) PollFeedNow(feedID int64) error {
	feed, err := p.DB.GetRSSFeedByID(feedID)
	if err != nil {
		return fmt.Errorf("feed not found: %w", err)
	}
	if err := p.pollFeed(feed); err != nil {
		p.DB.UpdateFeedError(feed.ID, err.Error())
		return err
	}
	p.DB.UpdateFeedPolled(feed.ID)
	return nil
}

// pollFeed fetches and processes a single feed.
func (p *Poller) pollFeed(feed *db.RSSFeed) error {
	startTime := time.Now()
	var httpStatus int
	var itemsFound, itemsImported int
	var pollErr error

	// Defer logging the poll result
	defer func() {
		durationMs := int(time.Since(startTime).Milliseconds())
		errMsg := ""
		if pollErr != nil {
			errMsg = pollErr.Error()
		}
		p.DB.LogRSSPoll(feed.ID, pollErr == nil, httpStatus, itemsFound, itemsImported, errMsg, durationMs)
	}()

	resp, err := p.Client.Get(feed.URL)
	if err != nil {
		pollErr = fmt.Errorf("fetching feed: %w", err)
		return pollErr
	}
	defer resp.Body.Close()

	httpStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		pollErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		return pollErr
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		pollErr = fmt.Errorf("reading body: %w", err)
		return pollErr
	}

	items, err := parseRSS(body)
	if err != nil {
		pollErr = fmt.Errorf("parsing RSS: %w", err)
		return pollErr
	}

	itemsFound = len(items)

	for _, item := range items {
		guid := item.GUID
		if guid == "" {
			guid = item.Link
		}
		if guid == "" {
			continue
		}

		// Skip if already imported
		if p.DB.HasRSSItem(feed.ID, guid) {
			continue
		}

		// Create post
		title := html.UnescapeString(item.Title)
		if len(title) > 200 {
			title = title[:197] + "..."
		}

		postBody := ""
		if item.Description != "" {
			postBody = html.UnescapeString(stripHTML(item.Description))
			// Limit to 5000 characters to keep posts reasonable
			if len(postBody) > 5000 {
				// Try to cut at a sentence or paragraph boundary
				cutPoint := 4900
				for i := 4900; i < 5000 && i < len(postBody); i++ {
					if postBody[i] == '.' || postBody[i] == '\n' {
						cutPoint = i + 1
						break
					}
				}
				postBody = strings.TrimSpace(postBody[:cutPoint]) + "..."
			}
		}

		// Create post with RSS source tracking, using original publish time if available
		feedID := feed.ID
		post, err := p.DB.CreatePostWithSourceAndTime(p.BotUser, title, item.Link, postBody, []int64{feed.TagID}, models.SourceRSS, &feedID, item.PublishedAt)
		if err != nil {
			log.Printf("Error creating post for RSS item %s: %v", guid, err)
			continue
		}

		p.DB.CreateRSSItem(feed.ID, guid, post.ID)
		itemsImported++

		// Respect max_items_per_poll limit
		if feed.MaxItemsPerPoll > 0 && itemsImported >= feed.MaxItemsPerPoll {
			break
		}
	}

	if itemsImported > 0 {
		log.Printf("Imported %d items from feed %s", itemsImported, feed.URL)
	}

	return nil
}

// RSS feed structures
type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title          string `xml:"title"`
	Link           string `xml:"link"`
	Description    string `xml:"description"`
	ContentEncoded string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	GUID           string `xml:"guid"`
	PubDate        string `xml:"pubDate"`
}

type atomEntry struct {
	Title   string    `xml:"title"`
	Link    atomLink  `xml:"link"`
	ID      string    `xml:"id"`
	Summary string    `xml:"summary"`
	Content string    `xml:"content"`
	Updated string    `xml:"updated"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

// FeedItem is a normalized feed item.
type FeedItem struct {
	Title       string
	Link        string
	Description string
	GUID        string
	PublishedAt *time.Time
}

// parseRSS parses RSS 2.0 or Atom feeds.
func parseRSS(data []byte) ([]FeedItem, error) {
	// Try RSS 2.0 first
	var rss rssFeed
	if err := xml.Unmarshal(data, &rss); err == nil && len(rss.Channel.Items) > 0 {
		var items []FeedItem
		for _, item := range rss.Channel.Items {
			// Prefer content:encoded over description (it usually has full content)
			desc := item.ContentEncoded
			if desc == "" {
				desc = item.Description
			}
			feedItem := FeedItem{
				Title:       item.Title,
				Link:        item.Link,
				Description: desc,
				GUID:        item.GUID,
			}
			if item.PubDate != "" {
				if t := parseRSSDate(item.PubDate); t != nil {
					feedItem.PublishedAt = t
				}
			}
			items = append(items, feedItem)
		}
		return items, nil
	}

	// Try Atom
	var atom atomFeed
	if err := xml.Unmarshal(data, &atom); err == nil && len(atom.Entries) > 0 {
		var items []FeedItem
		for _, entry := range atom.Entries {
			link := ""
			for _, l := range []atomLink{entry.Link} {
				if l.Href != "" {
					link = l.Href
					break
				}
			}

			desc := entry.Summary
			if desc == "" {
				desc = entry.Content
			}

			feedItem := FeedItem{
				Title:       entry.Title,
				Link:        link,
				Description: desc,
				GUID:        entry.ID,
			}
			if entry.Updated != "" {
				if t := parseRSSDate(entry.Updated); t != nil {
					feedItem.PublishedAt = t
				}
			}
			items = append(items, feedItem)
		}
		return items, nil
	}

	return nil, fmt.Errorf("could not parse as RSS or Atom")
}

// parseRSSDate tries to parse various date formats used in RSS/Atom feeds.
func parseRSSDate(dateStr string) *time.Time {
	formats := []string{
		time.RFC1123Z,                    // "Mon, 02 Jan 2006 15:04:05 -0700"
		time.RFC1123,                     // "Mon, 02 Jan 2006 15:04:05 MST"
		time.RFC3339,                     // "2006-01-02T15:04:05Z07:00"
		time.RFC3339Nano,                 // "2006-01-02T15:04:05.999999999Z07:00"
		"2006-01-02T15:04:05Z",           // ISO 8601 UTC
		"2006-01-02T15:04:05-07:00",      // ISO 8601 with timezone
		"2006-01-02 15:04:05",            // Simple datetime
		"Mon, 2 Jan 2006 15:04:05 -0700", // RFC1123Z with single digit day
		"Mon, 2 Jan 2006 15:04:05 MST",   // RFC1123 with single digit day
		"02 Jan 2006 15:04:05 -0700",     // Without weekday
		"2 Jan 2006 15:04:05 -0700",      // Without weekday, single digit day
	}

	dateStr = strings.TrimSpace(dateStr)
	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return &t
		}
	}
	return nil
}

// stripHTML removes HTML tags from a string, converting block elements to line breaks.
func stripHTML(s string) string {
	// First, convert block-level elements to newlines
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = strings.ReplaceAll(s, "</p>", "\n\n")
	s = strings.ReplaceAll(s, "</div>", "\n")
	s = strings.ReplaceAll(s, "</li>", "\n")
	s = strings.ReplaceAll(s, "</tr>", "\n")
	s = strings.ReplaceAll(s, "</h1>", "\n\n")
	s = strings.ReplaceAll(s, "</h2>", "\n\n")
	s = strings.ReplaceAll(s, "</h3>", "\n\n")
	s = strings.ReplaceAll(s, "</h4>", "\n")
	s = strings.ReplaceAll(s, "</blockquote>", "\n")

	// Strip remaining HTML tags using efficient character-based approach
	var result strings.Builder
	result.Grow(len(s)) // Preallocate
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			result.WriteRune(r)
		}
	}

	text := result.String()

	// Normalize whitespace using precompiled regex
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		// Collapse multiple spaces/tabs within each line
		lines[i] = strings.TrimSpace(multiSpaceRegex.ReplaceAllString(line, " "))
	}
	text = strings.Join(lines, "\n")

	// Collapse 3+ consecutive newlines to 2 using precompiled regex
	text = multiNewlineRegex.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}
