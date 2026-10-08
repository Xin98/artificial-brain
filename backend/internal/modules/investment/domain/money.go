package domain

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

type Money int64
type Price int64
type Quantity int64

var decimal = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

func parseScaled(s string, places int) (int64, error) {
	if !decimal.MatchString(s) {
		return 0, ErrInvalidInput
	}
	parts := strings.Split(s, ".")
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > places {
		return 0, ErrInvalidInput
	}
	n, ok := new(big.Int).SetString(parts[0]+fraction+strings.Repeat("0", places-len(fraction)), 10)
	if !ok || !n.IsInt64() {
		return 0, ErrOverflow
	}
	return n.Int64(), nil
}
func ParseMoney(s string) (Money, error) { v, e := parseScaled(s, 2); return Money(v), e }
func ParsePrice(s string) (Price, error) {
	v, e := parseScaled(s, 6)
	if e != nil {
		return 0, e
	}
	if v <= 0 || v > 1000000000000 {
		return 0, ErrInvalidInput
	}
	return Price(v), nil
}
func ParseQuantity(s string) (Quantity, error) {
	v, e := parseScaled(s, 0)
	if e != nil {
		return 0, e
	}
	if v <= 0 || v > 1000000000 {
		return 0, ErrInvalidInput
	}
	return Quantity(v), nil
}
func (m Money) String() string {
	n := big.NewInt(int64(m))
	sign := ""
	if n.Sign() < 0 {
		sign = "-"
		n.Abs(n)
	}
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, big.NewInt(100), r)
	return fmt.Sprintf("%s%s.%02d", sign, q.String(), r.Int64())
}
func (p Price) String() string    { return fmt.Sprintf("%d.%06d", p/1000000, p%1000000) }
func (q Quantity) String() string { return fmt.Sprintf("%d", q) }
func GrossValue(p Price, q Quantity) (Money, error) {
	if p <= 0 || q <= 0 {
		return 0, ErrInvalidInput
	}
	n := new(big.Int).Mul(big.NewInt(int64(p)), big.NewInt(int64(q)))
	n.Add(n, big.NewInt(5000))
	n.Quo(n, big.NewInt(10000))
	if !n.IsInt64() {
		return 0, ErrOverflow
	}
	return Money(n.Int64()), nil
}
func addMoney(a, b Money) (Money, error) {
	n := new(big.Int).Add(big.NewInt(int64(a)), big.NewInt(int64(b)))
	if !n.IsInt64() {
		return 0, ErrOverflow
	}
	return Money(n.Int64()), nil
}

func scalePrice(p Price, numerator, denominator int64) (Price, error) {
	if p <= 0 || numerator <= 0 || denominator <= 0 {
		return 0, ErrInvalidInput
	}
	n := new(big.Int).Mul(big.NewInt(int64(p)), big.NewInt(numerator))
	n.Add(n, big.NewInt(denominator/2))
	n.Quo(n, big.NewInt(denominator))
	if !n.IsInt64() || n.Sign() <= 0 {
		return 0, ErrOverflow
	}
	return Price(n.Int64()), nil
}
