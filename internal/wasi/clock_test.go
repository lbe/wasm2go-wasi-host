// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

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
