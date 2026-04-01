package models

import (
	"testing"
)

func TestNewPagination(t *testing.T) {
	tests := []struct {
		name       string
		page       int
		perPage    int
		total      int
		wantPage   int
		wantPer    int
		wantTotal  int
		wantPages  int
	}{
		{
			name:      "normal pagination",
			page:      2,
			perPage:   30,
			total:     100,
			wantPage:  2,
			wantPer:   30,
			wantTotal: 100,
			wantPages: 4,
		},
		{
			name:      "page less than 1 defaults to 1",
			page:      0,
			perPage:   30,
			total:     100,
			wantPage:  1,
			wantPer:   30,
			wantTotal: 100,
			wantPages: 4,
		},
		{
			name:      "negative page defaults to 1",
			page:      -5,
			perPage:   30,
			total:     100,
			wantPage:  1,
			wantPer:   30,
			wantTotal: 100,
			wantPages: 4,
		},
		{
			name:      "perPage less than 1 defaults to 30",
			page:      1,
			perPage:   0,
			total:     100,
			wantPage:  1,
			wantPer:   30,
			wantTotal: 100,
			wantPages: 4,
		},
		{
			name:      "perPage over 100 capped to 100",
			page:      1,
			perPage:   200,
			total:     250,
			wantPage:  1,
			wantPer:   100,
			wantTotal: 250,
			wantPages: 3,
		},
		{
			name:      "exact page boundary",
			page:      1,
			perPage:   10,
			total:     100,
			wantPage:  1,
			wantPer:   10,
			wantTotal: 100,
			wantPages: 10,
		},
		{
			name:      "partial last page",
			page:      1,
			perPage:   30,
			total:     85,
			wantPage:  1,
			wantPer:   30,
			wantTotal: 85,
			wantPages: 3,
		},
		{
			name:      "zero total",
			page:      1,
			perPage:   30,
			total:     0,
			wantPage:  1,
			wantPer:   30,
			wantTotal: 0,
			wantPages: 0,
		},
		{
			name:      "single item",
			page:      1,
			perPage:   30,
			total:     1,
			wantPage:  1,
			wantPer:   30,
			wantTotal: 1,
			wantPages: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPagination(tt.page, tt.perPage, tt.total)
			if p.Page != tt.wantPage {
				t.Errorf("Page = %d, want %d", p.Page, tt.wantPage)
			}
			if p.PerPage != tt.wantPer {
				t.Errorf("PerPage = %d, want %d", p.PerPage, tt.wantPer)
			}
			if p.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", p.Total, tt.wantTotal)
			}
			if p.TotalPages != tt.wantPages {
				t.Errorf("TotalPages = %d, want %d", p.TotalPages, tt.wantPages)
			}
		})
	}
}

func TestPaginationOffset(t *testing.T) {
	tests := []struct {
		name       string
		page       int
		perPage    int
		wantOffset int
	}{
		{"page 1", 1, 30, 0},
		{"page 2", 2, 30, 30},
		{"page 3", 3, 30, 60},
		{"page 1 with 10 per page", 1, 10, 0},
		{"page 5 with 10 per page", 5, 10, 40},
		{"page 1 with 100 per page", 1, 100, 0},
		{"page 2 with 100 per page", 2, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPagination(tt.page, tt.perPage, 1000)
			if got := p.Offset(); got != tt.wantOffset {
				t.Errorf("Offset() = %d, want %d", got, tt.wantOffset)
			}
		})
	}
}

func TestPaginationHasPrev(t *testing.T) {
	tests := []struct {
		name string
		page int
		want bool
	}{
		{"page 1 has no prev", 1, false},
		{"page 2 has prev", 2, true},
		{"page 10 has prev", 10, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPagination(tt.page, 30, 1000)
			if got := p.HasPrev(); got != tt.want {
				t.Errorf("HasPrev() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPaginationHasNext(t *testing.T) {
	tests := []struct {
		name  string
		page  int
		total int
		want  bool
	}{
		{"first page with more pages", 1, 100, true},
		{"last page", 4, 100, false},
		{"beyond last page", 5, 100, false},
		{"single page result", 1, 10, false},
		{"empty result", 1, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPagination(tt.page, 30, tt.total)
			if got := p.HasNext(); got != tt.want {
				t.Errorf("HasNext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPaginationPageWindow(t *testing.T) {
	tests := []struct {
		name       string
		page       int
		totalPages int
		maxPages   int
		wantStart  int
		wantEnd    int
		wantFirst  bool
		wantLast   bool
	}{
		{
			name:       "few pages shows all",
			page:       3,
			totalPages: 5,
			maxPages:   10,
			wantStart:  1,
			wantEnd:    5,
			wantFirst:  false,
			wantLast:   false,
		},
		{
			name:       "many pages at start",
			page:       1,
			totalPages: 50,
			maxPages:   10,
			wantStart:  1,
			wantEnd:    10,
			wantFirst:  false,
			wantLast:   true,
		},
		{
			name:       "many pages in middle",
			page:       25,
			totalPages: 50,
			maxPages:   10,
			wantStart:  21,
			wantEnd:    30,
			wantFirst:  true,
			wantLast:   true,
		},
		{
			name:       "many pages at end",
			page:       50,
			totalPages: 50,
			maxPages:   10,
			wantStart:  41,
			wantEnd:    50,
			wantFirst:  true,
			wantLast:   false,
		},
		{
			name:       "page 5 of 50",
			page:       5,
			totalPages: 50,
			maxPages:   10,
			wantStart:  1,
			wantEnd:    10,
			wantFirst:  false,
			wantLast:   true,
		},
		{
			name:       "page 6 of 50",
			page:       6,
			totalPages: 50,
			maxPages:   10,
			wantStart:  2,
			wantEnd:    11,
			wantFirst:  true,
			wantLast:   true,
		},
		{
			name:       "page 45 of 50",
			page:       45,
			totalPages: 50,
			maxPages:   10,
			wantStart:  41,
			wantEnd:    50,
			wantFirst:  true,
			wantLast:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create pagination with enough items to get the desired total pages
			total := tt.totalPages * 30
			p := NewPagination(tt.page, 30, total)

			start, end, showFirst, showLast := p.PageWindow(tt.maxPages)

			if start != tt.wantStart {
				t.Errorf("start = %d, want %d", start, tt.wantStart)
			}
			if end != tt.wantEnd {
				t.Errorf("end = %d, want %d", end, tt.wantEnd)
			}
			if showFirst != tt.wantFirst {
				t.Errorf("showFirst = %v, want %v", showFirst, tt.wantFirst)
			}
			if showLast != tt.wantLast {
				t.Errorf("showLast = %v, want %v", showLast, tt.wantLast)
			}
		})
	}
}

func TestPaginationWindowPages(t *testing.T) {
	// Test that WindowPages returns correct slice
	p := NewPagination(25, 30, 1500) // 50 pages, current is 25

	pages := p.WindowPages()

	// Should have exactly 10 pages
	if len(pages) != 10 {
		t.Errorf("WindowPages() returned %d pages, want 10", len(pages))
	}

	// Current page (25) should be in the window
	found := false
	for _, page := range pages {
		if page == 25 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("WindowPages() should contain current page 25, got %v", pages)
	}

	// Pages should be consecutive
	for i := 1; i < len(pages); i++ {
		if pages[i] != pages[i-1]+1 {
			t.Errorf("WindowPages() should be consecutive, got %v", pages)
			break
		}
	}
}

func TestPaginationShowFirstLastPage(t *testing.T) {
	// Few pages - no ellipsis needed
	p := NewPagination(3, 30, 150) // 5 pages
	if p.ShowFirstPage() {
		t.Error("ShowFirstPage() should be false for few pages")
	}
	if p.ShowLastPage() {
		t.Error("ShowLastPage() should be false for few pages")
	}

	// At start of many pages
	p = NewPagination(1, 30, 1500) // 50 pages
	if p.ShowFirstPage() {
		t.Error("ShowFirstPage() should be false when at start")
	}
	if !p.ShowLastPage() {
		t.Error("ShowLastPage() should be true when at start of many pages")
	}

	// In middle of many pages
	p = NewPagination(25, 30, 1500) // 50 pages
	if !p.ShowFirstPage() {
		t.Error("ShowFirstPage() should be true when in middle")
	}
	if !p.ShowLastPage() {
		t.Error("ShowLastPage() should be true when in middle")
	}

	// At end of many pages
	p = NewPagination(50, 30, 1500) // 50 pages
	if !p.ShowFirstPage() {
		t.Error("ShowFirstPage() should be true when at end")
	}
	if p.ShowLastPage() {
		t.Error("ShowLastPage() should be false when at end")
	}
}

func TestPostSourceConstants(t *testing.T) {
	// Verify source constants have expected values
	if SourceUser != "user" {
		t.Errorf("SourceUser = %q, want %q", SourceUser, "user")
	}
	if SourceRSS != "rss" {
		t.Errorf("SourceRSS = %q, want %q", SourceRSS, "rss")
	}
	if SourceBot != "bot" {
		t.Errorf("SourceBot = %q, want %q", SourceBot, "bot")
	}
	if SourceAPI != "api" {
		t.Errorf("SourceAPI = %q, want %q", SourceAPI, "api")
	}
}

func TestActionTypeConstants(t *testing.T) {
	// Verify a sampling of action constants
	expected := map[string]string{
		"ActionUserCreate":    ActionUserCreate,
		"ActionPostCreate":    ActionPostCreate,
		"ActionCommentCreate": ActionCommentCreate,
		"ActionVotePost":      ActionVotePost,
		"ActionInviteCreate":  ActionInviteCreate,
		"ActionModBan":        ActionModBan,
		"ActionTagCreate":     ActionTagCreate,
	}

	if ActionUserCreate != "user_create" {
		t.Errorf("ActionUserCreate = %q, want %q", expected["ActionUserCreate"], "user_create")
	}
	if ActionPostCreate != "post_create" {
		t.Errorf("ActionPostCreate = %q, want %q", expected["ActionPostCreate"], "post_create")
	}
	if ActionCommentCreate != "comment_create" {
		t.Errorf("ActionCommentCreate = %q, want %q", expected["ActionCommentCreate"], "comment_create")
	}
}
