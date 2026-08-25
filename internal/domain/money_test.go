package domain

import (
	"encoding/json"
	"testing"
)

func TestMoneyExactJSONRoundTrip(t *testing.T) {
	for input, want := range map[string]Money{"0": 0, "10": 1000, "25.50": 2550, "0.01": 1, "-1.25": -125} {
		var got Money
		if err := json.Unmarshal([]byte(input), &got); err != nil || got != want {
			t.Errorf("%s => %v,%v", input, got, err)
		}
		encoded, _ := json.Marshal(got)
		var again Money
		if err := json.Unmarshal(encoded, &again); err != nil || again != want {
			t.Errorf("round trip %s => %s", input, encoded)
		}
	}
}
func TestMoneyRejectsExcessPrecisionAndExponent(t *testing.T) {
	for _, input := range []string{"1.001", "1e2", "NaN", ""} {
		if _, err := ParseMoney(input); err == nil {
			t.Errorf("ParseMoney(%q) succeeded", input)
		}
	}
}
