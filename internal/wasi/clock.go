// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

// ClockResolution represents the resolution of a clock.
type ClockResolution uint32

// Walltime returns the current time in seconds and nanoseconds.
type Walltime func() (sec int64, nsec int32)

// Nanotime returns the current monotonic time in nanoseconds.
type Nanotime func() int64

// Nanosleep sleeps for the given number of nanoseconds.
type Nanosleep func(ns int64)

// Osyield yields the processor.
type Osyield func()
