package orders

import (
	"context"
	"testing"
)

func TestListAll(t *testing.T) {
	s, _, _, c := setup(t)
	ctx := context.Background()
	first := submitted(t, s, c) // pending_review
	second, err := s.Create(ctx, c, 77, Request{Kind: KindTopup, Amount: 50})
	if err != nil {
		t.Fatal(err)
	}

	all, total, err := s.ListAll(ctx, nil, 10, 0)
	if err != nil || total != 2 || len(all) != 2 || all[0].ID != second.ID || all[1].ID != first.ID {
		t.Fatalf("all, newest first: %v %d %+v", err, total, all)
	}
	pending, total, err := s.ListAll(ctx, []Status{StatusPendingReview}, 10, 0)
	if err != nil || total != 1 || len(pending) != 1 || pending[0].ID != first.ID {
		t.Fatalf("pending: %v %d %+v", err, total, pending)
	}
	if none, total, _ := s.ListAll(ctx, []Status{StatusApproved, StatusFailed}, 10, 0); total != 0 || len(none) != 0 {
		t.Fatalf("approved/failed: %d %+v", total, none)
	}
	page, total, _ := s.ListAll(ctx, nil, 1, 1)
	if total != 2 || len(page) != 1 || page[0].ID != first.ID {
		t.Fatalf("second page: %d %+v", total, page)
	}
}

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{StatusNew, StatusPendingReview, StatusFailed, StatusCancelled} {
		if !s.Valid() {
			t.Errorf("%s must be valid", s)
		}
	}
	for _, s := range []Status{"", "pending", "APPROVED", "approved,failed"} {
		if s.Valid() {
			t.Errorf("%q must be invalid", s)
		}
	}
}
