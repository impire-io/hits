//go:build !unix

package fleettest

import "testing"

func lock(_ *testing.T, _ string) func() { return func() {} }
