package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ExactRational is a reduced arbitrary-precision rational used by evidence gates.
type ExactRational struct {
	value *big.Rat
}

// FormatLowerEightDecimals rounds an exact bound toward negative infinity.
func FormatLowerEightDecimals(rational ExactRational) string {
	return formatOutwardEightDecimals(rational, false)
}

// FormatUpperEightDecimals rounds an exact bound toward positive infinity.
func FormatUpperEightDecimals(rational ExactRational) string {
	return formatOutwardEightDecimals(rational, true)
}

func formatOutwardEightDecimals(rational ExactRational, upper bool) string {
	value := rational.normalized()
	scaled := new(big.Int).Mul(value.Num(), big.NewInt(100_000_000))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(scaled, value.Denom(), remainder)
	if remainder.Sign() != 0 {
		if upper && scaled.Sign() > 0 {
			quotient.Add(quotient, big.NewInt(1))
		}
		if !upper && scaled.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		}
	}
	negative := quotient.Sign() < 0
	digits := new(big.Int).Abs(quotient).String()
	if len(digits) <= 8 {
		digits = strings.Repeat("0", 9-len(digits)) + digits
	}
	whole := digits[:len(digits)-8]
	fraction := digits[len(digits)-8:]
	if negative {
		return "-" + whole + "." + fraction
	}
	return whole + "." + fraction
}

// NewExactRational constructs a reduced exact rational.
func NewExactRational(numerator, denominator *big.Int) (ExactRational, error) {
	if numerator == nil || denominator == nil {
		return ExactRational{}, errors.New("exact rational requires numerator and denominator")
	}
	if denominator.Sign() <= 0 {
		return ExactRational{}, errors.New("exact rational denominator must be positive")
	}
	return ExactRational{value: new(big.Rat).SetFrac(
		new(big.Int).Set(numerator),
		new(big.Int).Set(denominator),
	)}, nil
}

// MustExactRational constructs a small exact rational or panics for invalid input.
func MustExactRational(numerator int64, denominator uint64) ExactRational {
	rational, err := NewExactRational(
		big.NewInt(numerator),
		new(big.Int).SetUint64(denominator),
	)
	if err != nil {
		panic(err)
	}
	return rational
}

func (rational ExactRational) normalized() *big.Rat {
	if rational.value == nil {
		return new(big.Rat)
	}
	return new(big.Rat).Set(rational.value)
}

// Cmp compares two exact rationals.
func (rational ExactRational) Cmp(other ExactRational) int {
	return rational.normalized().Cmp(other.normalized())
}

// Add returns the exact sum.
func (rational ExactRational) Add(other ExactRational) ExactRational {
	return ExactRational{value: new(big.Rat).Add(rational.normalized(), other.normalized())}
}

// Sub returns the exact difference.
func (rational ExactRational) Sub(other ExactRational) ExactRational {
	return ExactRational{value: new(big.Rat).Sub(rational.normalized(), other.normalized())}
}

// Quo returns the exact quotient.
func (rational ExactRational) Quo(other ExactRational) (ExactRational, error) {
	if other.normalized().Sign() == 0 {
		return ExactRational{}, errors.New("exact rational division by zero")
	}
	return ExactRational{value: new(big.Rat).Quo(rational.normalized(), other.normalized())}, nil
}

// String returns the reduced numerator/denominator representation.
func (rational ExactRational) String() string {
	value := rational.normalized()
	return value.Num().String() + "/" + value.Denom().String()
}

// Float64 converts only at the explicit diagnostic OPE boundary.
func (rational ExactRational) Float64() (float64, error) {
	value, exact := rational.normalized().Float64()
	if value != value || value > 1.7976931348623157e308 || value < -1.7976931348623157e308 {
		return 0, errors.New("exact rational is outside finite binary64")
	}
	_ = exact
	return value, nil
}

// MarshalJSON emits the frozen canonical rational object.
func (rational ExactRational) MarshalJSON() ([]byte, error) {
	value := rational.normalized()
	return fmt.Appendf(nil,
		`{"numerator":%s,"denominator":%s}`,
		value.Num().String(),
		value.Denom().String(),
	), nil
}

// UnmarshalJSON rejects unknown, missing, duplicate, and noncanonical fields.
func (rational *ExactRational) UnmarshalJSON(data []byte) error {
	if rational == nil {
		return errors.New("exact rational receiver is nil")
	}
	var wire struct {
		Numerator   json.Number `json:"numerator"`
		Denominator json.Number `json:"denominator"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return fmt.Errorf("decode exact rational: %w", err)
	}
	if decoder.More() || wire.Numerator == "" || wire.Denominator == "" {
		return errors.New("exact rational has missing or trailing data")
	}
	numerator, ok := new(big.Int).SetString(string(wire.Numerator), 10)
	if !ok {
		return errors.New("exact rational numerator is invalid")
	}
	denominator, ok := new(big.Int).SetString(string(wire.Denominator), 10)
	if !ok {
		return errors.New("exact rational denominator is invalid")
	}
	parsed, err := NewExactRational(numerator, denominator)
	if err != nil {
		return err
	}
	canonical, err := parsed.MarshalJSON()
	if err != nil {
		return err
	}
	if !bytes.Equal(data, canonical) {
		return errors.New("exact rational encoding is noncanonical")
	}
	*rational = parsed
	return nil
}
