package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Money int64

var ErrInvalidMoney = errors.New("invalid monetary amount")

func ParseMoney(value string) (Money, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "eE") {
		return 0, ErrInvalidMoney
	}
	negative := strings.HasPrefix(value, "-")
	if negative {
		value = strings.TrimPrefix(value, "-")
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, ErrInvalidMoney
	}
	if len(parts) == 2 && len(parts[1]) > 2 {
		return 0, ErrInvalidMoney
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	for len(frac) < 2 {
		frac += "0"
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, ErrInvalidMoney
	}
	cents := int64(0)
	if frac != "" {
		cents, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, ErrInvalidMoney
		}
	}
	if whole > (int64(^uint64(0)>>1)-cents)/100 {
		return 0, ErrInvalidMoney
	}
	result := Money(whole*100 + cents)
	if negative {
		result = -result
	}
	return result, nil
}
func (m Money) String() string {
	n := int64(m)
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	return fmt.Sprintf("%s%d.%02d", sign, n/100, n%100)
}
func (m Money) MarshalJSON() ([]byte, error) { return []byte(m.String()), nil }
func (m *Money) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 1 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		data = []byte(s)
	}
	v, err := ParseMoney(string(data))
	if err != nil {
		return err
	}
	*m = v
	return nil
}
