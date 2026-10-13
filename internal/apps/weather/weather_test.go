package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const forecastJSON = `{"utc_offset_seconds":28800,
"current":{"temperature_2m":27.0,"apparent_temperature":29.2,"relative_humidity_2m":71,"weather_code":3,"is_day":1,"wind_speed_10m":15.4},
"hourly":{"time":["2026-10-10T10:00","2026-10-10T11:00"],"temperature_2m":[27.0,27.4],"weather_code":[3,61],"is_day":[1,1]},
"daily":{"time":["2026-10-10","2026-10-11"],"weather_code":[3,95],"temperature_2m_max":[27.0,28.6],"temperature_2m_min":[22.7,22.2],
"precipitation_probability_max":[4,null],"sunrise":["2026-10-10T05:49","2026-10-11T05:50"],"sunset":["2026-10-10T17:32","2026-10-11T17:31"]}}`

func TestGet(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Write([]byte(forecastJSON))
	}))
	defer srv.Close()
	ForecastURL = srv.URL
	f, err := Get(context.Background(), srv.Client(), Place{Lat: 25.05, Lon: 121.53}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "temperature_unit=fahrenheit") || !strings.Contains(query, "latitude=25.0500") {
		t.Errorf("query %s", query)
	}
	if f.Now.Temp != 27 || f.Now.Humidity != 71 || !f.Now.Day || f.Unit != "°F" {
		t.Errorf("now %+v unit %s", f.Now, f.Unit)
	}
	if len(f.Hours) != 2 || f.Hours[1].Code != 61 || f.Hours[1].Time.Hour() != 11 {
		t.Errorf("hours %+v", f.Hours)
	}
	if _, off := f.Hours[0].Time.Zone(); off != 28800 {
		t.Errorf("hours are not in the place's time: offset %d", off)
	}
	if len(f.Days) != 2 || f.Days[1].Max != 28.6 || f.Days[0].Rain != 4 || f.Days[1].Rain != 0 || f.Days[0].Sunset.Hour() != 17 {
		t.Errorf("days %+v", f.Days)
	}
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "Taipei" {
			http.Error(w, "bad", 400)
			return
		}
		w.Write([]byte(`{"results":[{"name":"Taipei","latitude":25.05,"longitude":121.52,"timezone":"Asia/Taipei","country":"Taiwan","admin1":"Taiwan"}]}`))
	}))
	defer srv.Close()
	GeocodeURL = srv.URL
	ps, err := Search(context.Background(), srv.Client(), "Taipei")
	if err != nil || len(ps) != 1 || ps[0].Timezone != "Asia/Taipei" || ps[0].Where() != "Taiwan" {
		t.Fatalf("%+v %v", ps, err)
	}
	if _, err := Search(context.Background(), srv.Client(), "x"); err == nil {
		t.Error("a 400 should be an error")
	}
}

func TestDescribe(t *testing.T) {
	if text, icon := Describe(0, false); text != "Clear sky" || icon != "weather-clear-night-symbolic" {
		t.Error(text, icon)
	}
	if text, _ := Describe(95, true); text != "Thunderstorm" {
		t.Error(text)
	}
	for v, want := range map[float64]string{27.4: "27°", 27.5: "28°", -0.4: "0°", -2.6: "−3°"} {
		if got := Temp(v); got != want {
			t.Errorf("Temp(%v) = %q, want %q", v, got, want)
		}
	}
}
