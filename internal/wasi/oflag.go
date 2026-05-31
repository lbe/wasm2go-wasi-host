// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

type Oflag uint32

const (
	O_RDONLY Oflag = iota
	O_RDWR
	O_WRONLY

	O_APPEND    Oflag = 1 << 2
	O_CREAT     Oflag = 1 << 3
	O_DIRECTORY Oflag = 1 << 4
	O_DSYNC     Oflag = 1 << 5
	O_EXCL      Oflag = 1 << 6
	O_NOFOLLOW  Oflag = 1 << 7
	O_NONBLOCK  Oflag = 1 << 8
	O_RSYNC     Oflag = 1 << 9
	O_SYNC      Oflag = 1 << 10
	O_TRUNC     Oflag = 1 << 11
)
