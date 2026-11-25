package style

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

type chromeCounterCase struct {
	Name       string   `json:"name"`
	Reset      string   `json:"reset"`
	Increments []string `json:"increments"`
	Out        []struct {
		Decimal string `json:"decimal"`
	} `json:"out"`
}

func loadChromeCounters(t *testing.T) []chromeCounterCase {
	t.Helper()
	f, err := os.Open("../layout/testdata/chrome_counters.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	var out []chromeCounterCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		var c chromeCounterCase
		if err := json.Unmarshal([]byte(l), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func satAdd(a, b int32) int32 {
	s := int64(a) + int64(b)
	return int32(max(math.MinInt32, min(math.MaxInt32, s)))
}

// The counter-reset and counter-increment lists of Chrome's isolated counter
// cases (chrome_counters.txt), evaluated with the CSS Lists 3 rules over the
// computed op lists (reset: each entry sets, the last of a name wins;
// increment: each entry adds with int32 saturation; a counter that does not
// exist is created at 0), give the values Chrome printed.
func TestCountersMatchChrome(t *testing.T) {
	cases := loadChromeCounters(t)
	if len(cases) < 40 {
		t.Fatalf("%d cases", len(cases))
	}
	for _, c := range cases {
		if strings.Contains(c.Reset, "container") { // the nested scoping cases are box-level (M3-02)
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			var sb strings.Builder
			attr := ""
			if c.Reset != "" {
				attr = ` style="counter-reset: ` + c.Reset + `"`
			}
			fmt.Fprintf(&sb, "<div%s>", attr)
			for _, inc := range c.Increments {
				fmt.Fprintf(&sb, `<p style="counter-increment: %s"></p>`, inc)
			}
			sb.WriteString("</div>")
			x := resolve(t, sb.String(), "", "")
			var div *Computed
			var ps []*Computed
			for i := range x.d.Elements {
				switch x.d.Elements[i].Tag.String() {
				case "div":
					div = x.styles[i]
				case "p":
					ps = append(ps, x.styles[i])
				}
			}
			state := map[string]int32{}
			for _, op := range div.CounterReset {
				state[op.Name] = op.Value
			}
			if len(ps) != len(c.Out) {
				t.Fatalf("%d elements, %d Chrome values", len(ps), len(c.Out))
			}
			for k, p := range ps {
				for _, op := range p.CounterIncrement {
					state[op.Name] = satAdd(state[op.Name], op.Value)
				}
				if got := fmt.Sprint(state["c"]); got != c.Out[k].Decimal {
					t.Errorf("after element %d (reset %q, increments %q): counter c = %s, Chrome %s; ops reset %v inc %v", k+1, c.Reset, c.Increments, got, c.Out[k].Decimal, div.CounterReset, p.CounterIncrement)
				}
			}
		})
	}
}
