package httpx

import (
	"net/http"
	"strconv"
)

// MaxPageLimit caps ?limit= on paginated lists (API_CONTRACT §1).
const MaxPageLimit = 200

// Page describes how a list was requested (Q-4): without page/limit the
// response is a bare array capped at MAX_UNPAGED_ROWS; with them it is
// {data, pagination}.
type Page struct {
	Paged  bool
	Page   int
	Limit  int
	Offset int
}

// ParsePage reads ?page=&limit=.
func ParsePage(r *http.Request, maxUnpaged int) Page {
	q := r.URL.Query()
	if q.Get("page") == "" && q.Get("limit") == "" {
		return Page{Limit: maxUnpaged}
	}
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > MaxPageLimit {
		limit = 20
		if q.Get("limit") != "" {
			limit = MaxPageLimit
		}
	}
	return Page{Paged: true, Page: page, Limit: limit, Offset: (page - 1) * limit}
}

// WriteList writes a list in the shape the request asked for.
func WriteList[T any](w http.ResponseWriter, p Page, items []T, total int) {
	if items == nil {
		items = []T{}
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	if !p.Paged {
		if total > len(items) {
			w.Header().Set("X-Truncated", "true")
		}
		WriteJSON(w, http.StatusOK, items)
		return
	}
	pages := 0
	if p.Limit > 0 {
		pages = (total + p.Limit - 1) / p.Limit
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"data":       items,
		"pagination": map[string]int{"total": total, "page": p.Page, "limit": p.Limit, "totalPages": pages},
	})
}
