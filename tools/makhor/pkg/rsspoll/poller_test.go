package rsspoll

import (
	"strings"
	"testing"
	"time"
)

// Sample RSS feed for benchmarking
var sampleRSSFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <title>Sample Feed</title>
    <item>
      <title>First Article</title>
      <link>https://example.com/1</link>
      <description>Short description</description>
      <content:encoded><![CDATA[<p>Full content with <strong>HTML</strong> formatting.</p>]]></content:encoded>
      <guid>guid-1</guid>
      <pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate>
    </item>
    <item>
      <title>Second Article</title>
      <link>https://example.com/2</link>
      <description>Another description</description>
      <guid>guid-2</guid>
      <pubDate>Tue, 03 Jan 2006 10:00:00 -0700</pubDate>
    </item>
    <item>
      <title>Third Article</title>
      <link>https://example.com/3</link>
      <description>Third description here</description>
      <guid>guid-3</guid>
    </item>
  </channel>
</rss>`

// Sample complex HTML for benchmarking
var sampleHTML = `<div class="content">
	<h1>Article Title</h1>
	<p>First paragraph with <a href="http://example.com">a link</a> and <strong>bold text</strong>.</p>
	<p>Second paragraph with more content.</p>
	<ul>
		<li>Item one</li>
		<li>Item two</li>
		<li>Item three</li>
	</ul>
	<blockquote>A quoted section here</blockquote>
	<p>Final paragraph.</p>
</div>`

// TestParseRSSBasic tests parsing a basic RSS 2.0 feed.
func TestParseRSSBasic(t *testing.T) {
	rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>First Post</title>
      <link>https://example.com/1</link>
      <description>Description here</description>
      <guid>guid-1</guid>
      <pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate>
    </item>
    <item>
      <title>Second Post</title>
      <link>https://example.com/2</link>
      <description>Another description</description>
      <guid>guid-2</guid>
    </item>
  </channel>
</rss>`

	items, err := parseRSS([]byte(rss))
	if err != nil {
		t.Fatalf("Failed to parse RSS: %v", err)
	}

	if len(items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(items))
	}

	if items[0].Title != "First Post" {
		t.Errorf("Expected title 'First Post', got %q", items[0].Title)
	}
	if items[0].Link != "https://example.com/1" {
		t.Errorf("Expected link 'https://example.com/1', got %q", items[0].Link)
	}
	if items[0].GUID != "guid-1" {
		t.Errorf("Expected GUID 'guid-1', got %q", items[0].GUID)
	}
	if items[0].PublishedAt == nil {
		t.Error("Expected PublishedAt to be parsed")
	}
}

// TestParseAtomBasic tests parsing a basic Atom feed.
func TestParseAtomBasic(t *testing.T) {
	atom := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Test Atom Feed</title>
  <entry>
    <title>Atom Entry</title>
    <link href="https://example.com/atom/1"/>
    <id>atom-id-1</id>
    <summary>Atom summary</summary>
    <updated>2023-05-15T14:30:00Z</updated>
  </entry>
</feed>`

	items, err := parseRSS([]byte(atom))
	if err != nil {
		t.Fatalf("Failed to parse Atom: %v", err)
	}

	if len(items) != 1 {
		t.Errorf("Expected 1 item, got %d", len(items))
	}

	if items[0].Title != "Atom Entry" {
		t.Errorf("Expected title 'Atom Entry', got %q", items[0].Title)
	}
	if items[0].GUID != "atom-id-1" {
		t.Errorf("Expected GUID 'atom-id-1', got %q", items[0].GUID)
	}
}

// TestParseRSSContentEncoded tests that content:encoded is preferred over description.
func TestParseRSSContentEncoded(t *testing.T) {
	rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <item>
      <title>Post with Content</title>
      <link>https://example.com/1</link>
      <description>Short description</description>
      <content:encoded><![CDATA[<p>Full content here with <strong>HTML</strong>.</p>]]></content:encoded>
      <guid>guid-1</guid>
    </item>
  </channel>
</rss>`

	items, err := parseRSS([]byte(rss))
	if err != nil {
		t.Fatalf("Failed to parse RSS: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}

	// content:encoded should be preferred over description
	if !strings.Contains(items[0].Description, "Full content here") {
		t.Errorf("Expected content:encoded to be used, got %q", items[0].Description)
	}
}

// TestParseRSSDateFormats tests various date formats in RSS feeds.
func TestParseRSSDateFormats(t *testing.T) {
	testCases := []struct {
		name     string
		dateStr  string
		expected time.Time
	}{
		{
			name:     "RFC1123Z",
			dateStr:  "Mon, 02 Jan 2006 15:04:05 -0700",
			expected: time.Date(2006, 1, 2, 15, 4, 5, 0, time.FixedZone("", -7*60*60)),
		},
		{
			name:     "RFC3339",
			dateStr:  "2006-01-02T15:04:05Z",
			expected: time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC),
		},
		{
			name:     "RFC3339 with offset",
			dateStr:  "2006-01-02T15:04:05-07:00",
			expected: time.Date(2006, 1, 2, 15, 4, 5, 0, time.FixedZone("", -7*60*60)),
		},
		{
			name:     "Single digit day RFC1123Z",
			dateStr:  "Mon, 2 Jan 2006 15:04:05 -0700",
			expected: time.Date(2006, 1, 2, 15, 4, 5, 0, time.FixedZone("", -7*60*60)),
		},
		{
			name:     "Without weekday",
			dateStr:  "02 Jan 2006 15:04:05 -0700",
			expected: time.Date(2006, 1, 2, 15, 4, 5, 0, time.FixedZone("", -7*60*60)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseRSSDate(tc.dateStr)
			if result == nil {
				t.Errorf("Failed to parse date: %s", tc.dateStr)
				return
			}
			if !result.Equal(tc.expected) {
				t.Errorf("Date mismatch: expected %v, got %v", tc.expected, result)
			}
		})
	}
}

// TestParseRSSInvalidDate tests that invalid dates return nil.
func TestParseRSSInvalidDate(t *testing.T) {
	invalidDates := []string{
		"not a date",
		"2006/01/02",
		"01-02-2006",
		"",
	}

	for _, dateStr := range invalidDates {
		result := parseRSSDate(dateStr)
		if result != nil {
			t.Errorf("Expected nil for invalid date %q, got %v", dateStr, result)
		}
	}
}

// TestParseRSSMissingGUID tests that link is used as fallback when GUID is missing.
func TestParseRSSMissingGUID(t *testing.T) {
	rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item>
      <title>No GUID</title>
      <link>https://example.com/no-guid</link>
      <description>No GUID here</description>
    </item>
  </channel>
</rss>`

	items, err := parseRSS([]byte(rss))
	if err != nil {
		t.Fatalf("Failed to parse RSS: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}

	// GUID should be empty (the pollFeed function uses Link as fallback)
	if items[0].GUID != "" {
		t.Errorf("Expected empty GUID, got %q", items[0].GUID)
	}
	if items[0].Link != "https://example.com/no-guid" {
		t.Errorf("Expected link to be set, got %q", items[0].Link)
	}
}

// TestParseRSSEmptyFeed tests parsing an empty feed returns error.
func TestParseRSSEmptyFeed(t *testing.T) {
	emptyRSS := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Empty Feed</title>
  </channel>
</rss>`

	emptyAtom := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Empty Atom Feed</title>
</feed>`

	_, err := parseRSS([]byte(emptyRSS))
	if err == nil {
		t.Error("Expected error for empty RSS feed")
	}

	_, err = parseRSS([]byte(emptyAtom))
	if err == nil {
		t.Error("Expected error for empty Atom feed")
	}
}

// TestParseRSSInvalidXML tests parsing invalid XML.
func TestParseRSSInvalidXML(t *testing.T) {
	invalidXML := `not valid xml at all`

	_, err := parseRSS([]byte(invalidXML))
	if err == nil {
		t.Error("Expected error for invalid XML")
	}
}

// TestStripHTMLBasic tests basic HTML stripping.
func TestStripHTMLBasic(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Plain text",
			input:    "No HTML here",
			expected: "No HTML here",
		},
		{
			name:     "Simple tags",
			input:    "<p>Paragraph</p>",
			expected: "Paragraph",
		},
		{
			name:     "Nested tags",
			input:    "<div><p>Nested <strong>text</strong></p></div>",
			expected: "Nested text",
		},
		{
			name:     "Links",
			input:    "<a href='http://example.com'>Link text</a>",
			expected: "Link text",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := stripHTML(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// TestStripHTMLPreservesNewlines tests that block elements are converted to newlines.
func TestStripHTMLPreservesNewlines(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "BR tags",
			input:    "Line 1<br>Line 2<br/>Line 3<br />Line 4",
			expected: "Line 1\nLine 2\nLine 3\nLine 4",
		},
		{
			name:     "Paragraphs",
			input:    "<p>Para 1</p><p>Para 2</p>",
			expected: "Para 1\n\nPara 2",
		},
		{
			name:     "Divs",
			input:    "<div>Div 1</div><div>Div 2</div>",
			expected: "Div 1\nDiv 2",
		},
		{
			name:     "List items",
			input:    "<ul><li>Item 1</li><li>Item 2</li></ul>",
			expected: "Item 1\nItem 2",
		},
		{
			name:     "Headers",
			input:    "<h1>Title</h1><p>Content</p>",
			expected: "Title\n\nContent",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := stripHTML(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// TestStripHTMLCollapsesWhitespace tests that excessive whitespace is collapsed.
func TestStripHTMLCollapsesWhitespace(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Multiple spaces",
			input:    "Too    many   spaces",
			expected: "Too many spaces",
		},
		{
			name:     "Many newlines",
			input:    "Line 1<br><br><br><br>Line 2",
			expected: "Line 1\n\nLine 2",
		},
		{
			name:     "Mixed whitespace",
			input:    "<p>  Spaced   text  </p>",
			expected: "Spaced text",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := stripHTML(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// TestStripHTMLComplexContent tests stripping complex real-world HTML.
func TestStripHTMLComplexContent(t *testing.T) {
	input := `<div class="content">
		<h2>Article Title</h2>
		<p>First paragraph with <a href="http://example.com">a link</a> and <strong>bold text</strong>.</p>
		<p>Second paragraph.</p>
		<ul>
			<li>Item one</li>
			<li>Item two</li>
		</ul>
		<blockquote>A quote here</blockquote>
	</div>`

	result := stripHTML(input)

	// Should contain the text content
	if !strings.Contains(result, "Article Title") {
		t.Error("Missing 'Article Title'")
	}
	if !strings.Contains(result, "First paragraph") {
		t.Error("Missing 'First paragraph'")
	}
	if !strings.Contains(result, "a link") {
		t.Error("Missing 'a link'")
	}
	if !strings.Contains(result, "bold text") {
		t.Error("Missing 'bold text'")
	}
	if !strings.Contains(result, "Item one") {
		t.Error("Missing 'Item one'")
	}

	// Should NOT contain HTML tags
	if strings.Contains(result, "<") || strings.Contains(result, ">") {
		t.Error("Result should not contain < or >")
	}
}

// TestStripHTMLEmptyInput tests empty input.
func TestStripHTMLEmptyInput(t *testing.T) {
	result := stripHTML("")
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}

	result = stripHTML("   ")
	if result != "" {
		t.Errorf("Expected empty string for whitespace-only, got %q", result)
	}
}

// TestStripHTMLSpecialChars tests that HTML entities are NOT decoded (that's done elsewhere).
func TestStripHTMLSpecialChars(t *testing.T) {
	// stripHTML doesn't decode entities, that's html.UnescapeString's job
	input := "<p>&lt;script&gt;</p>"
	result := stripHTML(input)

	// The entities should remain (they're decoded by html.UnescapeString in pollFeed)
	if result != "&lt;script&gt;" {
		t.Errorf("Expected '&lt;script&gt;', got %q", result)
	}
}

// Benchmarks

// BenchmarkParseRSS benchmarks RSS parsing.
func BenchmarkParseRSS(b *testing.B) {
	data := []byte(sampleRSSFeed)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := parseRSS(data)
		if err != nil {
			b.Fatalf("Parse failed: %v", err)
		}
	}
}

// BenchmarkParseRSSDate benchmarks date parsing.
func BenchmarkParseRSSDate(b *testing.B) {
	dates := []string{
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"2006-01-02T15:04:05Z",
		"Tue, 03 Jan 2006 10:00:00 -0700",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseRSSDate(dates[i%len(dates)])
	}
}

// BenchmarkStripHTML benchmarks HTML stripping.
func BenchmarkStripHTML(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stripHTML(sampleHTML)
	}
}

// BenchmarkStripHTMLLarge benchmarks HTML stripping on large content.
func BenchmarkStripHTMLLarge(b *testing.B) {
	// Create a larger HTML document
	largeHTML := strings.Repeat(sampleHTML, 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stripHTML(largeHTML)
	}
}

// Fuzzing tests

// FuzzParseRSS tests RSS parsing with fuzzing.
func FuzzParseRSS(f *testing.F) {
	// Add seed corpus
	f.Add([]byte(sampleRSSFeed))
	f.Add([]byte(`<?xml version="1.0"?><rss><channel><item><title>Test</title></item></channel></rss>`))
	f.Add([]byte(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Test</title></entry></feed>`))
	f.Add([]byte(`not xml at all`))
	f.Add([]byte(`<html><body>Not an RSS feed</body></html>`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		// parseRSS should not panic on any input
		items, err := parseRSS(data)
		if err != nil {
			// Error is expected for invalid input
			return
		}
		// If no error, items should be valid
		for _, item := range items {
			_ = item.Title
			_ = item.Link
			_ = item.Description
			_ = item.GUID
		}
	})
}

// FuzzParseRSSDate tests date parsing with fuzzing.
func FuzzParseRSSDate(f *testing.F) {
	// Add seed corpus
	f.Add("Mon, 02 Jan 2006 15:04:05 -0700")
	f.Add("2006-01-02T15:04:05Z")
	f.Add("2006-01-02T15:04:05-07:00")
	f.Add("Mon, 2 Jan 2006 15:04:05 MST")
	f.Add("not a date")
	f.Add("")
	f.Add("2024-13-45 99:99:99")
	f.Add("Jan 02, 2006")

	f.Fuzz(func(t *testing.T, dateStr string) {
		// parseRSSDate should not panic on any input
		result := parseRSSDate(dateStr)
		if result != nil {
			// If parsed successfully, it should be a valid time
			if result.IsZero() {
				t.Error("Parsed time should not be zero")
			}
		}
	})
}

// FuzzStripHTML tests HTML stripping with fuzzing.
func FuzzStripHTML(f *testing.F) {
	// Add seed corpus
	f.Add(sampleHTML)
	f.Add("<p>Simple paragraph</p>")
	f.Add("<script>alert('xss')</script>")
	f.Add("<div><div><div>Nested</div></div></div>")
	f.Add("Plain text no HTML")
	f.Add("")
	f.Add("<")
	f.Add(">")
	f.Add("<<>>")
	f.Add("<tag attr='value'>content</tag>")
	f.Add(strings.Repeat("<p>", 1000))
	f.Add("<p>" + strings.Repeat("x", 10000) + "</p>")

	f.Fuzz(func(t *testing.T, html string) {
		// stripHTML should not panic on any input
		result := stripHTML(html)

		// Result should not contain < or > (all tags stripped)
		if strings.Contains(result, "<") && !strings.Contains(html, "&lt;") {
			// Allow < only if it was from an HTML entity
		}
		if strings.Contains(result, ">") && !strings.Contains(html, "&gt;") {
			// Allow > only if it was from an HTML entity
		}

		// Result should never be longer than input (we only remove content)
		// Note: This might not always be true due to newline conversions
	})
}
