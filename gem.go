package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"image/color"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/text"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
)

var gemTickers = []string{"EIMI.L", "CNDX.L", "CBU0.L", "IB01.L"}

var gemColors = map[string]color.RGBA{
	"EIMI.L": hexColor("0000FF"),
	"CNDX.L": hexColor("FFA500"),
	"CBU0.L": hexColor("008000"),
	"IB01.L": hexColor("FF0000"),
}

type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Close []*float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error interface{} `json:"error"`
	} `json:"chart"`
}

func generateGemChart(outputPath string) error {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		return err
	}

	end := time.Now().In(loc)
	start := end.AddDate(-1, 0, 0)

	client := &http.Client{Timeout: 20 * time.Second}
	seriesByTicker := make(map[string]map[int64]float64, len(gemTickers))
	baseTimestamps := []int64{}

	type fetchResult struct {
		ticker string
		ts     []int64
		vals   []float64
		err    error
	}

	results := make(chan fetchResult, len(gemTickers))
	for _, ticker := range gemTickers {
		go func(t string) {
			ts, vals, fetchErr := fetchYahooSeries(client, t, start, end)
			results <- fetchResult{ticker: t, ts: ts, vals: vals, err: fetchErr}
		}(ticker)
	}

	for i := 0; i < len(gemTickers); i++ {
		res := <-results
		if res.err != nil {
			return res.err
		}
		if len(baseTimestamps) == 0 {
			baseTimestamps = res.ts
		}
		points := make(map[int64]float64, len(res.ts))
		for idx, t := range res.ts {
			points[t] = res.vals[idx]
		}
		seriesByTicker[res.ticker] = points
	}

	if len(baseTimestamps) == 0 {
		return fmt.Errorf("brak danych do wykresu")
	}

	sort.Slice(baseTimestamps, func(i, j int) bool { return baseTimestamps[i] < baseTimestamps[j] })

	times := make([]time.Time, 0, len(baseTimestamps))
	valuesByTicker := make(map[string][]float64, len(gemTickers))
	lastKnown := make(map[string]float64, len(gemTickers))
	hasKnown := make(map[string]bool, len(gemTickers))

	for _, ts := range baseTimestamps {
		times = append(times, time.Unix(ts, 0).In(loc))
		for _, ticker := range gemTickers {
			if v, ok := seriesByTicker[ticker][ts]; ok && !math.IsNaN(v) {
				lastKnown[ticker] = v
				hasKnown[ticker] = true
			}
			if hasKnown[ticker] {
				valuesByTicker[ticker] = append(valuesByTicker[ticker], lastKnown[ticker])
			} else {
				valuesByTicker[ticker] = append(valuesByTicker[ticker], math.NaN())
			}
		}
	}

	startIdx := 0
	for i := range times {
		ok := true
		for _, ticker := range gemTickers {
			if math.IsNaN(valuesByTicker[ticker][i]) {
				ok = false
				break
			}
		}
		if ok {
			startIdx = i
			break
		}
	}

	if startIdx >= len(times) {
		return fmt.Errorf("brak kompletnych danych do wykresu")
	}

	times = times[startIdx:]
	returnsByTicker := make(map[string][]float64, len(gemTickers))
	maxValue := -math.MaxFloat64

	for _, ticker := range gemTickers {
		series := valuesByTicker[ticker][startIdx:]
		base := series[0]
		if base == 0 {
			return fmt.Errorf("wartość bazowa dla %s równa zero", ticker)
		}
		ret := make([]float64, len(series))
		for i, v := range series {
			val := (v/base - 1) * 100
			if math.IsNaN(val) || math.IsInf(val, 0) {
				return fmt.Errorf("nieprawidłowe dane zwrotu dla %s", ticker)
			}
			ret[i] = val
			if val > maxValue {
				maxValue = val
			}
		}
		returnsByTicker[ticker] = ret
	}

	if maxValue == -math.MaxFloat64 || math.IsNaN(maxValue) || math.IsInf(maxValue, 0) {
		return fmt.Errorf("brak danych do wykresu")
	}

	yMin := -25.0
	yMax := maxValue
	if yMax < yMin {
		yMax = yMin + 10
	} else {
		yMargin := (maxValue + 25) * 0.15
		if !isFinite(yMargin) {
			yMargin = 0
		}
		yMax = maxValue + yMargin
	}
	if yMax <= yMin || !isFinite(yMax) {
		yMax = yMin + 10
	}
	if !isFinite(yMin) {
		yMin = -25
	}
	if delta := yMax - yMin; !isFinite(delta) || delta <= 0 {
		yMin = -25
		yMax = 25
	}

	dateStr := end.Format("02 Jan 2006 15:04 MST")
	title := fmt.Sprintf("Porównanie ETF - 1 rok                    %s               ", dateStr)

	p := plot.New()
	p.Title.Text = title
	p.X.Label.Text = "Interwał Miesięczny"
	p.Y.Label.Text = ""
	p.X.Tick.Marker = monthTicks{Loc: loc, Format: "Jan 2006"}
	p.Y.Tick.Marker = percentTicks{}
	p.Y.Min = yMin
	p.Y.Max = yMax
	p.Add(plotter.NewGrid())

	rightTickStyle := p.Y.Tick.Label
	rightLabelStyle := rightTickStyle
	rightTickStyle.XAlign = draw.XLeft
	rightLabelStyle.XAlign = draw.XLeft
	axisLineStyle := draw.LineStyle{
		Color: color.Black,
		Width: vg.Points(0.5),
	}
	tickLineStyle := axisLineStyle
	p.Y.Tick.Label.Font.Size = 0
	p.Y.Tick.Label.Color = color.Transparent
	p.Y.Tick.Length = 0
	p.Y.Tick.LineStyle.Width = 0
	p.Y.LineStyle.Width = 0

	xMin := float64(times[0].Unix())
	xMax := float64(times[len(times)-1].Unix())
	xPad := float64(45 * 24 * 3600)

	p.X.Min = xMin
	p.X.Max = xMax + xPad

	seriesLabels := make([]seriesLabel, 0, len(gemTickers))
	for _, ticker := range gemTickers {
		series := returnsByTicker[ticker]
		if len(series) == 0 {
			continue
		}
		pts := make(plotter.XYs, len(times))
		for i := range times {
			pts[i].X = float64(times[i].Unix())
			pts[i].Y = series[i]
		}
		line, err := plotter.NewLine(pts)
		if err != nil {
			return err
		}
		line.Color = gemColors[ticker]
		line.Width = vg.Points(1.5)
		p.Add(line)
		legendLabel := fmt.Sprintf("%s: %+0.2f%%", ticker, series[len(series)-1])
		p.Legend.Add(legendLabel, line)
		seriesLabels = append(seriesLabels, seriesLabel{
			Text:  fmt.Sprintf("%s %+0.2f%%", ticker, series[len(series)-1]),
			Value: series[len(series)-1],
			Color: gemColors[ticker],
		})
	}

	p.Legend.Top = true
	p.Legend.Left = true
	p.Legend.XOffs = vg.Points(6)
	p.Legend.YOffs = vg.Points(-6)
	p.Add(rightSideAnnotations{
		Ticker:        percentTicks{},
		TickStyle:     rightTickStyle,
		LabelStyle:    rightLabelStyle,
		TickLength:    vg.Points(4),
		TickPadding:   vg.Points(6),
		LabelSpacing:  vg.Points(20),
		Gap:           vg.Points(2),
		AxisLineStyle: axisLineStyle,
		TickLineStyle: tickLineStyle,
		Labels:        seriesLabels,
	})

	if err := ensureDir(outputPath); err != nil {
		return err
	}

	fmt.Println("\n============================================================")
	fmt.Println("STOPY ZWROTU - 1 ROK:")
	fmt.Println("============================================================")
	for _, ticker := range gemTickers {
		series := returnsByTicker[ticker]
		if len(series) == 0 {
			continue
		}
		fmt.Printf("%-10s: %+7.2f%%\n", ticker, series[len(series)-1])
	}
	fmt.Println("============================================================\n")

	return p.Save(12*vg.Inch, 6*vg.Inch, outputPath)
}

func fetchYahooSeries(client *http.Client, ticker string, start, end time.Time) ([]int64, []float64, error) {
	requestURL := fmt.Sprintf(
		"https://query2.finance.yahoo.com/v8/finance/chart/%s?period1=%d&period2=%d&interval=1d&events=history&includeAdjustedClose=true",
		url.PathEscape(ticker),
		start.Unix(),
		end.Unix(),
	)

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "zlotemyslibot")

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("yahoo status %d dla %s", resp.StatusCode, ticker)
	}

	var payload yahooChartResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, nil, err
	}

	if len(payload.Chart.Result) == 0 {
		return nil, nil, fmt.Errorf("brak wyników dla %s", ticker)
	}

	result := payload.Chart.Result[0]
	if len(result.Timestamp) == 0 || len(result.Indicators.Quote) == 0 {
		return nil, nil, fmt.Errorf("brak danych cenowych dla %s", ticker)
	}

	closings := result.Indicators.Quote[0].Close
	if len(closings) != len(result.Timestamp) {
		return nil, nil, fmt.Errorf("niezgodna długość danych dla %s", ticker)
	}

	values := make([]float64, len(result.Timestamp))
	for i, v := range closings {
		if v == nil || math.IsNaN(*v) {
			values[i] = math.NaN()
		} else {
			values[i] = *v
		}
	}

	return result.Timestamp, values, nil
}

type percentTicks struct{}

func (percentTicks) Ticks(min, max float64) []plot.Tick {
	ticks := plot.DefaultTicks{}.Ticks(min, max)
	for i := range ticks {
		if ticks[i].Label != "" {
			ticks[i].Label = fmt.Sprintf("%.0f%%", ticks[i].Value)
		}
	}
	return ticks
}

type monthTicks struct {
	Loc    *time.Location
	Format string
}

func (m monthTicks) Ticks(min, max float64) []plot.Tick {
	loc := m.Loc
	if loc == nil {
		loc = time.UTC
	}
	minTime := time.Unix(int64(min), 0).In(loc)
	maxTime := time.Unix(int64(max), 0).In(loc)
	start := time.Date(minTime.Year(), minTime.Month(), 1, 0, 0, 0, 0, loc)
	if start.Before(minTime) {
		start = start.AddDate(0, 1, 0)
	}
	format := m.Format
	if format == "" {
		format = "Jan 2006"
	}
	ticks := []plot.Tick{}
	for t := start; !t.After(maxTime); t = t.AddDate(0, 1, 0) {
		ticks = append(ticks, plot.Tick{
			Value: float64(t.Unix()),
			Label: t.Format(format),
		})
	}
	return ticks
}

type seriesLabel struct {
	Text  string
	Value float64
	Color color.Color
}

type rightSideAnnotations struct {
	Ticker        plot.Ticker
	TickStyle     text.Style
	LabelStyle    text.Style
	TickLength    vg.Length
	TickPadding   vg.Length
	LabelSpacing  vg.Length
	Gap           vg.Length
	AxisLineStyle draw.LineStyle
	TickLineStyle draw.LineStyle
	Labels        []seriesLabel
}

func (r rightSideAnnotations) Plot(c draw.Canvas, p *plot.Plot) {
	ticker := r.Ticker
	if ticker == nil {
		ticker = plot.DefaultTicks{}
	}
	ticks := ticker.Ticks(p.Y.Min, p.Y.Max)
	axisX := c.Max.X

	if r.AxisLineStyle.Width > 0 {
		c.StrokeLine2(r.AxisLineStyle, axisX, c.Min.Y, axisX, c.Max.Y)
	}

	tickIntervals := make([]interval, 0, len(ticks))
	tickDescent := r.TickStyle.FontExtents().Descent
	for _, t := range ticks {
		if t.IsMinor() || t.Label == "" {
			continue
		}
		y := c.Y(p.Y.Norm(t.Value))
		if r.TickLength > 0 && r.TickLineStyle.Width > 0 {
			c.StrokeLine2(r.TickLineStyle, axisX, y, axisX-r.TickLength, y)
		}
		c.FillText(r.TickStyle, vg.Point{
			X: axisX + r.TickPadding,
			Y: y + tickDescent,
		}, t.Label)
		tickIntervals = append(tickIntervals, labelSpan(y, t.Label, r.TickStyle, r.Gap/2))
	}

	if len(r.Labels) == 0 {
		return
	}

	labels := make([]seriesLabel, len(r.Labels))
	copy(labels, r.Labels)
	sort.Slice(labels, func(i, j int) bool { return labels[i].Value < labels[j].Value })

	placed := make([]interval, 0, len(labels))
	for _, lbl := range labels {
		desiredY := c.Y(p.Y.Norm(lbl.Value))
		y := placeLabelY(desiredY, lbl.Text, r.LabelStyle, c.Min.Y, c.Max.Y, r.Gap, tickIntervals, placed)
		sty := r.LabelStyle
		sty.Color = lbl.Color
		descent := sty.FontExtents().Descent
		labelX := axisX + r.LabelSpacing
		c.FillText(sty, vg.Point{
			X: labelX,
			Y: y + descent,
		}, lbl.Text)
		placed = append(placed, labelSpan(y, lbl.Text, sty, r.Gap/2))
	}
}

func (r rightSideAnnotations) GlyphBoxes(p *plot.Plot) []plot.GlyphBox {
	ticker := r.Ticker
	if ticker == nil {
		ticker = plot.DefaultTicks{}
	}
	ticks := ticker.Ticks(p.Y.Min, p.Y.Max)
	boxes := make([]plot.GlyphBox, 0, len(ticks)+len(r.Labels))

	for _, t := range ticks {
		if t.IsMinor() || t.Label == "" {
			continue
		}
		rect := r.TickStyle.Rectangle(t.Label)
		rect.Min.X += r.TickPadding
		rect.Max.X += r.TickPadding
		boxes = append(boxes, plot.GlyphBox{
			X: 1,
			Y: p.Y.Norm(t.Value),
			Rectangle: vg.Rectangle{
				Min: rect.Min,
				Max: rect.Max,
			},
		})
	}

	for _, lbl := range r.Labels {
		rect := r.LabelStyle.Rectangle(lbl.Text)
		rect.Min.X += r.LabelSpacing
		rect.Max.X += r.LabelSpacing
		boxes = append(boxes, plot.GlyphBox{
			X: 1,
			Y: p.Y.Norm(lbl.Value),
			Rectangle: vg.Rectangle{
				Min: rect.Min,
				Max: rect.Max,
			},
		})
	}

	return boxes
}

type interval struct {
	min vg.Length
	max vg.Length
}

func labelSpan(y vg.Length, text string, style text.Style, pad vg.Length) interval {
	height := style.Height(text)
	descent := style.FontExtents().Descent
	return interval{
		min: y - (height - descent) - pad,
		max: y + descent + pad,
	}
}

func placeLabelY(desired vg.Length, text string, style text.Style, minY, maxY, gap vg.Length, reserved, placed []interval) vg.Length {
	height := style.Height(text)
	descent := style.FontExtents().Descent
	minAllowed := minY + (height - descent) + gap
	maxAllowed := maxY - descent - gap
	if minAllowed > maxAllowed {
		return desired
	}

	candidates := []vg.Length{desired}
	step := height + gap
	for i := 1; i <= 6; i++ {
		offset := vg.Length(i) * step
		candidates = append(candidates, desired+offset, desired-offset)
	}

	for _, y := range candidates {
		if y < minAllowed || y > maxAllowed {
			continue
		}
		span := labelSpan(y, text, style, gap/2)
		if !overlapsAny(span, reserved) && !overlapsAny(span, placed) {
			return y
		}
	}

	if desired < minAllowed {
		return minAllowed
	}
	if desired > maxAllowed {
		return maxAllowed
	}
	return desired
}

func overlapsAny(span interval, spans []interval) bool {
	for _, other := range spans {
		if span.min < other.max && other.min < span.max {
			return true
		}
	}
	return false
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func hexColor(hex string) color.RGBA {
	c := color.RGBA{A: 0xFF}
	if len(hex) != 6 {
		return c
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return c
	}
	c.R = uint8(v >> 16)
	c.G = uint8((v >> 8) & 0xFF)
	c.B = uint8(v & 0xFF)
	return c
}

func ensureDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
