package wasi

import "testing"

func TestClockTypes(t *testing.T) {
	var _ ClockResolution = 0

	var _ Walltime = func() (sec int64, nsec int32) {
		return 0, 0
	}

	var _ Nanotime = func() int64 {
		return 0
	}

	var _ Nanosleep = func(ns int64) {}

	var _ Osyield = func() {}
}
