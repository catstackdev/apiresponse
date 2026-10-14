package apiresponse_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/catstackdev/apiresponse"
)

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestOK_KeepsZeroValues(t *testing.T) {
	if got := marshal(t, apiresponse.OK(0)); got != `{"data":0}` {
		t.Fatalf("got %s", got)
	}
}

func TestList_NilSliceIsEmptyArray(t *testing.T) {
	got := marshal(t, apiresponse.List[string](nil, apiresponse.CursorPage(10, nil)))
	want := `{"data":[],"pagination":{"limit":10,"has_more":false}}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestCursorPage(t *testing.T) {
	next := "abc"
	p := apiresponse.CursorPage(20, &next)
	if !p.HasMore || *p.NextCursor != "abc" {
		t.Fatalf("unexpected %+v", p)
	}
}

func TestOffsetPage_HasMore(t *testing.T) {
	cases := []struct {
		page, limit int
		total       int64
		want        bool
	}{
		{1, 10, 25, true},
		{3, 10, 25, false},
		{2, 10, 20, false},
	}
	for _, c := range cases {
		if got := apiresponse.OffsetPage(c.page, c.limit, c.total).HasMore; got != c.want {
			t.Errorf("page=%d limit=%d total=%d: got %v", c.page, c.limit, c.total, got)
		}
	}
}

func TestOffsetPage_ClampsBadInput(t *testing.T) {
	cases := []struct {
		name              string
		page, limit       int
		total             int64
		wantPage, wantLim int
		wantTotal         int64
		wantMore          bool
	}{
		{"page below 1", 0, 10, 50, 1, 10, 50, true},
		{"negative limit", 1, -5, 50, 1, 0, 50, false},
		{"zero limit never has more", 1, 0, 50, 1, 0, 50, false},
		{"negative total", 1, 10, -3, 1, 10, 0, false},
		{"huge values do not overflow", 1 << 30, 1 << 30, 1 << 62, 1 << 30, 1 << 30, 1 << 62, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := apiresponse.OffsetPage(c.page, c.limit, c.total)
			if *p.Page != c.wantPage || p.Limit != c.wantLim || *p.Total != c.wantTotal || p.HasMore != c.wantMore {
				t.Fatalf("got page=%d limit=%d total=%d more=%v", *p.Page, p.Limit, *p.Total, p.HasMore)
			}
		})
	}
}

func TestPagination_JSONShapes(t *testing.T) {
	next := "c2"
	cursor := marshal(t, apiresponse.CursorPage(5, &next))
	if cursor != `{"limit":5,"has_more":true,"next_cursor":"c2"}` {
		t.Fatalf("cursor: %s", cursor)
	}
	offset := marshal(t, apiresponse.OffsetPage(2, 10, 35))
	if offset != `{"limit":10,"has_more":true,"page":2,"total":35}` {
		t.Fatalf("offset: %s", offset)
	}
}

func TestOK_Struct(t *testing.T) {
	type u struct {
		ID int `json:"id"`
	}
	if got := marshal(t, apiresponse.OK(u{ID: 7})); got != `{"data":{"id":7}}` {
		t.Fatalf("got %s", got)
	}
}

func TestListWithSummary(t *testing.T) {
	type totals struct {
		Amount int `json:"amount"`
	}
	resp := apiresponse.ListWithSummary([]int{1}, apiresponse.OffsetPage(1, 10, 1), totals{Amount: 42})
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	const want = `"summary":{"amount":42}`
	if !strings.Contains(string(b), want) {
		t.Fatalf("%s missing %s", b, want)
	}
	b, _ = json.Marshal(apiresponse.ListWithSummary([]int{}, apiresponse.OffsetPage(1, 10, 0), nil))
	if strings.Contains(string(b), "summary") {
		t.Fatalf("nil summary must be omitted: %s", b)
	}
}

func TestOffsetPage_NoOverflow(t *testing.T) {
	if p := apiresponse.OffsetPage(1<<40, 1<<40, 100); p.HasMore {
		t.Fatal("huge page and limit must not report HasMore")
	}
	if p := apiresponse.OffsetPage(1, 1<<40, 1<<41); !p.HasMore {
		t.Fatal("first page of a larger set must report HasMore")
	}
}
