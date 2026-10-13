package store

import (
	"context"
	"strings"
	"testing"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

func TestReplaceAndListWorkItems(t *testing.T) {
	s := openMem(t)
	res, err := s.UpsertEvents(context.Background(), []event.Event{planEvent(), commitEvent("aaa1111")})
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %d", len(items))
	}

	err = s.ReplaceWorkItems(context.Background(), []WorkItemDraft{{
		Title:  "Admin gate",
		Status: "",
		Events: []WorkItemLink{
			{EventID: res.IDs[1], Reason: "ref:GEN-301 in commit message"},
			{EventID: res.IDs[0], Reason: "ref:GEN-301 in branch"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	items, err = s.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != 1 || items[0].Title != "Admin gate" || items[0].Status != "" {
		t.Fatalf("items = %+v", items)
	}
	if len(items[0].Events) != 2 {
		t.Fatalf("events = %+v", items[0].Events)
	}
	// planEvent starts before the commit, so it is listed first.
	if items[0].Events[0].EventID != res.IDs[0] || items[0].Events[0].Reason != "ref:GEN-301 in branch" {
		t.Fatalf("first = %+v", items[0].Events[0])
	}
	if items[0].Events[1].SourceID != "aaa1111" || items[0].Events[1].Kind != event.KindCommit {
		t.Fatalf("second = %+v", items[0].Events[1])
	}

	err = s.ReplaceWorkItems(context.Background(), []WorkItemDraft{{
		Title:  "replacement",
		Events: []WorkItemLink{{EventID: res.IDs[0], Reason: "branch:topic"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	items, err = s.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != 1 || items[0].Title != "replacement" || len(items[0].Events) != 1 {
		t.Fatalf("replaced = %+v", items)
	}

	if err := s.ReplaceWorkItems(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	items, err = s.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("cleared = %+v", items)
	}
}

func TestListWorkItemsKeepsItemsSeparate(t *testing.T) {
	s := openMem(t)
	res, err := s.UpsertEvents(context.Background(), []event.Event{planEvent(), commitEvent("aaa1111")})
	if err != nil {
		t.Fatal(err)
	}
	err = s.ReplaceWorkItems(context.Background(), []WorkItemDraft{
		{Title: "Session", Events: []WorkItemLink{{EventID: res.IDs[0], Reason: "branch:topic"}}},
		{Title: "Commit", Events: []WorkItemLink{{EventID: res.IDs[1], Reason: "sha:aaa1111 in pr"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != 1 || items[1].ID != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Title != "Session" || len(items[0].Events) != 1 || items[0].Events[0].Source != "claude-code" {
		t.Fatalf("first = %+v", items[0])
	}
	if items[1].Title != "Commit" || len(items[1].Events) != 1 || items[1].Events[0].SourceID != "aaa1111" {
		t.Fatalf("second = %+v", items[1])
	}
	if items[0].CreatedAt.IsZero() || items[1].UpdatedAt.IsZero() {
		t.Fatal("timestamps were not stored")
	}
}

func TestReplaceWorkItemsRejectsBadLinks(t *testing.T) {
	s := openMem(t)
	res, err := s.UpsertEvents(context.Background(), []event.Event{planEvent()})
	if err != nil {
		t.Fatal(err)
	}
	kept := []WorkItemDraft{{
		Title:  "kept",
		Events: []WorkItemLink{{EventID: res.IDs[0], Reason: "ref:GEN-301 in branch"}},
	}}
	if err := s.ReplaceWorkItems(context.Background(), kept); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		items []WorkItemDraft
		want  string
	}{
		{
			name: "empty reason",
			items: []WorkItemDraft{{
				Events: []WorkItemLink{{EventID: res.IDs[0], Reason: "  "}},
			}},
			want: "missing a link reason",
		},
		{
			name: "duplicate event",
			items: []WorkItemDraft{
				{Events: []WorkItemLink{{EventID: res.IDs[0], Reason: "ref:GEN-301 ticket"}}},
				{Events: []WorkItemLink{{EventID: res.IDs[0], Reason: "branch:topic"}}},
			},
			want: "two work items",
		},
		{
			name:  "no events",
			items: []WorkItemDraft{{Title: "empty"}},
			want:  "no events",
		},
		{
			name: "missing event",
			items: []WorkItemDraft{{
				Events: []WorkItemLink{{EventID: 999, Reason: "ref:GEN-301 ticket"}},
			}},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.ReplaceWorkItems(context.Background(), tc.items)
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
			items, err := s.ListWorkItems(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].Title != "kept" || items[0].Events[0].Reason != "ref:GEN-301 in branch" {
				t.Fatalf("store changed: %+v", items)
			}
		})
	}
}
