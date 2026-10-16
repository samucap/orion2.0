package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoPool is returned when a dashboard query runs before the database connects.
var ErrNoPool = errors.New("database pool is not initialized")

// Live markets are rows refreshed within two days of the newest market snapshot,
// still open, and with recorded 24h volume. created_at is the sync time, so it
// is not used as a "new markets" count. The fee column is null on these rows.

const liveMarketsWhere = `
active AND NOT closed
AND end_date > (SELECT t FROM asof)
AND updated_at > (SELECT t FROM asof) - interval '2 days'
AND volume_24hr > 0`

// DashboardStats is the overview row for GET /api/dashboard/stats.
// Fees24h is always null: collected fees are not stored.
// Trader counts use the newest trade day, which can be older than MarketsAsOf.
type DashboardStats struct {
	MarketsAsOf         time.Time `json:"marketsAsOf"`
	TradesAsOf          time.Time `json:"tradesAsOf"`
	TVL                 float64   `json:"tvl"`
	Volume24h           float64   `json:"volume24h"`
	OpenInterest        float64   `json:"openInterest"`
	OpenInterestChange  *float64  `json:"openInterestChange"`
	ActiveTraders       int       `json:"activeTraders"`
	ActiveTradersChange *int      `json:"activeTradersChange"`
	Fees24h             *float64  `json:"fees24h"`
	ActiveMarkets       int       `json:"activeMarkets"`
}

// TrendingMarket is one row for GET /api/dashboard/trending.
// Drift is one_day_price_change in probability points (0.15 means +15pp).
type TrendingMarket struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Category    string    `json:"category"`
	EndDate     time.Time `json:"endDate"`
	Probability float64   `json:"probability"`
	Drift       float64   `json:"drift"`
	Volume24h   float64   `json:"volume24h"`
	Sparkline   []float64 `json:"sparkline"`
}

// Activity is one trade for GET /api/dashboard/activity.
// Trade price is stored as 0, so this does not include odds or notional.
type Activity struct {
	ID      string    `json:"id"`
	User    string    `json:"user"`
	Action  string    `json:"action"`
	Market  string    `json:"market"`
	Outcome string    `json:"outcome"`
	Shares  float64   `json:"shares"`
	Time    time.Time `json:"time"`
}

// TreemapLeaf is a market tile. Drift is one_day_price_change in percentage points.
type TreemapLeaf struct {
	Category       string  `json:"category"`
	Name           string  `json:"name"`
	Size           float64 `json:"size"`
	Drift          float64 `json:"drift"`
	LiquidityRatio float64 `json:"liquidityRatio"`
}

// EdgePoint is one edge_board row. Spread is the book spread, not a yield.
type EdgePoint struct {
	Name      string  `json:"name"`
	Category  string  `json:"category"`
	EdgeBps   float64 `json:"edgeBps"`
	Spread    float64 `json:"spread"`
	Volume24h float64 `json:"volume24h"`
}

// Whale is an account net flow. The table has no PnL column.
type Whale struct {
	Rank    int     `json:"rank"`
	Account string  `json:"account"`
	NetFlow float64 `json:"netFlow"`
}

// DepthLevel is one price level from the newest stored order book.
type DepthLevel struct {
	Type  string  `json:"type"`
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

// CorrelationLink is a pairwise correlation. None is stored.
type CorrelationLink struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Value  float64 `json:"value"`
}

func QueryDashboardStats(ctx context.Context) (DashboardStats, error) {
	if Pool == nil {
		return DashboardStats{}, ErrNoPool
	}
	const q = `
WITH asof AS (SELECT max(updated_at) t FROM plymkt_markets),
live AS (
	SELECT volume_24hr, liquidity_clob
	FROM plymkt_markets
	WHERE ` + liveMarketsWhere + `
),
oi_mx AS (SELECT max(time) t FROM oi_history),
oi_latest AS (
	SELECT sum(oi) s FROM (
		SELECT DISTINCT ON (condition_id) oi_value oi
		FROM oi_history, oi_mx
		WHERE time > oi_mx.t - interval '2 hours'
		ORDER BY condition_id, time DESC
	) x
),
oi_prior AS (
	SELECT sum(oi) s FROM (
		SELECT DISTINCT ON (condition_id) oi_value oi
		FROM oi_history, oi_mx
		WHERE time > oi_mx.t - interval '26 hours'
		  AND time <= oi_mx.t - interval '22 hours'
		ORDER BY condition_id, time DESC
	) x
),
tr_mx AS (SELECT max(timestamp) t FROM trades),
traders AS (
	SELECT
		count(DISTINCT proxy_wallet) FILTER (WHERE timestamp > tr_mx.t - 86400) cur,
		count(DISTINCT proxy_wallet) FILTER (WHERE timestamp <= tr_mx.t - 86400 AND timestamp > tr_mx.t - 172800) prev
	FROM trades, tr_mx
	WHERE timestamp > tr_mx.t - 172800
)
SELECT
	(SELECT t FROM asof),
	to_timestamp((SELECT t FROM tr_mx)),
	(SELECT coalesce(sum(liquidity_clob), 0) FROM live),
	(SELECT coalesce(sum(volume_24hr), 0) FROM live),
	coalesce((SELECT s FROM oi_latest), 0),
	(SELECT s FROM oi_prior),
	(SELECT cur FROM traders),
	(SELECT prev FROM traders),
	(SELECT count(*) FROM live)`

	var stats DashboardStats
	var oiPrev *float64
	var tradersPrev *int
	err := Pool.QueryRow(ctx, q).Scan(
		&stats.MarketsAsOf,
		&stats.TradesAsOf,
		&stats.TVL,
		&stats.Volume24h,
		&stats.OpenInterest,
		&oiPrev,
		&stats.ActiveTraders,
		&tradersPrev,
		&stats.ActiveMarkets,
	)
	if err != nil {
		return DashboardStats{}, fmt.Errorf("dashboard stats: %w", err)
	}
	if oiPrev != nil {
		if change, ok := relativeChange(stats.OpenInterest, *oiPrev); ok {
			stats.OpenInterestChange = &change
		}
	}
	if tradersPrev != nil {
		delta := stats.ActiveTraders - *tradersPrev
		stats.ActiveTradersChange = &delta
	}
	return stats, nil
}

func QueryTrendingMarkets(ctx context.Context) ([]TrendingMarket, error) {
	if Pool == nil {
		return nil, ErrNoPool
	}
	const q = `
WITH asof AS (SELECT max(updated_at) t FROM plymkt_markets),
top AS (
	SELECT id, question,
		coalesce(nullif(category, ''), 'uncategorized') category,
		end_date,
		coalesce(last_trade_price, 0) probability,
		coalesce(one_day_price_change, 0) drift,
		coalesce(volume_24hr, 0) volume_24h,
		CASE WHEN clob_token_ids LIKE '[%' THEN clob_token_ids::jsonb->>0 END tok
	FROM plymkt_markets
	WHERE ` + liveMarketsWhere + `
	ORDER BY volume_24hr DESC NULLS LAST
	LIMIT 8
)
SELECT t.id, t.question, t.category, t.end_date, t.probability, t.drift, t.volume_24h,
	p.timestamp, p.price
FROM top t
LEFT JOIN LATERAL (
	SELECT timestamp, price
	FROM prices_history
	WHERE token_id = t.tok
	ORDER BY timestamp DESC
	LIMIT 24
) p ON true
ORDER BY t.volume_24h DESC, p.timestamp ASC`

	rows, err := Pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("trending markets: %w", err)
	}
	defer rows.Close()

	order := make([]string, 0, 8)
	byID := map[string]*TrendingMarket{}
	for rows.Next() {
		var id, title, category string
		var end time.Time
		var probability, drift, volume float64
		var ts *int64
		var price *float64
		if err := rows.Scan(&id, &title, &category, &end, &probability, &drift, &volume, &ts, &price); err != nil {
			return nil, fmt.Errorf("scan trending: %w", err)
		}
		item, ok := byID[id]
		if !ok {
			item = &TrendingMarket{
				ID: id, Title: title, Category: category, EndDate: end,
				Probability: probability, Drift: drift, Volume24h: volume,
				Sparkline: []float64{},
			}
			byID[id] = item
			order = append(order, id)
		}
		if price != nil {
			item.Sparkline = append(item.Sparkline, *price)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trending markets: %w", err)
	}
	out := make([]TrendingMarket, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

func QueryActivity(ctx context.Context) ([]Activity, error) {
	if Pool == nil {
		return nil, ErrNoPool
	}
	const q = `
SELECT transaction_hash,
	CASE
		WHEN coalesce(name, '') <> '' THEN name
		WHEN coalesce(pseudonym, '') <> '' THEN pseudonym
		ELSE left(proxy_wallet, 6) || '…' || right(proxy_wallet, 4)
	END,
	lower(side),
	coalesce(title, ''),
	coalesce(outcome, ''),
	size,
	to_timestamp(timestamp)
FROM trades
ORDER BY timestamp DESC
LIMIT 25`

	rows, err := Pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("activity: %w", err)
	}
	defer rows.Close()
	out := make([]Activity, 0)
	for rows.Next() {
		var item Activity
		if err := rows.Scan(&item.ID, &item.User, &item.Action, &item.Market, &item.Outcome, &item.Shares, &item.Time); err != nil {
			return nil, fmt.Errorf("scan activity: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("activity: %w", err)
	}
	return out, nil
}

func QueryTreemap(ctx context.Context) ([]TreemapLeaf, error) {
	if Pool == nil {
		return nil, ErrNoPool
	}
	const q = `
WITH asof AS (SELECT max(updated_at) t FROM plymkt_markets),
live AS (
	SELECT question,
		coalesce(nullif(category, ''), 'uncategorized') category,
		volume_24hr,
		CASE WHEN volume_24hr > 0 THEN liquidity_clob / volume_24hr ELSE 0 END liq_ratio,
		coalesce(one_day_price_change, 0) * 100 drift
	FROM plymkt_markets
	WHERE ` + liveMarketsWhere + `
),
ranked AS (
	SELECT *,
		row_number() OVER (PARTITION BY category ORDER BY volume_24hr DESC) rn,
		sum(volume_24hr) OVER (PARTITION BY category) cat_vol
	FROM live
),
topcats AS (
	SELECT category, max(cat_vol) cat_vol
	FROM ranked
	GROUP BY category
	ORDER BY max(cat_vol) DESC
	LIMIT 6
)
SELECT r.category, r.question, r.volume_24hr, coalesce(r.liq_ratio, 0), r.drift
FROM ranked r
JOIN topcats c USING (category)
WHERE r.rn <= 4
ORDER BY c.cat_vol DESC, r.rn`

	rows, err := Pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("treemap: %w", err)
	}
	defer rows.Close()
	out := make([]TreemapLeaf, 0)
	for rows.Next() {
		var leaf TreemapLeaf
		if err := rows.Scan(&leaf.Category, &leaf.Name, &leaf.Size, &leaf.LiquidityRatio, &leaf.Drift); err != nil {
			return nil, fmt.Errorf("scan treemap: %w", err)
		}
		out = append(out, leaf)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("treemap: %w", err)
	}
	return out, nil
}

func QueryEdgeBoard(ctx context.Context) ([]EdgePoint, error) {
	if Pool == nil {
		return nil, ErrNoPool
	}
	const q = `
SELECT question_short,
	coalesce(nullif(category, ''), 'uncategorized'),
	coalesce(edge_bps, 0),
	coalesce(spread, 0),
	coalesce(volume_24hr, 0)
FROM edge_board
ORDER BY rank
LIMIT 50`
	rows, err := Pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("edge board: %w", err)
	}
	defer rows.Close()
	out := make([]EdgePoint, 0)
	for rows.Next() {
		var point EdgePoint
		if err := rows.Scan(&point.Name, &point.Category, &point.EdgeBps, &point.Spread, &point.Volume24h); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		out = append(out, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("edge board: %w", err)
	}
	return out, nil
}

func QueryWhales(ctx context.Context) ([]Whale, error) {
	if Pool == nil {
		return nil, ErrNoPool
	}
	const q = `
SELECT account_id, coalesce(sum(net_flow_usd), 0)
FROM whale_flow_24h
GROUP BY account_id
ORDER BY abs(sum(net_flow_usd)) DESC
LIMIT 10`
	rows, err := Pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("whales: %w", err)
	}
	defer rows.Close()
	out := make([]Whale, 0)
	rank := 1
	for rows.Next() {
		var whale Whale
		if err := rows.Scan(&whale.Account, &whale.NetFlow); err != nil {
			return nil, fmt.Errorf("scan whale: %w", err)
		}
		whale.Rank = rank
		whale.Account = shortAccount(whale.Account)
		rank++
		out = append(out, whale)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("whales: %w", err)
	}
	return out, nil
}

// QueryDepth returns levels from the newest orderbooks row.
// orderbooks and orderbook_snapshots are empty in the current snapshot, so this
// returns an empty slice until a book is stored. Slippage is not stored.
func QueryDepth(ctx context.Context) ([]DepthLevel, error) {
	if Pool == nil {
		return nil, ErrNoPool
	}
	const q = `
SELECT bids, asks
FROM orderbooks
ORDER BY timestamp DESC
LIMIT 1`
	var bidsRaw, asksRaw []byte
	err := Pool.QueryRow(ctx, q).Scan(&bidsRaw, &asksRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return []DepthLevel{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("depth: %w", err)
	}
	levels, err := ParseDepth(bidsRaw, asksRaw)
	if err != nil {
		return nil, fmt.Errorf("depth: %w", err)
	}
	return levels, nil
}

// QueryCorrelation returns no links. signals.correlation_group is a label, not a matrix,
// and no pairwise correlations are stored. The SELECT confirms the database is reachable.
func QueryCorrelation(ctx context.Context) ([]CorrelationLink, error) {
	if err := ping(ctx); err != nil {
		return nil, err
	}
	return []CorrelationLink{}, nil
}

// QueryArbitrage returns no rows. No Kalshi or Betfair prices are stored.
func QueryArbitrage(ctx context.Context) ([]struct{}, error) {
	if err := ping(ctx); err != nil {
		return nil, err
	}
	return []struct{}{}, nil
}

func ping(ctx context.Context) error {
	if Pool == nil {
		return ErrNoPool
	}
	var one int
	if err := Pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	return nil
}

func relativeChange(cur, prev float64) (float64, bool) {
	if prev == 0 || math.IsNaN(prev) || math.IsNaN(cur) {
		return 0, false
	}
	return (cur - prev) / prev, true
}

func shortAccount(account string) string {
	if len(account) <= 12 {
		return account
	}
	return account[:6] + "…" + account[len(account)-4:]
}

type bookLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// ParseDepth keeps the five bids closest to the spread (then deeper below)
// and the five asks closest to the spread (then further above).
func ParseDepth(bidsRaw, asksRaw []byte) ([]DepthLevel, error) {
	bids, err := decodeSide(bidsRaw, "bid")
	if err != nil {
		return nil, err
	}
	asks, err := decodeSide(asksRaw, "ask")
	if err != nil {
		return nil, err
	}
	sort.Slice(bids, func(i, j int) bool { return bids[i].Price > bids[j].Price })
	sort.Slice(asks, func(i, j int) bool { return asks[i].Price < asks[j].Price })
	if len(bids) > 5 {
		bids = bids[:5]
	}
	if len(asks) > 5 {
		asks = asks[:5]
	}
	// Draw bids from deep to the spread so the two best prices meet.
	for i, j := 0, len(bids)-1; i < j; i, j = i+1, j-1 {
		bids[i], bids[j] = bids[j], bids[i]
	}
	out := make([]DepthLevel, 0, len(bids)+len(asks))
	out = append(out, bids...)
	out = append(out, asks...)
	return out, nil
}

func decodeSide(raw []byte, side string) ([]DepthLevel, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var rows []bookLevel
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("parse %s: %w", side, err)
	}
	out := make([]DepthLevel, 0, len(rows))
	for _, row := range rows {
		price, errP := strconv.ParseFloat(row.Price, 64)
		size, errS := strconv.ParseFloat(row.Size, 64)
		if errP != nil || errS != nil || size <= 0 {
			continue
		}
		out = append(out, DepthLevel{Type: side, Price: price, Size: size})
	}
	return out, nil
}
