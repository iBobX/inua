package stocks

import (
	"context"
	"image/color"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const chartJSON = `{"chart":{"result":[{"meta":{"currency":"TWD","symbol":"2330.TW","fullExchangeName":"Taiwan",
"regularMarketPrice":2550.0,"previousClose":2585.0,"chartPreviousClose":2500.0,"regularMarketDayHigh":2575.0,"regularMarketDayLow":2550.0,
"regularMarketTime":1791437413,"longName":"Taiwan Semiconductor Manufacturing Company Limited","shortName":"TAIWAN SEMI"},
"timestamp":[1,2,3,4],"indicators":{"quote":[{"close":[2505.0,null,2520.0,2550.0]}]}}],"error":null}}`

func serve(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.UserAgent(), "Mozilla") {
			http.Error(w, "no agent", http.StatusForbidden)
			return
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGet(t *testing.T) {
	srv := serve(t, chartJSON, 200)
	ChartURL = srv.URL + "/"
	q, err := Get(context.Background(), srv.Client(), "2330.TW", "1d")
	if err != nil {
		t.Fatal(err)
	}
	if q.Name != "Taiwan Semiconductor Manufacturing" || q.Price != 2550 || q.Base != 2585 || len(q.Points) != 3 {
		t.Errorf("%+v", q)
	}
	if abs, pct := q.Change(); abs != -35 || math.Abs(pct+1.354) > 0.001 {
		t.Errorf("change %v %v", abs, pct)
	}
	q, _ = Get(context.Background(), srv.Client(), "2330.TW", "5d")
	if q.Base != 2500 {
		t.Errorf("a longer range compares with its start: base %v", q.Base)
	}
}

func TestGetError(t *testing.T) {
	srv := serve(t, `{"chart":{"result":null,"error":{"code":"Not Found","description":"No data found, symbol may be delisted"}}}`, 404)
	ChartURL = srv.URL + "/"
	if _, err := Get(context.Background(), srv.Client(), "NOPE", "1d"); err == nil || !strings.Contains(err.Error(), "delisted") {
		t.Errorf("err = %v", err)
	}
}

func TestSearch(t *testing.T) {
	srv := serve(t, `{"quotes":[{"symbol":"AAPL","shortname":"Apple Inc.","longname":"Apple Inc.","exchDisp":"NASDAQ","typeDisp":"Equity"},{"index":"x"}]}`, 200)
	SearchURL = srv.URL
	ms, err := Search(context.Background(), srv.Client(), "apple")
	if err != nil || len(ms) != 1 || ms[0].Name != "Apple" || ms[0].Exchange != "NASDAQ" {
		t.Fatalf("%+v %v", ms, err)
	}
}

func TestFormat(t *testing.T) {
	for _, tc := range []struct {
		got, want string
	}{
		{Price(1234.5), "1,234.50"},
		{Price(25500), "25,500"},
		{Price(0.42), "0.42"},
		{Signed(-35, false), "−35.00"},
		{Signed(1.354, true), "+1.35%"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestChart(t *testing.T) {
	img := Chart([]float64{1, 3, 2, 5}, 2, 120, 40, color.NRGBA{0, 128, 255, 255})
	if img.Bounds().Dx() != 120 || img.Bounds().Dy() != 40 {
		t.Fatal(img.Bounds())
	}
	var drawn, strong int
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 0 {
			drawn++
		}
		if img.Pix[i] > 200 {
			strong++
		}
	}
	if drawn < 120*40/4 || strong < 120 {
		t.Errorf("drawn %d strong %d: the fill or the line is missing", drawn, strong)
	}
	if empty := Chart([]float64{1}, 0, 10, 10, color.NRGBA{}); empty.Pix[3] != 0 {
		t.Error("one point draws nothing")
	}
}
