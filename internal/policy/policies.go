package policy

import (
	"go-odtbank/internal/domain"
	"time"
)

type FlatFeePolicy struct {
	Fee domain.Money
}

func (p *FlatFeePolicy) CalculateFee(amount domain.Money) domain.Money {
	return p.Fee
}

type ZeroFeePolicy struct{}

func (p *ZeroFeePolicy) CalculateFee(amount domain.Money) domain.Money {
	return 0
}

type VariableFeePolicy struct {
	BasisPoints int64
}

func (p *VariableFeePolicy) CalculateFee(amount domain.Money) domain.Money {
	return domain.Money((int64(amount)*p.BasisPoints + 5000) / 10000)
}

type DefaultTimeService struct {
	ServiceAvailable bool
}

func (s *DefaultTimeService) IsServiceAvailable(t time.Time) bool {
	return s.ServiceAvailable
}
