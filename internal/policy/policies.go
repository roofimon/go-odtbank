package policy

import (
	"time"
)

type FlatFeePolicy struct {
	Fee float64
}

func (p *FlatFeePolicy) CalculateFee(amount float64) float64 {
	return p.Fee
}

type ZeroFeePolicy struct{}

func (p *ZeroFeePolicy) CalculateFee(amount float64) float64 {
	return 0
}

type VariableFeePolicy struct {
	Percentage float64
}

func (p *VariableFeePolicy) CalculateFee(amount float64) float64 {
	return amount * p.Percentage
}

type DefaultTimeService struct {
	ServiceAvailable bool
}

func (s *DefaultTimeService) IsServiceAvailable(t time.Time) bool {
	return s.ServiceAvailable
}
