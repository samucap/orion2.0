package db

import (
	"context"
	"os"
	"testing"

	"github.com/joho/godotenv"
)

func TestRelativeChange(t *testing.T) {
	got, ok := relativeChange(80, 100)
	if !ok || got != -0.2 {
		t.Fatalf("change = %v ok=%v", got, ok)
	}
	if _, ok := relativeChange(1, 0); ok {
		t.Fatal("zero baseline should be unavailable")
	}
}

func TestParseDepthMeetsAtSpread(t *testing.T) {
	bids := []byte(`[{"price":"0.40","size":"5"},{"price":"0.44","size":"2"},{"price":"0.41","size":"1"},{"price":"0.42","size":"3"},{"price":"0.43","size":"4"},{"price":"0.39","size":"9"}]`)
	asks := []byte(`[{"price":"0.50","size":"1"},{"price":"0.46","size":"8"},{"price":"0.47","size":"2"}]`)
	levels, err := ParseDepth(bids, asks)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 8 {
		t.Fatalf("len = %d", len(levels))
	}
	// Five nearest bids, deep to best, then asks best to deep.
	want := []float64{0.40, 0.41, 0.42, 0.43, 0.44, 0.46, 0.47, 0.50}
	for i, price := range want {
		if levels[i].Price != price {
			t.Fatalf("level %d price = %v, want %v", i, levels[i].Price, price)
		}
	}
	if levels[4].Type != "bid" || levels[5].Type != "ask" {
		t.Fatalf("spread pair = %s %s", levels[4].Type, levels[5].Type)
	}
}

func TestDashboardQueriesLive(t *testing.T) {
	if os.Getenv("DASHBOARD_LIVE") != "1" {
		t.Skip("set DASHBOARD_LIVE=1 to query the local snapshot")
	}
	_ = godotenv.Load("../../.env")
	if _, err := InitDB(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	stats, err := QueryDashboardStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ActiveMarkets == 0 || stats.Volume24h <= 0 || stats.OpenInterest <= 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.Fees24h != nil {
		t.Fatal("fees are not stored")
	}

	trending, err := QueryTrendingMarkets(ctx)
	if err != nil || len(trending) == 0 {
		t.Fatalf("trending %d %v", len(trending), err)
	}
	activity, err := QueryActivity(ctx)
	if err != nil || len(activity) == 0 {
		t.Fatalf("activity %d %v", len(activity), err)
	}
	leaves, err := QueryTreemap(ctx)
	if err != nil || len(leaves) == 0 {
		t.Fatalf("treemap %d %v", len(leaves), err)
	}
	edge, err := QueryEdgeBoard(ctx)
	if err != nil || len(edge) == 0 {
		t.Fatalf("edge %d %v", len(edge), err)
	}
	if _, err := QueryWhales(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := QueryDepth(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := QueryCorrelation(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestParseDepthSkipsBadRows(t *testing.T) {
	levels, err := ParseDepth([]byte(`[{"price":"nope","size":"1"},{"price":"0.2","size":"0"}]`), []byte(`null`))
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 0 {
		t.Fatalf("len = %d", len(levels))
	}
}
