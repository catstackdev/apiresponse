package apiresponse

// Response is the success envelope. Data is a pointer so that a legitimately
// empty value (0, "", false) is still serialized. Summary is optional extra
// data about the whole result set, such as report totals; see [ListWithSummary].
type Response[T any] struct {
	Data       *T          `json:"data"`
	Pagination *Pagination `json:"pagination,omitempty"`
	Summary    any         `json:"summary,omitempty"`
}

// Pagination describes a page of results. It supports both cursor-based
// (NextCursor) and offset-based (Page, Total) paging; unused fields are omitted.
type Pagination struct {
	Limit      int     `json:"limit"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor,omitempty"`
	Page       *int    `json:"page,omitempty"`
	Total      *int64  `json:"total,omitempty"`
}

// OK wraps data in a success envelope.
func OK[T any](data T) Response[T] {
	return Response[T]{Data: &data}
}

// List wraps items in a success envelope with pagination. A nil slice is
// serialized as [] rather than null.
func List[T any](items []T, p Pagination) Response[[]T] {
	if items == nil {
		items = []T{}
	}
	return Response[[]T]{Data: &items, Pagination: &p}
}

// ListWithSummary is [List] plus a summary of the whole filtered set (not just
// this page), e.g. a grand total for a report. A nil (untyped) summary is omitted; a typed nil
// pointer is encoded as null.
func ListWithSummary[T any](items []T, p Pagination, summary any) Response[[]T] {
	r := List(items, p)
	r.Summary = summary
	return r
}

// CursorPage builds cursor-based pagination. A nil next means the last page.
// A negative limit becomes 0, like in [OffsetPage].
func CursorPage(limit int, next *string) Pagination {
	return Pagination{Limit: max(limit, 0), HasMore: next != nil, NextCursor: next}
}

// OffsetPage builds offset-based pagination. page is 1-based. Out-of-range
// input is clamped: page < 1 becomes 1, limit < 0 becomes 0, total < 0 becomes
// 0. With a limit of 0 there is never a next page.
func OffsetPage(page, limit int, total int64) Pagination {
	page = max(page, 1)
	limit = max(limit, 0)
	total = max(total, 0)
	return Pagination{
		Limit:   limit,
		HasMore: limit > 0 && int64(page) < pageCount(total, int64(limit)),
		Page:    &page,
		Total:   &total,
	}
}

// pageCount is ceil(total/limit) written so it cannot overflow for huge totals.
func pageCount(total, limit int64) int64 {
	pages := total / limit
	if total%limit != 0 {
		pages++
	}
	return pages
}
