//go:build !unix

package render

import "time"

const backstop = 5 * time.Second

func limitCPU() {}

func cpuKilled(error) bool { return false }
