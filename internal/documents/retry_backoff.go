package documents

import (
	cryptorand "crypto/rand"
	"math/big"
	"time"
)

const maximumRetryBackoff = time.Hour

func exponentialRetryCap(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = time.Minute
	}
	if attempt < 1 {
		attempt = 1
	}
	cap := base
	for count := 1; count < attempt && cap < maximumRetryBackoff; count++ {
		if cap > maximumRetryBackoff/2 {
			return maximumRetryBackoff
		}
		cap *= 2
	}
	if cap > maximumRetryBackoff {
		return maximumRetryBackoff
	}
	return cap
}

func fullJitter(cap time.Duration) time.Duration {
	if cap <= 0 {
		return 0
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(cap)+1))
	if err != nil {
		return cap / 2
	}
	return time.Duration(value.Int64())
}
