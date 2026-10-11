// Package stocks is the Stocks app's logic: quotes, price history and
// symbol search from Yahoo Finance's public chart and search endpoints
// (undocumented and unofficial: they may change or refuse requests), and
// the price chart, drawn in Go.
package stocks

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/apps/chart"
)

// StoreName is the app's file in the store.
const StoreName = "stocks"

// Data is what the Stocks app keeps.
type Data struct {
	Symbols  []string `json:"symbols"`
	Selected string   `json:"selected"`
	Range    string   `json:"range"`
}

// DefaultSymbols start an empty list.
var DefaultSymbols = []string{"^GSPC", "^IXIC", "AAPL", "MSFT", "2330.TW"}

// Ranges are the chart's spans, in order, with the sampling for each.
var Ranges = []struct{ Name, Label, Interval string }{
	{"1d", "1D", "5m"},
	{"5d", "1W", "30m"},
	{"1mo", "1M", "1d"},
	{"6mo", "6M", "1d"},
	{"1y", "1Y", "1wk"},
	{"5y", "5Y", "1mo"},
}

// The service; tests point it elsewhere.
var (
	ChartURL  = "https://query1.finance.yahoo.com/v8/finance/chart/"
	SearchURL = "https://query1.finance.yahoo.com/v1/finance/search"
)

// Quote is a symbol's price and its history over a range.
type Quote struct {
	Symbol, Name, Exchange, Currency string

	Price     float64
	Base      float64 // what the change is against: the previous close for 1D, else the range's start
	High, Low float64 // today's
	Time      time.Time
	Points    []float64 // closing prices over the range, gaps left out
	Times     []time.Time
}

// Change is the price's change since Base, and in percent.
func (q Quote) Change() (abs, pct float64) {
	if q.Base == 0 {
		return 0, 0
	}
	abs = q.Price - q.Base
	return abs, abs / q.Base * 100
}

// Get reads a symbol's quote over a range (a Ranges name).
func Get(ctx context.Context, c *http.Client, symbol, rng string) (Quote, error) {
	interval := "5m"
	for _, r := range Ranges {
		if r.Name == rng {
			interval = r.Interval
		}
	}
	u := ChartURL + url.PathEscape(symbol) + "?" + url.Values{"range": {rng}, "interval": {interval}}.Encode()
	var r struct {
		Chart struct {
			Result []struct {
				Meta struct {
					Symbol             string  `json:"symbol"`
					Currency           string  `json:"currency"`
					Exchange           string  `json:"fullExchangeName"`
					LongName           string  `json:"longName"`
					ShortName          string  `json:"shortName"`
					Price              float64 `json:"regularMarketPrice"`
					PreviousClose      float64 `json:"previousClose"`
					ChartPreviousClose float64 `json:"chartPreviousClose"`
					High               float64 `json:"regularMarketDayHigh"`
					Low                float64 `json:"regularMarketDayLow"`
					Time               int64   `json:"regularMarketTime"`
				} `json:"meta"`
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Close []*float64 `json:"close"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
			Error *struct {
				Description string `json:"description"`
			} `json:"error"`
		} `json:"chart"`
	}
	var q Quote
	if err := getJSON(ctx, c, u, &r); err != nil {
		return q, err
	}
	if r.Chart.Error != nil {
		return q, fmt.Errorf("%s: %s", symbol, r.Chart.Error.Description)
	}
	if len(r.Chart.Result) == 0 {
		return q, fmt.Errorf("%s: no data", symbol)
	}
	res := r.Chart.Result[0]
	m := res.Meta
	q = Quote{
		Symbol: m.Symbol, Name: Name(m.LongName, m.ShortName, m.Symbol), Exchange: m.Exchange, Currency: m.Currency,
		Price: m.Price, High: m.High, Low: m.Low, Time: time.Unix(m.Time, 0),
	}
	q.Base = m.ChartPreviousClose
	if rng == "1d" && m.PreviousClose != 0 {
		q.Base = m.PreviousClose
	}
	if len(res.Indicators.Quote) > 0 {
		closes := res.Indicators.Quote[0].Close
		for i := range min(len(closes), len(res.Timestamp)) {
			if closes[i] != nil {
				q.Points = append(q.Points, *closes[i])
				q.Times = append(q.Times, time.Unix(res.Timestamp[i], 0))
			}
		}
	}
	return q, nil
}

// Name is the shortest good name for a symbol: the long name without the
// legal suffixes, else the short name, else the symbol.
func Name(long, short, symbol string) string {
	n := strings.TrimSpace(long)
	if n == "" {
		n = strings.TrimSpace(short)
	}
	for _, suffix := range []string{", Inc.", " Inc.", " Corporation", " Corp.", " Company Limited", " Limited", " Ltd.", " plc", " N.V.", " S.A."} {
		n = strings.TrimSuffix(n, suffix)
	}
	if n == "" {
		return symbol
	}
	return n
}

// Match is a symbol search result.
type Match struct {
	Symbol, Name, Exchange, Kind string
}

// Search finds symbols by name or ticker.
func Search(ctx context.Context, c *http.Client, query string) ([]Match, error) {
	u := SearchURL + "?" + url.Values{"q": {query}, "quotesCount": {"10"}, "newsCount": {"0"}}.Encode()
	var r struct {
		Quotes []struct {
			Symbol    string `json:"symbol"`
			ShortName string `json:"shortname"`
			LongName  string `json:"longname"`
			Exchange  string `json:"exchDisp"`
			Kind      string `json:"typeDisp"`
		} `json:"quotes"`
	}
	if err := getJSON(ctx, c, u, &r); err != nil {
		return nil, err
	}
	var out []Match
	for _, m := range r.Quotes {
		if m.Symbol == "" {
			continue
		}
		out = append(out, Match{Symbol: m.Symbol, Name: Name(m.LongName, m.ShortName, m.Symbol), Exchange: m.Exchange, Kind: m.Kind})
	}
	return out, nil
}

// Price shows a price with two decimals, or none past 10 000.
func Price(v float64) string {
	if math.Abs(v) >= 10000 {
		return group(strconv.FormatFloat(v, 'f', 0, 64))
	}
	return group(strconv.FormatFloat(v, 'f', 2, 64))
}

// Signed shows a change with its sign: "+1.25", "−0.40", "+2.31%".
func Signed(v float64, pct bool) string {
	s := Price(math.Abs(v))
	if pct {
		s = strconv.FormatFloat(math.Abs(v), 'f', 2, 64) + "%"
	}
	if v < 0 {
		return "−" + s
	}
	return "+" + s
}

// group puts thousands separators in a formatted number.
func group(s string) string {
	intPart, frac, hasFrac := strings.Cut(s, ".")
	neg := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if hasFrac {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// Chart draws a price history as a line over a soft fill, with a dotted
// line at base when it falls inside the range (0 for none).
func Chart(points []float64, base float64, w, h int, line color.NRGBA) *image.NRGBA {
	return chart.Line(points, w, h, line, chart.Options{Base: base})
}

func getJSON(ctx context.Context, c *http.Client, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	// The endpoints refuse requests without a browser-like agent.
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) inua")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
